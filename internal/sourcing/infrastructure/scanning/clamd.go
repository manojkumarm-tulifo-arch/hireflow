package scanning

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/dutchcoders/go-clamd"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// Clamd talks to a clamav daemon over TCP. The returned errors are treated as
// retryable by the worker (the daemon may be transiently down or busy);
// infections come back via ScanVerdict.
type Clamd struct {
	client *clamd.Clamd
}

// NewClamd wires the adapter against the given clamd address, e.g.
// "tcp://clamav:3310" or "unix:///var/run/clamav/clamd.ctl".
func NewClamd(addr string) *Clamd {
	return &Clamd{client: clamd.NewClamd(addr)}
}

// Ping checks the daemon is reachable. Called at startup.
func (c *Clamd) Ping() error { return c.client.Ping() }

// Scan streams the body to clamd via INSTREAM and parses the response.
func (c *Clamd) Scan(ctx context.Context, r io.Reader) (services.ScanVerdict, error) {
	ch, err := c.client.ScanStream(r, make(chan bool))
	if err != nil {
		return services.ScanVerdict{}, fmt.Errorf("clamd scan: %w", err)
	}

	var verdict services.ScanVerdict
	verdict.Clean = true
	for result := range ch {
		switch result.Status {
		case clamd.RES_OK:
			// Already initialized Clean=true.
		case clamd.RES_FOUND:
			verdict.Clean = false
			verdict.Signature = result.Description
		case clamd.RES_ERROR:
			return services.ScanVerdict{}, fmt.Errorf("clamd error: %s", result.Description)
		default:
			return services.ScanVerdict{}, errors.New("clamd: unknown status")
		}
	}
	return verdict, nil
}

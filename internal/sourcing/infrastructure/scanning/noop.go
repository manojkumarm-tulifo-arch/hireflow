// Package scanning holds adapters implementing the FileScanner port.
package scanning

import (
	"context"
	"io"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// Noop is the dev-default scanner that reports every input as clean.
// Must not be used in production.
type Noop struct{}

// NewNoop wires the adapter.
func NewNoop() *Noop { return &Noop{} }

// Scan reads the body (to support callers that need to consume the stream)
// and reports Clean=true.
func (Noop) Scan(ctx context.Context, r io.Reader) (services.ScanVerdict, error) {
	if r != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return services.ScanVerdict{Clean: true}, nil
}

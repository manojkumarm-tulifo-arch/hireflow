// Package anthropic provides a thin wrapper around the official Anthropic SDK
// configured for hireflow's needs (env-driven API key, deterministic timeout,
// retry budget). It is *not* a generic LLM abstraction — each bounded context
// defines its own port and uses this client directly inside its adapter.
package anthropic

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultModel is the model used when ANTHROPIC_MODEL is unset.
// Per Anthropic guidance, Opus 4.7 is the default for new applications.
const DefaultModel = "claude-opus-4-7"

// Config drives client construction. All fields have sane defaults; only
// APIKey is required.
type Config struct {
	APIKey  string
	Model   string
	Timeout time.Duration
	// HTTPClient lets tests substitute a fake transport. Production code
	// should leave this nil to inherit the SDK's default.
	HTTPClient *http.Client
}

// LoadConfig reads configuration from the environment. ANTHROPIC_API_KEY is
// required; ANTHROPIC_MODEL and ANTHROPIC_TIMEOUT are optional.
func LoadConfig() (Config, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return Config{}, errors.New("anthropic: ANTHROPIC_API_KEY is required")
	}
	cfg := Config{
		APIKey:  key,
		Model:   getenv("ANTHROPIC_MODEL", DefaultModel),
		Timeout: 30 * time.Second,
	}
	if v := os.Getenv("ANTHROPIC_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Timeout = d
		}
	}
	return cfg, nil
}

// Client wraps an SDK client with the configured model. It is safe for
// concurrent use; callers share one Client across the process.
type Client struct {
	sdk   *anthropic.Client
	model string
}

// NewClient builds a configured SDK client. The SDK handles retries on 5xx
// and 429 with exponential backoff out of the box.
func NewClient(cfg Config) *Client {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(cfg.Timeout))
	}
	c := anthropic.NewClient(opts...)
	return &Client{
		sdk:   &c,
		model: cfg.Model,
	}
}

// SDK exposes the underlying typed SDK so context-specific adapters can build
// their own MessageNewParams. The wrapper deliberately doesn't try to abstract
// the entire surface — adapters know exactly what request they need to send.
func (c *Client) SDK() *anthropic.Client { return c.sdk }

// Model returns the configured model identifier.
func (c *Client) Model() string { return c.model }

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

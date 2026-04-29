package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_RequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := LoadConfig()
	assert.Error(t, err)
}

func TestLoadConfig_AppliesDefaults(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("ANTHROPIC_TIMEOUT", "")

	cfg, err := LoadConfig()
	assert.NoError(t, err)
	assert.Equal(t, DefaultModel, cfg.Model)
	assert.Equal(t, 30_000_000_000, int(cfg.Timeout))
}

func TestLoadConfig_HonoursOverrides(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_MODEL", "claude-haiku-4-5")
	t.Setenv("ANTHROPIC_TIMEOUT", "10s")

	cfg, err := LoadConfig()
	assert.NoError(t, err)
	assert.Equal(t, "claude-haiku-4-5", cfg.Model)
	assert.Equal(t, "10s", cfg.Timeout.String())
}

func TestNewClient_ExposesModel(t *testing.T) {
	c := NewClient(Config{APIKey: "sk-test", Model: "claude-opus-4-7"})
	assert.Equal(t, "claude-opus-4-7", c.Model())
}

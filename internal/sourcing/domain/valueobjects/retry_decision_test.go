package valueobjects_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestRetryable_Helper(t *testing.T) {
	d := vo.Retryable("anthropic_429", "rate limited")
	assert.True(t, d.Retryable)
	assert.Equal(t, "anthropic_429", d.Reason)
	assert.Equal(t, "rate limited", d.Detail)
}

func TestFatal_Helper(t *testing.T) {
	d := vo.Fatal("virus_detected", "EICAR-TEST")
	assert.False(t, d.Retryable)
	assert.Equal(t, "virus_detected", d.Reason)
}

func TestRetryDecision_BackoffHintRespected(t *testing.T) {
	d := vo.Retryable("transient", "x").WithBackoff(30 * time.Second)
	assert.Equal(t, 30*time.Second, d.BackoffHint)
}

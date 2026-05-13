//go:build integration

package scanning_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scanning"
)

// eicarTestString is the standard antivirus test pattern; not actually malware.
// Splitting via concatenation so the file itself doesn't trigger scanners.
const eicarTestString = `X5O!P%@AP[4\PZX54(P^)7CC)7}` +
	`$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

func clamdAddr(t *testing.T) string {
	addr := os.Getenv("CLAMD_ADDR")
	if addr == "" {
		t.Skip("CLAMD_ADDR not set")
	}
	return addr
}

func TestClamd_PingSucceeds(t *testing.T) {
	c := scanning.NewClamd(clamdAddr(t))
	require.NoError(t, c.Ping())
}

func TestClamd_CleanInputReportsClean(t *testing.T) {
	c := scanning.NewClamd(clamdAddr(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	v, err := c.Scan(ctx, bytes.NewReader([]byte("the quick brown fox")))
	require.NoError(t, err)
	assert.True(t, v.Clean)
}

func TestClamd_EICARReportsInfected(t *testing.T) {
	c := scanning.NewClamd(clamdAddr(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	v, err := c.Scan(ctx, bytes.NewReader([]byte(eicarTestString)))
	require.NoError(t, err)
	assert.False(t, v.Clean)
	assert.NotEmpty(t, v.Signature)
}

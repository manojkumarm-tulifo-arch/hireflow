package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/storage"
)

func newFS(t *testing.T) *storage.LocalFS {
	t.Helper()
	dir := t.TempDir()
	fs, err := storage.NewLocalFS(dir)
	require.NoError(t, err)
	return fs
}

func TestPut_ThenOpen_RoundTrip(t *testing.T) {
	fs := newFS(t)
	body := []byte("hello world")

	err := fs.Put(context.Background(), "ab/cd/file.pdf", bytes.NewReader(body))
	require.NoError(t, err)

	r, err := fs.Open(context.Background(), "ab/cd/file.pdf")
	require.NoError(t, err)
	defer r.Close()
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func TestPut_Idempotent(t *testing.T) {
	fs := newFS(t)
	require.NoError(t, fs.Put(context.Background(), "x/y", bytes.NewReader([]byte("a"))))
	require.NoError(t, fs.Put(context.Background(), "x/y", bytes.NewReader([]byte("a"))))
}

func TestOpen_NotFound(t *testing.T) {
	fs := newFS(t)
	_, err := fs.Open(context.Background(), "missing")
	assert.ErrorIs(t, err, storage.ErrNotFound)
}

func TestMoveToQuarantine_MovesFile(t *testing.T) {
	fs := newFS(t)
	require.NoError(t, fs.Put(context.Background(), "ab/file", bytes.NewReader([]byte("x"))))

	newKey, err := fs.MoveToQuarantine(context.Background(), "ab/file")
	require.NoError(t, err)
	assert.Contains(t, newKey, "quarantine/")

	// Original key gone.
	_, err = fs.Open(context.Background(), "ab/file")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	// New key accessible.
	r, err := fs.Open(context.Background(), newKey)
	require.NoError(t, err)
	defer r.Close()
}

func TestNewLocalFS_RejectsRelativePath(t *testing.T) {
	_, err := storage.NewLocalFS("not-absolute")
	assert.Error(t, err)
}

func TestPut_RejectsKeyEscapingRoot(t *testing.T) {
	fs := newFS(t)
	err := fs.Put(context.Background(), "../escape", bytes.NewReader([]byte("x")))
	assert.Error(t, err)
}

// Sanity: bytes really hit disk under the configured root.
func TestPut_WritesUnderRoot(t *testing.T) {
	dir := t.TempDir()
	fs, err := storage.NewLocalFS(dir)
	require.NoError(t, err)
	require.NoError(t, fs.Put(context.Background(), "k", bytes.NewReader([]byte("v"))))
	_, err = os.Stat(filepath.Join(dir, "k"))
	require.NoError(t, err)
}

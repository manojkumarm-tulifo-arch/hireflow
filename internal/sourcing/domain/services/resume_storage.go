package services

import (
	"context"
	"io"
)

// ResumeStorage is the port for byte-level resume storage. Adapters key by
// content hash so re-uploads are free.
type ResumeStorage interface {
	// Put stores the bytes at the given key. Idempotent — re-putting the same
	// key is a no-op (must not error).
	Put(ctx context.Context, key string, body io.Reader) error
	// Open returns a reader for the bytes at the given key.
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// MoveToQuarantine renames a key into a quarantine namespace. Used after
	// a positive virus scan.
	MoveToQuarantine(ctx context.Context, key string) (newKey string, err error)
}

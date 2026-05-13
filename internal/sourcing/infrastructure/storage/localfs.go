package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalFS is a ResumeStorage adapter that writes files under a fixed root dir.
// Keys may include "/" — they create nested directories under the root.
type LocalFS struct {
	root string
}

// NewLocalFS validates root is an absolute path and ensures it exists.
func NewLocalFS(root string) (*LocalFS, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("storage root must be absolute, got %q", root)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir root: %w", err)
	}
	return &LocalFS{root: root}, nil
}

// Put writes the bytes at root/key. Creates parent directories as needed.
// Idempotent on identical content.
func (l *LocalFS) Put(ctx context.Context, key string, body io.Reader) error {
	full, err := l.safePath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := full + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp, full); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Open returns a reader for the file at key.
func (l *LocalFS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	full, err := l.safePath(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

// MoveToQuarantine moves a file under "quarantine/" + original key, leaving
// the original key absent. Returns the new key.
func (l *LocalFS) MoveToQuarantine(ctx context.Context, key string) (string, error) {
	src, err := l.safePath(key)
	if err != nil {
		return "", err
	}
	newKey := "quarantine/" + key
	dst, err := l.safePath(newKey)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("mkdir quarantine: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}
	return newKey, nil
}

// safePath joins root and key, refusing keys that escape root.
func (l *LocalFS) safePath(key string) (string, error) {
	// Reject keys that explicitly contain ".." path elements.
	for _, part := range strings.Split(filepath.ToSlash(key), "/") {
		if part == ".." {
			return "", ErrUnsafeKey
		}
	}
	clean := filepath.Clean("/" + key) // anchor at "/" so ".." can't climb above
	full := filepath.Join(l.root, clean)
	// Final check — full must still be under root.
	rel, err := filepath.Rel(l.root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrUnsafeKey
	}
	return full, nil
}

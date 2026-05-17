// Package text holds text-extraction helpers used by the upload pipeline.
// zip_extractor.go fans a multipart-uploaded ZIP file into per-entry byte
// blobs that the upload command can route through the dedup-and-persist flow,
// one entry at a time. Anti-zip-bomb rails are enforced here (entry count,
// uncompressed total size, per-entry size, nested-ZIP, path-traversal,
// encryption).
package text

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// ZipLimits caps what ExtractZip will accept. Zero / negative values fall
// back to the defaults from DefaultZipLimits.
type ZipLimits struct {
	MaxEntries           int
	MaxUncompressedBytes int64
	MaxEntrySizeBytes    int64
}

// DefaultZipLimits matches the spec's anti-abuse rails.
var DefaultZipLimits = ZipLimits{
	MaxEntries:           100,
	MaxUncompressedBytes: 50 * 1024 * 1024, // 50 MiB
	MaxEntrySizeBytes:    10 * 1024 * 1024, // 10 MiB
}

// ExtractedEntry is one file pulled out of a ZIP.
type ExtractedEntry struct {
	Filename string
	Bytes    []byte
}

// Error sentinels returned by ExtractZip. Upload command maps these to
// specific outcome error codes for the HTTP response.
var (
	ErrZipEncrypted            = errors.New("zip: encrypted entries not supported")
	ErrZipNested               = errors.New("zip: nested zips not supported")
	ErrZipPathTraversal        = errors.New("zip: path traversal not allowed")
	ErrZipTooManyEntries       = errors.New("zip: too many entries")
	ErrZipUncompressedTooLarge = errors.New("zip: uncompressed total too large")
	ErrZipEntryTooLarge        = errors.New("zip: entry too large")
)

// ExtractZip pulls entries out of body. Directories are skipped silently.
// Returns the first error encountered; partial extracts are not surfaced.
func ExtractZip(body []byte, limits ZipLimits) ([]ExtractedEntry, error) {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = DefaultZipLimits.MaxEntries
	}
	if limits.MaxUncompressedBytes <= 0 {
		limits.MaxUncompressedBytes = DefaultZipLimits.MaxUncompressedBytes
	}
	if limits.MaxEntrySizeBytes <= 0 {
		limits.MaxEntrySizeBytes = DefaultZipLimits.MaxEntrySizeBytes
	}

	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("zip: read: %w", err)
	}

	// Filter to non-directory entries first so we can count against MaxEntries
	// using the same number the recruiter expects to see.
	type pending struct{ entry *zip.File }
	var queue []pending
	for _, e := range r.File {
		if e.FileInfo().IsDir() {
			continue
		}
		queue = append(queue, pending{entry: e})
	}
	if len(queue) > limits.MaxEntries {
		return nil, fmt.Errorf("%w: %d > %d", ErrZipTooManyEntries, len(queue), limits.MaxEntries)
	}

	out := make([]ExtractedEntry, 0, len(queue))
	var totalBytes int64
	for _, p := range queue {
		name := p.entry.Name

		// Path traversal: reject "..", absolute paths, leading slash.
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") || name == "" {
			return nil, fmt.Errorf("%w: %q", ErrZipPathTraversal, name)
		}
		// Encryption: ZIP spec bit 0 of the general-purpose flag indicates encryption.
		if p.entry.Flags&0x1 != 0 {
			return nil, fmt.Errorf("%w: %q", ErrZipEncrypted, name)
		}
		// Per-entry size: trust the declared size only as a sanity gate;
		// the running total below is the real bomb-resistance.
		if int64(p.entry.UncompressedSize64) > limits.MaxEntrySizeBytes {
			return nil, fmt.Errorf("%w: %q (%d > %d)", ErrZipEntryTooLarge, name, p.entry.UncompressedSize64, limits.MaxEntrySizeBytes)
		}

		rc, err := p.entry.Open()
		if err != nil {
			return nil, fmt.Errorf("zip: open entry %q: %w", name, err)
		}

		// LimitReader caps the read so a lying header can't blow past
		// MaxEntrySizeBytes during stream extraction.
		buf := make([]byte, 0, p.entry.UncompressedSize64)
		w := bytes.NewBuffer(buf)
		n, err := io.Copy(w, io.LimitReader(rc, limits.MaxEntrySizeBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("zip: read entry %q: %w", name, err)
		}
		if n > limits.MaxEntrySizeBytes {
			return nil, fmt.Errorf("%w: %q (stream exceeded %d)", ErrZipEntryTooLarge, name, limits.MaxEntrySizeBytes)
		}
		totalBytes += n
		if totalBytes > limits.MaxUncompressedBytes {
			return nil, fmt.Errorf("%w: %d > %d", ErrZipUncompressedTooLarge, totalBytes, limits.MaxUncompressedBytes)
		}

		// Reject nested ZIPs by content sniff (extension lies).
		entryBytes := w.Bytes()
		if mt := mimetype.Detect(entryBytes); mt.Is("application/zip") {
			return nil, fmt.Errorf("%w: %q", ErrZipNested, name)
		}

		out = append(out, ExtractedEntry{Filename: name, Bytes: entryBytes})
	}
	return out, nil
}

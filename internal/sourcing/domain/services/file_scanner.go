// Package services defines the ports (interfaces) the sourcing application
// layer depends on. Adapters live under infrastructure/.
package services

import (
	"context"
	"io"
)

// ScanVerdict reports the outcome of scanning a single file.
type ScanVerdict struct {
	Clean     bool   // true = no malware found
	Signature string // populated when Clean=false (e.g., "EICAR-TEST")
}

// FileScanner is the port for byte-level malware scanning.
// Errors returned from Scan are treated as retryable by the worker;
// the adapter never returns ErrInfected — infections come back via
// ScanVerdict{Clean: false, Signature: ...}.
type FileScanner interface {
	Scan(ctx context.Context, r io.Reader) (ScanVerdict, error)
}

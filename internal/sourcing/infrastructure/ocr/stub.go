// Package ocr holds OCRExtractor adapters. Stub is a deterministic
// implementation for local development and demo use (STUB_LLMS=true).
// No real Anthropic call is made; a fixed canned text block is returned.
package ocr

import (
	"context"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

const stubText = "Demo Candidate\nSenior Backend Engineer\nGo, Kafka, Postgres\nExperience: Demo Corp 2020-2025\nEducation: BTech CS, IIT Bombay 2018"

// Stub is a deterministic OCRExtractor for use when STUB_LLMS=true.
// It always returns the same canned resume text regardless of input bytes.
type Stub struct{}

// compile-time interface check.
var _ services.OCRExtractor = (*Stub)(nil)

// NewStub returns a ready-to-use Stub OCR extractor.
func NewStub() *Stub {
	return &Stub{}
}

// ExtractFromBytes returns a fixed canned text block. The body and mime
// arguments are accepted but ignored — no API call is made.
func (Stub) ExtractFromBytes(_ context.Context, _ []byte, _ string) (services.RawText, error) {
	return services.RawText{
		Text:      stubText,
		PageCount: 1,
	}, nil
}

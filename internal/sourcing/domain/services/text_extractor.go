package services

import (
	"context"
	"io"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// RawText is the structured output of a text-extraction pass.
type RawText struct {
	Text      string
	PageCount int
}

// TextExtractor is the port for deterministic text extraction from PDF/DOCX.
// Returns an empty Text + nil error for "no extractable text" (caller falls
// through to OCR in slice 2+). Returns an error for genuine extraction failures.
type TextExtractor interface {
	Extract(ctx context.Context, r io.Reader, mime vo.MimeType) (RawText, error)
}

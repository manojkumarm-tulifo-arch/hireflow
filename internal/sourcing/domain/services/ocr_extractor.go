package services

import "context"

// OCRExtractor is the port for image-based text extraction (slice 2 fallback
// when the text extractor returns empty). Input is the raw resume bytes
// (typically image-only PDF); output mirrors RawText for symmetry with the
// regular TextExtractor.
type OCRExtractor interface {
	ExtractFromBytes(ctx context.Context, body []byte, mime string) (RawText, error)
}

package valueobjects

import (
	"errors"

	"github.com/gabriel-vasile/mimetype"
)

// ErrUnsupportedMime is returned when a MIME type isn't an accepted resume format.
var ErrUnsupportedMime = errors.New("unsupported mime type")

// MimeType is an accepted resume MIME type (PDF, DOC, DOCX, ODT, or ZIP).
type MimeType struct {
	value string
}

// Allowed lists the accepted resume MIME types.
var Allowed = map[string]struct{}{
	"application/pdf": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	"application/msword": {},
	"application/vnd.oasis.opendocument.text": {},
	"application/zip": {},
}

// ParseMimeType validates and returns a MimeType for the given string.
func ParseMimeType(s string) (MimeType, error) {
	if _, ok := Allowed[s]; !ok {
		return MimeType{}, ErrUnsupportedMime
	}
	return MimeType{value: s}, nil
}

// SniffMimeType detects the MIME type from a byte prefix (truth source over
// the upload header). Returns ErrUnsupportedMime if the detected type isn't allowed.
func SniffMimeType(b []byte) (MimeType, error) {
	m := mimetype.Detect(b)
	return ParseMimeType(m.String())
}

// String returns the MIME type string.
func (m MimeType) String() string { return m.value }

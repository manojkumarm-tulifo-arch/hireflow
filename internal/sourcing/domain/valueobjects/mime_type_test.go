package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestParseMimeType_AcceptsPDFandDOCX(t *testing.T) {
	for _, m := range []string{
		"application/pdf",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/msword",
	} {
		got, err := vo.ParseMimeType(m)
		require.NoError(t, err, m)
		assert.Equal(t, m, got.String())
	}
}

func TestParseMimeType_RejectsOthers(t *testing.T) {
	for _, m := range []string{"image/png", "text/html", ""} {
		_, err := vo.ParseMimeType(m)
		assert.ErrorIs(t, err, vo.ErrUnsupportedMime, m)
	}
}

func TestSniffMimeType_PDFMagicNumber(t *testing.T) {
	pdfBytes := []byte("%PDF-1.4\n%fake\n")
	got, err := vo.SniffMimeType(pdfBytes)
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", got.String())
}

func TestSniffMimeType_RejectsUnsupported(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	_, err := vo.SniffMimeType(pngBytes)
	assert.ErrorIs(t, err, vo.ErrUnsupportedMime)
}

func TestParseMimeType_AcceptsODT(t *testing.T) {
	m, err := vo.ParseMimeType("application/vnd.oasis.opendocument.text")
	require.NoError(t, err)
	assert.Equal(t, "application/vnd.oasis.opendocument.text", m.String())
}

func TestParseMimeType_AcceptsZIP(t *testing.T) {
	m, err := vo.ParseMimeType("application/zip")
	require.NoError(t, err)
	assert.Equal(t, "application/zip", m.String())
}

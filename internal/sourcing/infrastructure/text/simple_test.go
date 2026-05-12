package text_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
)

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	return f
}

func mustMime(t *testing.T, s string) vo.MimeType {
	m, err := vo.ParseMimeType(s)
	require.NoError(t, err)
	return m
}

func TestExtract_PDF_ReturnsText(t *testing.T) {
	ex := text.NewSimple()
	got, err := ex.Extract(context.Background(), fixture(t, "hello.pdf"),
		mustMime(t, "application/pdf"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(strings.ToLower(got.Text), "hello"))
	assert.GreaterOrEqual(t, got.PageCount, 1)
}

func TestExtract_DOCX_ReturnsText(t *testing.T) {
	ex := text.NewSimple()
	got, err := ex.Extract(context.Background(), fixture(t, "hello.docx"),
		mustMime(t, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(strings.ToLower(got.Text), "hello"))
}

func TestExtract_EmptyPDF_ReturnsEmptyText(t *testing.T) {
	ex := text.NewSimple()
	got, err := ex.Extract(context.Background(), fixture(t, "empty.pdf"),
		mustMime(t, "application/pdf"))
	require.NoError(t, err)
	assert.Equal(t, "", strings.TrimSpace(got.Text))
}

func TestExtract_CorruptInput_Errors(t *testing.T) {
	ex := text.NewSimple()
	got, err := ex.Extract(context.Background(), fixture(t, "not_a_pdf.txt"),
		mustMime(t, "application/pdf"))
	assert.Error(t, err)
	assert.Empty(t, got.Text)
}

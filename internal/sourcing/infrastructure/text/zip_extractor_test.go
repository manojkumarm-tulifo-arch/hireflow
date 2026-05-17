package text_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/infrastructure/text"
)

// buildZip writes a deterministic test zip. Each entry pair is (name, content).
// content == nil creates a directory entry.
func buildZip(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		name, content := e[0], e[1]
		if content == "" && strings.HasSuffix(name, "/") {
			_, err := w.Create(name)
			require.NoError(t, err)
			continue
		}
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func pdfBytes(unique string) string {
	// Minimal but valid-enough PDF header so mimetype.Detect returns application/pdf.
	return "%PDF-1.4\n%" + unique + "\n%%EOF\n"
}

func TestExtractZip_HappyPath(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"alice.pdf", pdfBytes("alice")},
		{"bharat.pdf", pdfBytes("bharat")},
		{"chitra.pdf", pdfBytes("chitra")},
	})

	out, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "alice.pdf", out[0].Filename)
	assert.Contains(t, string(out[0].Bytes), "%PDF-1.4")
}

func TestExtractZip_SkipsDirectoryEntries(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"folder/", ""}, // directory entry
		{"folder/alice.pdf", pdfBytes("alice")},
	})

	out, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "folder/alice.pdf", out[0].Filename)
}

func TestExtractZip_RejectsPathTraversal(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"../etc/passwd", "evil"},
	})

	_, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipPathTraversal))
}

func TestExtractZip_RejectsAbsolutePaths(t *testing.T) {
	z := buildZip(t, [][2]string{
		{"/etc/passwd", "evil"},
	})

	_, err := text.ExtractZip(z, text.DefaultZipLimits)
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipPathTraversal))
}

func TestExtractZip_RejectsTooManyEntries(t *testing.T) {
	entries := make([][2]string, 0, 101)
	for i := 0; i < 101; i++ {
		entries = append(entries, [2]string{
			"r" + string(rune('a'+i%26)) + ".pdf",
			pdfBytes(string(rune('a' + i%26))),
		})
	}
	// dedupe filenames so the zip writer doesn't choke
	for i := range entries {
		entries[i][0] = "f" + strings.Repeat("a", i) + ".pdf"
	}
	z := buildZip(t, entries)

	_, err := text.ExtractZip(z, text.ZipLimits{MaxEntries: 100})
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipTooManyEntries))
}

func TestExtractZip_RejectsNestedZip(t *testing.T) {
	inner := buildZip(t, [][2]string{{"x.pdf", pdfBytes("x")}})
	outer := buildZip(t, [][2]string{{"nested.zip", string(inner)}})

	_, err := text.ExtractZip(outer, text.DefaultZipLimits)
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipNested))
}

func TestExtractZip_RejectsEntryTooLarge(t *testing.T) {
	big := strings.Repeat("A", 11*1024*1024) // 11 MiB
	z := buildZip(t, [][2]string{{"big.pdf", "%PDF-1.4\n" + big}})

	_, err := text.ExtractZip(z, text.ZipLimits{MaxEntrySizeBytes: 10 * 1024 * 1024})
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipEntryTooLarge))
}

func TestExtractZip_RejectsUncompressedTotalTooLarge(t *testing.T) {
	chunk := strings.Repeat("A", 4*1024*1024) // 4 MiB
	// 3 entries × 4 MiB = 12 MiB total; limit 10 MiB → rejected.
	z := buildZip(t, [][2]string{
		{"a.pdf", "%PDF-1.4\n" + chunk},
		{"b.pdf", "%PDF-1.4\n" + chunk},
		{"c.pdf", "%PDF-1.4\n" + chunk},
	})

	_, err := text.ExtractZip(z, text.ZipLimits{
		MaxEntries:           10,
		MaxUncompressedBytes: 10 * 1024 * 1024,
		MaxEntrySizeBytes:    5 * 1024 * 1024,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, text.ErrZipUncompressedTooLarge))
}

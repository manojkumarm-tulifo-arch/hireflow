// Package text holds TextExtractor adapters. The Simple adapter uses
// ledongthuc/pdf for PDF and a small in-house zip+xml walker for DOCX.
// Both are deterministic, free, and produce plain text suitable for the
// downstream LLM parser.
package text

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// Simple is the v1 TextExtractor adapter.
type Simple struct{}

// NewSimple wires the adapter.
func NewSimple() *Simple { return &Simple{} }

// Extract dispatches on MIME type. Returns ErrUnsupportedMime for anything
// else (the upload pipeline guards this earlier; treated as defense-in-depth).
func (s *Simple) Extract(ctx context.Context, r io.Reader, mime vo.MimeType) (services.RawText, error) {
	// Buffer to bytes — ledongthuc/pdf needs ReaderAt + size; DOCX needs
	// zip.Reader which also needs a ReaderAt. Resumes are <= 10MB so memory
	// is fine.
	buf, err := io.ReadAll(r)
	if err != nil {
		return services.RawText{}, fmt.Errorf("read body: %w", err)
	}

	switch mime.String() {
	case "application/pdf":
		return extractPDF(buf)
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return extractDOCX(buf)
	case "application/msword":
		// Legacy .doc; out of scope for slice 1. Return error so the worker
		// fails the row with a clear reason rather than silently empty-extract.
		return services.RawText{}, fmt.Errorf("legacy .doc not supported in slice 1")
	}
	return services.RawText{}, fmt.Errorf("unsupported mime: %s", mime.String())
}

func extractPDF(buf []byte) (services.RawText, error) {
	rdr := bytes.NewReader(buf)
	doc, err := pdf.NewReader(rdr, int64(len(buf)))
	if err != nil {
		return services.RawText{}, fmt.Errorf("open pdf: %w", err)
	}
	pages := doc.NumPage()
	var b strings.Builder
	for i := 1; i <= pages; i++ {
		page := doc.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			return services.RawText{}, fmt.Errorf("page %d: %w", i, err)
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	return services.RawText{Text: b.String(), PageCount: pages}, nil
}

// extractDOCX reads word/document.xml from the .docx zip and concatenates
// the contents of <w:t> elements. Sufficient for plain text — formatting,
// tables, headers/footers are intentionally not preserved.
func extractDOCX(buf []byte) (services.RawText, error) {
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return services.RawText{}, fmt.Errorf("open docx: %w", err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return services.RawText{}, fmt.Errorf("docx: word/document.xml missing")
	}
	rc, err := doc.Open()
	if err != nil {
		return services.RawText{}, fmt.Errorf("open document.xml: %w", err)
	}
	defer rc.Close()

	var b strings.Builder
	dec := xml.NewDecoder(rc)
	inT := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return services.RawText{}, fmt.Errorf("xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inT = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
			}
			if t.Name.Local == "p" {
				b.WriteString("\n")
			}
		case xml.CharData:
			if inT {
				b.Write(t)
			}
		}
	}
	// DOCX doesn't have a cheap page-count source; report 1 as the floor.
	return services.RawText{Text: b.String(), PageCount: 1}, nil
}

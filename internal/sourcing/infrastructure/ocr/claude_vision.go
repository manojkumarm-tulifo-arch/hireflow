// Package ocr holds OCRExtractor adapters. ClaudeVision sends the resume bytes
// as a multimodal document content block and asks Claude to transcribe the
// text. Used only when the regular text extractor returns near-empty output
// (image-only PDFs).
package ocr

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

const ocrSystemPrompt = `You are an OCR engine. Given an image-only or scanned resume,
return only the extracted plain text, preserving line breaks. Do not paraphrase,
summarize, or add commentary. If no text is legible, return an empty string.`

// ClaudeVision is the OCR adapter using the Anthropic Messages API with a
// document content block. It implements services.OCRExtractor.
type ClaudeVision struct {
	client *anthropic.Client
	model  string
}

// NewClaudeVision wires the adapter.
func NewClaudeVision(client *anthropic.Client, model string) *ClaudeVision {
	return &ClaudeVision{client: client, model: model}
}

// ExtractFromBytes sends the PDF bytes to Claude with the OCR system prompt and
// returns the transcribed text. Only "application/pdf" is supported; other MIME
// types are rejected immediately without an API call.
func (c *ClaudeVision) ExtractFromBytes(ctx context.Context, body []byte, mime string) (services.RawText, error) {
	if mime != "application/pdf" {
		return services.RawText{}, fmt.Errorf("ocr: unsupported mime %q (only application/pdf supported)", mime)
	}

	b64 := base64.StdEncoding.EncodeToString(body)

	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: ocrSystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
					Data: b64,
				}),
				anthropic.NewTextBlock("Transcribe the resume text."),
			),
		},
	})
	if err != nil {
		return services.RawText{}, fmt.Errorf("anthropic messages: %w", err)
	}

	var text string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text += tb.Text
		}
	}

	// Page count is not available from the API response; report 1 as the floor.
	return services.RawText{Text: text, PageCount: 1}, nil
}

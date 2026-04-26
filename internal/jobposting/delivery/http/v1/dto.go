// Package v1 holds the v1 HTTP request/response shapes for the jobposting context.
package v1

// PublishPostingRequest is the body for POST /postings/{id}/publish.
type PublishPostingRequest struct {
	Channels []string `json:"channels"`
}

// ClosePostingRequest is the body for POST /postings/{id}/close.
type ClosePostingRequest struct {
	Reason string `json:"reason"`
}

// Envelope is the standard API response envelope.
type Envelope struct {
	Success bool       `json:"success"`
	Data    any        `json:"data,omitempty"`
	Error   *ErrorInfo `json:"error,omitempty"`
}

// ErrorInfo is the standard API error block.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

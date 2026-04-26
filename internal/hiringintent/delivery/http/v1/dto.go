// Package v1 holds the v1 HTTP request/response shapes and handlers for
// the hiringintent context. These types are version-locked — v2 lives next door.
package v1

// CreateIntentRequest is the v1 request body for POST /intents.
type CreateIntentRequest struct {
	RoleTitle string         `json:"role_title"`
	Skills    []SkillRequest `json:"skills"`
	MinYears  int            `json:"min_years"`
	MaxYears  int            `json:"max_years"`
	Headcount int            `json:"headcount"`
	Locations []string       `json:"locations"`
	WorkMode  string         `json:"work_mode"`
	Priority  string         `json:"priority"`
	Budget    *BudgetRequest `json:"budget,omitempty"`
}

// SkillRequest is a skill on the create-intent payload.
type SkillRequest struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

// BudgetRequest is the optional budget block on create-intent.
type BudgetRequest struct {
	MinMinor int64  `json:"min_minor"`
	MaxMinor int64  `json:"max_minor"`
	Currency string `json:"currency"`
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

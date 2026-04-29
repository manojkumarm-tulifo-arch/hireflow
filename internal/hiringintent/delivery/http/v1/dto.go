// Package v1 holds the v1 HTTP request/response shapes and handlers for
// the hiringintent context. These types are version-locked — v2 lives next door.
package v1

import "github.com/hustle/hireflow/internal/hiringintent/application/dto"

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
	Reason    string         `json:"reason,omitempty"`
	Team      string         `json:"team,omitempty"`
	ReportsTo string         `json:"reports_to,omitempty"`
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

// ExtractIntentRequest is the v1 request body for POST /intents/extract.
// `messages` is the prior chat history excluding the current user turn,
// which is sent separately in `user_message` so the server can append it
// as the latest message in the LLM call.
type ExtractIntentRequest struct {
	Messages    []ExtractMessage `json:"messages"`
	Draft       ExtractDraft     `json:"draft"`
	UserMessage string           `json:"user_message"`
}

// ExtractMessage is one prior chat turn.
type ExtractMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// ExtractDraft mirrors CreateIntentRequest's shape so the FE can pass its
// in-progress draft directly. All fields are optional; zero values mean
// "not yet filled".
type ExtractDraft struct {
	RoleTitle string         `json:"role_title,omitempty"`
	Skills    []SkillRequest `json:"skills,omitempty"`
	MinYears  int            `json:"min_years,omitempty"`
	MaxYears  int            `json:"max_years,omitempty"`
	Headcount int            `json:"headcount,omitempty"`
	Locations []string       `json:"locations,omitempty"`
	WorkMode  string         `json:"work_mode,omitempty"`
	Priority  string         `json:"priority,omitempty"`
	Budget    *BudgetRequest `json:"budget,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Team      string         `json:"team,omitempty"`
	ReportsTo string         `json:"reports_to,omitempty"`
}

// toPatch converts the wire shape to the application DTO. Zero values are
// left as nil pointers so the LLM treats them as "not yet filled" rather
// than "explicitly zero".
func (d ExtractDraft) toPatch() dto.DraftPatch {
	p := dto.DraftPatch{}
	if d.RoleTitle != "" {
		p.RoleTitle = &d.RoleTitle
	}
	if len(d.Skills) > 0 {
		p.Skills = make([]dto.SkillPatch, len(d.Skills))
		for i, s := range d.Skills {
			p.Skills[i] = dto.SkillPatch{Name: s.Name, Required: s.Required}
		}
	}
	if d.MinYears != 0 {
		p.MinYears = &d.MinYears
	}
	if d.MaxYears != 0 {
		p.MaxYears = &d.MaxYears
	}
	if d.Headcount != 0 {
		p.Headcount = &d.Headcount
	}
	if len(d.Locations) > 0 {
		p.Locations = d.Locations
	}
	if d.WorkMode != "" {
		p.WorkMode = &d.WorkMode
	}
	if d.Priority != "" {
		p.Priority = &d.Priority
	}
	if d.Budget != nil {
		p.Budget = &dto.BudgetPatch{
			MinMinor: d.Budget.MinMinor,
			MaxMinor: d.Budget.MaxMinor,
			Currency: d.Budget.Currency,
		}
	}
	if d.Reason != "" {
		p.Reason = &d.Reason
	}
	if d.Team != "" {
		p.Team = &d.Team
	}
	if d.ReportsTo != "" {
		p.ReportsTo = &d.ReportsTo
	}
	return p
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

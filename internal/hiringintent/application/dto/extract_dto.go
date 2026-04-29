package dto

// ChatMessage is one turn in the recruiter's extraction conversation.
// Role is always "user" or "assistant"; the system prompt lives inside the
// extractor adapter, not in this list.
type ChatMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// SkillPatch is the patch shape for a single skill. Mirrors SkillDTO so the
// FE can merge into its draft without a second mapping.
type SkillPatch struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

// BudgetPatch is the patch shape for the salary band. Amounts are in the
// minor unit of `Currency` (paise for INR, cents for USD). Currency is a
// 3-letter ISO 4217 code; the sanitizer normalizes case and length.
type BudgetPatch struct {
	MinMinor int64  `json:"min_minor"`
	MaxMinor int64  `json:"max_minor"`
	Currency string `json:"currency"`
}

// DraftPatch is a sparse set of fields the extractor proposes this turn.
// Pointer fields use nil to mean "not provided"; the FE merges only the
// non-nil fields into the current draft. Slice fields are nil when the
// extractor didn't update them; an empty (non-nil) slice means "clear it".
type DraftPatch struct {
	RoleTitle *string      `json:"role_title,omitempty"`
	Skills    []SkillPatch `json:"skills,omitempty"`
	MinYears  *int         `json:"min_years,omitempty"`
	MaxYears  *int         `json:"max_years,omitempty"`
	Headcount *int         `json:"headcount,omitempty"`
	Locations []string     `json:"locations,omitempty"`
	WorkMode  *string      `json:"work_mode,omitempty"`
	Priority  *string      `json:"priority,omitempty"`
	Budget    *BudgetPatch `json:"budget,omitempty"`
	Reason    *string      `json:"reason,omitempty"`
	Team      *string      `json:"team,omitempty"`
	ReportsTo *string      `json:"reports_to,omitempty"`
}

// IsEmpty reports whether the patch contains no field updates.
func (p DraftPatch) IsEmpty() bool {
	return p.RoleTitle == nil &&
		p.Skills == nil &&
		p.MinYears == nil &&
		p.MaxYears == nil &&
		p.Headcount == nil &&
		p.Locations == nil &&
		p.WorkMode == nil &&
		p.Priority == nil &&
		p.Budget == nil &&
		p.Reason == nil &&
		p.Team == nil &&
		p.ReportsTo == nil
}

// ExtractInput is the application-layer input for one extraction turn.
type ExtractInput struct {
	TenantID    string
	RecruiterID string
	Messages    []ChatMessage
	Draft       DraftPatch
	UserMessage string
}

// ExtractOutput is the application-layer result of one turn.
type ExtractOutput struct {
	Reply    string      `json:"reply"`
	Patch    DraftPatch  `json:"patch"`
	Complete bool        `json:"complete"`
	Missing  []string    `json:"missing,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
}

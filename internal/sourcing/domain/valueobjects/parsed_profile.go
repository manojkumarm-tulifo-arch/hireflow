package valueobjects

import "errors"

// ErrInvalidProfile is returned by ParsedProfile.Validate when the structure is malformed.
var ErrInvalidProfile = errors.New("invalid parsed profile")

// ParsedProfile is the canonical structured form of a resume.
// Versioned via SchemaVersion so future shape changes don't break old rows.
type ParsedProfile struct {
	SchemaVersion  int                   `json:"schema_version"`
	Personal       ParsedPersonal        `json:"personal"`
	Headline       string                `json:"headline,omitempty"`
	Summary        string                `json:"summary,omitempty"`
	Skills         []ParsedSkill         `json:"skills,omitempty"`
	Experiences    []ParsedExperience    `json:"experiences,omitempty"`
	Education      []ParsedEducation     `json:"education,omitempty"`
	Certifications []ParsedCertification `json:"certifications,omitempty"`
	Languages      []ParsedLanguage      `json:"languages,omitempty"`
	Warnings       []string              `json:"warnings,omitempty"`
}

// ParsedPersonal holds the PII portion of a parsed profile. These fields are
// encrypted at the application layer via the PIIEncryptor port before storage.
type ParsedPersonal struct {
	FullName string       `json:"full_name,omitempty"`
	Email    string       `json:"email,omitempty"`
	Phone    string       `json:"phone,omitempty"`
	Location string       `json:"location,omitempty"`
	Links    []ParsedLink `json:"links,omitempty"`
}

// ParsedLink is one external profile link (LinkedIn, GitHub, portfolio).
type ParsedLink struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// ParsedSkill is one skill claim with optional years and a reference back to the
// experience that supports it.
type ParsedSkill struct {
	Name        string  `json:"name"`
	Years       float64 `json:"years,omitempty"`
	EvidenceRef string  `json:"evidence_ref,omitempty"`
}

// ParsedExperience is one work-experience entry.
type ParsedExperience struct {
	ID          string   `json:"id"`
	Company     string   `json:"company"`
	Title       string   `json:"title"`
	Start       string   `json:"start"` // YYYY-MM
	End         string   `json:"end,omitempty"`
	Current     bool     `json:"current,omitempty"`
	Description string   `json:"description,omitempty"`
	SkillsUsed  []string `json:"skills_used,omitempty"`
}

// ParsedEducation is one education entry.
type ParsedEducation struct {
	Institution string `json:"institution"`
	Degree      string `json:"degree,omitempty"`
	Field       string `json:"field,omitempty"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
}

// ParsedCertification is one certification entry.
type ParsedCertification struct {
	Name    string `json:"name"`
	Issuer  string `json:"issuer,omitempty"`
	Issued  string `json:"issued,omitempty"`
	Expires string `json:"expires,omitempty"`
}

// ParsedLanguage is one language proficiency entry.
type ParsedLanguage struct {
	Name        string `json:"name"`
	Proficiency string `json:"proficiency,omitempty"` // native|fluent|professional|basic
}

// NewParsedProfile returns a fresh empty profile pinned to schema_version=1.
func NewParsedProfile() ParsedProfile {
	return ParsedProfile{SchemaVersion: 1}
}

// Validate enforces minimum invariants. Currently only "schema_version > 0".
// Field-level validation is deferred to the parser adapter so the LLM's
// `warnings` array can carry parse-time issues instead of hard-rejecting.
func (p ParsedProfile) Validate() error {
	if p.SchemaVersion <= 0 {
		return ErrInvalidProfile
	}
	return nil
}

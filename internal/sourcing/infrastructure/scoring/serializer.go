// Package scoring contains infrastructure helpers for the match-scoring pipeline.
package scoring

import (
	"strings"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// SerializeProfile produces the embedding-input text for a ParsedProfile.
// It deliberately excludes PII (full_name, email, phone) and education so that
// the resulting text is safe to store cleartext alongside the embedding vector
// (see scoring.md §2).
//
// Format (pipe-separated, sections omitted when empty):
//
//	{headline} | {summary} | Skills: {skill_names, ", "} | Experience: {company} {title} {start}-{end}: {description} | …
//
// Date range uses "Present" when experience.Current is true, regardless of
// the End field value. When End is empty and Current is false the range is
// written as "{start}-" (open-ended), signalling the parser left it blank.
func SerializeProfile(p vo.ParsedProfile) string {
	var b strings.Builder

	addSection(&b, p.Headline)
	addSection(&b, p.Summary)

	if skills := serializeSkills(p.Skills); skills != "" {
		addSection(&b, "Skills: "+skills)
	}

	// The first non-empty experience entry carries the "Experience:" label;
	// subsequent entries are pipe-separated continuations without an extra label.
	firstExp := true
	for _, exp := range p.Experiences {
		entry := serializeExperience(exp)
		if entry == "" {
			continue
		}
		if firstExp {
			addSection(&b, "Experience: "+entry)
			firstExp = false
		} else {
			addSection(&b, entry)
		}
	}

	return b.String()
}

// SerializeRole produces the embedding-input text for a RoleSpec.
// Symmetric with SerializeProfile so the two vectors live in the same semantic
// space (see scoring.md §2).
//
// Format (pipe-separated, sections omitted when empty):
//
//	{title} | Required skills: {names} | Optional skills: {names} | Experience range: {min}-{max} years | {work_mode} role in {locations, ", "}
//
// Sections omitted when their data is absent:
//   - "Optional skills:" omitted when OptionalSkills is empty.
//   - "Experience range:" omitted when both MinYears and MaxYears are zero.
//   - Location suffix (" in {locations}") omitted when Locations is empty
//     (e.g. pure-remote roles); leaves just "{work_mode} role".
func SerializeRole(r services.RoleSpec) string {
	var b strings.Builder

	addSection(&b, r.Title)

	if req := skillNames(r.RequiredSkills); req != "" {
		addSection(&b, "Required skills: "+req)
	}

	if opt := skillNames(r.OptionalSkills); opt != "" {
		addSection(&b, "Optional skills: "+opt)
	}

	if r.MinYears != 0 || r.MaxYears != 0 {
		var er strings.Builder
		er.WriteString("Experience range: ")
		writeInt(&er, r.MinYears)
		er.WriteByte('-')
		writeInt(&er, r.MaxYears)
		er.WriteString(" years")
		addSection(&b, er.String())
	}

	if r.WorkMode != "" {
		loc := r.WorkMode + " role"
		if len(r.Locations) > 0 {
			loc += " in " + strings.Join(r.Locations, ", ")
		}
		addSection(&b, loc)
	}

	return b.String()
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// addSection appends " | {text}" to b if b is already non-empty, otherwise
// just writes text. Callers must ensure text is non-empty before calling.
func addSection(b *strings.Builder, text string) {
	if text == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteString(" | ")
	}
	b.WriteString(text)
}

// serializeSkills returns a comma-joined list of skill names, or "" if none.
func serializeSkills(skills []vo.ParsedSkill) string {
	if len(skills) == 0 {
		return ""
	}
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, ", ")
}

// serializeExperience formats one work-experience entry.
// Returns "" if both Company and Title are empty (degenerate entry).
func serializeExperience(e vo.ParsedExperience) string {
	// Build the "company title start-end" head.
	var head strings.Builder
	if e.Company != "" {
		head.WriteString(e.Company)
	}
	if e.Title != "" {
		if head.Len() > 0 {
			head.WriteByte(' ')
		}
		head.WriteString(e.Title)
	}
	if head.Len() == 0 {
		return ""
	}

	// Date range.
	if e.Start != "" {
		head.WriteByte(' ')
		head.WriteString(e.Start)
		head.WriteByte('-')
		if e.Current {
			head.WriteString("Present")
		} else {
			head.WriteString(e.End) // may be empty → open-ended "YYYY-MM-"
		}
	}

	// Append description after a colon only when it is non-empty.
	if e.Description != "" {
		head.WriteString(": ")
		head.WriteString(e.Description)
	}

	return head.String()
}

// skillNames returns a comma-joined list of skill names from a []SkillSpec.
func skillNames(specs []services.SkillSpec) string {
	if len(specs) == 0 {
		return ""
	}
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, ", ")
}

// writeInt writes a base-10 integer to b without importing strconv/fmt.
func writeInt(b *strings.Builder, n int) {
	if n == 0 {
		b.WriteByte('0')
		return
	}
	if n < 0 {
		b.WriteByte('-')
		n = -n
	}
	// Collect digits in reverse then flip.
	var digits [20]byte
	i := 0
	for n > 0 {
		digits[i] = byte('0' + n%10)
		n /= 10
		i++
	}
	for j := i - 1; j >= 0; j-- {
		b.WriteByte(digits[j])
	}
}

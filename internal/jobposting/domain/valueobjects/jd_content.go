package valueobjects

import (
	"errors"
	"strings"
)

var (
	// ErrEmptyTitle is returned when the JD title is empty after trimming.
	ErrEmptyTitle = errors.New("jd title must not be empty")
	// ErrEmptySummary is returned when the JD summary is empty.
	ErrEmptySummary = errors.New("jd summary must not be empty")
	// ErrInvalidJDVersion is returned when version is not positive.
	ErrInvalidJDVersion = errors.New("jd version must be positive")
)

// JDContent is the published job description payload — versioned so we
// can amend wording without losing history (each PublishPosting may roll a new version).
type JDContent struct {
	title            string
	summary          string
	responsibilities []string
	requirements     []string
	version          int
}

// NewJDContent validates and constructs a JD payload. Version must be >= 1.
func NewJDContent(title, summary string, responsibilities, requirements []string, version int) (JDContent, error) {
	t := strings.TrimSpace(title)
	if t == "" {
		return JDContent{}, ErrEmptyTitle
	}
	s := strings.TrimSpace(summary)
	if s == "" {
		return JDContent{}, ErrEmptySummary
	}
	if version <= 0 {
		return JDContent{}, ErrInvalidJDVersion
	}
	return JDContent{
		title:            t,
		summary:          s,
		responsibilities: cleanLines(responsibilities),
		requirements:     cleanLines(requirements),
		version:          version,
	}, nil
}

func (j JDContent) Title() string              { return j.title }
func (j JDContent) Summary() string            { return j.summary }
func (j JDContent) Responsibilities() []string { return append([]string(nil), j.responsibilities...) }
func (j JDContent) Requirements() []string     { return append([]string(nil), j.requirements...) }
func (j JDContent) Version() int               { return j.version }

// WithBumpedVersion returns a copy with version + 1. Used when amending JD wording.
func (j JDContent) WithBumpedVersion() JDContent {
	cp := j
	cp.version++
	return cp
}

func cleanLines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

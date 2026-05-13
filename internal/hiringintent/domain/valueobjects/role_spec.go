package valueobjects

import (
	"errors"
	"strings"
)

var (
	// ErrEmptyRoleTitle is returned when role title is empty after trimming.
	ErrEmptyRoleTitle = errors.New("role title must not be empty")
	// ErrInvalidExperienceRange is returned when min > max or values are negative.
	ErrInvalidExperienceRange = errors.New("invalid experience range")
	// ErrInvalidHeadcount is returned when headcount is not positive.
	ErrInvalidHeadcount = errors.New("headcount must be greater than zero")
	// ErrInvalidWorkMode is returned when work mode is unrecognized.
	ErrInvalidWorkMode = errors.New("invalid work mode")
)

// WorkMode captures whether the role is on-site, remote, or hybrid.
type WorkMode string

const (
	WorkModeOnsite WorkMode = "ONSITE"
	WorkModeRemote WorkMode = "REMOTE"
	WorkModeHybrid WorkMode = "HYBRID"
)

// ParseWorkMode validates a string and returns the matching mode.
func ParseWorkMode(s string) (WorkMode, error) {
	switch WorkMode(s) {
	case WorkModeOnsite, WorkModeRemote, WorkModeHybrid:
		return WorkMode(s), nil
	default:
		return "", ErrInvalidWorkMode
	}
}

// ExperienceRange is a closed interval [Min, Max] of years of experience.
type ExperienceRange struct {
	min int
	max int
}

// NewExperienceRange constructs and validates an experience range.
func NewExperienceRange(minYears, maxYears int) (ExperienceRange, error) {
	if minYears < 0 || maxYears < 0 || minYears > maxYears {
		return ExperienceRange{}, ErrInvalidExperienceRange
	}
	return ExperienceRange{min: minYears, max: maxYears}, nil
}

func (e ExperienceRange) Min() int { return e.min }
func (e ExperienceRange) Max() int { return e.max }

// Headcount is a positive integer count of positions to fill.
type Headcount struct {
	value int
}

// NewHeadcount constructs and validates headcount.
func NewHeadcount(n int) (Headcount, error) {
	if n <= 0 {
		return Headcount{}, ErrInvalidHeadcount
	}
	return Headcount{value: n}, nil
}

func (h Headcount) Value() int { return h.value }

// Skill is a required or nice-to-have skill on the role spec. Construct
// only via NewSkill so the trim + non-empty invariant is enforced; the
// fields are private to keep the zero value (`Skill{}`) out of valid
// state.
type Skill struct {
	name     string
	required bool
}

// NewSkill trims whitespace and rejects empty names.
func NewSkill(name string, required bool) (Skill, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return Skill{}, errors.New("skill name must not be empty")
	}
	return Skill{name: n, required: required}, nil
}

// Name returns the skill name.
func (s Skill) Name() string { return s.name }

// Required reports whether the skill is required (vs nice-to-have).
func (s Skill) Required() bool { return s.required }

// RoleSpec is the structured role description on a hiring intent.
// It is itself a value object — replaced in whole on update, never mutated in place.
type RoleSpec struct {
	title      string
	skills     []Skill
	experience ExperienceRange
	headcount  Headcount
	locations  []string
	workMode   WorkMode
}

// NewRoleSpec constructs and validates a role spec.
func NewRoleSpec(
	title string,
	skills []Skill,
	experience ExperienceRange,
	headcount Headcount,
	locations []string,
	workMode WorkMode,
) (RoleSpec, error) {
	t := strings.TrimSpace(title)
	if t == "" {
		return RoleSpec{}, ErrEmptyRoleTitle
	}
	// skills can be empty at draft time; confirmation enforces at-least-one.
	cleaned := make([]string, 0, len(locations))
	for _, l := range locations {
		l = strings.TrimSpace(l)
		if l != "" {
			cleaned = append(cleaned, l)
		}
	}
	return RoleSpec{
		title:      t,
		skills:     append([]Skill(nil), skills...),
		experience: experience,
		headcount:  headcount,
		locations:  cleaned,
		workMode:   workMode,
	}, nil
}

func (r RoleSpec) Title() string               { return r.title }
func (r RoleSpec) Skills() []Skill             { return append([]Skill(nil), r.skills...) }
func (r RoleSpec) Experience() ExperienceRange { return r.experience }
func (r RoleSpec) Headcount() Headcount        { return r.headcount }
func (r RoleSpec) Locations() []string         { return append([]string(nil), r.locations...) }
func (r RoleSpec) WorkMode() WorkMode          { return r.workMode }

// HasRequiredSkill reports whether at least one skill is marked required.
func (r RoleSpec) HasRequiredSkill() bool {
	for _, s := range r.skills {
		if s.required {
			return true
		}
	}
	return false
}

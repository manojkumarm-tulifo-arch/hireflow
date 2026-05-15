// Package entities holds the aggregate roots and entities of the
// interview bounded context.
package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// TemplateRound describes one round position in a loop template.
type TemplateRound struct {
	Kind     vo.RoundKind
	Sequence int
}

// LoopTemplate is a per-intent definition of the rounds an InterviewProcess
// should contain when a candidate is shortlisted for that intent. Aggregate
// root.
type LoopTemplate struct {
	id        uuid.UUID
	tenantID  shared.TenantID
	intentID  uuid.UUID
	rounds    []TemplateRound
	createdAt time.Time
	updatedAt time.Time
}

// NewLoopTemplateInput is the constructor input.
type NewLoopTemplateInput struct {
	TenantID shared.TenantID
	IntentID uuid.UUID
	Rounds   []TemplateRound
	// Optional overrides for deterministic tests; zero values mean "use real values".
	Now func() time.Time
	ID  uuid.UUID
}

// NewLoopTemplate constructs a validated LoopTemplate. Validation:
//   - tenant required
//   - intent required
//   - rounds non-empty
//   - sequences contiguous starting at 1 (after sorting)
//   - all rounds have valid kinds
//   - no duplicate sequence numbers
func NewLoopTemplate(in NewLoopTemplateInput) (*LoopTemplate, error) {
	if in.TenantID.IsZero() {
		return nil, errors.New("loop_template: tenant_id required")
	}
	if in.IntentID == uuid.Nil {
		return nil, errors.New("loop_template: intent_id required")
	}
	if len(in.Rounds) == 0 {
		return nil, errors.New("loop_template: rounds must be non-empty")
	}

	seen := make(map[int]bool, len(in.Rounds))
	maxSeq := 0
	for _, r := range in.Rounds {
		if _, err := vo.ParseRoundKind(string(r.Kind)); err != nil {
			return nil, err
		}
		if r.Sequence < 1 {
			return nil, errors.New("loop_template: sequence must be >= 1")
		}
		if seen[r.Sequence] {
			return nil, errors.New("loop_template: duplicate sequence")
		}
		seen[r.Sequence] = true
		if r.Sequence > maxSeq {
			maxSeq = r.Sequence
		}
	}
	// Contiguous from 1 means {1..maxSeq} all present.
	if maxSeq != len(in.Rounds) {
		return nil, errors.New("loop_template: sequences must be contiguous starting at 1")
	}

	now := time.Now().UTC
	if in.Now != nil {
		now = in.Now
	}
	id := in.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := now()
	return &LoopTemplate{
		id:        id,
		tenantID:  in.TenantID,
		intentID:  in.IntentID,
		rounds:    append([]TemplateRound(nil), in.Rounds...), // defensive copy
		createdAt: t,
		updatedAt: t,
	}, nil
}

// RehydrateLoopTemplateInput is for loading from persistence.
type RehydrateLoopTemplateInput struct {
	ID        uuid.UUID
	TenantID  shared.TenantID
	IntentID  uuid.UUID
	Rounds    []TemplateRound
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RehydrateLoopTemplate constructs from persisted values without re-validating
// invariants. Use only from the repository.
func RehydrateLoopTemplate(in RehydrateLoopTemplateInput) *LoopTemplate {
	return &LoopTemplate{
		id:        in.ID,
		tenantID:  in.TenantID,
		intentID:  in.IntentID,
		rounds:    append([]TemplateRound(nil), in.Rounds...),
		createdAt: in.CreatedAt,
		updatedAt: in.UpdatedAt,
	}
}

// Accessors.
func (l *LoopTemplate) ID() uuid.UUID             { return l.id }
func (l *LoopTemplate) TenantID() shared.TenantID { return l.tenantID }
func (l *LoopTemplate) IntentID() uuid.UUID       { return l.intentID }
func (l *LoopTemplate) Rounds() []TemplateRound {
	return append([]TemplateRound(nil), l.rounds...)
}
func (l *LoopTemplate) CreatedAt() time.Time { return l.createdAt }
func (l *LoopTemplate) UpdatedAt() time.Time { return l.updatedAt }

// Replace replaces the rounds and bumps updated_at. Validates the new set
// against the same rules as the constructor.
func (l *LoopTemplate) Replace(rounds []TemplateRound, now func() time.Time) error {
	tmp, err := NewLoopTemplate(NewLoopTemplateInput{
		TenantID: l.tenantID,
		IntentID: l.intentID,
		Rounds:   rounds,
		Now:      now,
		ID:       l.id,
	})
	if err != nil {
		return err
	}
	l.rounds = tmp.rounds
	if now != nil {
		l.updatedAt = now()
	} else {
		l.updatedAt = time.Now().UTC()
	}
	return nil
}

// Package subscribers wires cross-context event consumers for the jobposting
// context. Each subscriber maps an upstream domain event to a use case here.
package subscribers

import (
	"context"
	"fmt"

	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"
	"github.com/hustle/hireflow/internal/jobposting/application/commands"
	"github.com/hustle/hireflow/internal/jobposting/application/dto"
)

// IntentSnapshot is the minimal projection of a HiringIntent that jobposting
// needs to draft a JD. Defined here (not imported from hiringintent) so the
// upstream context can change its aggregate without breaking us — anti-
// corruption layer.
type IntentSnapshot struct {
	IntentID       string
	TenantID       string
	RoleTitle      string
	RoleSummary    string
	RequiredSkills []string
	Headcount      int
	MinYears       int
	MaxYears       int
}

// IntentReader fetches the snapshot for a confirmed intent.
// Implementations adapt the hiringintent context's read API.
type IntentReader interface {
	ReadConfirmed(ctx context.Context, tenantID, intentID string) (IntentSnapshot, error)
}

// IntentConfirmedConsumer reacts to hiringintent.IntentConfirmed by creating
// a draft JobPosting. Idempotent: re-delivering the same event yields the
// same posting (CreateFromIntent looks up by intentID first).
type IntentConfirmedConsumer struct {
	reader IntentReader
	create *commands.CreateFromIntentHandler
}

// NewIntentConfirmedConsumer wires the consumer.
func NewIntentConfirmedConsumer(reader IntentReader, create *commands.CreateFromIntentHandler) *IntentConfirmedConsumer {
	return &IntentConfirmedConsumer{reader: reader, create: create}
}

// Consume processes one IntentConfirmed event.
func (c *IntentConfirmedConsumer) Consume(ctx context.Context, event intentevents.IntentConfirmed) error {
	snap, err := c.reader.ReadConfirmed(ctx, event.TenantID.String(), event.IntentID.String())
	if err != nil {
		return fmt.Errorf("read intent: %w", err)
	}
	in := dto.CreateFromIntentInput{
		TenantID:         snap.TenantID,
		IntentID:         snap.IntentID,
		Title:            snap.RoleTitle,
		Summary:          buildSummary(snap),
		Responsibilities: snap.RequiredSkills,
		Requirements:     buildRequirements(snap),
	}
	if _, err := c.create.Handle(ctx, in); err != nil {
		return fmt.Errorf("create posting: %w", err)
	}
	return nil
}

func buildSummary(s IntentSnapshot) string {
	if s.RoleSummary != "" {
		return s.RoleSummary
	}
	return fmt.Sprintf("Hiring %d %s — drafted from confirmed intent.", s.Headcount, s.RoleTitle)
}

func buildRequirements(s IntentSnapshot) []string {
	out := make([]string, 0, 2)
	if s.MinYears > 0 || s.MaxYears > 0 {
		out = append(out, fmt.Sprintf("%d–%d years of experience", s.MinYears, s.MaxYears))
	}
	if s.Headcount > 0 {
		out = append(out, fmt.Sprintf("Filling %d position(s)", s.Headcount))
	}
	return out
}

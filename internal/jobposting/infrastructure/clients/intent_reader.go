// Package clients holds adapters that let the jobposting context call into
// other contexts via their application APIs. Every adapter here is an
// Anti-Corruption Layer: it accepts upstream DTOs and returns jobposting's
// own port types, so a change to the upstream model can't cascade here.
package clients

import (
	"context"
	"errors"
	"fmt"

	intentdto "github.com/hustle/hireflow/internal/hiringintent/application/dto"
	intentqueries "github.com/hustle/hireflow/internal/hiringintent/application/queries"
	"github.com/hustle/hireflow/internal/jobposting/infrastructure/subscribers"
)

// ErrIntentNotConfirmed signals an attempt to read an intent that exists
// but is not in the CONFIRMED state. Treated as a permanent failure for
// the cross-context flow — re-delivery won't change the answer.
var ErrIntentNotConfirmed = errors.New("intent is not confirmed")

// intentQuery is the slice of the hiringintent query API the reader uses.
// Defined here so we can't accidentally widen the dependency surface.
type intentQuery interface {
	Handle(ctx context.Context, in intentqueries.GetIntentInput) (intentdto.IntentDTO, error)
}

// IntentReader adapts the hiringintent GetIntent query into the
// IntentSnapshot port used by jobposting subscribers. It is the single
// place that knows the upstream DTO layout.
type IntentReader struct {
	query intentQuery
}

// NewIntentReader wires the reader against a GetIntentHandler.
func NewIntentReader(query *intentqueries.GetIntentHandler) *IntentReader {
	return &IntentReader{query: query}
}

// ReadConfirmed fetches the intent and projects it into the snapshot. It
// rejects intents whose status is not CONFIRMED, which would indicate a
// stale or misordered event delivery.
func (r *IntentReader) ReadConfirmed(ctx context.Context, tenantID, intentID string) (subscribers.IntentSnapshot, error) {
	out, err := r.query.Handle(ctx, intentqueries.GetIntentInput{TenantID: tenantID, IntentID: intentID})
	if err != nil {
		return subscribers.IntentSnapshot{}, fmt.Errorf("intent reader: %w", err)
	}
	if out.Status != "CONFIRMED" {
		return subscribers.IntentSnapshot{}, fmt.Errorf("intent reader %s: %w", out.ID, ErrIntentNotConfirmed)
	}
	return toSnapshot(out), nil
}

// toSnapshot translates the upstream DTO to the local port type. The
// intent DTO has no free-text "summary" field today; the subscriber
// synthesizes one from RoleTitle + Headcount when RoleSummary is empty.
func toSnapshot(d intentdto.IntentDTO) subscribers.IntentSnapshot {
	required := make([]string, 0, len(d.Role.Skills))
	for _, s := range d.Role.Skills {
		if s.Required {
			required = append(required, s.Name)
		}
	}
	return subscribers.IntentSnapshot{
		IntentID:       d.ID,
		TenantID:       d.TenantID,
		RoleTitle:      d.Role.Title,
		RoleSummary:    "",
		RequiredSkills: required,
		Headcount:      d.Role.Headcount,
		MinYears:       d.Role.Experience.MinYears,
		MaxYears:       d.Role.Experience.MaxYears,
	}
}

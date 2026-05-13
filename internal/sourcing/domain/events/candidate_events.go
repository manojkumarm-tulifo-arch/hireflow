package events

import (
	"time"

	"github.com/google/uuid"

	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// CandidateParsed is emitted when a Candidate aggregate is first created
// (i.e., a new resume produced a new structured profile). Downstream:
// slice-3 scoring uses this to enqueue scoring jobs against open intents.
type CandidateParsed struct {
	CandidateID   uuid.UUID       `json:"candidate_id"`
	TenantID      shared.TenantID `json:"tenant_id"`
	ContentHash   string          `json:"content_hash"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
}

func (e CandidateParsed) EventName() string       { return "sourcing.CandidateParsed" }
func (e CandidateParsed) AggregateID() uuid.UUID  { return e.CandidateID }
func (e CandidateParsed) Tenant() shared.TenantID { return e.TenantID }
func (e CandidateParsed) At() time.Time           { return e.OccurredAt }

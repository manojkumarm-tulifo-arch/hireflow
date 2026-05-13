// Package clients holds anti-corruption adapters for the sourcing context
// that read data from sibling contexts via direct SQL queries. No domain
// packages from those contexts are imported — only raw column reads and
// local projection to sourcing's own port types.
package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/domain/services"
)

// PostgresIntentReader reads hiring intents from the hiring_intents table
// using raw SQL. It is an Anti-Corruption Layer: it projects the persisted
// hiringintent shape into sourcing's own IntentSnapshot type without importing
// any hiringintent domain packages.
type PostgresIntentReader struct {
	pool *pgxpool.Pool
}

// NewPostgresIntentReader wires the reader against a pgxpool.
func NewPostgresIntentReader(pool *pgxpool.Pool) *PostgresIntentReader {
	return &PostgresIntentReader{pool: pool}
}

// intentSelectSQL selects the four columns the reader needs.
// We intentionally omit all other columns — the ACL only sees what it maps.
const intentSelectSQL = `
SELECT id, tenant_id, role, status
FROM hiring_intents
`

// roleJSON mirrors the JSONB layout written by hiringintent's intent_serializer.
// Slice-3 limitation: the Experience sub-object uses "min"/"max" keys
// (not "min_years"/"max_years") — confirmed from intent_serializer.go.
type roleJSON struct {
	Title      string          `json:"title"`
	Skills     []skillJSON     `json:"skills"`
	Experience experienceJSON  `json:"experience"`
	Headcount  int             `json:"headcount"`
	Locations  []string        `json:"locations"`
	WorkMode   string          `json:"work_mode"`
}

type skillJSON struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

type experienceJSON struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// projectRole translates the raw JSONB payload into sourcing's RoleSpec.
// Degree and Languages are left empty in v1 — the hiringintent schema does
// not capture them yet.
func projectRole(raw []byte) (services.RoleSpec, error) {
	var rp roleJSON
	if err := json.Unmarshal(raw, &rp); err != nil {
		return services.RoleSpec{}, fmt.Errorf("unmarshal role jsonb: %w", err)
	}

	var required, optional []services.SkillSpec
	for _, s := range rp.Skills {
		spec := services.SkillSpec{Name: s.Name, MinYears: 0}
		if s.Required {
			required = append(required, spec)
		} else {
			optional = append(optional, spec)
		}
	}

	return services.RoleSpec{
		Title:          rp.Title,
		RequiredSkills: required,
		OptionalSkills: optional,
		MinYears:       rp.Experience.Min,
		MaxYears:       rp.Experience.Max,
		Locations:      rp.Locations,
		WorkMode:       rp.WorkMode,
		// Degree and Languages: not present in the hiringintent schema (v1).
		Degree:    "",
		Languages: nil,
	}, nil
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows, allowing a single scan
// helper for QueryRow and Query loops.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanSnapshot reads one row from a rowScanner into an IntentSnapshot.
//
// Slice-3 limitation: hiring_intents doesn't expose a spec_version column yet.
// SpecVersion is hardcoded to 1. When the hiringintent aggregate adds re-confirm
// support (which bumps spec_version on the row), the sourcing team should add the
// column to this SELECT and map it here instead of using the constant.
func scanSnapshot(row rowScanner) (services.IntentSnapshot, error) {
	var (
		id       uuid.UUID
		tenantID uuid.UUID
		roleRaw  []byte
		status   string
	)
	if err := row.Scan(&id, &tenantID, &roleRaw, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return services.IntentSnapshot{}, fmt.Errorf("intent not found: %w", err)
		}
		return services.IntentSnapshot{}, fmt.Errorf("scan intent row: %w", err)
	}

	tenant, err := shared.ParseTenantID(tenantID.String())
	if err != nil {
		return services.IntentSnapshot{}, fmt.Errorf("parse tenant_id: %w", err)
	}

	role, err := projectRole(roleRaw)
	if err != nil {
		return services.IntentSnapshot{}, err
	}

	return services.IntentSnapshot{
		ID:          id,
		TenantID:    tenant,
		Status:      status,
		SpecVersion: 1, // Slice-3 limitation: see doc-comment on scanSnapshot.
		Role:        role,
	}, nil
}

// FindByID returns the intent snapshot for the given tenant and intent ID.
// Returns an error wrapping pgx.ErrNoRows when no matching row exists.
func (r *PostgresIntentReader) FindByID(ctx context.Context, tenant shared.TenantID, id uuid.UUID) (services.IntentSnapshot, error) {
	row := r.pool.QueryRow(ctx,
		intentSelectSQL+`WHERE tenant_id = $1 AND id = $2`,
		tenant.String(), id,
	)
	snap, err := scanSnapshot(row)
	if err != nil {
		return services.IntentSnapshot{}, fmt.Errorf("intent_reader FindByID: %w", err)
	}
	return snap, nil
}

// ListConfirmedIntents returns all currently-CONFIRMED intents for the tenant.
// Used by ScoreCandidate to fan out over open roles.
func (r *PostgresIntentReader) ListConfirmedIntents(ctx context.Context, tenant shared.TenantID) ([]services.IntentSnapshot, error) {
	rows, err := r.pool.Query(ctx,
		intentSelectSQL+`WHERE tenant_id = $1 AND status = 'CONFIRMED'`,
		tenant.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("intent_reader ListConfirmedIntents query: %w", err)
	}
	defer rows.Close()

	var out []services.IntentSnapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intent_reader ListConfirmedIntents scan: %w", err)
	}
	return out, nil
}

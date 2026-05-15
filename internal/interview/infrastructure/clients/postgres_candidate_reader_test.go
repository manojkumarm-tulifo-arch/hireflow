//go:build integration

package clients_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	"github.com/hustle/hireflow/internal/interview/infrastructure/clients"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func TestCandidateReader_ReadsProfile(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresCandidateReader(pool)

	candidateID := uuid.New()
	tenantID := uuid.New()
	tenant, err := shared.ParseTenantID(tenantID.String())
	require.NoError(t, err)

	parsedProfile := `{"skills":["Go","Kafka"],"experiences":[{"title":"Staff Engineer","company":"Razorpay","duration":"2020-2025","summary":"Led ingestion platform."}],"education":[{"degree":"BS","field":"CS","institution":"IIT Bombay","year":"2018"}],"certifications":["AWS SA Pro"]}`
	_, err = pool.Exec(context.Background(), `
		INSERT INTO candidates (id, tenant_id, content_hash, full_name_enc, email_enc, phone_enc, location, headline, parsed_profile, profile_schema, source, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc:fn', 'enc:em', 'enc:ph', $4, $5, $6, 1, 'manual_upload', now(), now())
	`, candidateID, tenant.String(), uuidHex(t), "Bangalore", "Senior Backend Engineer", parsedProfile)
	require.NoError(t, err)

	profile, err := reader.GetProfileForQuestions(context.Background(), tenant, candidateID)
	require.NoError(t, err)

	assert.Equal(t, candidateID, profile.ID)
	assert.Equal(t, "Senior Backend Engineer", profile.Headline)
	assert.Equal(t, "Bangalore", profile.Location)
	assert.Equal(t, 1, profile.SchemaVersion)

	require.Len(t, profile.Skills, 2)
	assert.Equal(t, "Go", profile.Skills[0])
	assert.Equal(t, "Kafka", profile.Skills[1])

	require.Len(t, profile.Experiences, 1)
	assert.Equal(t, "Staff Engineer", profile.Experiences[0].Title)
	assert.Equal(t, "Razorpay", profile.Experiences[0].Company)
	assert.Equal(t, "2020-2025", profile.Experiences[0].Duration)
	assert.Equal(t, "Led ingestion platform.", profile.Experiences[0].Summary)

	require.Len(t, profile.Education, 1)
	assert.Equal(t, "BS", profile.Education[0].Degree)
	assert.Equal(t, "CS", profile.Education[0].Field)
	assert.Equal(t, "IIT Bombay", profile.Education[0].Institution)
	assert.Equal(t, "2018", profile.Education[0].Year)

	require.Len(t, profile.Certifications, 1)
	assert.Equal(t, "AWS SA Pro", profile.Certifications[0])
}

func TestCandidateReader_TenantScoped(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresCandidateReader(pool)

	candidateID := uuid.New()
	tenantAID := uuid.New()
	tenantBID := uuid.New()
	tenantA, err := shared.ParseTenantID(tenantAID.String())
	require.NoError(t, err)
	tenantB, err := shared.ParseTenantID(tenantBID.String())
	require.NoError(t, err)

	// Insert candidate under tenantA.
	parsedProfile := `{"skills":["Python"],"experiences":[],"education":[],"certifications":[]}`
	_, err = pool.Exec(context.Background(), `
		INSERT INTO candidates (id, tenant_id, content_hash, full_name_enc, email_enc, phone_enc, location, headline, parsed_profile, profile_schema, source, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc:fn', 'enc:em', 'enc:ph', 'Mumbai', 'Backend Dev', $4, 1, 'manual_upload', now(), now())
	`, candidateID, tenantA.String(), uuidHex(t), parsedProfile)
	require.NoError(t, err)

	// tenantB cannot see tenantA's candidate.
	_, err = reader.GetProfileForQuestions(context.Background(), tenantB, candidateID)
	assert.ErrorIs(t, err, services.ErrCandidateNotFound)
}

func TestCandidateReader_NotFound(t *testing.T) {
	pool := newPool(t)
	reader := clients.NewPostgresCandidateReader(pool)

	tenantID := uuid.New()
	tenant, err := shared.ParseTenantID(tenantID.String())
	require.NoError(t, err)

	_, err = reader.GetProfileForQuestions(context.Background(), tenant, uuid.New())
	assert.ErrorIs(t, err, services.ErrCandidateNotFound)
}

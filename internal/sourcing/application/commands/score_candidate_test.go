package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
)

func TestScoreCandidate_HappyPath_CreatesApplicationsForConfirmedIntents(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	candidate := makeCandidate(t, tenantID)
	intent1 := makeIntent(tenantID)
	intent2 := makeIntent(tenantID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent1)
	intentReader.addIntent(intent2)

	appRepo := newFakeApplicationRepo()

	handler := commands.NewScoreCandidateHandler(candidateRepo, intentReader, appRepo)
	err := handler.Handle(ctx, commands.ScoreCandidateInput{
		TenantID:    tenantID,
		CandidateID: candidate.ID(),
	})

	require.NoError(t, err)
	assert.Equal(t, 2, appRepo.savedCount(), "expected one Application saved per confirmed intent")
}

func TestScoreCandidate_NoConfirmedIntents_NoApps(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	candidate := makeCandidate(t, tenantID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	// No intents added — ListConfirmedIntents returns empty slice.

	appRepo := newFakeApplicationRepo()

	handler := commands.NewScoreCandidateHandler(candidateRepo, intentReader, appRepo)
	err := handler.Handle(ctx, commands.ScoreCandidateInput{
		TenantID:    tenantID,
		CandidateID: candidate.ID(),
	})

	require.NoError(t, err)
	assert.Equal(t, 0, appRepo.savedCount(), "no applications should be created when there are no confirmed intents")
}

func TestScoreCandidate_CandidateNotFound_ReturnsError(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	candidateRepo := newFakeExtendedCandidateRepo()
	// candidate not added — FindByID returns ErrCandidateNotFound

	intentReader := newFakeIntentReader()
	appRepo := newFakeApplicationRepo()

	handler := commands.NewScoreCandidateHandler(candidateRepo, intentReader, appRepo)
	err := handler.Handle(ctx, commands.ScoreCandidateInput{
		TenantID:    tenantID,
		CandidateID: uuid.New(),
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, repositories.ErrCandidateNotFound), "expected wrapped ErrCandidateNotFound, got: %v", err)
}

func TestScoreCandidate_Idempotent_ExistingApplicationSkipped(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	candidate := makeCandidate(t, tenantID)
	intent := makeIntent(tenantID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.addIntent(intent)

	appRepo := newFakeApplicationRepo()
	// Pre-seed an existing application for this (candidate, intent) pair.
	existing := makeNewApplication(t, tenantID, candidate.ID(), intent.ID)
	require.NoError(t, appRepo.Save(ctx, existing))

	// Reset save counter after seeding.
	appRepo.saves = 0

	handler := commands.NewScoreCandidateHandler(candidateRepo, intentReader, appRepo)
	err := handler.Handle(ctx, commands.ScoreCandidateInput{
		TenantID:    tenantID,
		CandidateID: candidate.ID(),
	})

	require.NoError(t, err)
	assert.Equal(t, 0, appRepo.savedCount(), "existing application should be skipped (idempotent)")
}

func TestScoreCandidate_ListConfirmedIntentsError_ReturnsError(t *testing.T) {
	tenantID := shared.NewTenantID()
	ctx := context.Background()

	candidate := makeCandidate(t, tenantID)

	candidateRepo := newFakeExtendedCandidateRepo()
	candidateRepo.addCandidate(candidate)

	intentReader := newFakeIntentReader()
	intentReader.listErr = errors.New("db unavailable")

	appRepo := newFakeApplicationRepo()

	handler := commands.NewScoreCandidateHandler(candidateRepo, intentReader, appRepo)
	err := handler.Handle(ctx, commands.ScoreCandidateInput{
		TenantID:    tenantID,
		CandidateID: candidate.ID(),
	})

	require.Error(t, err)
}

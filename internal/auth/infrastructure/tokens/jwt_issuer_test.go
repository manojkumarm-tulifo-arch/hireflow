package tokens_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	"github.com/hustle/hireflow/internal/auth/infrastructure/tokens"
	"github.com/hustle/hireflow/internal/auth/application/commands"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	sharedauth "github.com/hustle/hireflow/internal/shared/infrastructure/auth"
)

// TestIssuedToken_VerifiesAgainstSharedMiddleware is the contract that
// guarantees tokens we mint will be accepted by the JWT middleware on the
// other side of the wall — without it, every authenticated endpoint 401s.
func TestIssuedToken_VerifiesAgainstSharedMiddleware(t *testing.T) {
	const secret = "test-secret-please-use-32+-bytes-in-prod"
	const issuer = "hireflow"

	issuer_, err := tokens.NewJWTIssuer([]byte(secret), issuer)
	require.NoError(t, err)
	verifier, err := sharedauth.NewVerifier([]byte(secret), issuer)
	require.NoError(t, err)

	email, _ := valueobjects.NewEmail("alice@example.com")
	user, err := entities.NewUser(shared.NewTenantID(), email, "Alice", []string{"recruiter", "admin"})
	require.NoError(t, err)
	require.NoError(t, user.MarkVerified())

	token, exp, err := issuer_.IssueAccess(user, commands.AccessTokenTTL)
	require.NoError(t, err)
	assert.True(t, exp.After(user.CreatedAt()))

	claims, err := verifier.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, user.TenantID().String(), claims.TenantID)
	assert.Equal(t, user.ID().String(), claims.RecruiterID)
	assert.Equal(t, []string{"recruiter", "admin"}, claims.Roles)
}

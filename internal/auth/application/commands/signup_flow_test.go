package commands_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/auth/application/commands"
	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// ----- Fakes -----

type fakeUserRepo struct {
	mu      sync.Mutex
	byID    map[string]*entities.User
	byEmail map[string]*entities.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byID:    map[string]*entities.User{},
		byEmail: map[string]*entities.User{},
	}
}
func (r *fakeUserRepo) Save(_ context.Context, u *entities.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byEmail[u.Email().String()]; ok && existing.ID().String() != u.ID().String() {
		return repositories.ErrEmailAlreadyRegistered
	}
	r.byID[u.ID().String()] = u
	r.byEmail[u.Email().String()] = u
	_ = u.PullEvents()
	return nil
}
func (r *fakeUserRepo) FindByID(_ context.Context, id valueobjects.UserID) (*entities.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.byID[id.String()]; ok {
		return u, nil
	}
	return nil, repositories.ErrUserNotFound
}
func (r *fakeUserRepo) FindByEmail(_ context.Context, _ shared.TenantID, email valueobjects.Email) (*entities.User, error) {
	return r.FindByEmailAcrossTenants(context.Background(), email)
}
func (r *fakeUserRepo) FindByEmailAcrossTenants(_ context.Context, email valueobjects.Email) (*entities.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.byEmail[email.String()]; ok {
		return u, nil
	}
	return nil, repositories.ErrUserNotFound
}

type fakeTenantRepo struct{ id shared.TenantID }

func (r *fakeTenantRepo) FindIDBySlug(_ context.Context, slug valueobjects.TenantSlug) (shared.TenantID, error) {
	if slug.String() != "demo" {
		return shared.TenantID{}, repositories.ErrTenantNotFound
	}
	return r.id, nil
}

type fakeOTPSessionRepo struct {
	mu      sync.Mutex
	current map[string]*entities.OTPSession // key: email|purpose
}

func newFakeOTPSessionRepo() *fakeOTPSessionRepo {
	return &fakeOTPSessionRepo{current: map[string]*entities.OTPSession{}}
}
func keyFor(email valueobjects.Email, purpose valueobjects.OTPPurpose) string {
	return email.String() + "|" + purpose.String()
}
func (r *fakeOTPSessionRepo) Save(_ context.Context, s *entities.OTPSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current[keyFor(s.Email(), s.Purpose())] = s
	return nil
}
func (r *fakeOTPSessionRepo) FindLatestForEmail(_ context.Context, email valueobjects.Email, purpose valueobjects.OTPPurpose) (*entities.OTPSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.current[keyFor(email, purpose)]; ok {
		return s, nil
	}
	return nil, repositories.ErrOTPSessionNotFound
}

type fakeRefreshRepo struct {
	mu   sync.Mutex
	rows map[string]*entities.RefreshToken
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{rows: map[string]*entities.RefreshToken{}}
}
func (r *fakeRefreshRepo) Save(_ context.Context, t *entities.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[t.ID().String()] = t
	return nil
}
func (r *fakeRefreshRepo) FindByID(_ context.Context, id valueobjects.RefreshTokenID) (*entities.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.rows[id.String()]; ok {
		return t, nil
	}
	return nil, repositories.ErrRefreshTokenNotFound
}
func (r *fakeRefreshRepo) RevokeAllForUser(_ context.Context, _ valueobjects.UserID) error { return nil }

// fakeOTPGen always returns "123456".
type fakeOTPGen struct{}

func (fakeOTPGen) Generate() (valueobjects.OTPCode, error) {
	return valueobjects.NewOTPCode("123456")
}

// fakeOTPHasher: hash = "h:"+code.
type fakeOTPHasher struct{}

func (fakeOTPHasher) Hash(c valueobjects.OTPCode) (string, error) { return "h:" + c.String(), nil }
func (fakeOTPHasher) Matches(hash, candidate string) bool         { return hash == "h:"+candidate }

type fakeOTPSender struct {
	calls int
	last  valueobjects.OTPCode
}

func (s *fakeOTPSender) Send(_ context.Context, _ valueobjects.Email, c valueobjects.OTPCode, _ valueobjects.OTPPurpose) error {
	s.calls++
	s.last = c
	return nil
}

// fakeIssuer returns a constant string.
type fakeIssuer struct{}

func (fakeIssuer) IssueAccess(u *entities.User, ttl time.Duration) (string, time.Time, error) {
	return "ACCESS_" + u.ID().String(), time.Now().Add(ttl), nil
}

// fakeRefreshGen: raw == hash for trivial matching in tests.
type fakeRefreshGen struct{ counter int }

func (g *fakeRefreshGen) Generate() (string, string, error) {
	g.counter++
	v := "secret-" + time.Now().Format("150405.000000000")
	return v, v, nil
}
func (g *fakeRefreshGen) Matches(hash, candidate string) bool { return hash == candidate }

// ----- Tests -----

func TestSignupFlow_HappyPath(t *testing.T) {
	ctx := context.Background()
	users := newFakeUserRepo()
	tenants := &fakeTenantRepo{id: shared.NewTenantID()}
	sessions := newFakeOTPSessionRepo()
	refresh := newFakeRefreshRepo()
	sender := &fakeOTPSender{}

	requestH := commands.NewSignupRequestOTPHandler(users, tenants, sessions, fakeOTPGen{}, fakeOTPHasher{}, sender)
	verifyH := commands.NewSignupVerifyOTPHandler(users, sessions, refresh, fakeOTPHasher{}, fakeIssuer{}, &fakeRefreshGen{})

	req, err := requestH.Handle(ctx, commands.SignupRequestOTPInput{Email: "alice@example.com", Name: "Alice", TenantSlug: "demo"})
	require.NoError(t, err)
	assert.True(t, req.Sent)
	assert.Equal(t, 1, sender.calls)

	pair, err := verifyH.Handle(ctx, commands.VerifyOTPInput{Email: "alice@example.com", Code: "123456"})
	require.NoError(t, err)
	assert.NotEmpty(t, pair.AccessToken)
	assert.NotEmpty(t, pair.RefreshToken)
	assert.Equal(t, "ACTIVE", pair.User.Status)
}

func TestSignupRequestOTP_RejectsDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	users := newFakeUserRepo()
	tenants := &fakeTenantRepo{id: shared.NewTenantID()}
	sessions := newFakeOTPSessionRepo()
	requestH := commands.NewSignupRequestOTPHandler(users, tenants, sessions, fakeOTPGen{}, fakeOTPHasher{}, &fakeOTPSender{})

	in := commands.SignupRequestOTPInput{Email: "alice@example.com", Name: "Alice", TenantSlug: "demo"}
	_, err := requestH.Handle(ctx, in)
	require.NoError(t, err)
	_, err = requestH.Handle(ctx, in)
	require.Error(t, err)
	assert.True(t, errors.Is(err, repositories.ErrEmailAlreadyRegistered))
}

func TestSignupVerifyOTP_WrongCodeRejects(t *testing.T) {
	ctx := context.Background()
	users := newFakeUserRepo()
	tenants := &fakeTenantRepo{id: shared.NewTenantID()}
	sessions := newFakeOTPSessionRepo()
	refresh := newFakeRefreshRepo()
	requestH := commands.NewSignupRequestOTPHandler(users, tenants, sessions, fakeOTPGen{}, fakeOTPHasher{}, &fakeOTPSender{})
	verifyH := commands.NewSignupVerifyOTPHandler(users, sessions, refresh, fakeOTPHasher{}, fakeIssuer{}, &fakeRefreshGen{})

	_, _ = requestH.Handle(ctx, commands.SignupRequestOTPInput{Email: "alice@example.com", Name: "Alice", TenantSlug: "demo"})
	_, err := verifyH.Handle(ctx, commands.VerifyOTPInput{Email: "alice@example.com", Code: "999999"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrOTPCodeMismatch))
}

func TestSigninRequestOTP_RejectsUnknownEmail(t *testing.T) {
	ctx := context.Background()
	users := newFakeUserRepo()
	sessions := newFakeOTPSessionRepo()
	sender := &fakeOTPSender{}
	requestH := commands.NewSigninRequestOTPHandler(users, sessions, fakeOTPGen{}, fakeOTPHasher{}, sender)

	_, err := requestH.Handle(ctx, commands.SigninRequestOTPInput{Email: "ghost@example.com"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, commands.ErrUnknownSigninEmail), "unknown email must surface ErrUnknownSigninEmail so the FE can prompt for sign-up")
	assert.Equal(t, 0, sender.calls, "no OTP sent for unknown user")
}

func TestRefreshFlow_RotationRevokesOldToken(t *testing.T) {
	ctx := context.Background()
	users := newFakeUserRepo()
	tenants := &fakeTenantRepo{id: shared.NewTenantID()}
	sessions := newFakeOTPSessionRepo()
	refresh := newFakeRefreshRepo()
	gen := &fakeRefreshGen{}

	requestH := commands.NewSignupRequestOTPHandler(users, tenants, sessions, fakeOTPGen{}, fakeOTPHasher{}, &fakeOTPSender{})
	verifyH := commands.NewSignupVerifyOTPHandler(users, sessions, refresh, fakeOTPHasher{}, fakeIssuer{}, gen)
	refreshH := commands.NewRefreshSessionHandler(users, refresh, fakeIssuer{}, gen)

	_, err := requestH.Handle(ctx, commands.SignupRequestOTPInput{Email: "alice@example.com", Name: "Alice", TenantSlug: "demo"})
	require.NoError(t, err)
	pair, err := verifyH.Handle(ctx, commands.VerifyOTPInput{Email: "alice@example.com", Code: "123456"})
	require.NoError(t, err)

	first := pair.RefreshToken
	rotated, err := refreshH.Handle(ctx, commands.RefreshSessionInput{RefreshToken: first})
	require.NoError(t, err)
	assert.NotEqual(t, first, rotated.RefreshToken)

	// Re-using the old refresh after rotation must be rejected (revoked).
	_, err = refreshH.Handle(ctx, commands.RefreshSessionInput{RefreshToken: first})
	require.Error(t, err)
	assert.True(t, errors.Is(err, entities.ErrRefreshTokenRevoked))
}

func TestLogout_RevokesRefresh(t *testing.T) {
	ctx := context.Background()
	users := newFakeUserRepo()
	tenants := &fakeTenantRepo{id: shared.NewTenantID()}
	sessions := newFakeOTPSessionRepo()
	refresh := newFakeRefreshRepo()
	gen := &fakeRefreshGen{}
	requestH := commands.NewSignupRequestOTPHandler(users, tenants, sessions, fakeOTPGen{}, fakeOTPHasher{}, &fakeOTPSender{})
	verifyH := commands.NewSignupVerifyOTPHandler(users, sessions, refresh, fakeOTPHasher{}, fakeIssuer{}, gen)
	logoutH := commands.NewLogoutHandler(refresh, gen)
	refreshH := commands.NewRefreshSessionHandler(users, refresh, fakeIssuer{}, gen)

	_, _ = requestH.Handle(ctx, commands.SignupRequestOTPInput{Email: "alice@example.com", Name: "Alice", TenantSlug: "demo"})
	pair, err := verifyH.Handle(ctx, commands.VerifyOTPInput{Email: "alice@example.com", Code: "123456"})
	require.NoError(t, err)

	require.NoError(t, logoutH.Handle(ctx, commands.LogoutInput{RefreshToken: pair.RefreshToken}))

	// Subsequent refresh on the logged-out token must fail.
	_, err = refreshH.Handle(ctx, commands.RefreshSessionInput{RefreshToken: pair.RefreshToken})
	require.Error(t, err)
}

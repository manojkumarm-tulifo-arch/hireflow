package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hustle/hireflow/internal/auth/application/dto"
	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/services"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// AccessTokenTTL is how long an issued JWT is valid.
const AccessTokenTTL = 15 * time.Minute

// VerifyOTPInput collects the email + code pair the user submits.
type VerifyOTPInput struct {
	Email string
	Code  string
}

// SignupVerifyOTPHandler verifies a signup OTP, marks the user Active, and
// issues a token pair.
type SignupVerifyOTPHandler struct {
	users    repositories.UserRepository
	sessions repositories.OTPSessionRepository
	hasher   services.OTPHasher
	tokens   *issueTokensService
}

// NewSignupVerifyOTPHandler wires the handler.
func NewSignupVerifyOTPHandler(
	users repositories.UserRepository,
	sessions repositories.OTPSessionRepository,
	refreshTokens repositories.RefreshTokenRepository,
	hasher services.OTPHasher,
	issuer services.TokenIssuer,
	refreshGen services.RefreshTokenSecretGenerator,
) *SignupVerifyOTPHandler {
	return &SignupVerifyOTPHandler{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		tokens:   newIssueTokensService(refreshTokens, issuer, refreshGen),
	}
}

// Handle executes the use case.
func (h *SignupVerifyOTPHandler) Handle(ctx context.Context, in VerifyOTPInput) (dto.TokenPairDTO, error) {
	email, code, err := parseEmailAndCode(in.Email, in.Code)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: %w", err)
	}

	session, err := h.sessions.FindLatestForEmail(ctx, email, valueobjects.OTPPurposeSignup)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: %w", err)
	}
	if err := session.Verify(code, h.hasher.Matches); err != nil {
		// Persist the attempt counter / verifiedAt update either way.
		_ = h.sessions.Save(ctx, session)
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: %w", err)
	}
	if err := h.sessions.Save(ctx, session); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: persist session: %w", err)
	}

	user, err := h.users.FindByEmailAcrossTenants(ctx, email)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: load user: %w", err)
	}
	if err := user.MarkVerified(); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: %w", err)
	}
	if err := user.RecordSignIn(); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: %w", err)
	}
	if err := h.users.Save(ctx, user); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signup verify otp: save user: %w", err)
	}
	return h.tokens.issue(ctx, user)
}

// SigninVerifyOTPHandler verifies a signin OTP and issues a token pair.
type SigninVerifyOTPHandler struct {
	users    repositories.UserRepository
	sessions repositories.OTPSessionRepository
	hasher   services.OTPHasher
	tokens   *issueTokensService
}

// NewSigninVerifyOTPHandler wires the handler.
func NewSigninVerifyOTPHandler(
	users repositories.UserRepository,
	sessions repositories.OTPSessionRepository,
	refreshTokens repositories.RefreshTokenRepository,
	hasher services.OTPHasher,
	issuer services.TokenIssuer,
	refreshGen services.RefreshTokenSecretGenerator,
) *SigninVerifyOTPHandler {
	return &SigninVerifyOTPHandler{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		tokens:   newIssueTokensService(refreshTokens, issuer, refreshGen),
	}
}

// Handle executes the use case.
func (h *SigninVerifyOTPHandler) Handle(ctx context.Context, in VerifyOTPInput) (dto.TokenPairDTO, error) {
	email, code, err := parseEmailAndCode(in.Email, in.Code)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: %w", err)
	}
	session, err := h.sessions.FindLatestForEmail(ctx, email, valueobjects.OTPPurposeSignin)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: %w", err)
	}
	user, userErr := h.users.FindByEmailAcrossTenants(ctx, email)
	if userErr != nil && !errors.Is(userErr, repositories.ErrUserNotFound) {
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: load user: %w", userErr)
	}

	if err := session.Verify(code, h.hasher.Matches); err != nil {
		// Track failure on the user when we can — drives the lockout.
		if user != nil {
			user.RecordFailedAttempt()
			_ = h.users.Save(ctx, user)
		}
		_ = h.sessions.Save(ctx, session)
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: %w", err)
	}
	if err := h.sessions.Save(ctx, session); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: persist session: %w", err)
	}
	if user == nil {
		// OTP matched but user vanished — pathological, but treat as not found.
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: %w", repositories.ErrUserNotFound)
	}
	if err := user.RecordSignIn(); err != nil {
		_ = h.users.Save(ctx, user)
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: %w", err)
	}
	if err := h.users.Save(ctx, user); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("signin verify otp: save user: %w", err)
	}
	return h.tokens.issue(ctx, user)
}

func parseEmailAndCode(rawEmail, rawCode string) (valueobjects.Email, valueobjects.OTPCode, error) {
	email, err := valueobjects.NewEmail(rawEmail)
	if err != nil {
		return valueobjects.Email{}, valueobjects.OTPCode{}, err
	}
	code, err := valueobjects.NewOTPCode(rawCode)
	if err != nil {
		return valueobjects.Email{}, valueobjects.OTPCode{}, err
	}
	return email, code, nil
}

// issueTokensService is a tiny private helper shared by signup-verify,
// signin-verify, and refresh handlers — issues an access JWT, generates a
// fresh refresh secret, persists the refresh row.
type issueTokensService struct {
	refreshTokens repositories.RefreshTokenRepository
	issuer        services.TokenIssuer
	refreshGen    services.RefreshTokenSecretGenerator
}

func newIssueTokensService(rt repositories.RefreshTokenRepository, issuer services.TokenIssuer, gen services.RefreshTokenSecretGenerator) *issueTokensService {
	return &issueTokensService{refreshTokens: rt, issuer: issuer, refreshGen: gen}
}

func (s *issueTokensService) issue(ctx context.Context, user *entities.User) (dto.TokenPairDTO, error) {
	accessToken, accessExp, err := s.issuer.IssueAccess(user, AccessTokenTTL)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("issue access: %w", err)
	}
	raw, hash, err := s.refreshGen.Generate()
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("issue refresh: generate: %w", err)
	}
	rt, err := entities.NewRefreshToken(user.ID(), user.TenantID(), hash)
	if err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("issue refresh: %w", err)
	}
	if err := s.refreshTokens.Save(ctx, rt); err != nil {
		return dto.TokenPairDTO{}, fmt.Errorf("issue refresh: save: %w", err)
	}
	return dto.TokenPairDTO{
		AccessToken:      accessToken,
		AccessExpiresAt:  accessExp,
		RefreshToken:     rt.ID().String() + "." + raw, // id.secret — see refresh handler
		RefreshExpiresAt: rt.ExpiresAt(),
		User:             dto.UserDTOFromEntity(user),
	}, nil
}

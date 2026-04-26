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

// ErrUnknownSigninEmail signals that the email submitted to signin/request-otp
// has no matching user. Surfaced to the FE so it can prompt the visitor to
// sign up instead of stalling on the OTP screen.
var ErrUnknownSigninEmail = errors.New("no account found for this email")

// SigninRequestOTPInput collects only the email — tenant is resolved from
// the user record.
type SigninRequestOTPInput struct {
	Email string
}

// SigninRequestOTPHandler issues a signin OTP for an existing active user.
// Returns ErrUnknownSigninEmail when the email has no account, so the FE
// can prompt for sign-up. Returns the relevant entity error when the user
// exists but cannot sign in (locked / not verified).
type SigninRequestOTPHandler struct {
	users       repositories.UserRepository
	otpSessions repositories.OTPSessionRepository
	gen         services.OTPGenerator
	hasher      services.OTPHasher
	sender      services.OTPSender
}

// NewSigninRequestOTPHandler wires the handler.
func NewSigninRequestOTPHandler(
	users repositories.UserRepository,
	otpSessions repositories.OTPSessionRepository,
	gen services.OTPGenerator,
	hasher services.OTPHasher,
	sender services.OTPSender,
) *SigninRequestOTPHandler {
	return &SigninRequestOTPHandler{
		users: users, otpSessions: otpSessions,
		gen: gen, hasher: hasher, sender: sender,
	}
}

// Handle executes the use case.
func (h *SigninRequestOTPHandler) Handle(ctx context.Context, in SigninRequestOTPInput) (dto.OTPRequestResultDTO, error) {
	email, err := valueobjects.NewEmail(in.Email)
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signin request otp: %w", err)
	}

	user, err := h.users.FindByEmailAcrossTenants(ctx, email)
	if errors.Is(err, repositories.ErrUserNotFound) || user == nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signin request otp: %w", ErrUnknownSigninEmail)
	}
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signin request otp: %w", err)
	}
	if !user.CanSignInNow() {
		if lu := user.LockedUntil(); lu != nil && time.Now().Before(*lu) {
			return dto.OTPRequestResultDTO{}, fmt.Errorf("signin request otp: %w", entities.ErrAccountLocked)
		}
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signin request otp: %w", entities.ErrCannotSignInWhenNotActive)
	}
	return issueOTP(ctx, h.otpSessions, h.gen, h.hasher, h.sender, user.TenantID(), email, valueobjects.OTPPurposeSignin)
}

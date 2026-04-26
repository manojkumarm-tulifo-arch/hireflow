// Package commands holds command handlers for the auth context.
package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/hustle/hireflow/internal/auth/application/dto"
	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/services"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// SignupRequestOTPInput collects what we need to start a signup challenge.
type SignupRequestOTPInput struct {
	Email      string
	Name       string
	TenantSlug string
}

// SignupRequestOTPHandler creates a PendingVerification user and issues a
// signup OTP. Rejects if email already exists in any tenant — those users
// should sign in instead.
type SignupRequestOTPHandler struct {
	users       repositories.UserRepository
	tenants     repositories.TenantRepository
	otpSessions repositories.OTPSessionRepository
	gen         services.OTPGenerator
	hasher      services.OTPHasher
	sender      services.OTPSender
}

// NewSignupRequestOTPHandler wires the handler.
func NewSignupRequestOTPHandler(
	users repositories.UserRepository,
	tenants repositories.TenantRepository,
	otpSessions repositories.OTPSessionRepository,
	gen services.OTPGenerator,
	hasher services.OTPHasher,
	sender services.OTPSender,
) *SignupRequestOTPHandler {
	return &SignupRequestOTPHandler{
		users: users, tenants: tenants, otpSessions: otpSessions,
		gen: gen, hasher: hasher, sender: sender,
	}
}

// Handle executes the use case.
func (h *SignupRequestOTPHandler) Handle(ctx context.Context, in SignupRequestOTPInput) (dto.OTPRequestResultDTO, error) {
	email, err := valueobjects.NewEmail(in.Email)
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signup request otp: %w", err)
	}
	slug, err := valueobjects.NewTenantSlug(in.TenantSlug)
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signup request otp: %w", err)
	}
	tenantID, err := h.tenants.FindIDBySlug(ctx, slug)
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signup request otp: %w", err)
	}

	existing, err := h.users.FindByEmailAcrossTenants(ctx, email)
	if err != nil && !errors.Is(err, repositories.ErrUserNotFound) {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signup request otp: lookup: %w", err)
	}
	if existing != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signup request otp: %w", repositories.ErrEmailAlreadyRegistered)
	}

	// Create the user (PendingVerification) — committed before issuing the OTP
	// so a delivery failure won't leave us with an orphan OTP session.
	user, err := entities.NewUser(tenantID, email, in.Name, nil)
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signup request otp: %w", err)
	}
	if err := h.users.Save(ctx, user); err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("signup request otp: save user: %w", err)
	}

	return issueOTP(ctx, h.otpSessions, h.gen, h.hasher, h.sender, tenantID, email, valueobjects.OTPPurposeSignup)
}

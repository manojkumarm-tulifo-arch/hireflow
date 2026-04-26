package commands

import (
	"context"
	"fmt"

	"github.com/hustle/hireflow/internal/auth/application/dto"
	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/services"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// issueOTP generates a fresh code, hashes and persists a session, and asks
// the sender to deliver the plaintext code. Used by both signup and signin
// request handlers — same shape, different OTPPurpose.
func issueOTP(
	ctx context.Context,
	sessions repositories.OTPSessionRepository,
	gen services.OTPGenerator,
	hasher services.OTPHasher,
	sender services.OTPSender,
	tenantID shared.TenantID,
	email valueobjects.Email,
	purpose valueobjects.OTPPurpose,
) (dto.OTPRequestResultDTO, error) {
	code, err := gen.Generate()
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("issue otp: generate: %w", err)
	}
	hash, err := hasher.Hash(code)
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("issue otp: hash: %w", err)
	}
	session, err := entities.NewOTPSession(tenantID, email, purpose, hash)
	if err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("issue otp: new session: %w", err)
	}
	if err := sessions.Save(ctx, session); err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("issue otp: save session: %w", err)
	}
	// Delivery happens last — we'd rather have a session-without-delivery
	// (the user requests a resend) than a delivered-without-session (the
	// user enters a code we can't validate).
	if err := sender.Send(ctx, email, code, purpose); err != nil {
		return dto.OTPRequestResultDTO{}, fmt.Errorf("issue otp: send: %w", err)
	}
	return dto.OTPRequestResultDTO{
		Sent:      true,
		ExpiresAt: session.ExpiresAt(),
	}, nil
}

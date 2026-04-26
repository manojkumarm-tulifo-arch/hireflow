package entities

import (
	"errors"
	"time"

	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

// Domain errors enforced at the OTPSession boundary.
var (
	ErrOTPExpired         = errors.New("otp session expired")
	ErrOTPAlreadyVerified = errors.New("otp session already verified")
	ErrOTPNoAttemptsLeft  = errors.New("otp session has no attempts remaining")
	ErrOTPCodeMismatch    = errors.New("otp code does not match")
)

// MaxOTPAttempts is the number of guesses allowed per session.
const MaxOTPAttempts = 5

// OTPSessionTTL is how long an OTP code is valid after issue.
const OTPSessionTTL = 10 * time.Minute

// OTPSession is the aggregate that tracks an in-flight OTP challenge.
// One session per (email + purpose) is active at a time — repository
// implementations enforce this by invalidating prior sessions on create.
type OTPSession struct {
	id           valueobjects.OTPSessionID
	tenantID     shared.TenantID
	email        valueobjects.Email
	purpose      valueobjects.OTPPurpose
	codeHash     string
	attemptsLeft int
	expiresAt    time.Time
	verifiedAt   *time.Time
	createdAt    time.Time
	updatedAt    time.Time
}

// NewOTPSession creates a fresh challenge.
// codeHash must already be hashed by an OTPHasher implementation —
// the plaintext code is given to the OTPSender to deliver to the user
// and is never stored.
func NewOTPSession(
	tenantID shared.TenantID,
	email valueobjects.Email,
	purpose valueobjects.OTPPurpose,
	codeHash string,
) (*OTPSession, error) {
	if email.IsZero() || codeHash == "" {
		return nil, errors.New("invalid otp session construction")
	}
	now := time.Now().UTC()
	return &OTPSession{
		id:           valueobjects.NewOTPSessionID(),
		tenantID:     tenantID,
		email:        email,
		purpose:      purpose,
		codeHash:     codeHash,
		attemptsLeft: MaxOTPAttempts,
		expiresAt:    now.Add(OTPSessionTTL),
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

// HydrateOTPSession reconstitutes a session from persistence.
func HydrateOTPSession(
	id valueobjects.OTPSessionID,
	tenantID shared.TenantID,
	email valueobjects.Email,
	purpose valueobjects.OTPPurpose,
	codeHash string,
	attemptsLeft int,
	expiresAt time.Time,
	verifiedAt *time.Time,
	createdAt, updatedAt time.Time,
) *OTPSession {
	return &OTPSession{
		id:           id,
		tenantID:     tenantID,
		email:        email,
		purpose:      purpose,
		codeHash:     codeHash,
		attemptsLeft: attemptsLeft,
		expiresAt:    expiresAt,
		verifiedAt:   verifiedAt,
		createdAt:    createdAt,
		updatedAt:    updatedAt,
	}
}

// Getters.
func (s *OTPSession) ID() valueobjects.OTPSessionID { return s.id }
func (s *OTPSession) TenantID() shared.TenantID     { return s.tenantID }
func (s *OTPSession) Email() valueobjects.Email     { return s.email }
func (s *OTPSession) Purpose() valueobjects.OTPPurpose { return s.purpose }
func (s *OTPSession) CodeHash() string              { return s.codeHash }
func (s *OTPSession) AttemptsLeft() int             { return s.attemptsLeft }
func (s *OTPSession) ExpiresAt() time.Time          { return s.expiresAt }
func (s *OTPSession) VerifiedAt() *time.Time        { return s.verifiedAt }
func (s *OTPSession) CreatedAt() time.Time          { return s.createdAt }
func (s *OTPSession) UpdatedAt() time.Time          { return s.updatedAt }

// Verify checks a candidate code against this session and consumes one attempt
// regardless of outcome. The caller supplies an OTPHasher to do the comparison
// in constant time — we don't import the hasher to keep the domain pure.
func (s *OTPSession) Verify(candidate valueobjects.OTPCode, matches func(hash, code string) bool) error {
	if s.verifiedAt != nil {
		return ErrOTPAlreadyVerified
	}
	if time.Now().UTC().After(s.expiresAt) {
		return ErrOTPExpired
	}
	if s.attemptsLeft <= 0 {
		return ErrOTPNoAttemptsLeft
	}
	s.attemptsLeft--
	s.updatedAt = time.Now().UTC()

	if !matches(s.codeHash, candidate.String()) {
		return ErrOTPCodeMismatch
	}
	now := time.Now().UTC()
	s.verifiedAt = &now
	return nil
}

// IsVerified reports whether this session has been successfully consumed.
func (s *OTPSession) IsVerified() bool { return s.verifiedAt != nil }

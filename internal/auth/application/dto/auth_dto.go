// Package dto holds the response shapes for the auth application layer.
package dto

import "time"

// UserDTO is the public projection of a User aggregate.
type UserDTO struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenant_id"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles"`
}

// TokenPairDTO is the access + refresh pair returned at signup verify, signin
// verify, and refresh. Refresh token is opaque (random secret); only the
// client should ever see this raw value.
type TokenPairDTO struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	User             UserDTO   `json:"user"`
}

// OTPRequestResultDTO is what the request-OTP endpoints return.
// We never reveal whether the email exists — same shape regardless,
// to defend against email-enumeration attacks.
type OTPRequestResultDTO struct {
	Sent      bool      `json:"sent"`
	ExpiresAt time.Time `json:"expires_at"`
}

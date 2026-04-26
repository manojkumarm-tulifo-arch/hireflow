// Package v1 holds the v1 HTTP request/response shapes for the auth context.
package v1

// SignupRequestOTPRequest body for POST /auth/signup/request-otp.
type SignupRequestOTPRequest struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	TenantSlug string `json:"tenant_slug"`
}

// SigninRequestOTPRequest body for POST /auth/signin/request-otp.
type SigninRequestOTPRequest struct {
	Email string `json:"email"`
}

// VerifyOTPRequest body for both /auth/signup/verify-otp and /auth/signin/verify-otp.
type VerifyOTPRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// RefreshRequest body for POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest body for POST /auth/logout.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Envelope is the standard API response envelope.
type Envelope struct {
	Success bool       `json:"success"`
	Data    any        `json:"data,omitempty"`
	Error   *ErrorInfo `json:"error,omitempty"`
}

// ErrorInfo is the standard API error block.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

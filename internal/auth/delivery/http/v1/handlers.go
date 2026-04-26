package v1

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/auth/application/commands"
	"github.com/hustle/hireflow/internal/auth/domain/entities"
	"github.com/hustle/hireflow/internal/auth/domain/repositories"
	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// AuthHandler holds the application services that back v1 routes.
type AuthHandler struct {
	signupRequest *commands.SignupRequestOTPHandler
	signupVerify  *commands.SignupVerifyOTPHandler
	signinRequest *commands.SigninRequestOTPHandler
	signinVerify  *commands.SigninVerifyOTPHandler
	refresh       *commands.RefreshSessionHandler
	logout        *commands.LogoutHandler
	logger        zerolog.Logger
}

// NewAuthHandler wires the v1 handler.
func NewAuthHandler(
	signupRequest *commands.SignupRequestOTPHandler,
	signupVerify *commands.SignupVerifyOTPHandler,
	signinRequest *commands.SigninRequestOTPHandler,
	signinVerify *commands.SigninVerifyOTPHandler,
	refresh *commands.RefreshSessionHandler,
	logout *commands.LogoutHandler,
	logger zerolog.Logger,
) *AuthHandler {
	return &AuthHandler{
		signupRequest: signupRequest,
		signupVerify:  signupVerify,
		signinRequest: signinRequest,
		signinVerify:  signinVerify,
		refresh:       refresh,
		logout:        logout,
		logger:        logger,
	}
}

// SignupRequestOTP handles POST /auth/signup/request-otp.
func (h *AuthHandler) SignupRequestOTP(w http.ResponseWriter, r *http.Request) {
	var req SignupRequestOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	out, err := h.signupRequest.Handle(r.Context(), commands.SignupRequestOTPInput{
		Email: req.Email, Name: req.Name, TenantSlug: req.TenantSlug,
	})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, Envelope{Success: true, Data: out})
}

// SignupVerifyOTP handles POST /auth/signup/verify-otp.
func (h *AuthHandler) SignupVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	out, err := h.signupVerify.Handle(r.Context(), commands.VerifyOTPInput{Email: req.Email, Code: req.Code})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// SigninRequestOTP handles POST /auth/signin/request-otp.
func (h *AuthHandler) SigninRequestOTP(w http.ResponseWriter, r *http.Request) {
	var req SigninRequestOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	out, err := h.signinRequest.Handle(r.Context(), commands.SigninRequestOTPInput{Email: req.Email})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, Envelope{Success: true, Data: out})
}

// SigninVerifyOTP handles POST /auth/signin/verify-otp.
func (h *AuthHandler) SigninVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	out, err := h.signinVerify.Handle(r.Context(), commands.VerifyOTPInput{Email: req.Email, Code: req.Code})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// Refresh handles POST /auth/refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	out, err := h.refresh.Handle(r.Context(), commands.RefreshSessionInput{RefreshToken: req.RefreshToken})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// Logout handles POST /auth/logout.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	if err := h.logout.Handle(r.Context(), commands.LogoutInput{RefreshToken: req.RefreshToken}); err != nil {
		h.respondDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// respondDomainError maps domain/repository errors to HTTP status codes.
// Auth deliberately collapses many failure modes into the same code/message
// to avoid information leakage (e.g., we don't differentiate "wrong code"
// vs "session expired" beyond a generic invalid_otp).
func (h *AuthHandler) respondDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repositories.ErrEmailAlreadyRegistered):
		writeError(w, http.StatusConflict, "email_taken", "email is already registered")
	case errors.Is(err, repositories.ErrTenantNotFound):
		writeError(w, http.StatusNotFound, "tenant_not_found", "no tenant for that slug")
	case errors.Is(err, commands.ErrUnknownSigninEmail):
		writeError(w, http.StatusNotFound, "user_not_found", "no account found for this email")
	case errors.Is(err, repositories.ErrUserNotFound),
		errors.Is(err, repositories.ErrOTPSessionNotFound),
		errors.Is(err, repositories.ErrRefreshTokenNotFound):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "credentials rejected")
	case errors.Is(err, entities.ErrOTPCodeMismatch),
		errors.Is(err, entities.ErrOTPExpired),
		errors.Is(err, entities.ErrOTPAlreadyVerified),
		errors.Is(err, entities.ErrOTPNoAttemptsLeft):
		writeError(w, http.StatusUnauthorized, "invalid_otp", "Invalid OTP. Please try again.")
	case errors.Is(err, entities.ErrCannotSignInWhenNotActive),
		errors.Is(err, entities.ErrAccountLocked):
		writeError(w, http.StatusForbidden, "account_unavailable", "account cannot sign in")
	case errors.Is(err, entities.ErrRefreshTokenRevoked),
		errors.Is(err, entities.ErrRefreshTokenExpired),
		errors.Is(err, entities.ErrRefreshTokenInvalid),
		errors.Is(err, commands.ErrMalformedRefreshToken):
		writeError(w, http.StatusUnauthorized, "invalid_refresh", "refresh token rejected")
	case errors.Is(err, valueobjects.ErrInvalidEmail),
		errors.Is(err, valueobjects.ErrInvalidOTPCode),
		errors.Is(err, valueobjects.ErrInvalidTenantSlug):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		h.logger.Error().Err(err).Msg("unexpected auth error")
		writeError(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, Envelope{Success: false, Error: &ErrorInfo{Code: code, Message: message}})
}

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
// OTP failure modes are split into distinct codes so the FE can show
// actionable copy: "code expired" → request a new one; "no attempts left"
// → request a new one; "wrong code" → try again. The security cost is
// negligible — an attacker without the OTP can already distinguish "wrong
// code" from "no session" via timing, and these distinctions only help
// legitimate users.
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

	// OTP failure modes — distinct codes for actionable FE messaging.
	case errors.Is(err, entities.ErrOTPExpired):
		writeError(w, http.StatusUnauthorized, "otp_expired",
			"This code has expired. Request a fresh one.")
	case errors.Is(err, entities.ErrOTPNoAttemptsLeft):
		writeError(w, http.StatusUnauthorized, "otp_max_attempts",
			"Too many wrong attempts on this code. Request a fresh one.")
	case errors.Is(err, entities.ErrOTPAlreadyVerified):
		writeError(w, http.StatusUnauthorized, "otp_already_used",
			"This code has already been used. Request a fresh one.")
	case errors.Is(err, entities.ErrOTPCodeMismatch):
		writeError(w, http.StatusUnauthorized, "otp_mismatch",
			"That code doesn't match. Try again or request a fresh one.")

	// Account-state failures — split so FE can guide the user.
	case errors.Is(err, entities.ErrAccountLocked):
		writeError(w, http.StatusForbidden, "account_locked",
			"This account is locked after repeated failed attempts. Wait 15 minutes and try again, or contact support.")
	case errors.Is(err, entities.ErrCannotSignInWhenNotActive):
		writeError(w, http.StatusForbidden, "account_pending",
			"This account hasn't been verified yet. Check your email for the original verification link, or sign up again.")

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

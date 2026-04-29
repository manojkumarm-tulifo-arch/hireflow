package v1

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/auth/domain/entities"
)

// newHandlerForErrorTest returns a handler with nil command dependencies —
// respondDomainError doesn't touch them. It only needs a logger.
func newHandlerForErrorTest() *AuthHandler {
	return &AuthHandler{logger: zerolog.Nop()}
}

func TestRespondDomainError_OTPMappings(t *testing.T) {
	h := newHandlerForErrorTest()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		messageHas string
	}{
		{"expired", entities.ErrOTPExpired, 401, "otp_expired", "expired"},
		{"max attempts", entities.ErrOTPNoAttemptsLeft, 401, "otp_max_attempts", "Too many"},
		{"already used", entities.ErrOTPAlreadyVerified, 401, "otp_already_used", "already been used"},
		{"mismatch", entities.ErrOTPCodeMismatch, 401, "otp_mismatch", "doesn't match"},
		{"locked", entities.ErrAccountLocked, 403, "account_locked", "locked"},
		{"pending", entities.ErrCannotSignInWhenNotActive, 403, "account_pending", "verified"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.respondDomainError(rec, tc.err)

			assert.Equal(t, tc.wantStatus, rec.Code)

			var body Envelope
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
			require.NotNil(t, body.Error, "envelope must carry an error")
			assert.Equal(t, tc.wantCode, body.Error.Code)
			assert.Contains(t, body.Error.Message, tc.messageHas)
		})
	}
}

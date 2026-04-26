// Package notifications holds OTP delivery implementations.
package notifications

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/auth/domain/valueobjects"
)

// LogOTPSender writes OTP codes to the log instead of sending email.
// Use only in dev — there's a comically loud warning at construction time.
type LogOTPSender struct {
	logger zerolog.Logger
}

// NewLogOTPSender wires the dev sender. Logs a warning so it can't be
// enabled in production without anyone noticing.
func NewLogOTPSender(logger zerolog.Logger) *LogOTPSender {
	logger.Warn().Msg("LogOTPSender active — OTP codes are written to logs (dev only, never deploy)")
	return &LogOTPSender{logger: logger.With().Str("component", "log_otp_sender").Logger()}
}

// Send logs the code at INFO level and also prints a high-visibility banner
// to stdout so it's impossible to miss in the dev console regardless of
// LOG_LEVEL or surrounding log volume.
func (s *LogOTPSender) Send(_ context.Context, email valueobjects.Email, code valueobjects.OTPCode, purpose valueobjects.OTPPurpose) error {
	s.logger.Info().
		Str("email", email.String()).
		Str("purpose", purpose.String()).
		Str("code", code.String()).
		Msg("OTP issued")

	fmt.Fprintf(os.Stdout,
		"\n"+
			"╔══════════════════════════════════════════════════════╗\n"+
			"║  OTP for %-20s  (%-7s)        ║\n"+
			"║  CODE: %-46s║\n"+
			"╚══════════════════════════════════════════════════════╝\n\n",
		email.String(), purpose.String(), code.String(),
	)
	return nil
}

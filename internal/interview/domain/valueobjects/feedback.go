package valueobjects

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Feedback is one recruiter-entered feedback row for an interview round.
// Immutable value object; appended via FeedbackRepository.Save.
type Feedback struct {
	InterviewerName  string
	InterviewerEmail string // optional
	Decision         FeedbackDecision
	Notes            string
	SubmittedBy      uuid.UUID
	SubmittedAt      time.Time
}

// Validate enforces minimum invariants.
func (f Feedback) Validate() error {
	if strings.TrimSpace(f.InterviewerName) == "" {
		return errors.New("feedback: interviewer_name required")
	}
	if f.InterviewerEmail != "" {
		if _, err := mail.ParseAddress(f.InterviewerEmail); err != nil {
			return errors.New("feedback: interviewer_email invalid")
		}
	}
	if _, err := ParseFeedbackDecision(string(f.Decision)); err != nil {
		return err
	}
	if f.SubmittedBy == uuid.Nil {
		return errors.New("feedback: submitted_by required")
	}
	return nil
}

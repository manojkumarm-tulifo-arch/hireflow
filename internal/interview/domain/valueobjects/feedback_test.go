package valueobjects

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func validFeedback() Feedback {
	return Feedback{
		InterviewerName:  "Alice Smith",
		InterviewerEmail: "alice@example.com",
		Decision:         FeedbackDecisionYes,
		Notes:            "Strong candidate.",
		SubmittedBy:      uuid.New(),
		SubmittedAt:      time.Now(),
	}
}

func TestFeedbackValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(f *Feedback)
		wantErr bool
		errMsg  string
		errIs   error
	}{
		{
			name:    "happy path",
			mutate:  func(f *Feedback) {},
			wantErr: false,
		},
		{
			name:    "missing interviewer_name",
			mutate:  func(f *Feedback) { f.InterviewerName = "" },
			wantErr: true,
			errMsg:  "feedback: interviewer_name required",
		},
		{
			name:    "whitespace interviewer_name",
			mutate:  func(f *Feedback) { f.InterviewerName = "   " },
			wantErr: true,
			errMsg:  "feedback: interviewer_name required",
		},
		{
			name:    "bad interviewer_email",
			mutate:  func(f *Feedback) { f.InterviewerEmail = "not-an-email" },
			wantErr: true,
			errMsg:  "feedback: interviewer_email invalid",
		},
		{
			name:    "malformed email with spaces",
			mutate:  func(f *Feedback) { f.InterviewerEmail = "foo @bar.com" },
			wantErr: true,
			errMsg:  "feedback: interviewer_email invalid",
		},
		{
			name:    "empty email is OK (optional)",
			mutate:  func(f *Feedback) { f.InterviewerEmail = "" },
			wantErr: false,
		},
		{
			name:    "invalid decision",
			mutate:  func(f *Feedback) { f.Decision = "maybe" },
			wantErr: true,
			errIs:   ErrInvalidFeedbackDecision,
		},
		{
			name:    "empty decision",
			mutate:  func(f *Feedback) { f.Decision = "" },
			wantErr: true,
			errIs:   ErrInvalidFeedbackDecision,
		},
		{
			name:    "nil submitted_by",
			mutate:  func(f *Feedback) { f.SubmittedBy = uuid.Nil },
			wantErr: true,
			errMsg:  "feedback: submitted_by required",
		},
		{
			name:    "all valid decisions accepted - strong_yes",
			mutate:  func(f *Feedback) { f.Decision = FeedbackDecisionStrongYes },
			wantErr: false,
		},
		{
			name:    "all valid decisions accepted - mixed",
			mutate:  func(f *Feedback) { f.Decision = FeedbackDecisionMixed },
			wantErr: false,
		},
		{
			name:    "all valid decisions accepted - no",
			mutate:  func(f *Feedback) { f.Decision = FeedbackDecisionNo },
			wantErr: false,
		},
		{
			name:    "all valid decisions accepted - strong_no",
			mutate:  func(f *Feedback) { f.Decision = FeedbackDecisionStrongNo },
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := validFeedback()
			tc.mutate(&f)
			err := f.Validate()
			if tc.wantErr {
				if err == nil {
					if tc.errMsg != "" {
						t.Fatalf("expected error %q, got nil", tc.errMsg)
					} else {
						t.Fatalf("expected an error, got nil")
					}
				}
				if tc.errIs != nil {
					if !errors.Is(err, tc.errIs) {
						t.Fatalf("expected errors.Is(%v), got %q", tc.errIs, err.Error())
					}
				}
				if tc.errMsg != "" && err.Error() != tc.errMsg {
					t.Fatalf("expected error %q, got %q", tc.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %q", err.Error())
				}
			}
		})
	}
}

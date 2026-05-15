package valueobjects

import (
	"testing"
)

func validQuestion() Question {
	return Question{
		Prompt:          "Describe a time you optimized a slow query.",
		SkillProbed:     "Database optimization",
		Why:             "Measures depth of SQL knowledge under pressure.",
		ExpectedSignals: []string{"identifies bottleneck", "uses EXPLAIN", "measures before and after"},
		ModelAnswer:     "The candidate should describe profiling the query, identifying an index gap, and validating with EXPLAIN.",
		RedFlags:        []string{"no measurement", "guessed without profiling"},
		FollowUps:       []string{"What would you do differently at 100x scale?"},
	}
}

func TestQuestionValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(q *Question)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "happy path",
			mutate:  func(q *Question) {},
			wantErr: false,
		},
		{
			name:    "missing prompt",
			mutate:  func(q *Question) { q.Prompt = "   " },
			wantErr: true,
			errMsg:  "question: prompt required",
		},
		{
			name:    "empty prompt",
			mutate:  func(q *Question) { q.Prompt = "" },
			wantErr: true,
			errMsg:  "question: prompt required",
		},
		{
			name:    "missing skill_probed",
			mutate:  func(q *Question) { q.SkillProbed = "" },
			wantErr: true,
			errMsg:  "question: skill_probed required",
		},
		{
			name:    "whitespace skill_probed",
			mutate:  func(q *Question) { q.SkillProbed = "  " },
			wantErr: true,
			errMsg:  "question: skill_probed required",
		},
		{
			name:    "missing why",
			mutate:  func(q *Question) { q.Why = "" },
			wantErr: true,
			errMsg:  "question: why required",
		},
		{
			name:    "too few expected_signals (2)",
			mutate:  func(q *Question) { q.ExpectedSignals = []string{"a", "b"} },
			wantErr: true,
			errMsg:  "question: expected_signals must have at least 3 entries",
		},
		{
			name:    "too few expected_signals (0)",
			mutate:  func(q *Question) { q.ExpectedSignals = nil },
			wantErr: true,
			errMsg:  "question: expected_signals must have at least 3 entries",
		},
		{
			name:    "exactly 3 expected_signals is valid",
			mutate:  func(q *Question) { q.ExpectedSignals = []string{"a", "b", "c"} },
			wantErr: false,
		},
		{
			name:    "empty model_answer",
			mutate:  func(q *Question) { q.ModelAnswer = "" },
			wantErr: true,
			errMsg:  "question: model_answer required",
		},
		{
			name:    "whitespace model_answer",
			mutate:  func(q *Question) { q.ModelAnswer = "\t" },
			wantErr: true,
			errMsg:  "question: model_answer required",
		},
		{
			name:    "too few red_flags (1)",
			mutate:  func(q *Question) { q.RedFlags = []string{"only one"} },
			wantErr: true,
			errMsg:  "question: red_flags must have at least 2 entries",
		},
		{
			name:    "too few red_flags (0)",
			mutate:  func(q *Question) { q.RedFlags = nil },
			wantErr: true,
			errMsg:  "question: red_flags must have at least 2 entries",
		},
		{
			name:    "exactly 2 red_flags is valid",
			mutate:  func(q *Question) { q.RedFlags = []string{"a", "b"} },
			wantErr: false,
		},
		{
			name:    "missing follow_ups",
			mutate:  func(q *Question) { q.FollowUps = nil },
			wantErr: true,
			errMsg:  "question: follow_ups must have at least 1 entry",
		},
		{
			name:    "empty follow_ups slice",
			mutate:  func(q *Question) { q.FollowUps = []string{} },
			wantErr: true,
			errMsg:  "question: follow_ups must have at least 1 entry",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := validQuestion()
			tc.mutate(&q)
			err := q.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.errMsg)
				}
				if err.Error() != tc.errMsg {
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

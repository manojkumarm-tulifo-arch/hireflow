package valueobjects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func TestLLMJudgment_JSONRoundTrip_MixedEvidence(t *testing.T) {
	j := vo.LLMJudgment{
		Score: 87,
		Evidence: []vo.JudgmentEvidence{
			{
				Kind:    "skill",
				Skill:   "Go",
				Claim:   "5 years",
				Support: "Senior Backend at Razorpay 2020–2025 — 4.8 years",
			},
			{
				Kind:       "experience",
				Experience: "payments",
				Support:    "Razorpay (payments) + PayU (payments)",
			},
		},
		Summary:       "Strong Go background with deep payments-domain experience.",
		Concerns:      []string{"Career gap 2018–2019 (1 year) not explained"},
		PromptVersion: "v1",
	}

	b, err := j.Marshal()
	require.NoError(t, err)

	got, err := vo.UnmarshalLLMJudgment(b)
	require.NoError(t, err)

	assert.Equal(t, j.Score, got.Score)
	assert.Equal(t, j.Summary, got.Summary)
	assert.Equal(t, j.PromptVersion, got.PromptVersion)
	require.Len(t, got.Concerns, 1)
	assert.Equal(t, j.Concerns[0], got.Concerns[0])

	require.Len(t, got.Evidence, 2)
	assert.Equal(t, "skill", got.Evidence[0].Kind)
	assert.Equal(t, "Go", got.Evidence[0].Skill)
	assert.Equal(t, "5 years", got.Evidence[0].Claim)
	assert.Equal(t, "Senior Backend at Razorpay 2020–2025 — 4.8 years", got.Evidence[0].Support)

	assert.Equal(t, "experience", got.Evidence[1].Kind)
	assert.Equal(t, "payments", got.Evidence[1].Experience)
	assert.Equal(t, "Razorpay (payments) + PayU (payments)", got.Evidence[1].Support)
}

func TestLLMJudgment_JSONRoundTrip_Empty(t *testing.T) {
	j := vo.LLMJudgment{
		Score:         0,
		PromptVersion: "v1",
	}
	b, err := j.Marshal()
	require.NoError(t, err)

	got, err := vo.UnmarshalLLMJudgment(b)
	require.NoError(t, err)
	assert.Equal(t, 0, got.Score)
	assert.Equal(t, "v1", got.PromptVersion)
	assert.Empty(t, got.Evidence)
	assert.Empty(t, got.Concerns)
}

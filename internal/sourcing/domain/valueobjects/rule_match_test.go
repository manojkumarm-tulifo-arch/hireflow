package valueobjects_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

func makeRule(typ, name string, required bool) vo.RuleCriterion {
	return vo.RuleCriterion{Type: typ, Name: name, Required: required}
}

func TestRuleMatchReport_RequiredPassRate_AllPass(t *testing.T) {
	r := vo.RuleMatchReport{Results: []vo.RuleResult{
		{Criterion: makeRule("skill", "Go", true), Passed: true},
		{Criterion: makeRule("skill", "SQL", true), Passed: true},
		{Criterion: makeRule("experience", "5y", true), Passed: true},
	}}
	assert.InDelta(t, 1.0, r.RequiredPassRate(), 0.0001)
	assert.True(t, r.PassedRequired())
}

func TestRuleMatchReport_RequiredPassRate_PartialFail(t *testing.T) {
	r := vo.RuleMatchReport{Results: []vo.RuleResult{
		{Criterion: makeRule("skill", "Go", true), Passed: true},
		{Criterion: makeRule("skill", "Kubernetes", true), Passed: false},
		{Criterion: makeRule("skill", "SQL", true), Passed: true},
	}}
	assert.InDelta(t, 2.0/3.0, r.RequiredPassRate(), 0.0001)
	assert.False(t, r.PassedRequired())
}

func TestRuleMatchReport_RequiredPassRate_NonePass(t *testing.T) {
	r := vo.RuleMatchReport{Results: []vo.RuleResult{
		{Criterion: makeRule("skill", "Go", true), Passed: false},
		{Criterion: makeRule("skill", "Kubernetes", true), Passed: false},
		{Criterion: makeRule("skill", "SQL", true), Passed: false},
	}}
	assert.InDelta(t, 0.0, r.RequiredPassRate(), 0.0001)
	assert.False(t, r.PassedRequired())
}

func TestRuleMatchReport_RequiredPassRate_AllOptional(t *testing.T) {
	// No required criteria → pass-rate is 1.0 (vacuous truth)
	r := vo.RuleMatchReport{Results: []vo.RuleResult{
		{Criterion: makeRule("skill", "Python", false), Passed: false},
		{Criterion: makeRule("skill", "Rust", false), Passed: false},
	}}
	assert.InDelta(t, 1.0, r.RequiredPassRate(), 0.0001)
	assert.True(t, r.PassedRequired())
}

func TestRuleMatchReport_RequiredPassRate_Empty(t *testing.T) {
	r := vo.RuleMatchReport{}
	assert.InDelta(t, 1.0, r.RequiredPassRate(), 0.0001)
	assert.True(t, r.PassedRequired())
}

func TestRuleMatchReport_JSONRoundTrip(t *testing.T) {
	r := vo.RuleMatchReport{Results: []vo.RuleResult{
		{
			Criterion:   makeRule("skill", "Go", true),
			Passed:      true,
			Actual:      "5y",
			EvidenceRef: "exp_0",
		},
		{
			Criterion: makeRule("location", "Bangalore|remote", false),
			Passed:    false,
		},
	}}

	b, err := r.Marshal()
	require.NoError(t, err)

	got, err := vo.UnmarshalRuleMatch(b)
	require.NoError(t, err)

	require.Len(t, got.Results, 2)
	assert.Equal(t, r.Results[0].Criterion.Name, got.Results[0].Criterion.Name)
	assert.Equal(t, r.Results[0].Passed, got.Results[0].Passed)
	assert.Equal(t, r.Results[0].Actual, got.Results[0].Actual)
	assert.Equal(t, r.Results[0].EvidenceRef, got.Results[0].EvidenceRef)
	assert.Equal(t, r.Results[1].Passed, got.Results[1].Passed)
}

func TestRuleMatchReport_MarshalIsValidJSON(t *testing.T) {
	r := vo.RuleMatchReport{}
	b, err := r.Marshal()
	require.NoError(t, err)
	assert.True(t, json.Valid(b))
}

func TestRuleMatchReport_Marshal_IncludesRequiredPassRate(t *testing.T) {
	r := vo.RuleMatchReport{Results: []vo.RuleResult{
		{Criterion: makeRule("skill", "Go", true), Passed: true},
		{Criterion: makeRule("skill", "Kubernetes", true), Passed: false},
	}}
	b, err := r.Marshal()
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))

	rate, ok := raw["required_pass_rate"].(float64)
	require.True(t, ok, "required_pass_rate must be present in marshalled JSON")
	assert.InDelta(t, 0.5, rate, 0.0001)
}

func TestRuleMatchReport_UnmarshalRoundTrip_WithPassRate(t *testing.T) {
	orig := vo.RuleMatchReport{Results: []vo.RuleResult{
		{Criterion: makeRule("skill", "Go", true), Passed: true},
		{Criterion: makeRule("location", "remote", false), Passed: false},
	}}
	b, err := orig.Marshal()
	require.NoError(t, err)

	got, err := vo.UnmarshalRuleMatch(b)
	require.NoError(t, err)
	require.Len(t, got.Results, 2)
	assert.Equal(t, orig.Results[0].Criterion.Name, got.Results[0].Criterion.Name)
	assert.Equal(t, orig.Results[1].Passed, got.Results[1].Passed)
}

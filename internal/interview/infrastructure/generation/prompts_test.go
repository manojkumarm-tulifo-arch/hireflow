package generation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/interview/domain/services"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/interview/infrastructure/generation"
)

// sampleInput builds a GenerationInput with representative data for use
// across multiple prompt-builder tests.
func sampleInput(kind vo.RoundKind) services.GenerationInput {
	return services.GenerationInput{
		RoundKind: kind,
		RoleSpec: services.RoleSpec{
			Title:     "Senior Backend Engineer",
			Seniority: "Senior",
			YearsMin:  4,
			YearsMax:  10,
			Team:      "Platform",
			Reports:   "VP Engineering",
			Skills: []services.SkillRequirement{
				{Name: "Go", Required: true},
				{Name: "PostgreSQL", Required: false},
			},
		},
		CandidateProfile: services.CandidateProfile{
			Headline: "Backend Engineer @ Razorpay",
			Location: "Bangalore",
			Skills:   []string{"Go", "PostgreSQL", "Kubernetes"},
			Experiences: []services.Experience{
				{
					Title:    "Senior Backend Engineer",
					Company:  "Razorpay",
					Duration: "2020-2025",
					Summary:  "Built distributed payment systems.",
				},
			},
			Education: []services.EducationEntry{
				{
					Degree:      "B.Tech",
					Field:       "Computer Science",
					Institution: "IIT Bombay",
					Year:        "2015",
				},
			},
			Certifications: []string{"CKA"},
		},
	}
}

// TestBuildPrompt_ContainsRoundKindName checks that the user prompt for each
// of the 5 RoundKinds contains the kind's canonical string.
func TestBuildPrompt_ContainsRoundKindName(t *testing.T) {
	kinds := []vo.RoundKind{
		vo.RoundKindScreen,
		vo.RoundKindTechnical,
		vo.RoundKindSystemDesign,
		vo.RoundKindBehavioral,
		vo.RoundKindBarRaiser,
	}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			_, user := generation.BuildPrompt(sampleInput(kind))
			assert.Contains(t, user, string(kind),
				"user prompt must contain the round kind name")
		})
	}
}

// TestBuildPrompt_RoleSpecFieldsAppear verifies that key role spec fields
// (title, seniority, skills, year range) are present in the user prompt.
func TestBuildPrompt_RoleSpecFieldsAppear(t *testing.T) {
	in := sampleInput(vo.RoundKindTechnical)
	_, user := generation.BuildPrompt(in)

	assert.Contains(t, user, "Senior Backend Engineer", "role title must appear in prompt")
	assert.Contains(t, user, "Senior", "seniority must appear in prompt")
	assert.Contains(t, user, "Go", "required skill must appear in prompt")
	assert.Contains(t, user, "(required)", "required marker must appear for required skills")
	assert.Contains(t, user, "PostgreSQL", "optional skill must appear in prompt")
	assert.Contains(t, user, "4-10", "year range must appear in prompt")
	assert.Contains(t, user, "Platform", "team must appear in prompt")
	assert.Contains(t, user, "VP Engineering", "reports-to must appear in prompt")
}

// TestBuildPrompt_CandidateProfileFieldsAppear verifies headline, location,
// skills, experience, education, and certifications appear in the user prompt.
func TestBuildPrompt_CandidateProfileFieldsAppear(t *testing.T) {
	in := sampleInput(vo.RoundKindBehavioral)
	_, user := generation.BuildPrompt(in)

	assert.Contains(t, user, "Backend Engineer @ Razorpay", "candidate headline must appear")
	assert.Contains(t, user, "Bangalore", "candidate location must appear")
	assert.Contains(t, user, "Kubernetes", "candidate skill must appear")
	assert.Contains(t, user, "Razorpay", "candidate company must appear")
	assert.Contains(t, user, "2020-2025", "experience duration must appear")
	assert.Contains(t, user, "Built distributed payment systems", "experience summary must appear")
	assert.Contains(t, user, "IIT Bombay", "education institution must appear")
	assert.Contains(t, user, "B.Tech", "education degree must appear")
	assert.Contains(t, user, "CKA", "certification must appear")
}

// TestBuildPrompt_SteeringAppearsWhenSet verifies the steering text is
// included in the user prompt when non-empty.
func TestBuildPrompt_SteeringAppearsWhenSet(t *testing.T) {
	in := sampleInput(vo.RoundKindTechnical)
	in.Steering = "Focus more on distributed systems design"

	_, user := generation.BuildPrompt(in)
	assert.Contains(t, user, "Focus more on distributed systems design",
		"steering text must appear in prompt when set")
}

// TestBuildPrompt_SteeringAbsentWhenEmpty verifies no steering section
// appears when Steering is an empty string.
func TestBuildPrompt_SteeringAbsentWhenEmpty(t *testing.T) {
	in := sampleInput(vo.RoundKindTechnical)
	in.Steering = ""

	_, user := generation.BuildPrompt(in)
	assert.NotContains(t, user, "Additional recruiter steering",
		"steering section must not appear when Steering is empty")
}

// TestBuildPrompt_UnmappedKindDefaultsToCount4 verifies that an unknown
// RoundKind falls back to count=4 in the prompt.
func TestBuildPrompt_UnmappedKindDefaultsToCount4(t *testing.T) {
	in := services.GenerationInput{
		RoundKind: vo.RoundKind("unknown_kind"),
	}
	_, user := generation.BuildPrompt(in)
	assert.Contains(t, user, "exactly 4 question",
		"unknown round kind must default to count=4")
}

// TestBuildPrompt_QuestionCountInPrompt verifies each known kind produces
// the expected count in the "Produce exactly N question" instruction.
func TestBuildPrompt_QuestionCountInPrompt(t *testing.T) {
	cases := []struct {
		kind  vo.RoundKind
		count string
	}{
		{vo.RoundKindScreen, "exactly 4"},
		{vo.RoundKindTechnical, "exactly 6"},
		{vo.RoundKindSystemDesign, "exactly 6"},
		{vo.RoundKindBehavioral, "exactly 4"},
		{vo.RoundKindBarRaiser, "exactly 4"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			_, user := generation.BuildPrompt(sampleInput(tc.kind))
			assert.Contains(t, user, tc.count,
				"user prompt must specify the correct question count")
		})
	}
}

// TestBuildPrompt_SystemPromptContainsKeyContract checks that the system
// prompt mentions model_answer and the JSON contract.
func TestBuildPrompt_SystemPromptContainsKeyContract(t *testing.T) {
	system, _ := generation.BuildPrompt(sampleInput(vo.RoundKindTechnical))
	assert.Contains(t, system, "model_answer",
		"system prompt must reference model_answer")
	assert.Contains(t, system, "JSON array",
		"system prompt must mention JSON array format")
}

// TestBuildPrompt_UserPromptContainsAllFields verifies all 7 required question
// fields are documented in the user prompt.
func TestBuildPrompt_UserPromptContainsAllFields(t *testing.T) {
	_, user := generation.BuildPrompt(sampleInput(vo.RoundKindTechnical))

	fields := []string{"prompt", "skill_probed", "why", "expected_signals", "model_answer", "red_flags", "follow_ups"}
	for _, f := range fields {
		assert.Contains(t, user, f, "field %q must be documented in user prompt", f)
	}
}

// TestBuildPrompt_WhitespaceSteering verifies whitespace-only steering is
// treated the same as empty (no section added).
func TestBuildPrompt_WhitespaceSteering(t *testing.T) {
	in := sampleInput(vo.RoundKindScreen)
	in.Steering = "   \t\n  "

	_, user := generation.BuildPrompt(in)
	assert.NotContains(t, user, "Additional recruiter steering",
		"whitespace-only steering must not produce a steering section")
}

// TestBuildPrompt_EmptyRoleSpec checks that BuildPrompt does not panic when
// the RoleSpec has no fields set (zero-value).
func TestBuildPrompt_EmptyRoleSpec(t *testing.T) {
	in := services.GenerationInput{
		RoundKind:        vo.RoundKindScreen,
		RoleSpec:         services.RoleSpec{},
		CandidateProfile: services.CandidateProfile{},
	}
	require.NotPanics(t, func() {
		_, _ = generation.BuildPrompt(in)
	})
}

// TestBuildPrompt_RoundKindBriefAppearsInUserPrompt verifies that the brief
// text for each round kind appears in the user prompt (not just the kind name).
func TestBuildPrompt_RoundKindBriefAppearsInUserPrompt(t *testing.T) {
	cases := map[vo.RoundKind]string{
		vo.RoundKindScreen:       "role-fit",
		vo.RoundKindTechnical:    "hands-on technical round",
		vo.RoundKindSystemDesign: "architecture",
		vo.RoundKindBehavioral:   "STAR-style",
		vo.RoundKindBarRaiser:    "broader judgment",
	}
	for kind, expectedSubstr := range cases {
		t.Run(string(kind), func(t *testing.T) {
			_, user := generation.BuildPrompt(sampleInput(kind))
			assert.True(t, strings.Contains(user, expectedSubstr),
				"brief for %s must contain %q", kind, expectedSubstr)
		})
	}
}

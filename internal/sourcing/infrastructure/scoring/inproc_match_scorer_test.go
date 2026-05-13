package scoring_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scoring"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func newScorer() *scoring.InProcMatchScorer { return scoring.NewInProcMatchScorer() }

// unitVec returns a 1024-dim unit vector with all components equal (L2-normalised).
func unitVec() []float32 {
	v := make([]float32, 1024)
	val := float32(1.0 / math.Sqrt(1024))
	for i := range v {
		v[i] = val
	}
	return v
}

// orthogonalVec returns a vector orthogonal to unitVec(): alternating +val/-val.
func orthogonalVec() []float32 {
	v := make([]float32, 1024)
	val := float32(1.0 / math.Sqrt(1024))
	for i := range v {
		if i%2 == 0 {
			v[i] = val
		} else {
			v[i] = -val
		}
	}
	return v
}

// nowYYYYMM returns the current date formatted as YYYY-MM.
func nowYYYYMM() string {
	return time.Now().Format("2006-01")
}

// monthsAgo returns a YYYY-MM string n months before now.
func monthsAgo(n int) string {
	return time.Now().AddDate(0, -n, 0).Format("2006-01")
}

// ─── Test 1: All criteria pass — full output ──────────────────────────────────

func TestMatchScorer_AllPass_FullOutput(t *testing.T) {
	// Profile with all required skills, optional skills, experience, location.
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Personal:      vo.ParsedPersonal{Location: "Bangalore, India"},
		Skills: []vo.ParsedSkill{
			{Name: "Go", Years: 5},
			{Name: "Kubernetes", Years: 3},
			{Name: "Terraform", Years: 2},
		},
		Experiences: []vo.ParsedExperience{
			{ID: "exp_0", Company: "Acme", Title: "SRE", Start: monthsAgo(72), End: monthsAgo(12)},
		},
		Languages: []vo.ParsedLanguage{{Name: "English"}},
	}
	role := services.RoleSpec{
		RequiredSkills: []services.SkillSpec{{Name: "Go"}, {Name: "Kubernetes"}},
		OptionalSkills: []services.SkillSpec{{Name: "Terraform"}},
		MinYears:       3,
		Locations:      []string{"Bangalore"},
		WorkMode:       "onsite",
		Languages:      []string{"English"},
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	assert.True(t, out.Rules.PassedRequired(), "all required criteria should pass")
	require.NotNil(t, out.EmbeddingScore, "embedding_score must be set when required criteria pass")
	require.NotNil(t, out.CoarseScore, "coarse_score must be set when required criteria pass")

	// Identical unit vectors → cosine = 1.0 → coarse = 100 + 20 = 120.
	assert.InDelta(t, 1.0, *out.EmbeddingScore, 1e-6)
	assert.InDelta(t, 120.0, *out.CoarseScore, 1e-4)
}

// ─── Test 2: Missing required skill → nil embedding + coarse ─────────────────

func TestMatchScorer_MissingRequiredSkill_Excluded(t *testing.T) {
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Skills:        []vo.ParsedSkill{{Name: "Python"}}, // has Python, not Go
	}
	role := services.RoleSpec{
		RequiredSkills: []services.SkillSpec{{Name: "Go"}},
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	assert.Nil(t, out.EmbeddingScore, "embedding_score must be nil when a required skill is missing")
	assert.Nil(t, out.CoarseScore, "coarse_score must be nil when a required skill is missing")
	require.Len(t, out.Rules.Results, 1)
	assert.False(t, out.Rules.Results[0].Passed)
}

// ─── Test 3: Required pass-rate math — 2/3 required pass ─────────────────────

func TestMatchScorer_RequiredPassRate_TwoOfThree(t *testing.T) {
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Skills: []vo.ParsedSkill{
			{Name: "Go"},
			{Name: "PostgreSQL"},
			// Kubernetes missing
		},
	}
	role := services.RoleSpec{
		RequiredSkills: []services.SkillSpec{
			{Name: "Go"},
			{Name: "Kubernetes"}, // will fail
			{Name: "PostgreSQL"},
		},
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	// 2/3 required pass → required pass-rate < 1.0 → should be excluded.
	assert.Nil(t, out.EmbeddingScore)
	assert.Nil(t, out.CoarseScore)
	assert.InDelta(t, 2.0/3.0, out.Rules.RequiredPassRate(), 1e-6)
}

// ─── Test 4: Coarse score formula verification ────────────────────────────────

func TestMatchScorer_CoarseScoreFormula(t *testing.T) {
	// Set up a profile that passes all required, then use orthogonal vectors so
	// cosine ≈ 0. CoarseScore = 1.0×100 + 0×20 = 100.
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Skills:        []vo.ParsedSkill{{Name: "Go"}, {Name: "SQL"}},
	}
	role := services.RoleSpec{
		RequiredSkills: []services.SkillSpec{{Name: "Go"}, {Name: "SQL"}},
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      orthogonalVec(),
	})

	require.NoError(t, err)
	require.NotNil(t, out.EmbeddingScore)
	require.NotNil(t, out.CoarseScore)

	assert.InDelta(t, 0.0, *out.EmbeddingScore, 1e-6)
	// required_pass_rate=1.0, sim=0 → coarse=100
	assert.InDelta(t, 100.0, *out.CoarseScore, 1e-4)
}

// ─── Test 5: Experience total years correctly summed ──────────────────────────

func TestMatchScorer_ExperienceTotalYears_MultipleRoles(t *testing.T) {
	// Two 2-year stints → 4 years total, which satisfies MinYears=3.
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Experiences: []vo.ParsedExperience{
			{ID: "exp_0", Company: "A", Title: "Eng", Start: "2018-01", End: "2020-01"}, // ~2 years
			{ID: "exp_1", Company: "B", Title: "Eng", Start: "2020-06", End: "2022-06"}, // ~2 years
		},
	}
	role := services.RoleSpec{
		MinYears: 3,
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)

	// Find the experience result.
	var expResult *vo.RuleResult
	for i, r := range out.Rules.Results {
		if r.Criterion.Type == "experience" && r.Criterion.Required {
			expResult = &out.Rules.Results[i]
			break
		}
	}
	require.NotNil(t, expResult, "expected an experience criterion result")
	assert.True(t, expResult.Passed, "4 total years should pass MinYears=3")
}

// ─── Test 6: Experience insufficient ─────────────────────────────────────────

func TestMatchScorer_ExperienceInsufficientYears(t *testing.T) {
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Experiences: []vo.ParsedExperience{
			{ID: "exp_0", Company: "X", Title: "Jr", Start: "2023-01", End: "2024-01"}, // ~1 year
		},
	}
	role := services.RoleSpec{
		MinYears: 5,
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	assert.Nil(t, out.EmbeddingScore, "should be excluded due to insufficient experience")
	var expResult *vo.RuleResult
	for i, r := range out.Rules.Results {
		if r.Criterion.Type == "experience" && r.Criterion.Required {
			expResult = &out.Rules.Results[i]
			break
		}
	}
	require.NotNil(t, expResult)
	assert.False(t, expResult.Passed)
}

// ─── Test 7: current=true → end is treated as now ────────────────────────────

func TestMatchScorer_ExperienceCurrentRole(t *testing.T) {
	// Started 4 years ago, current=true → total ~4 years, which passes MinYears=3.
	start := time.Now().AddDate(-4, 0, 0).Format("2006-01")
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Experiences: []vo.ParsedExperience{
			{ID: "exp_0", Company: "BigCo", Title: "SWE", Start: start, Current: true},
		},
	}
	role := services.RoleSpec{
		MinYears: 3,
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	var expResult *vo.RuleResult
	for i, r := range out.Rules.Results {
		if r.Criterion.Type == "experience" && r.Criterion.Required {
			expResult = &out.Rules.Results[i]
			break
		}
	}
	require.NotNil(t, expResult)
	assert.True(t, expResult.Passed, "current role started 4 years ago should pass MinYears=3")
}

// ─── Test 8: Cosine — identical L2-normalised vectors → 1.0 ──────────────────

func TestMatchScorer_Cosine_IdenticalVectors(t *testing.T) {
	profile := vo.ParsedProfile{SchemaVersion: 1}
	role := services.RoleSpec{} // no criteria → vacuous pass

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	require.NotNil(t, out.EmbeddingScore)
	assert.InDelta(t, 1.0, *out.EmbeddingScore, 1e-6)
}

// ─── Test 9: Cosine — orthogonal vectors → 0.0 ───────────────────────────────

func TestMatchScorer_Cosine_OrthogonalVectors(t *testing.T) {
	profile := vo.ParsedProfile{SchemaVersion: 1}
	role := services.RoleSpec{} // vacuous pass

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      orthogonalVec(),
	})

	require.NoError(t, err)
	require.NotNil(t, out.EmbeddingScore)
	assert.InDelta(t, 0.0, *out.EmbeddingScore, 1e-6)
}

// ─── Test 10: Cosine — empty vectors → 0.0 (no panic) ────────────────────────

func TestMatchScorer_Cosine_EmptyVectors(t *testing.T) {
	profile := vo.ParsedProfile{SchemaVersion: 1}
	role := services.RoleSpec{} // vacuous pass

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: []float32{},
		RoleVec:      []float32{},
	})

	require.NoError(t, err)
	require.NotNil(t, out.EmbeddingScore, "empty vectors should still return an embedding score (0)")
	assert.InDelta(t, 0.0, *out.EmbeddingScore, 1e-6)
}

// ─── Test 11: Location — substring match ─────────────────────────────────────

func TestMatchScorer_Location_SubstringMatch(t *testing.T) {
	// "Bangalore, India" contains "Bangalore"
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Personal:      vo.ParsedPersonal{Location: "Bangalore, India"},
	}
	role := services.RoleSpec{
		Locations: []string{"Bangalore"},
		WorkMode:  "onsite",
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)

	var locResult *vo.RuleResult
	for i, r := range out.Rules.Results {
		if r.Criterion.Type == "location" {
			locResult = &out.Rules.Results[i]
			break
		}
	}
	require.NotNil(t, locResult)
	assert.True(t, locResult.Passed)
}

// ─── Test 12: Location — work_mode=remote bypasses location check ─────────────

func TestMatchScorer_Location_RemoteBypasses(t *testing.T) {
	// Candidate is in "Chennai" but role is remote → no location criterion emitted.
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Personal:      vo.ParsedPersonal{Location: "Chennai"},
	}
	role := services.RoleSpec{
		Locations: []string{"Bangalore", "Mumbai"},
		WorkMode:  "remote",
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	// No location criterion should appear in the report.
	for _, r := range out.Rules.Results {
		assert.NotEqual(t, "location", r.Criterion.Type,
			"location criterion must not be emitted for remote roles")
	}
	// Full output (no exclusion from location).
	require.NotNil(t, out.EmbeddingScore)
	require.NotNil(t, out.CoarseScore)
}

// ─── Test 13: Optional skill missing — doesn't affect RequiredPassRate ────────

func TestMatchScorer_OptionalSkillMissing_DoesNotAffectRequired(t *testing.T) {
	// Has the required skill but not the optional one.
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Skills:        []vo.ParsedSkill{{Name: "Go"}},
	}
	role := services.RoleSpec{
		RequiredSkills: []services.SkillSpec{{Name: "Go"}},
		OptionalSkills: []services.SkillSpec{{Name: "Rust"}}, // missing
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	assert.InDelta(t, 1.0, out.Rules.RequiredPassRate(), 1e-6,
		"missing optional skill must not affect RequiredPassRate")
	assert.True(t, out.Rules.PassedRequired())
	require.NotNil(t, out.EmbeddingScore, "should not be excluded")

	// The optional skill result should be present and failed.
	var optResult *vo.RuleResult
	for i, r := range out.Rules.Results {
		if r.Criterion.Type == "skill" && !r.Criterion.Required {
			optResult = &out.Rules.Results[i]
			break
		}
	}
	require.NotNil(t, optResult)
	assert.False(t, optResult.Passed)
}

// ─── Test 14: Skill min_years threshold — below threshold ────────────────────

func TestMatchScorer_Skill_MinYearsThreshold_Fail(t *testing.T) {
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Skills:        []vo.ParsedSkill{{Name: "Go", Years: 2}},
	}
	role := services.RoleSpec{
		RequiredSkills: []services.SkillSpec{{Name: "Go", MinYears: 5}},
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	// Has "Go" but only 2 years — below 5 required → fails → excluded.
	assert.Nil(t, out.EmbeddingScore, "should be excluded due to insufficient skill years")
	require.Len(t, out.Rules.Results, 1)
	assert.False(t, out.Rules.Results[0].Passed)
}

// ─── Test 15: Language match ──────────────────────────────────────────────────

func TestMatchScorer_Language_Match(t *testing.T) {
	profile := vo.ParsedProfile{
		SchemaVersion: 1,
		Languages:     []vo.ParsedLanguage{{Name: "Hindi", Proficiency: "native"}, {Name: "English", Proficiency: "fluent"}},
	}
	role := services.RoleSpec{
		Languages: []string{"English", "German"}, // has English, not German
	}

	out, err := newScorer().Score(context.Background(), services.MatchInput{
		Profile:      profile,
		Role:         role,
		CandidateVec: unitVec(),
		RoleVec:      unitVec(),
	})

	require.NoError(t, err)
	// Languages are optional — should not cause exclusion.
	require.NotNil(t, out.EmbeddingScore)

	englishPassed, germanPassed := false, false
	for _, r := range out.Rules.Results {
		if r.Criterion.Type == "language" {
			switch r.Criterion.Name {
			case "English":
				englishPassed = r.Passed
			case "German":
				germanPassed = r.Passed
			}
		}
	}
	assert.True(t, englishPassed)
	assert.False(t, germanPassed)
}

package scoring_test

import (
	"strings"
	"testing"

	"github.com/hustle/hireflow/internal/sourcing/domain/services"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
	"github.com/hustle/hireflow/internal/sourcing/infrastructure/scoring"
)

// ─── SerializeProfile ─────────────────────────────────────────────────────────

func TestSerializeProfile_Full(t *testing.T) {
	p := vo.ParsedProfile{
		SchemaVersion: 1,
		Headline:      "Senior Backend Engineer",
		Summary:       "8 years building distributed systems in Go",
		Skills: []vo.ParsedSkill{
			{Name: "Go"},
			{Name: "Kubernetes"},
			{Name: "PostgreSQL"},
		},
		Experiences: []vo.ParsedExperience{
			{
				ID:          "exp_0",
				Company:     "Razorpay",
				Title:       "Staff Engineer",
				Start:       "2020-01",
				End:         "2025-03",
				Description: "Led payments platform migration to microservices",
			},
			{
				ID:          "exp_1",
				Company:     "Flipkart",
				Title:       "Senior Engineer",
				Start:       "2017-06",
				End:         "2019-12",
				Description: "Built order processing pipeline handling 1M orders/day",
			},
		},
	}

	want := "Senior Backend Engineer | 8 years building distributed systems in Go | Skills: Go, Kubernetes, PostgreSQL | Experience: Razorpay Staff Engineer 2020-01-2025-03: Led payments platform migration to microservices | Flipkart Senior Engineer 2017-06-2019-12: Built order processing pipeline handling 1M orders/day"

	got := scoring.SerializeProfile(p)
	if got != want {
		t.Errorf("SerializeProfile full:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeProfile_HeadlineOnly(t *testing.T) {
	p := vo.ParsedProfile{
		SchemaVersion: 1,
		Headline:      "Data Scientist",
	}

	want := "Data Scientist"

	got := scoring.SerializeProfile(p)
	if got != want {
		t.Errorf("SerializeProfile headline only:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeProfile_CurrentExperience(t *testing.T) {
	// current=true must render as "Present" regardless of End value.
	p := vo.ParsedProfile{
		SchemaVersion: 1,
		Headline:      "ML Engineer",
		Experiences: []vo.ParsedExperience{
			{
				ID:      "exp_0",
				Company: "DeepMind",
				Title:   "Research Engineer",
				Start:   "2023-03",
				End:     "",    // empty; Current overrides
				Current: true,
				Description: "Reinforcement learning for protein folding",
			},
		},
	}

	want := "ML Engineer | Experience: DeepMind Research Engineer 2023-03-Present: Reinforcement learning for protein folding"

	got := scoring.SerializeProfile(p)
	if got != want {
		t.Errorf("SerializeProfile current experience:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeProfile_NoSkills(t *testing.T) {
	// When Skills is empty the "Skills:" section should be omitted entirely.
	p := vo.ParsedProfile{
		SchemaVersion: 1,
		Headline:      "Product Manager",
		Summary:       "10 years in B2B SaaS",
		Experiences: []vo.ParsedExperience{
			{
				ID:          "exp_0",
				Company:     "Atlassian",
				Title:       "Senior PM",
				Start:       "2019-01",
				End:         "2024-06",
				Description: "Owned Jira roadmap for enterprise segment",
			},
		},
	}

	got := scoring.SerializeProfile(p)
	if strings.Contains(got, "Skills:") {
		t.Errorf("SerializeProfile no skills: unexpected 'Skills:' in output: %q", got)
	}

	want := "Product Manager | 10 years in B2B SaaS | Experience: Atlassian Senior PM 2019-01-2024-06: Owned Jira roadmap for enterprise segment"
	if got != want {
		t.Errorf("SerializeProfile no skills:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeProfile_NoExperiences(t *testing.T) {
	// No experiences → no "Experience" entries and no pipe after Skills.
	p := vo.ParsedProfile{
		SchemaVersion: 1,
		Headline:      "Fresh Graduate",
		Skills: []vo.ParsedSkill{
			{Name: "Python"},
		},
	}

	want := "Fresh Graduate | Skills: Python"

	got := scoring.SerializeProfile(p)
	if got != want {
		t.Errorf("SerializeProfile no experiences:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeProfile_ExperienceNoDescription(t *testing.T) {
	// Description empty → colon-description suffix must be omitted.
	p := vo.ParsedProfile{
		SchemaVersion: 1,
		Headline:      "DevOps Engineer",
		Experiences: []vo.ParsedExperience{
			{
				ID:      "exp_0",
				Company: "HashiCorp",
				Title:   "SRE",
				Start:   "2021-05",
				End:     "2023-08",
				// Description intentionally empty
			},
		},
	}

	want := "DevOps Engineer | Experience: HashiCorp SRE 2021-05-2023-08"

	got := scoring.SerializeProfile(p)
	if got != want {
		t.Errorf("SerializeProfile experience no description:\ngot:  %q\nwant: %q", got, want)
	}
}

// ─── SerializeRole ────────────────────────────────────────────────────────────

func TestSerializeRole_Full(t *testing.T) {
	r := services.RoleSpec{
		Title: "Senior Go Engineer",
		RequiredSkills: []services.SkillSpec{
			{Name: "Go"},
			{Name: "Kubernetes"},
		},
		OptionalSkills: []services.SkillSpec{
			{Name: "Terraform"},
			{Name: "Prometheus"},
		},
		MinYears:  5,
		MaxYears:  10,
		Locations: []string{"Bangalore", "Mumbai"},
		WorkMode:  "hybrid",
	}

	want := "Senior Go Engineer | Required skills: Go, Kubernetes | Optional skills: Terraform, Prometheus | Experience range: 5-10 years | hybrid role in Bangalore, Mumbai"

	got := scoring.SerializeRole(r)
	if got != want {
		t.Errorf("SerializeRole full:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeRole_NoOptionalSkills(t *testing.T) {
	// OptionalSkills empty → "Optional skills:" section must be omitted.
	r := services.RoleSpec{
		Title: "Backend Engineer",
		RequiredSkills: []services.SkillSpec{
			{Name: "Python"},
			{Name: "PostgreSQL"},
		},
		MinYears:  3,
		MaxYears:  7,
		Locations: []string{"Remote"},
		WorkMode:  "remote",
	}

	got := scoring.SerializeRole(r)
	if strings.Contains(got, "Optional skills:") {
		t.Errorf("SerializeRole no optional skills: unexpected 'Optional skills:' in output: %q", got)
	}

	want := "Backend Engineer | Required skills: Python, PostgreSQL | Experience range: 3-7 years | remote role in Remote"
	if got != want {
		t.Errorf("SerializeRole no optional skills:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeRole_RemoteNoLocations(t *testing.T) {
	// Locations empty + work_mode=remote → just "{work_mode} role" without " in …"
	r := services.RoleSpec{
		Title: "Staff Engineer",
		RequiredSkills: []services.SkillSpec{
			{Name: "Go"},
		},
		MinYears: 8,
		MaxYears: 15,
		WorkMode: "remote",
		// Locations intentionally empty
	}

	want := "Staff Engineer | Required skills: Go | Experience range: 8-15 years | remote role"

	got := scoring.SerializeRole(r)
	if got != want {
		t.Errorf("SerializeRole remote no locations:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSerializeRole_NoExperienceRange(t *testing.T) {
	// MinYears and MaxYears both zero → "Experience range:" omitted.
	r := services.RoleSpec{
		Title: "Junior Developer",
		RequiredSkills: []services.SkillSpec{
			{Name: "JavaScript"},
		},
		Locations: []string{"Delhi"},
		WorkMode:  "onsite",
	}

	got := scoring.SerializeRole(r)
	if strings.Contains(got, "Experience range:") {
		t.Errorf("SerializeRole no experience range: unexpected 'Experience range:' in output: %q", got)
	}

	want := "Junior Developer | Required skills: JavaScript | onsite role in Delhi"
	if got != want {
		t.Errorf("SerializeRole no experience range:\ngot:  %q\nwant: %q", got, want)
	}
}

package valueobjects

import "encoding/json"

// RuleCriterion describes one matching criterion from a RoleSpec.
// Type is one of "skill", "experience", "location", "education", "language".
type RuleCriterion struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

// RuleResult is the outcome of evaluating one RuleCriterion against a candidate profile.
type RuleResult struct {
	Criterion   RuleCriterion `json:"criterion"`
	Passed      bool          `json:"passed"`
	Actual      string        `json:"actual,omitempty"`
	EvidenceRef string        `json:"evidence_ref,omitempty"`
}

// RuleMatchReport is the complete set of per-criterion pass/fail results for one
// (Candidate, Intent) pair. It is stored as JSONB in applications.rule_match.
type RuleMatchReport struct {
	Results []RuleResult `json:"results"`
}

// RequiredPassRate returns the fraction of required criteria that passed.
// Returns 1.0 if there are no required criteria (vacuous truth).
func (r RuleMatchReport) RequiredPassRate() float64 {
	var total, passed int
	for _, res := range r.Results {
		if res.Criterion.Required {
			total++
			if res.Passed {
				passed++
			}
		}
	}
	if total == 0 {
		return 1.0
	}
	return float64(passed) / float64(total)
}

// PassedRequired reports whether all required criteria passed.
// Equivalent to RequiredPassRate() == 1.0.
func (r RuleMatchReport) PassedRequired() bool {
	return r.RequiredPassRate() == 1.0
}

// Marshal serialises the report to JSON for pgx jsonb storage.
func (r RuleMatchReport) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// UnmarshalRuleMatch deserialises a RuleMatchReport from JSON (pgx jsonb retrieval).
func UnmarshalRuleMatch(b []byte) (RuleMatchReport, error) {
	var r RuleMatchReport
	if err := json.Unmarshal(b, &r); err != nil {
		return RuleMatchReport{}, err
	}
	return r, nil
}

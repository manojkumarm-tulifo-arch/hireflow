package valueobjects

import "encoding/json"

// StageArtifacts is the per-stage output bag stored on a ResumeUpload row.
// Slice 1 only persists extracted_text + page_count; slice 2 adds parsed_profile;
// slice 3 adds embedding + match results.
type StageArtifacts struct {
	ExtractedTextValue string `json:"extracted_text,omitempty"`
	PageCount          int    `json:"page_count,omitempty"`
}

// NewStageArtifacts returns a zero-value artifacts bag.
func NewStageArtifacts() StageArtifacts { return StageArtifacts{} }

// SetExtractedText records the output of the Extracting stage.
func (a *StageArtifacts) SetExtractedText(text string, pages int) {
	a.ExtractedTextValue = text
	a.PageCount = pages
}

// ExtractedText returns the text + page count, or ok=false if Extracting hasn't run.
func (a StageArtifacts) ExtractedText() (string, int, bool) {
	if a.ExtractedTextValue == "" {
		return "", 0, false
	}
	return a.ExtractedTextValue, a.PageCount, true
}

// Marshal serializes to JSON for the stage_artifacts jsonb column.
func (a StageArtifacts) Marshal() ([]byte, error) {
	return json.Marshal(a)
}

// UnmarshalStageArtifacts builds a StageArtifacts from a JSON blob.
func UnmarshalStageArtifacts(b []byte) (StageArtifacts, error) {
	if len(b) == 0 {
		return StageArtifacts{}, nil
	}
	var a StageArtifacts
	if err := json.Unmarshal(b, &a); err != nil {
		return StageArtifacts{}, err
	}
	return a, nil
}

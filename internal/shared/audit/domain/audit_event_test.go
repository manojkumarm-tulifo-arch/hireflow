package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/hustle/hireflow/internal/shared/audit/domain"
	shared "github.com/hustle/hireflow/internal/shared/domain"
)

func validEvent() domain.AuditEvent {
	tid, _ := shared.ParseTenantID(uuid.New().String())
	return domain.AuditEvent{
		ActorUserID:  uuid.New(),
		TenantID:     tid,
		Action:       "candidate.viewed",
		ResourceKind: "candidate",
		ResourceID:   uuid.New(),
		OccurredAt:   time.Now(),
	}
}

// --- Validate ---

func TestValidate_HappyPath(t *testing.T) {
	if err := validEvent().Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_MissingAction(t *testing.T) {
	e := validEvent()
	e.Action = ""
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for missing action")
	}
	if err.Error() != "audit: action required" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}

func TestValidate_MissingResourceKind(t *testing.T) {
	e := validEvent()
	e.ResourceKind = ""
	err := e.Validate()
	if err == nil {
		t.Fatal("expected error for missing resource_kind")
	}
	if err.Error() != "audit: resource_kind required" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}

// --- MarshalPayload ---

func TestMarshalPayload_Empty(t *testing.T) {
	e := validEvent()
	e.Payload = nil
	b, err := e.MarshalPayload()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "{}" {
		t.Fatalf("expected `{}`, got %q", string(b))
	}
}

func TestMarshalPayload_NonEmpty(t *testing.T) {
	e := validEvent()
	e.Payload = map[string]any{"key": "value", "count": 3}
	b, err := e.MarshalPayload()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if out["key"] != "value" {
		t.Fatalf("expected key=value, got %v", out["key"])
	}
	// json.Unmarshal decodes numbers as float64
	if out["count"] != float64(3) {
		t.Fatalf("expected count=3, got %v", out["count"])
	}
}

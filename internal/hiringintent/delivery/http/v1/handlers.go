package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/hiringintent/application/commands"
	"github.com/hustle/hireflow/internal/hiringintent/application/queries"
	"github.com/hustle/hireflow/internal/hiringintent/domain/entities"
	"github.com/hustle/hireflow/internal/hiringintent/domain/repositories"
	"github.com/hustle/hireflow/internal/hiringintent/domain/valueobjects"
	shared "github.com/hustle/hireflow/internal/shared/domain"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
)

// IntentHandler holds the application services that back v1 routes.
type IntentHandler struct {
	create  *commands.CreateIntentHandler
	confirm *commands.ConfirmIntentHandler
	get     *queries.GetIntentHandler
	list    *queries.ListIntentsHandler
	summary *queries.IntentSummaryHandler
	logger  zerolog.Logger
}

// NewIntentHandler wires the v1 handler.
func NewIntentHandler(
	create *commands.CreateIntentHandler,
	confirm *commands.ConfirmIntentHandler,
	get *queries.GetIntentHandler,
	list *queries.ListIntentsHandler,
	summary *queries.IntentSummaryHandler,
	logger zerolog.Logger,
) *IntentHandler {
	return &IntentHandler{create: create, confirm: confirm, get: get, list: list, summary: summary, logger: logger}
}

// CreateIntent handles POST /intents.
func (h *IntentHandler) CreateIntent(w http.ResponseWriter, r *http.Request) {
	tenantID, recruiterID, ok := h.requireIdentity(w, r)
	if !ok {
		return
	}

	var req CreateIntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}

	skills := make([]commands.SkillInput, len(req.Skills))
	for i, s := range req.Skills {
		skills[i] = commands.SkillInput{Name: s.Name, Required: s.Required}
	}
	in := commands.CreateIntentInput{
		TenantID:    tenantID,
		RecruiterID: recruiterID,
		RoleTitle:   req.RoleTitle,
		Skills:      skills,
		MinYears:    req.MinYears,
		MaxYears:    req.MaxYears,
		Headcount:   req.Headcount,
		Locations:   req.Locations,
		WorkMode:    req.WorkMode,
		Priority:    req.Priority,
	}
	if req.Budget != nil {
		in.Budget = &commands.BudgetInput{MinMinor: req.Budget.MinMinor, MaxMinor: req.Budget.MaxMinor, Currency: req.Budget.Currency}
	}

	out, err := h.create.Handle(r.Context(), in)
	if err != nil {
		h.respondDomainError(w, err)
		return
	}

	w.Header().Set("Location", "/api/v1/intents/"+out.ID)
	writeJSON(w, http.StatusCreated, Envelope{Success: true, Data: out})
}

// ConfirmIntent handles POST /intents/{id}/confirm.
func (h *IntentHandler) ConfirmIntent(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.requireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	out, err := h.confirm.Handle(r.Context(), commands.ConfirmIntentInput{TenantID: tenantID, IntentID: id})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// GetIntent handles GET /intents/{id}.
func (h *IntentHandler) GetIntent(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.requireIdentity(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	out, err := h.get.Handle(r.Context(), queries.GetIntentInput{TenantID: tenantID, IntentID: id})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// ListIntents handles GET /intents.
func (h *IntentHandler) ListIntents(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.requireIdentity(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	out, err := h.list.Handle(r.Context(), queries.ListIntentsInput{
		TenantID:    tenantID,
		Status:      q.Get("status"),
		RecruiterID: q.Get("recruiter_id"),
		Search:      q.Get("q"),
		SortBy:      q.Get("sort"),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// IntentSummary handles GET /intents/summary.
func (h *IntentHandler) IntentSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.requireIdentity(w, r)
	if !ok {
		return
	}
	out, err := h.summary.Handle(r.Context(), queries.IntentSummaryInput{TenantID: tenantID})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// requireIdentity extracts tenant + recruiter from the request context.
// The JWT middleware (internal/shared/infrastructure/auth) verifies the
// bearer token and attaches an Identity before the handler runs.
func (h *IntentHandler) requireIdentity(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	id, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_identity", "verified identity required")
		return "", "", false
	}
	return id.TenantID.String(), id.RecruiterID.String(), true
}

// respondDomainError maps domain/repository errors to HTTP status codes.
func (h *IntentHandler) respondDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repositories.ErrIntentNotFound):
		writeError(w, http.StatusNotFound, "intent_not_found", "hiring intent not found")
	case errors.Is(err, entities.ErrCannotConfirmNonDrafted),
		errors.Is(err, entities.ErrCannotModifyConfirmed),
		errors.Is(err, entities.ErrCannotCancelTerminal):
		writeError(w, http.StatusConflict, "invalid_state_transition", err.Error())
	case errors.Is(err, entities.ErrCannotConfirmWithoutSkills):
		writeError(w, http.StatusUnprocessableEntity, "invariant_violation", err.Error())
	case errors.Is(err, valueobjects.ErrInvalidIntentID),
		errors.Is(err, shared.ErrInvalidTenantID),
		errors.Is(err, shared.ErrInvalidRecruiterID):
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
	case errors.Is(err, valueobjects.ErrEmptyRoleTitle),
		errors.Is(err, valueobjects.ErrInvalidExperienceRange),
		errors.Is(err, valueobjects.ErrInvalidHeadcount),
		errors.Is(err, valueobjects.ErrInvalidWorkMode),
		errors.Is(err, valueobjects.ErrInvalidPriority),
		errors.Is(err, valueobjects.ErrInvalidIntentStatus),
		errors.Is(err, valueobjects.ErrInvalidSignalLevel),
		errors.Is(err, valueobjects.ErrInvalidBudget):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
	default:
		h.logger.Error().Err(err).Msg("unexpected domain error")
		writeError(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, Envelope{Success: false, Error: &ErrorInfo{Code: code, Message: message}})
}

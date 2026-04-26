package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/jobposting/application/commands"
	"github.com/hustle/hireflow/internal/jobposting/application/queries"
	"github.com/hustle/hireflow/internal/jobposting/domain/entities"
	"github.com/hustle/hireflow/internal/jobposting/domain/repositories"
	"github.com/hustle/hireflow/internal/jobposting/domain/valueobjects"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
)

// PostingHandler holds the application services that back v1 routes.
type PostingHandler struct {
	publish *commands.PublishPostingHandler
	close   *commands.ClosePostingHandler
	get     *queries.GetPostingHandler
	list    *queries.ListPostingsHandler
	logger  zerolog.Logger
}

// NewPostingHandler wires the v1 handler.
func NewPostingHandler(
	publish *commands.PublishPostingHandler,
	close *commands.ClosePostingHandler,
	get *queries.GetPostingHandler,
	list *queries.ListPostingsHandler,
	logger zerolog.Logger,
) *PostingHandler {
	return &PostingHandler{publish: publish, close: close, get: get, list: list, logger: logger}
}

// PublishPosting handles POST /postings/{id}/publish.
func (h *PostingHandler) PublishPosting(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	var req PublishPostingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	out, err := h.publish.Handle(r.Context(), commands.PublishPostingInput{
		TenantID: tenantID, PostingID: id, Channels: req.Channels,
	})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// ClosePosting handles POST /postings/{id}/close.
func (h *PostingHandler) ClosePosting(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	var req ClosePostingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON")
		return
	}
	out, err := h.close.Handle(r.Context(), commands.ClosePostingInput{
		TenantID: tenantID, PostingID: id, Reason: req.Reason,
	})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// GetPosting handles GET /postings/{id}.
func (h *PostingHandler) GetPosting(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	out, err := h.get.Handle(r.Context(), queries.GetPostingInput{TenantID: tenantID, PostingID: id})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

// ListPostings handles GET /postings.
func (h *PostingHandler) ListPostings(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	out, err := h.list.Handle(r.Context(), queries.ListPostingsInput{
		TenantID: tenantID,
		Status:   q.Get("status"),
		IntentID: q.Get("intent_id"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		h.respondDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Envelope{Success: true, Data: out})
}

func (h *PostingHandler) requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_identity", "verified identity required")
		return "", false
	}
	return id.TenantID.String(), true
}

func (h *PostingHandler) respondDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repositories.ErrPostingNotFound):
		writeError(w, http.StatusNotFound, "posting_not_found", "job posting not found")
	case errors.Is(err, entities.ErrCannotPublishTerminal),
		errors.Is(err, entities.ErrCannotCloseTerminal),
		errors.Is(err, entities.ErrCannotAmendTerminal):
		writeError(w, http.StatusConflict, "invalid_state_transition", err.Error())
	case errors.Is(err, entities.ErrPublishNeedsChannels):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
	case errors.Is(err, valueobjects.ErrInvalidPostingID),
		errors.Is(err, valueobjects.ErrInvalidPostingStatus),
		errors.Is(err, valueobjects.ErrInvalidSourceChannel):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
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

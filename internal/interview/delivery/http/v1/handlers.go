package v1

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/interview/application/commands"
	"github.com/hustle/hireflow/internal/interview/application/queries"
	"github.com/hustle/hireflow/internal/interview/domain/entities"
	"github.com/hustle/hireflow/internal/interview/domain/repositories"
	vo "github.com/hustle/hireflow/internal/interview/domain/valueobjects"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
)

// InterviewHandlerDeps bundles all dependencies for InterviewHandler.
type InterviewHandlerDeps struct {
	UpsertTemplate           *commands.UpsertLoopTemplateHandler
	RecordFeedback           *commands.RecordFeedbackHandler
	MarkRoundCompleted       *commands.MarkRoundCompletedHandler
	MarkRoundSkipped         *commands.MarkRoundSkippedHandler
	CompleteProcess          *commands.CompleteProcessHandler
	CancelProcess            *commands.CancelProcessHandler
	RegenerateRoundQuestions *commands.RegenerateRoundQuestionsHandler
	GetInterviewProcess      *queries.GetInterviewProcessHandler
	ListInterviewProcesses   *queries.ListInterviewProcessesHandler
	GetLoopTemplate          *queries.GetLoopTemplateHandler
	Logger                   zerolog.Logger
}

// InterviewHandler is the v1 HTTP entry point of the interview context.
type InterviewHandler struct {
	deps InterviewHandlerDeps
}

// NewInterviewHandler wires the handler.
func NewInterviewHandler(deps InterviewHandlerDeps) *InterviewHandler {
	return &InterviewHandler{deps: deps}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, ErrorResponse{Code: code, Message: msg})
}

// --- handlers ---

// UpsertLoopTemplate handles PUT /intents/{intent_id}/loop-template.
func (h *InterviewHandler) UpsertLoopTemplate(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	intentID, err := uuid.Parse(chi.URLParam(r, "intent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent_id", "intent_id must be a uuid")
		return
	}
	var body UpsertLoopTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	rounds := make([]entities.TemplateRound, 0, len(body.Rounds))
	for _, br := range body.Rounds {
		kind, err := vo.ParseRoundKind(br.Kind)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_round_kind", br.Kind)
			return
		}
		rounds = append(rounds, entities.TemplateRound{Kind: kind, Sequence: br.Sequence})
	}
	if err := h.deps.UpsertTemplate.Handle(r.Context(), commands.UpsertLoopTemplateInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		IntentID:    intentID,
		Rounds:      rounds,
	}); err != nil {
		h.deps.Logger.Error().Err(err).Msg("upsert template failed")
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetLoopTemplate handles GET /intents/{intent_id}/loop-template.
func (h *InterviewHandler) GetLoopTemplate(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	intentID, err := uuid.Parse(chi.URLParam(r, "intent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent_id", "intent_id must be a uuid")
		return
	}
	out, err := h.deps.GetLoopTemplate.Handle(r.Context(), identity.TenantID, intentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp := LoopTemplateResponse{
		IntentID:  intentID.String(),
		IsDefault: out.IsDefault,
	}
	for _, rd := range out.Rounds {
		resp.Rounds = append(resp.Rounds, TemplateRoundResponse{Kind: rd.Kind, Sequence: rd.Sequence})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListInterviewProcesses handles GET /intents/{intent_id}/interview-processes.
func (h *InterviewHandler) ListInterviewProcesses(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	intentID, err := uuid.Parse(chi.URLParam(r, "intent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_intent_id", "intent_id must be a uuid")
		return
	}
	status := r.URL.Query().Get("status")
	out, err := h.deps.ListInterviewProcesses.Handle(r.Context(), queries.ListInput{
		TenantID: identity.TenantID,
		IntentID: intentID,
		Status:   status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp := ListProcessesResponse{}
	for _, p := range out {
		resp.Processes = append(resp.Processes, InterviewProcessResponse{
			ID:            p.ID.String(),
			ApplicationID: p.ApplicationID.String(),
			CandidateID:   p.CandidateID.String(),
			IntentID:      p.IntentID.String(),
			Status:        p.Status,
			CreatedAt:     p.CreatedAt,
			UpdatedAt:     p.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetInterviewProcess handles GET /interview/processes/{process_id}.
func (h *InterviewHandler) GetInterviewProcess(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	processID, err := uuid.Parse(chi.URLParam(r, "process_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_process_id", "process_id must be a uuid")
		return
	}
	out, err := h.deps.GetInterviewProcess.Handle(r.Context(), identity.TenantID, identity.RecruiterID.UUID(), processID)
	if err != nil {
		if errors.Is(err, repositories.ErrProcessNotFound) {
			writeError(w, http.StatusNotFound, "process_not_found", "process not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp := InterviewProcessResponse{
		ID:            out.ID.String(),
		ApplicationID: out.ApplicationID.String(),
		CandidateID:   out.CandidateID.String(),
		IntentID:      out.IntentID.String(),
		Status:        out.Status,
		CreatedAt:     out.CreatedAt,
		UpdatedAt:     out.UpdatedAt,
	}
	for _, rd := range out.Rounds {
		resp.Rounds = append(resp.Rounds, InterviewRoundResponse{
			ID: rd.ID.String(), Kind: rd.Kind, Sequence: rd.Sequence, Status: rd.Status,
			Questions: rd.Questions, AttemptCount: rd.AttemptCount, LastError: rd.LastError,
			FeedbackSummary: FeedbackSummaryResponse{
				StrongYes: rd.FeedbackSummary.StrongYes, Yes: rd.FeedbackSummary.Yes,
				Mixed: rd.FeedbackSummary.Mixed, No: rd.FeedbackSummary.No,
				StrongNo: rd.FeedbackSummary.StrongNo, Total: rd.FeedbackSummary.Total,
				LatestDecision: rd.FeedbackSummary.LatestDecision,
			},
			CreatedAt: rd.CreatedAt, UpdatedAt: rd.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// CompleteProcess handles POST /interview/processes/{process_id}:complete.
func (h *InterviewHandler) CompleteProcess(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	processID, err := uuid.Parse(chi.URLParam(r, "process_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_process_id", "process_id must be a uuid")
		return
	}
	if err := h.deps.CompleteProcess.Handle(r.Context(), commands.CompleteProcessInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		ProcessID:   processID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "process_not_found", "process not found")
		case errors.Is(err, commands.ErrProcessInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CancelProcess handles POST /interview/processes/{process_id}:cancel.
func (h *InterviewHandler) CancelProcess(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	processID, err := uuid.Parse(chi.URLParam(r, "process_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_process_id", "process_id must be a uuid")
		return
	}
	if err := h.deps.CancelProcess.Handle(r.Context(), commands.CancelProcessInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		ProcessID:   processID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "process_not_found", "process not found")
		case errors.Is(err, commands.ErrProcessInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RecordFeedback handles POST /interview/rounds/{round_id}/feedback.
func (h *InterviewHandler) RecordFeedback(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	var body RecordFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	decision, err := vo.ParseFeedbackDecision(body.Decision)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_decision", body.Decision)
		return
	}
	fb := vo.Feedback{
		InterviewerName:  body.InterviewerName,
		InterviewerEmail: body.InterviewerEmail,
		Decision:         decision,
		Notes:            body.Notes,
		SubmittedBy:      identity.RecruiterID.UUID(),
	}
	if err := fb.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_feedback", err.Error())
		return
	}
	if err := h.deps.RecordFeedback.Handle(r.Context(), commands.RecordFeedbackInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		RoundID:     roundID,
		Feedback:    fb,
	}); err != nil {
		if errors.Is(err, repositories.ErrProcessNotFound) {
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// RegenerateRoundQuestions handles POST /interview/rounds/{round_id}:regenerate.
func (h *InterviewHandler) RegenerateRoundQuestions(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	var body RegenerateRoundRequest
	// Body is optional — ignore decode errors when body is empty.
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := h.deps.RegenerateRoundQuestions.Handle(r.Context(), commands.RegenerateRoundQuestionsInput{
		TenantID: identity.TenantID,
		RoundID:  roundID,
		Steering: body.Steering,
	}); err != nil {
		switch {
		case errors.Is(err, entities.ErrRoundNotFound):
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
		case errors.Is(err, commands.ErrRoundNotRegenerable):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// MarkRoundCompleted handles POST /interview/rounds/{round_id}:mark-done.
func (h *InterviewHandler) MarkRoundCompleted(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	if err := h.deps.MarkRoundCompleted.Handle(r.Context(), commands.MarkRoundCompletedInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		RoundID:     roundID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
		case errors.Is(err, commands.ErrRoundInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkRoundSkipped handles POST /interview/rounds/{round_id}:skip.
func (h *InterviewHandler) MarkRoundSkipped(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	roundID, err := uuid.Parse(chi.URLParam(r, "round_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_round_id", "round_id must be a uuid")
		return
	}
	if err := h.deps.MarkRoundSkipped.Handle(r.Context(), commands.MarkRoundSkippedInput{
		TenantID:    identity.TenantID,
		ActorUserID: identity.RecruiterID.UUID(),
		RoundID:     roundID,
	}); err != nil {
		switch {
		case errors.Is(err, repositories.ErrProcessNotFound):
			writeError(w, http.StatusNotFound, "round_not_found", "round not found")
		case errors.Is(err, commands.ErrRoundInvalidTransition):
			writeError(w, http.StatusConflict, "invalid_transition", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

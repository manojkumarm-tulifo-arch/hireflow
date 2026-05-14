package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
	"github.com/hustle/hireflow/internal/sourcing/domain/entities"
	"github.com/hustle/hireflow/internal/sourcing/domain/repositories"
	vo "github.com/hustle/hireflow/internal/sourcing/domain/valueobjects"
)

// SourcingHandler exposes the v1 endpoints of the sourcing context.
type SourcingHandler struct {
	upload           *commands.UploadResumeBatchHandler
	status           *queries.GetBatchStatusHandler
	candidate        *queries.GetCandidateHandler
	listApplications *queries.ListApplicationsHandler
	transition       *commands.TransitionApplicationHandler
	logger           zerolog.Logger
}

// NewSourcingHandler wires the handler.
func NewSourcingHandler(
	upload *commands.UploadResumeBatchHandler,
	status *queries.GetBatchStatusHandler,
	candidate *queries.GetCandidateHandler,
	listApplications *queries.ListApplicationsHandler,
	transition *commands.TransitionApplicationHandler,
	logger zerolog.Logger,
) *SourcingHandler {
	return &SourcingHandler{
		upload:           upload,
		status:           status,
		candidate:        candidate,
		listApplications: listApplications,
		transition:       transition,
		logger:           logger,
	}
}

// BatchUpload handles POST /intents/{intent_id}/resumes:batch.
func (h *SourcingHandler) BatchUpload(w http.ResponseWriter, r *http.Request) {
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

	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", err.Error())
		return
	}

	src := &multipartSource{r: mr}
	out, err := h.upload.Handle(r.Context(), dto.BatchUploadInput{
		TenantID: identity.TenantID, // already shared.TenantID — no cast needed
		IntentID: intentID,
		Source:   src,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("batch upload failed")
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if len(out.Items) == 0 {
		writeError(w, http.StatusBadRequest, "no_files", "request had zero file parts named 'resume'")
		return
	}

	resp := BatchUploadResponse{BatchID: out.BatchID.String()}
	for _, it := range out.Items {
		item := BatchItemResponse{Filename: it.Filename}
		if it.UploadID != nil {
			item.UploadID = it.UploadID.String()
		}
		if it.CandidateID != nil {
			item.CandidateID = it.CandidateID.String()
		}
		item.Status = it.Status
		if it.Error != nil {
			item.Error = &BatchItemError{
				Code:    it.Error.Code,
				Message: it.Error.Message,
				Detail:  it.Error.Detail,
			}
		}
		resp.Items = append(resp.Items, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetBatchStatus handles GET /resumes/batches/{batch_id}.
func (h *SourcingHandler) GetBatchStatus(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	batchID, err := uuid.Parse(chi.URLParam(r, "batch_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_batch_id", "batch_id must be a uuid")
		return
	}

	out, err := h.status.Handle(r.Context(), identity.TenantID, batchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp := BatchStatusResponse{
		BatchID:  out.BatchID.String(),
		IntentID: out.IntentID.String(),
		Summary: BatchStatusSummary{
			Total:       out.Summary.Total,
			InFlight:    out.Summary.InFlight,
			Extracted:   out.Summary.Extracted,
			Failed:      out.Summary.Failed,
			Quarantined: out.Summary.Quarantined,
		},
	}
	for _, it := range out.Items {
		resp.Items = append(resp.Items, BatchStatusItem{
			UploadID:  it.UploadID.String(),
			Filename:  it.Filename,
			Status:    it.Status,
			Attempt:   it.Attempt,
			LastError: it.LastError,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetCandidate handles GET /candidates/{candidate_id}.
func (h *SourcingHandler) GetCandidate(w http.ResponseWriter, r *http.Request) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	candidateID, err := uuid.Parse(chi.URLParam(r, "candidate_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_candidate_id", "candidate_id must be a uuid")
		return
	}
	if h.candidate == nil {
		writeError(w, http.StatusServiceUnavailable, "not_wired", "candidate handler not configured")
		return
	}

	out, err := h.candidate.Handle(r.Context(), identity.TenantID, candidateID)
	if err != nil {
		if errors.Is(err, repositories.ErrCandidateNotFound) {
			writeError(w, http.StatusNotFound, "candidate_not_found", "candidate not found")
			return
		}
		h.logger.Error().Err(err).Msg("get candidate failed")
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	resp := CandidateDetailResponse{
		ID:          out.ID.String(),
		ContentHash: out.ContentHash,
		Personal: CandidatePersonal{
			FullName: out.Personal.FullName,
			Email:    out.Personal.Email,
			Phone:    out.Personal.Phone,
		},
		Location:  out.Location,
		Headline:  out.Headline,
		Profile:   out.Profile,
		Source:    out.Source,
		CreatedAt: out.CreatedAt.Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListApplications handles GET /intents/{intent_id}/applications.
func (h *SourcingHandler) ListApplications(w http.ResponseWriter, r *http.Request) {
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
	if h.listApplications == nil {
		writeError(w, http.StatusServiceUnavailable, "not_wired", "listApplications handler not configured")
		return
	}

	q := r.URL.Query()

	// Parse optional status filter.
	var statusFilter *vo.ApplicationStatus
	if s := q.Get("status"); s != "" {
		parsed := vo.ApplicationStatus(s)
		statusFilter = &parsed
	}

	// Parse optional min_score filter.
	var minScore *float64
	if ms := q.Get("min_score"); ms != "" {
		f, err := strconv.ParseFloat(ms, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_min_score", "min_score must be a number")
			return
		}
		minScore = &f
	}

	// Parse sort (default: score_desc).
	sort := q.Get("sort")
	if sort == "" {
		sort = "score_desc"
	}

	// Parse limit (default 50, cap 200).
	limit := 50
	if ls := q.Get("limit"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	// Parse offset (default 0).
	offset := 0
	if os := q.Get("offset"); os != "" {
		n, err := strconv.Atoi(os)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_offset", "offset must be a non-negative integer")
			return
		}
		offset = n
	}

	out, err := h.listApplications.Handle(r.Context(), queries.ListApplicationsInput{
		TenantID: identity.TenantID,
		IntentID: intentID,
		Filter: repositories.ApplicationListFilter{
			Status:   statusFilter,
			MinScore: minScore,
			Sort:     sort,
			Limit:    limit,
			Offset:   offset,
		},
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("list applications failed")
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	resp := ApplicationListResponse{
		Total: out.Total,
		Facets: ApplicationListFacets{
			Strong:   out.Facets.Strong,
			Moderate: out.Facets.Moderate,
			Weak:     out.Facets.Weak,
		},
	}
	for _, it := range out.Items {
		item := ApplicationListItem{
			ApplicationID: it.ApplicationID.String(),
			Candidate: ApplicationCandidate{
				ID:             it.CandidateID.String(),
				FullNameMasked: it.CandidateName,
				Headline:       it.Headline,
				Location:       it.Location,
			},
			Score: ApplicationScore{
				Overall:        it.OverallScore,
				Band:           it.ScoreBand,
				EmbeddingScore: it.EmbeddingScore,
				RuleMatch:      it.RuleMatch,
				LLM:            it.LLMJudgment,
			},
			Status: it.Status,
		}
		if it.ScoredAt != nil {
			item.ScoredAt = it.ScoredAt.Format(time.RFC3339)
		}
		resp.Items = append(resp.Items, item)
	}
	if resp.Items == nil {
		resp.Items = []ApplicationListItem{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ShortlistApplication handles POST /applications/{application_id}:shortlist.
func (h *SourcingHandler) ShortlistApplication(w http.ResponseWriter, r *http.Request) {
	h.transitionApplication(w, r, commands.ActionShortlist, "")
}

// RejectApplication handles POST /applications/{application_id}:reject.
func (h *SourcingHandler) RejectApplication(w http.ResponseWriter, r *http.Request) {
	var body ApplicationRejectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason_required", "reason is required")
		return
	}
	h.transitionApplication(w, r, commands.ActionReject, body.Reason)
}

// HireApplication handles POST /applications/{application_id}:hire.
func (h *SourcingHandler) HireApplication(w http.ResponseWriter, r *http.Request) {
	h.transitionApplication(w, r, commands.ActionHire, "")
}

// transitionApplication is the shared implementation for shortlist/reject/hire.
func (h *SourcingHandler) transitionApplication(
	w http.ResponseWriter,
	r *http.Request,
	action commands.ApplicationAction,
	rejectReason string,
) {
	identity, err := auth.IdentityFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing identity")
		return
	}
	applicationID, err := uuid.Parse(chi.URLParam(r, "application_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_application_id", "application_id must be a uuid")
		return
	}

	handleErr := h.transition.Handle(r.Context(), commands.TransitionApplicationInput{
		TenantID:      identity.TenantID,
		ActorUserID:   identity.RecruiterID.UUID(),
		ApplicationID: applicationID,
		Action:        action,
		RejectReason:  rejectReason,
	})
	if handleErr != nil {
		if errors.Is(handleErr, repositories.ErrApplicationNotFound) {
			writeError(w, http.StatusNotFound, "application_not_found", "application not found")
			return
		}
		if errors.Is(handleErr, entities.ErrInvalidTransition) {
			writeError(w, http.StatusBadRequest, "invalid_transition", "transition not permitted for current status")
			return
		}
		// "reject: reason required" is already guarded above, but handle defensively.
		h.logger.Error().Err(handleErr).Str("action", string(action)).Msg("application transition failed")
		writeError(w, http.StatusInternalServerError, "internal_error", handleErr.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// multipartSource adapts multipart.Reader to dto.BatchItemSource.
// Each Next() call advances to the next part and yields its body as an
// io.Reader. The command consumes the body before calling Next again.
type multipartSource struct {
	r *multipart.Reader
}

func (s *multipartSource) Next() (dto.BatchItem, error) {
	for {
		p, err := s.r.NextPart()
		if errors.Is(err, io.EOF) {
			return dto.BatchItem{}, io.EOF
		}
		if err != nil {
			return dto.BatchItem{}, fmt.Errorf("next part: %w", err)
		}
		// Skip non-file parts and parts not named "resume".
		if p.FormName() != "resume" || p.FileName() == "" {
			_, _ = io.Copy(io.Discard, p)
			p.Close()
			continue
		}
		return dto.BatchItem{Filename: p.FileName(), Body: p}, nil
	}
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a standard error body.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Code: code, Message: msg})
}

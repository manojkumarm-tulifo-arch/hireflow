package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/sourcing/application/commands"
	"github.com/hustle/hireflow/internal/sourcing/application/dto"
	"github.com/hustle/hireflow/internal/sourcing/application/queries"
)

// SourcingHandler exposes the v1 endpoints of the sourcing context.
type SourcingHandler struct {
	upload *commands.UploadResumeBatchHandler
	status *queries.GetBatchStatusHandler
	logger zerolog.Logger
}

// NewSourcingHandler wires the handler.
func NewSourcingHandler(upload *commands.UploadResumeBatchHandler, status *queries.GetBatchStatusHandler, logger zerolog.Logger) *SourcingHandler {
	return &SourcingHandler{upload: upload, status: status, logger: logger}
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

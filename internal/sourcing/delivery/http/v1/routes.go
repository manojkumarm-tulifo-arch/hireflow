package v1

import "github.com/go-chi/chi/v5"

// Mount registers v1 sourcing routes onto the given router. Note the path
// for batch upload uses a `:batch` action suffix; chi's URL parser supports
// it via plain path matching.
func Mount(r chi.Router, h *SourcingHandler) {
	r.Post("/intents/{intent_id}/resumes:batch", h.BatchUpload)
	r.Get("/resumes/batches/{batch_id}", h.GetBatchStatus)
	r.Get("/resumes/batches/{batch_id}/events", h.BatchEvents)
	r.Get("/candidates/{candidate_id}", h.GetCandidate)
	r.Delete("/candidates/{candidate_id}", h.EraseCandidate)
	r.Get("/intents/{intent_id}/applications", h.ListApplications)
	r.Post("/intents/{intent_id}/applications:rescore", h.RescoreIntent)
	r.Post("/applications/{application_id}:shortlist", h.ShortlistApplication)
	r.Post("/applications/{application_id}:reject", h.RejectApplication)
	r.Post("/applications/{application_id}:hire", h.HireApplication)
	r.Post("/resumes/{upload_id}:retry", h.RetryUpload)
}

package v1

import "github.com/go-chi/chi/v5"

// Mount registers the interview context's v1 routes onto the given router.
func Mount(r chi.Router, h *InterviewHandler) {
	r.Put("/intents/{intent_id}/loop-template", h.UpsertLoopTemplate)
	r.Get("/intents/{intent_id}/loop-template", h.GetLoopTemplate)
	r.Get("/intents/{intent_id}/interview-processes", h.ListInterviewProcesses)
	r.Get("/interview/processes/{process_id}", h.GetInterviewProcess)
	r.Post("/interview/processes/{process_id}:complete", h.CompleteProcess)
	r.Post("/interview/processes/{process_id}:cancel", h.CancelProcess)
	r.Post("/interview/rounds/{round_id}/feedback", h.RecordFeedback)
	r.Post("/interview/rounds/{round_id}:regenerate", h.RegenerateRoundQuestions)
	r.Post("/interview/rounds/{round_id}:mark-done", h.MarkRoundCompleted)
	r.Post("/interview/rounds/{round_id}:skip", h.MarkRoundSkipped)
}

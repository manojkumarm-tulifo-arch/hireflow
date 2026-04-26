package v1

import "github.com/go-chi/chi/v5"

// Mount registers v1 jobposting routes onto the given router.
// Postings cannot be created directly via HTTP — they are produced by the
// IntentConfirmed event consumer. Recruiters interact via publish/close/list/get.
func Mount(r chi.Router, h *PostingHandler) {
	r.Route("/postings", func(r chi.Router) {
		r.Get("/", h.ListPostings)
		r.Get("/{id}", h.GetPosting)
		r.Post("/{id}/publish", h.PublishPosting)
		r.Post("/{id}/close", h.ClosePosting)
	})
}

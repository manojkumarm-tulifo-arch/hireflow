package v1

import "github.com/go-chi/chi/v5"

// Mount registers v1 hiringintent routes onto the given router.
func Mount(r chi.Router, h *IntentHandler) {
	r.Route("/intents", func(r chi.Router) {
		r.Get("/", h.ListIntents)
		r.Get("/summary", h.IntentSummary)
		r.Post("/", h.CreateIntent)
		r.Get("/{id}", h.GetIntent)
		r.Post("/{id}/confirm", h.ConfirmIntent)
	})
}

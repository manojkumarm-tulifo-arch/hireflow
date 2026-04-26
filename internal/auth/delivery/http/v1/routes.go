package v1

import "github.com/go-chi/chi/v5"

// Mount registers v1 auth routes onto the given router.
// All routes are unauthenticated — JWT middleware must NOT wrap this router.
func Mount(r chi.Router, h *AuthHandler) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/signup/request-otp", h.SignupRequestOTP)
		r.Post("/signup/verify-otp", h.SignupVerifyOTP)
		r.Post("/signin/request-otp", h.SigninRequestOTP)
		r.Post("/signin/verify-otp", h.SigninVerifyOTP)
		r.Post("/refresh", h.Refresh)
		r.Post("/logout", h.Logout)
	})
}

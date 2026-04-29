// Package main is the hireflow API entry point.
// 12-factor: all configuration via environment variables.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	authcommands "github.com/hustle/hireflow/internal/auth/application/commands"
	authhttp "github.com/hustle/hireflow/internal/auth/delivery/http/v1"
	authcrypto "github.com/hustle/hireflow/internal/auth/infrastructure/crypto"
	authnotif "github.com/hustle/hireflow/internal/auth/infrastructure/notifications"
	authpersist "github.com/hustle/hireflow/internal/auth/infrastructure/persistence"
	authtokens "github.com/hustle/hireflow/internal/auth/infrastructure/tokens"
	intentevents "github.com/hustle/hireflow/internal/hiringintent/domain/events"

	intentcommands "github.com/hustle/hireflow/internal/hiringintent/application/commands"
	intentqueries "github.com/hustle/hireflow/internal/hiringintent/application/queries"
	intenthttp "github.com/hustle/hireflow/internal/hiringintent/delivery/http/v1"
	intentllm "github.com/hustle/hireflow/internal/hiringintent/infrastructure/llm"
	intentmsg "github.com/hustle/hireflow/internal/hiringintent/infrastructure/messaging"
	intentpersist "github.com/hustle/hireflow/internal/hiringintent/infrastructure/persistence"
	sharedanthropic "github.com/hustle/hireflow/internal/shared/infrastructure/llm/anthropic"
	postingcommands "github.com/hustle/hireflow/internal/jobposting/application/commands"
	postingqueries "github.com/hustle/hireflow/internal/jobposting/application/queries"
	postinghttp "github.com/hustle/hireflow/internal/jobposting/delivery/http/v1"
	postingclients "github.com/hustle/hireflow/internal/jobposting/infrastructure/clients"
	postingmsg "github.com/hustle/hireflow/internal/jobposting/infrastructure/messaging"
	postingpersist "github.com/hustle/hireflow/internal/jobposting/infrastructure/persistence"
	postingsubs "github.com/hustle/hireflow/internal/jobposting/infrastructure/subscribers"
	"github.com/hustle/hireflow/internal/shared/infrastructure/auth"
	"github.com/hustle/hireflow/internal/shared/infrastructure/eventbus"
)

func main() {
	logger := newLogger()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Fatal().Msg("DATABASE_URL is required")
	}
	jwtSecret := os.Getenv("JWT_ACCESS_SECRET")
	if jwtSecret == "" {
		logger.Fatal().Msg("JWT_ACCESS_SECRET is required")
	}
	jwtIssuer := getenv("JWT_ISSUER", "hireflow")
	port := getenv("PORT", "8080")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("connect postgres")
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Fatal().Err(err).Msg("ping postgres")
	}

	// Process-local event bus. Both context dispatchers publish into it;
	// cross-context subscribers register against it. Replaces the LogPublisher
	// stand-ins so IntentConfirmed → CreateFromIntent actually fires.
	bus := eventbus.NewInMemory(logger)

	// Anthropic-backed intent extractor. Fatal at startup if the API key is
	// missing so misconfiguration surfaces immediately rather than 500ing on
	// the first chat turn.
	anthropicCfg, err := sharedanthropic.LoadConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("anthropic config")
	}
	anthropicClient := sharedanthropic.NewClient(anthropicCfg)
	intentExtractor := intentllm.NewAnthropicExtractor(anthropicClient)

	// Wire hiringintent context.
	intentRepo := intentpersist.NewPostgresIntentRepository(pool)
	intentGetHandler := intentqueries.NewGetIntentHandler(intentRepo)
	intentPub := intentmsg.NewBusPublisher(bus)
	intentDispatcher := intentmsg.NewOutboxDispatcher(pool, intentPub, logger, intentmsg.DispatcherConfig{})

	intentHandler := intenthttp.NewIntentHandler(
		intentcommands.NewCreateIntentHandler(intentRepo),
		intentcommands.NewConfirmIntentHandler(intentRepo),
		intentcommands.NewExtractIntentHandler(intentExtractor),
		intentGetHandler,
		intentqueries.NewListIntentsHandler(intentRepo),
		intentqueries.NewIntentSummaryHandler(intentRepo),
		logger,
	)

	// Wire jobposting context.
	postingRepo := postingpersist.NewPostgresPostingRepository(pool)
	postingPub := postingmsg.NewBusPublisher(bus)
	postingDispatcher := postingmsg.NewOutboxDispatcher(pool, postingPub, logger, postingmsg.DispatcherConfig{})
	postingHandler := postinghttp.NewPostingHandler(
		postingcommands.NewPublishPostingHandler(postingRepo),
		postingcommands.NewClosePostingHandler(postingRepo),
		postingqueries.NewGetPostingHandler(postingRepo),
		postingqueries.NewListPostingsHandler(postingRepo),
		logger,
	)

	// Cross-context bridge: jobposting reacts to IntentConfirmed by drafting
	// a posting. The IntentReader projects the upstream IntentDTO through
	// an anti-corruption layer so jobposting depends on its own port type.
	intentReader := postingclients.NewIntentReader(intentGetHandler)
	createFromIntentHandler := postingcommands.NewCreateFromIntentHandler(postingRepo)
	intentConfirmedConsumer := postingsubs.NewIntentConfirmedConsumer(intentReader, createFromIntentHandler)
	bus.Subscribe("hiringintent.IntentConfirmed", func(ctx context.Context, ev any) error {
		typed, ok := ev.(intentevents.IntentConfirmed)
		if !ok {
			return fmt.Errorf("intent confirmed handler: unexpected event type %T", ev)
		}
		return intentConfirmedConsumer.Consume(ctx, typed)
	})

	// Wire auth context.
	userRepo := authpersist.NewPostgresUserRepository(pool)
	tenantRepo := authpersist.NewPostgresTenantRepository(pool)
	otpRepo := authpersist.NewPostgresOTPSessionRepository(pool)
	refreshRepo := authpersist.NewPostgresRefreshTokenRepository(pool)
	otpGen := authcrypto.NewSecureOTPGenerator()
	otpHasher := authcrypto.NewArgon2OTPHasher()
	refreshGen := authcrypto.NewRefreshTokenSecretGenerator()
	otpSender := authnotif.NewLogOTPSender(logger)
	jwtIssuerSvc, err := authtokens.NewJWTIssuer([]byte(jwtSecret), jwtIssuer)
	if err != nil {
		logger.Fatal().Err(err).Msg("init jwt issuer")
	}
	authHandler := authhttp.NewAuthHandler(
		authcommands.NewSignupRequestOTPHandler(userRepo, tenantRepo, otpRepo, otpGen, otpHasher, otpSender),
		authcommands.NewSignupVerifyOTPHandler(userRepo, otpRepo, refreshRepo, otpHasher, jwtIssuerSvc, refreshGen),
		authcommands.NewSigninRequestOTPHandler(userRepo, otpRepo, otpGen, otpHasher, otpSender),
		authcommands.NewSigninVerifyOTPHandler(userRepo, otpRepo, refreshRepo, otpHasher, jwtIssuerSvc, refreshGen),
		authcommands.NewRefreshSessionHandler(userRepo, refreshRepo, jwtIssuerSvc, refreshGen),
		authcommands.NewLogoutHandler(refreshRepo, refreshGen),
		logger,
	)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	verifier, err := auth.NewVerifier([]byte(jwtSecret), jwtIssuer)
	if err != nil {
		logger.Fatal().Err(err).Msg("init jwt verifier")
	}

	r.Route("/api/v1", func(r chi.Router) {
		// Public auth endpoints — must be mounted BEFORE the JWT middleware,
		// since signup/signin/refresh produce tokens; users can't have one yet.
		authhttp.Mount(r, authHandler)

		// Authenticated endpoints — require a valid bearer JWT.
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(verifier))
			intenthttp.Mount(r, intentHandler)
			postinghttp.Mount(r, postingHandler)
		})
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		intentDispatcher.Run(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		postingDispatcher.Run(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info().Str("port", port).Msg("server starting")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal().Err(err).Msg("server error")
		}
	}()

	<-ctx.Done()
	logger.Info().Msg("shutdown initiated")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("graceful shutdown failed")
	}
	wg.Wait()
	logger.Info().Msg("shutdown complete")
}

func newLogger() zerolog.Logger {
	level := zerolog.InfoLevel
	if l, err := zerolog.ParseLevel(os.Getenv("LOG_LEVEL")); err == nil && l != zerolog.NoLevel {
		level = l
	}
	zerolog.SetGlobalLevel(level)
	return zerolog.New(os.Stdout).With().Timestamp().Str("service", "hireflow-api").Logger()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requestLogger(logger zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("dur_ms", time.Since(start)).
				Str("request_id", middleware.GetReqID(r.Context())).
				Msg("http request")
		})
	}
}

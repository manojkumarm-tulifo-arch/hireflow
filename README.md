# hireflow

AI-driven recruiter operating system. Captures hiring intent from natural language, drafts job postings from confirmed intents, ingests resumes from connected sources, and feeds the verification + interview pipeline downstream.

## Status

Three bounded contexts live; one stub. End-to-end recruiter flow works against real Postgres: sign up → request hiring intent → confirm → draft posting → publish to channels.

| Context | Owns | Status |
|---|---|---|
| `auth` | User identity, OTP signup/signin, JWT access tokens (claim-shape compatible with shared middleware), refresh-token rotation | **Live** |
| `hiringintent` | Recruiter's intent to hire (role, signals, trust requirements). Source of truth for *why* and *what*. Includes Claude-backed conversational extraction. | **Live** |
| `jobposting` | Published JD lifecycle, source distribution, versioning. Drafted automatically from `IntentConfirmed` via an in-process event bus. | **Live** |
| `sourcing` | Resume ingestion + virus-scan + text extraction (slice 1). Parsing, scoring, dedup pool coming in slices 2–4. | **Live (ingestion-only)** |

## Project layout

Follows the standards in `jobfinder-v3/.kiro/steering/golang-standards.md` and `domain-driven-design.md`. Each bounded context lives in `internal/<context>/` with `domain/`, `application/`, `infrastructure/`, `delivery/` layers.

```
hireflow/
├── cmd/
│   ├── api/                          HTTP server entry point
│   └── devtoken/                     Local-only JWT issuer (legacy — auth context now issues real tokens)
├── internal/
│   ├── auth/                         Bounded context
│   │   ├── domain/                   User, OTPSession, RefreshToken aggregates
│   │   ├── application/commands/     signup/signin/verify/refresh/logout
│   │   ├── infrastructure/
│   │   │   ├── crypto/               Argon2id OTP hash, secure OTP gen, refresh-token hash
│   │   │   ├── tokens/               JWT issuer (matches shared middleware)
│   │   │   ├── notifications/        OTP delivery (LogOTPSender for dev)
│   │   │   └── persistence/          Postgres repos
│   │   └── delivery/http/v1/         Public auth endpoints
│   ├── hiringintent/                 Bounded context (same shape)
│   ├── jobposting/                   Bounded context (same shape)
│   └── shared/
│       ├── domain/                   Shared value objects (TenantID, RecruiterID)
│       └── infrastructure/
│           ├── auth/                 JWT verifier + middleware + Identity-in-context
│           ├── eventbus/             Process-local pub/sub used to deliver outbox events cross-context
│           └── llm/anthropic/        Configured Anthropic SDK client (reused by future contexts)
├── migrations/
│   ├── auth/                         Per-context migrations
│   ├── hiringintent/
│   └── jobposting/
├── docs/
│   ├── api/v1/                       OpenAPI specs (auth, hiringintent, jobposting)
│   └── modules/                      Per-context README + mermaid flows
├── web/                              React + Vite + TypeScript FE
├── tests/                            Cross-context integration tests (placeholder)
├── compose.yml                       Optional Docker Postgres on :5433
├── Makefile
└── developer.md                      Step-by-step run guide
```

## Run locally

```bash
make tidy        # Resolve dependencies
make migrate-up  # Apply migrations for all three contexts (requires DATABASE_URL)
make run         # Start the API on :8080
make test        # Run unit tests
```

For a guided setup (incl. Postgres options, frontend, OTP flow), see [`developer.md`](./developer.md).

## Configuration

12-factor — all config via environment:

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | (required) | Postgres connection string |
| `JWT_ACCESS_SECRET` | (required) | HS256 signing secret. Auth context issues, shared middleware verifies. |
| `JWT_ISSUER` | `hireflow` | `iss` claim and verifier check |
| `ANTHROPIC_API_KEY` | (required) | Powers the LLM intent extractor. Fatal at startup if unset. |
| `ANTHROPIC_MODEL` | `claude-opus-4-7` | Model identifier passed to the Messages API |
| `ANTHROPIC_TIMEOUT` | `30s` | Per-request timeout on the Anthropic call |
| `LOG_LEVEL` | `info` | zerolog level |

## API surface (v1)

| Path | Auth | Purpose |
|---|---|---|
| `POST /api/v1/auth/signup/{request,verify}-otp` | public | Create + activate user |
| `POST /api/v1/auth/signin/{request,verify}-otp` | public | Passwordless signin |
| `POST /api/v1/auth/refresh` | public (token in body) | Rotate access + refresh |
| `POST /api/v1/auth/logout` | public (token in body) | Revoke refresh |
| `GET/POST /api/v1/intents` | bearer JWT | List + draft hiring intents (list supports `q`, `sort=NEWEST\|URGENT`, `status`, `recruiter_id`, `limit`, `offset`) |
| `GET /api/v1/intents/summary` | bearer JWT | Per-status counts for the tenant |
| `POST /api/v1/intents/extract` | bearer JWT | LLM-driven extraction: free-text recruiter message → sparse draft patch + chat reply. Stateless server. |
| `POST /api/v1/intents/{id}/confirm` | bearer JWT | Confirm intent |
| `GET/POST /api/v1/postings` | bearer JWT | Job postings |
| `POST /api/v1/postings/{id}/{publish,close}` | bearer JWT | Distribution + lifecycle |

OpenAPI specs in [`docs/api/v1/`](./docs/api/v1/).

## Architecture invariants

- **Each bounded context owns its tables.** Migrations live per-context with their own tracking table.
- **Outbox pattern + in-process bus** for every state change that emits a domain event. Each context's dispatcher polls its outbox and forwards to a shared `eventbus.InMemory`; subscribers (e.g. `jobposting`'s `IntentConfirmedConsumer`) register against it. Synchronous delivery; handler errors leave the row undispatched so the dispatcher retries — losing a cross-context event silently is far worse than retrying one.
- **Anti-corruption between contexts.** Cross-context reads go through a local port (e.g. `jobposting/infrastructure/clients/IntentReader`) that projects upstream DTOs into the consumer's own snapshot type. Changes in `hiringintent`'s aggregate can't cascade.
- **LLM treated as untrusted input.** The `IntentExtractor` adapter (Claude-backed) returns proposed patches; the application handler runs domain validation and drops invalid fields into `warnings` rather than failing the turn.
- **Multi-tenant from line one.** Every aggregate carries `TenantID`; auth issues claims that downstream middleware resolves to a tenant-scoped Identity.
- **JWT claim shape is single-source-of-truth** in `internal/shared/infrastructure/auth/claims.go`. The auth context emits exactly that shape — no token-exchange step needed.
- **CQRS-lite at the application layer.** `commands/` for state changes, `queries/` for reads. DTOs returned across the boundary; domain types never leak.

## Where to read more

- [`developer.md`](./developer.md) — daily workflow, env vars, dev JWT, smoke test
- [`docs/modules/<context>/README.md`](./docs/modules/) — per-context ubiquitous language, lifecycle, invariants
- [`docs/modules/<context>/flows/*.mermaid`](./docs/modules/) — flow diagrams for each use case
- [`docs/api/v1/*.openapi.yaml`](./docs/api/v1/) — REST contracts

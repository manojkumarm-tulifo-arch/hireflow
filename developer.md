# Developer Guide

Step-by-step path from a fresh checkout to a running hireflow stack on your laptop. Tested on macOS (Homebrew). Linux notes inline where commands differ.

---

## 1. What you're running

A monorepo with two pieces:

| Folder | Stack | Default port |
|---|---|---|
| `./` (Go API) | Go 1.23+, chi, pgx, zerolog, JWT (HS256) | `8080` |
| `./web` (React FE) | Vite + React 18 + TypeScript + Tailwind | `5173` (proxies `/api` → `8080`) |

Backend exposes three bounded contexts: `auth` (public), `hiringintent`, and `jobposting` (both bearer-protected). Postgres holds three per-context schemas, three outboxes, and three migration tracking tables.

---

## 2. Prerequisites

Pick **one** Postgres path — local or Docker. Everything else is the same.

| Tool | Version | Install (macOS) | Verify |
|---|---|---|---|
| Go | 1.23+ | `brew install go` | `go version` |
| Node | 18+ | `brew install node` | `node -v` |
| golang-migrate | 4.x | `brew install golang-migrate` | `migrate -version` |
| **Path A — local Postgres** | 14+ | `brew install postgresql@14 && brew services start postgresql@14` | `pg_isready` |
| **Path B — Docker** | 20+ | `brew install --cask docker` (then launch Docker Desktop) | `docker compose version` |
| ClamAV (optional) | via Docker | Included in `compose.yml`; only needed for resume-scanning. `make db-up` starts it automatically alongside Postgres. Skip if you don't need the sourcing upload pipeline locally. | `docker compose ps clamav` |
| `psql` client (any path) | 14+ | bundled with `postgresql@14`, or `brew install libpq` | `psql --version` |

Linux: `apt install golang-go nodejs npm golang-migrate postgresql-client` plus your distro's docker/postgresql packages.

---

## 3. First-time setup

### 3.1 Clone

```bash
git clone <repo-url> hireflow
cd hireflow
```

### 3.2 Backend dependencies

```bash
make tidy           # go mod tidy
```

### 3.3 Frontend dependencies

```bash
cd web && npm install && cd ..
```

### 3.4 Pick a Postgres path

#### Path A — local Homebrew Postgres (port 5432)

```bash
createdb -h localhost hireflow
psql -h localhost -d hireflow -c "SELECT current_database(), current_user;"
```

Trust auth, no password — your OS user is the superuser.

#### Path B — Docker Compose Postgres (port 5433)

```bash
make db-up         # starts hireflow-postgres container, waits for healthy
```

The container exposes Postgres on **5433** (not 5432) so it doesn't collide with a local install. Other handy targets:

| Target | What it does |
|---|---|
| `make db-up` | Start container, wait for healthcheck |
| `make db-down` | Stop container, **keep** data volume |
| `make db-reset` | Stop + drop volume + restart fresh (full wipe) |
| `make db-logs` | Tail Postgres logs |
| `make db-psql` | Open `psql` shell inside the container |

The DB is named `hireflow`, owned by `hireflow / hireflow`, persisted in the named volume `hireflow_hireflow-pgdata`.

### 3.5 Set environment variables

Add to your shell profile (`~/.zshrc` / `~/.bashrc`) or a project-local `.env` you `source`. Pick the line that matches the path you chose above:

```bash
# Path A — local Homebrew Postgres (port 5432, trust auth, your OS user)
export DATABASE_URL="postgres://$USER@localhost:5432/hireflow?sslmode=disable"

# Path B — Docker Compose Postgres (port 5433, password auth)
export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"

# Common to both
export JWT_ACCESS_SECRET="dev-secret-change-me"
export JWT_ISSUER="hireflow"     # optional, default "hireflow"
export PORT="8080"               # optional, default 8080
export LOG_LEVEL="info"          # optional, debug|info|warn|error

# LLM extraction (required — server is fatal at startup if unset)
export ANTHROPIC_API_KEY="sk-ant-..."   # https://console.anthropic.com/settings/keys
export ANTHROPIC_MODEL="claude-opus-4-7" # optional, default claude-opus-4-7
export ANTHROPIC_TIMEOUT="30s"           # optional, default 30s
```

#### Sourcing pipeline env vars

| Variable                | Default            | Notes                                                     |
|-------------------------|--------------------|-----------------------------------------------------------|
| SOURCING_STORAGE_PATH   | `/tmp/hireflow-resumes` | Root directory for local resume file storage.        |
| SOURCING_MAX_FILE_BYTES | `10485760` (10 MB) | Per-file upload size cap.                                 |
| SOURCING_SCANNER_BACKEND | `noop`            | `noop` or `clamd`. Set `clamd` to enable ClamAV scanning. |
| SOURCING_SCANNER_ADDR   | `tcp://localhost:3310` | ClamAV address (only used when backend is `clamd`).  |
| SOURCING_WORKER_POOL    | `4`                | Number of concurrent processing worker goroutines.        |
| SOURCING_PII_DEK        | (required)         | 64-hex AES-256 key. Generate with `openssl rand -hex 32`. |
| SOURCING_OCR_THRESHOLD  | 50                 | Char threshold below which OCR fallback runs              |
| SOURCING_PARSER_BACKEND | claude             | (only option in slice 2)                                  |
| SOURCING_OCR_BACKEND    | claude             | (only option in slice 2)                                  |

Switching paths later is just changing `DATABASE_URL` and re-running `make migrate-up` — both DBs can coexist on different ports.

Reload your shell or `source ~/.zshrc` so the variables are picked up.

### 3.6 Apply migrations

Each bounded context owns its own schema and tracking table — `make migrate-up` runs all three in order:

```bash
make migrate-up
# auth → hiringintent → jobposting
```

Verify the 13 tables exist:

```bash
# Path A
psql -h localhost -d hireflow -c "\dt"

# Path B
make db-psql -- -c "\dt"
# or:  PGPASSWORD=hireflow psql -h localhost -p 5433 -U hireflow -d hireflow -c "\dt"
```

You should see:

- **auth**: `tenants` (seeded with `demo`), `users`, `otp_sessions`, `refresh_tokens`, `auth_outbox`
- **hiringintent**: `hiring_intents`, `hiring_intent_outbox`
- **jobposting**: `job_postings`, `job_posting_outbox`, `jobposting_processed_events`
- **tracking**: `schema_migrations_auth`, `schema_migrations_hiringintent`, `schema_migrations_jobposting`

---

## 4. Run the backend

In one terminal:

```bash
make run
```

You'll see structured JSON logs:

```
{"level":"info","port":"8080","message":"server starting"}
{"level":"info","component":"outbox_dispatcher","batch":50,"message":"dispatcher started"}
```

Smoke test from another terminal:

```bash
curl http://localhost:8080/health
# {"status":"ok"}

curl -o /dev/null -w "%{http_code}\n" http://localhost:8080/api/v1/intents
# 401   (expected — no token)
```

---

## 5. Run the frontend

In a second terminal:

```bash
cd web
npm run dev
```

Vite serves at `http://localhost:5173` and proxies `/api/*` → `http://localhost:8080`. Open the URL in your browser — you'll be redirected to `/login`.

---

## 6. Sign in (real OTP — no more paste-JWT)

The `auth` bounded context issues real tokens. The dev OTP sender writes codes to the backend log instead of sending email — so you watch the `make run` window for your code.

### From the FE (recommended)

1. Open `http://localhost:5173`
2. Click **"New here? Create an account"**
3. Enter:
   - **email** — anything (e.g., `you@example.com`)
   - **name** — your name
   - **tenant slug** — `demo` (pre-seeded by the auth migration)
4. Click **Send code** → check the **`make run` terminal** for a line like:
   ```
   {"component":"log_otp_sender","email":"you@example.com","purpose":"SIGNUP","code":"123456","message":"OTP issued"}
   ```
5. Type the 6-digit code → you're in. Access + refresh tokens are stored in localStorage; the FE refreshes transparently on 401.

Subsequent sign-ins: same flow, click **"Have an account? Sign in"** — only email + code needed.

### From the CLI

```bash
# Step 1: request OTP
curl -s -X POST http://localhost:8080/api/v1/auth/signup/request-otp \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","name":"You","tenant_slug":"demo"}' | jq .

# Step 2: read the OTP from the make-run log, then verify
OTP="<6-digit from log>"
RESP=$(curl -s -X POST http://localhost:8080/api/v1/auth/signup/verify-otp \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"you@example.com\",\"code\":\"$OTP\"}")
TOKEN=$(echo "$RESP" | jq -r .data.access_token)
REFRESH=$(echo "$RESP" | jq -r .data.refresh_token)

# Verify it works
curl -s http://localhost:8080/api/v1/intents -H "Authorization: Bearer $TOKEN" | jq .
```

### Tokens

| Token | TTL | Purpose | Storage |
|---|---|---|---|
| Access (JWT) | 15 min | `Authorization: Bearer ...` on every request | FE: localStorage `hireflow.token` |
| Refresh (opaque `<id>.<secret>`) | 30 days | `POST /auth/refresh` to get a new pair | FE: localStorage `hireflow.refresh` |

Refresh **rotates** — every `/refresh` revokes the old refresh and issues a fresh pair. Re-using an old refresh after rotation surfaces as `invalid_refresh`.

### Legacy: `cmd/devtoken`

Still available for scripting, but the auth context is the right path now:

```bash
JWT_ACCESS_SECRET="$JWT_ACCESS_SECRET" go run ./cmd/devtoken --ttl 1h
```

Useful when you want a token with a hand-picked `tenant_id`/`recruiter_id` for testing tenant isolation.

---

## 7. End-to-end smoke test

Backend running on `:8080`. Token in `$TOKEN`.

### Create a draft hiring intent

```bash
curl -s -X POST http://localhost:8080/api/v1/intents \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "role_title":"Senior Backend Engineer",
    "skills":[
      {"name":"Go","required":true},
      {"name":"Postgres","required":true},
      {"name":"Kubernetes","required":false}
    ],
    "min_years":3, "max_years":7, "headcount":2,
    "locations":["Bangalore","Remote"],
    "work_mode":"HYBRID", "priority":"HIGH"
  }' | jq .
```

Note the returned `data.id` — that's your intent ID.

### Confirm the intent

```bash
INTENT_ID="<paste id from above>"
curl -s -X POST http://localhost:8080/api/v1/intents/$INTENT_ID/confirm \
  -H "Authorization: Bearer $TOKEN" | jq .
# status now CONFIRMED, IntentConfirmed event written to hiring_intent_outbox
```

### List intents

```bash
curl -s http://localhost:8080/api/v1/intents -H "Authorization: Bearer $TOKEN" | jq .
curl -s 'http://localhost:8080/api/v1/intents?status=CONFIRMED' -H "Authorization: Bearer $TOKEN" | jq .
```

### Try LLM extraction

```bash
curl -s -X POST http://localhost:8080/api/v1/intents/extract \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [],
    "draft": {},
    "user_message": "Hiring 2 senior Go engineers in Bangalore, 3-7 years, hybrid, high priority"
  }' | jq .
# data.patch is the sparse fields the model extracted; data.reply is the chat turn;
# data.complete is true once the patch + draft together have all required fields.
#
# Error codes the FE branches on (see docs/api/v1/hiringintent.openapi.yaml):
#   503 llm_billing            — workspace out of credits (top up at console.anthropic.com)
#   503 llm_auth_error         — bad ANTHROPIC_API_KEY
#   503 llm_permission_error   — workspace can't access ANTHROPIC_MODEL
#   429 llm_rate_limited       — back off and retry
#   503 llm_overloaded         — upstream overloaded; retry
#   504 llm_timeout            — upstream slow; retry
#   503 llm_response_error     — model returned bad shape (rare with tool_choice)
#   503 llm_unavailable        — generic / unclassified upstream failure
```

### Use the FE

If you signed up via the FE, you're already in. If you signed up via curl, the FE has its own session — use the FE login flow there. The two paths are independent (each starts its own session).

---

## 8. Common operations

| Goal | Command |
|---|---|
| Run all Go unit tests | `make test` |
| Run only domain/app tests | `make test-unit` |
| Run integration tests (needs DB) | `make test-integration` |
| Lint Go | `make lint` |
| FE typecheck | `cd web && npm run typecheck` |
| FE production build | `cd web && npm run build` |
| Reset migrations only | `make migrate-down && make migrate-up` |
| Wipe DB (Path A) | `dropdb hireflow && createdb hireflow && make migrate-up` |
| Wipe DB (Path B) | `make db-reset && make migrate-up` |
| Tail logs as JSON | `make run | jq -c .` |

---

## 9. Project layout

```
hireflow/
├── cmd/
│   ├── api/                  HTTP server entry point
│   └── devtoken/             Local-only JWT issuer (legacy; auth context now issues real tokens)
├── internal/
│   ├── auth/                 Bounded context: signup, signin, refresh, logout
│   │   ├── domain/           User, OTPSession, RefreshToken aggregates
│   │   ├── application/      Commands (CQRS-lite) + DTOs
│   │   ├── infrastructure/
│   │   │   ├── crypto/       Argon2id OTP hash, secure OTP gen, refresh-token hash
│   │   │   ├── tokens/       JWT issuer (claim shape matches shared middleware)
│   │   │   ├── notifications/  OTP delivery (LogOTPSender for dev)
│   │   │   └── persistence/  Postgres repos + outbox
│   │   └── delivery/http/v1/ Public auth endpoints
│   ├── hiringintent/         Bounded context: capture + confirm intents
│   │   ├── domain/           Aggregate, value objects, events, repo interface
│   │   ├── application/      Commands + queries (CQRS-lite) + DTOs
│   │   ├── infrastructure/   Postgres repo + outbox dispatcher
│   │   └── delivery/http/v1/ HTTP handlers + routes
│   ├── jobposting/           Bounded context: published JD lifecycle
│   │   └── …                 Same shape; cross-context consumer of IntentConfirmed
│   └── shared/
│       ├── domain/           Cross-context value objects (TenantID, RecruiterID)
│       └── infrastructure/auth/  JWT verifier + middleware + Identity-in-context
├── migrations/
│   ├── auth/                 Per-context migrations (seeds default tenant)
│   ├── hiringintent/
│   └── jobposting/
├── docs/
│   ├── api/v1/               OpenAPI 3 specs (auth, hiringintent, jobposting)
│   └── modules/              Per-context README + mermaid flow diagrams
├── web/                      React FE
│   ├── src/
│   │   ├── api/              Typed client (mirrors OpenAPI) + auth client + refresh-on-401
│   │   ├── auth/             AuthContext, ProtectedRoute, 2-step OTP LoginPage
│   │   ├── components/       UI primitives + AppShell
│   │   ├── features/         intent/* and posting/* pages
│   │   └── routes.tsx        Route table
│   └── package.json
├── tests/                    Cross-context integration tests (placeholder)
├── compose.yml               Docker Postgres for Path B (port 5433)
├── Makefile
├── go.mod
├── README.md
└── developer.md              You are here
```

See `docs/modules/<context>/README.md` for context-specific design notes and `docs/modules/<context>/flows/*.mermaid` for the per-flow diagrams.

---

## 10. Troubleshooting

| Symptom | Cause / Fix |
|---|---|
| `bind: address already in use` | Another process on `:8080` — `lsof -ti:8080 \| xargs kill` |
| `pq: SSL is not enabled on the server` | Missing `?sslmode=disable` in `DATABASE_URL` |
| `pq: password authentication failed` | Homebrew Postgres uses trust auth — drop the password from the URL |
| `no migration files found` | You're not in the repo root; `make` reads paths relative to `Makefile` |
| Migration says "no change" but tables missing | Old run hit the shared `schema_migrations` table; wipe and re-apply (`dropdb hireflow && createdb hireflow && make migrate-up`) |
| `401 invalid_token` | Access token expired, signed with wrong secret, or `iss` mismatch — sign in again, or let the FE auto-refresh |
| `401 invalid_refresh` | Refresh token revoked (rotation, logout, account locked) or expired — sign in again from `/login` |
| `401 invalid_credentials` | Email unknown, OTP session missing — request a fresh OTP |
| `401 invalid_otp` | Wrong code, all attempts used, or session expired (10-min TTL) — request a fresh OTP |
| `409 email_taken` | Email already registered — use the **Sign in** flow instead of **Sign up** |
| `404 tenant_not_found` | Tenant slug doesn't exist — for local dev use `demo` (seeded by the auth migration) |
| OTP code not visible | LogOTPSender writes to the `make run` terminal at INFO level — `LOG_LEVEL=debug` if you don't see it |
| FE returns CORS error | You're hitting `:8080` from `:5173` directly instead of through the Vite proxy — use `/api/...` (relative), not `http://localhost:8080/api/...` |
| `pg_isready` says no | Postgres isn't running — `brew services start postgresql@14` (Path A) or `make db-up` (Path B) |
| Docker: `Cannot connect to the Docker daemon` | Docker Desktop isn't running — launch it from Applications |
| Docker: port 5433 already allocated | Something else on 5433 — `lsof -ti:5433 \| xargs kill`, or change the port mapping in `compose.yml` |
| Docker: data survived `make db-down` | That's by design — `make db-down` keeps the volume; use `make db-reset` for a true wipe |
| `go: cannot find main module` | You're outside the repo root; `cd` into `hireflow/` |

---

## 11. Daily workflow cheat sheet

```bash
# Once per day (Path B only — skip for Path A, brew services keeps it running)
cd hireflow
make db-up

# Terminal 1 — backend
cd hireflow
source .env       # or have env vars in ~/.zshrc
make run

# Terminal 2 — frontend
cd hireflow/web
npm run dev

# Sign in via the FE — visit http://localhost:5173, "Create an account",
# read the OTP from the make-run terminal log, type it in. Done.

# Or, hit the API directly via the auth context:
EMAIL="you@example.com"
curl -s -X POST http://localhost:8080/api/v1/auth/signin/request-otp \
  -H "Content-Type: application/json" -d "{\"email\":\"$EMAIL\"}" | jq .
# → grab the 6-digit OTP from the make-run log
OTP="<6 digits>"
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/signin/verify-otp \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$EMAIL\",\"code\":\"$OTP\"}" | jq -r .data.access_token)
curl -s http://localhost:8080/api/v1/intents -H "Authorization: Bearer $TOKEN" | jq .

# End of day (Path B optional — `make db-down` frees container resources;
# the data volume survives so tomorrow's `make db-up` is instant)
make db-down
```

That's the whole loop.

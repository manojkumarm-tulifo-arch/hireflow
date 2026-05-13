# Developer Guide

Step-by-step path from a fresh checkout to a running hireflow stack on your laptop. Tested on macOS (Homebrew). Linux notes inline where commands differ.

---

## 1. What you're running

A monorepo with two pieces, plus an **optional sibling service** for the BGV reviewer queue:

| Folder | Stack | Default port |
|---|---|---|
| `./` (Go API) | Go 1.23+, chi, pgx, zerolog, JWT (HS256) | `8080` |
| `./web` (React FE) | Vite + React 18 + TypeScript + Tailwind | `5173` (proxies `/api` → `8080`, **`/api/v1/bgv` → `8081`**) |
| `~/hustle/code/theo/candidate-bgv` (sibling repo) | Same stack, separate Postgres DB | `8081` |

Backend exposes three bounded contexts: `auth` (public), `hiringintent`, and `jobposting` (both bearer-protected). Postgres holds three per-context schemas, three outboxes, and three migration tracking tables.

The recruiter web app **also includes a BGV reviewer queue** under `/bgv` (sidebar: "BGV Submissions"). Those screens call the sibling [candidate-bgv](../candidate-bgv) service on `:8081`, reusing the recruiter JWT this service issues (same `JWT_ACCESS_SECRET` + `JWT_ISSUER` on both sides). **For full end-to-end testing, bring both API processes up.** If you skip candidate-bgv, the rest of the recruiter flow still works — but `/bgv` will 404 every request.

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

Vite serves at `http://localhost:5173`. Proxy rules (in declaration order — Vite takes the first prefix match):

1. `/api/v1/bgv/*` → `http://localhost:8081` (candidate-bgv)
2. `/api/*`        → `http://localhost:8080` (this service)

Open the URL in your browser — you'll be redirected to `/login`.

### 5.1 (Optional) Run candidate-bgv for the BGV reviewer queue

The "BGV Submissions" sidebar item calls the sibling service. To bring it up alongside hireflow, in a third terminal:

```bash
cd ~/hustle/code/theo/candidate-bgv
make stack          # idempotent: brings up Postgres + applies migrations
make run            # API on :8081, reuses the same JWT_ACCESS_SECRET as hireflow
```

To populate the queue with a test submission:

```bash
# In candidate-bgv:
make seed           # mints a fresh INVITED submission, prints the candidate URL
```

You can then either walk that candidate URL through to Submit (in `candidate-bgv/web` on `:5180`) for a SUBMITTED row in your queue, or leave it INVITED and filter the queue by that pill. Skipping this step is fine for testing intents/postings/auth in isolation — the BGV screens just won't have data.

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

### Reviewer-side BGV queue

> **Don't skip this when smoke-testing end-to-end.** The recruiter UI now hosts the candidate-bgv reviewer surface, and it talks to a separate process. Easy to forget that `/bgv` is failing because `:8081` isn't running, not because the FE is broken.

Pre-req: candidate-bgv API up on `:8081` (see §5.1) and at least one submission in its DB (`make seed` in that repo).

1. In the recruiter web (`:5173`), click **BGV Submissions** in the left sidebar.
2. The queue should list your seeded submission(s). Filter pills at the top scope by lifecycle state (INVITED / IN_PROGRESS / SUBMITTED / UNDER_REVIEW / VERIFIED / FLAGGED).
3. Click into a row → the detail page renders: candidate header, lifecycle stamps, Personal / Address / Emergency / Documents / References / Digital / Declarations cards, and the event timeline at the bottom.
4. **Document download** — captured docs render with a Download button. Clicking auth-fetches the file (works for both the local-fs adapter, which streams bytes inline, and the S3 adapter, which 302s to a pre-signed URL).
5. **Reopen** — for SUBMITTED / UNDER_REVIEW / FLAGGED submissions, the **Reopen** button appears in the header. Opens a modal; reason is required (BE invariant). On success, status flips back to IN_PROGRESS and a `bgv.BGVReopened` event lands on the timeline with the reason inline.
6. Common smoke-test failures:
   - **Empty queue** — usually the candidate-bgv API isn't running, or hireflow's tenant doesn't match the seeded submission's tenant. Check that both services share the same `JWT_ACCESS_SECRET` and that the tenant in your recruiter JWT matches the one used by `make seed`.
   - **401 / 403** — JWT secret mismatch, or recruiter JWT was issued with `subject_kind=candidate` somewhere. Re-sign in.
   - **Document download fails with `download_failed`** — the file actually missing on disk (LocalFile) or the bucket (S3). Check candidate-bgv's storage backend env vars.

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
| Tail logs as JSON | `make run \| jq -c .` |
| Bring up the BGV reviewer dependency | `(cd ~/hustle/code/theo/candidate-bgv && make stack && make run)` |
| Seed a BGV submission for the recruiter queue | `(cd ~/hustle/code/theo/candidate-bgv && make seed)` |

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
| `/bgv` returns 404 / network error | Sibling candidate-bgv API on `:8081` isn't running — see §5.1 |
| `/bgv` queue is empty after sign-in | Either no submissions exist (`make seed` in candidate-bgv) or your recruiter's `tenant_id` doesn't match the seeded submission's tenant — both repos must use the same tenant; the dev seed uses the `demo` tenant by default |
| BGV detail page renders but document Download fails | Storage backend mismatch — candidate-bgv ran with `BGV_STORAGE_BACKEND=s3` but bucket creds are wrong, or `local` files were wiped under `var/bgv-documents/`. Check candidate-bgv's `make run` log |

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

# Terminal 3 — sibling candidate-bgv API (only if you're touching /bgv;
# safe to skip if you're working on intents / postings / auth)
cd ~/hustle/code/theo/candidate-bgv
make stack && make run

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

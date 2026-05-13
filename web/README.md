# hireflow-web

React + Vite + TypeScript front-end for hireflow. Talks to the Go API at `/api/v1` (proxied to `http://localhost:8080` in dev) **and** to the sibling candidate-bgv API for the reviewer queue (`/api/v1/bgv/*` proxied to `http://localhost:8081` — see `vite.config.ts`).

> **Two backends in dev.** Routes under `/intents`, `/postings`, `/auth` hit hireflow on `:8080`; the recruiter BGV queue (`/bgv`) hits candidate-bgv on `:8081`. **Both API processes must be up** for end-to-end testing — see "Sibling services" below.

## Run

```bash
npm install
npm run dev          # http://localhost:5173
npm run build        # production build to dist/
npm run typecheck    # tsc --noEmit
```

## Auth

OTP-only signup + signin against the `auth` bounded context.

**First time:** click **"New here? Create an account"** → email + name + tenant slug (use `demo` for local dev) → check the **backend `make run` terminal** for a log line like:

```
{"component":"log_otp_sender","email":"you@example.com","purpose":"SIGNUP","code":"123456","message":"OTP issued"}
```

Type the 6-digit code → you're in.

**Subsequent visits:** click **"Have an account? Sign in"** → email only → OTP from the same log line.

### Tokens

The FE stores both access and refresh in `localStorage`:

| Key | Holds | TTL |
|---|---|---|
| `hireflow.token` | Access JWT (HS256) | 15 min |
| `hireflow.refresh` | Opaque `<id>.<secret>` refresh | 30 days |
| `hireflow.user` | Cached `User` projection (id, email, status, roles, tenant_id) | until sign-out |

`api/client.ts` automatically retries any failing 401 once after attempting `POST /auth/refresh`. If the refresh also fails, the user is signed out and the `ProtectedRoute` bounces to `/login`.

## Layout

```
src/
├── api/
│   ├── types.ts        Hand-mirrored from docs/api/v1/*.openapi.yaml (hireflow contexts)
│   ├── bgvTypes.ts     Hand-mirrored from candidate-bgv/docs/api/v1/bgv.openapi.yaml
│   ├── client.ts       fetch wrapper + bearer + refresh-on-401 + envelope
│   ├── auth.ts         Typed signup / signin / refresh / logout
│   ├── intent.ts       Hiring-intent endpoints (list with q/sort, summary, get, create, confirm, extract)
│   ├── posting.ts      Job-posting endpoints
│   └── bgv.ts          BGV reviewer endpoints (list, get, timeline, reopen) + downloadDocument helper
├── auth/
│   ├── AuthContext.tsx 2-token store, auto-refresh hook, signOut
│   ├── ProtectedRoute.tsx
│   └── LoginPage.tsx   2-step OTP flow (email → code)
├── components/
│   ├── ui/             Primitives: Button, Card, Badge, Input, Spinner, StatusBadge (incl. BGVStatusBadge)
│   └── layout/         AppShell with sidebar nav (Capture Intent, Intents, Postings, BGV Submissions)
├── features/
│   ├── intent/         Capture chat (live LLM extraction via /intents/extract) + IntentCard, list, detail
│   ├── posting/        List, detail with publish + close actions
│   └── bgv/            Recruiter-side BGV: list (queue), detail (steps + docs + timeline), Reopen dialog
├── lib/                cn() className helper
├── routes.tsx          Route table
├── App.tsx             QueryClient + AuthProvider + Router
└── main.tsx
```

## Sibling services (BGV reviewer)

The `/bgv` recruiter screens proxy through to the **candidate-bgv** Go service running on `:8081` (sibling repo at `~/hustle/code/theo/candidate-bgv`). For the queue to populate end-to-end you need three processes side by side:

| Process | Port | Role |
|---|---|---|
| hireflow API (`make run` in `~/hustle/code/theo/hireflow/`) | 8080 | Recruiter auth + intents + postings |
| candidate-bgv API (`make run` in `~/hustle/code/theo/candidate-bgv/`) | 8081 | BGV submissions, timeline, reopen, document download |
| hireflow-web (`npm run dev` here) | 5173 | Recruiter UI; proxies `/api/v1/bgv/*` → 8081, everything else → 8080 |

**Don't skip the candidate-bgv process when smoke-testing — `/bgv` will simply 404 every request.** The recruiter JWT minted by hireflow's `auth` is reused unchanged (same `JWT_ACCESS_SECRET` and `JWT_ISSUER` in both services).

### To populate the queue with a test submission

```bash
# In the candidate-bgv repo:
make seed   # creates an INVITED submission tied to a fresh candidate, prints the candidate URL
```

Then either walk the candidate flow (in `candidate-bgv/web` on `:5180`) all the way through Submit, or just leave it INVITED — both shapes show up in the recruiter queue, just in different filter pills.

## Design tokens

Colors mirror the Go-side palette and the Figr design (indigo accent `#5B4CFF`). See `tailwind.config.js`.

## Adding new API types

For now `src/api/types.ts` is hand-mirrored from `docs/api/v1/*.openapi.yaml`. When the spec changes, update the TS types to match. Drop-in replacement with `openapi-typescript` is on the roadmap; the typed client wrappers in `intent.ts`/`posting.ts`/`auth.ts` are stable and won't change shape.

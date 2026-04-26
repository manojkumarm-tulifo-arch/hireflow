# auth — bounded context

Owns user identity, OTP-based signup/signin, JWT access tokens, and refresh-token rotation. Issues tokens in the exact claim shape the shared middleware (`internal/shared/infrastructure/auth`) verifies — no token-exchange step needed between contexts.

## Ubiquitous language

| Term | Meaning |
|---|---|
| **User** | A person who can sign in to a tenant. Has lifecycle: PendingVerification → Active (→ Locked / Suspended). |
| **Tenant** | A hiring organization. Pre-seeded for now; full lifecycle belongs in a future platform-admin context. Identified externally by **slug**. |
| **OTP Session** | A 10-minute, single-use challenge with 5 attempts, scoped to (email, purpose). |
| **Refresh Token** | An opaque per-user secret persisted as `<id>.<hash>`. Rotates on every `/refresh`. |

## Aggregates

- `User` (root) — identity, status, roles, failed-attempts + auto-lock
- `OTPSession` — one in-flight challenge per (email, purpose); older un-verified sessions are deleted on insert of a new one
- `RefreshToken` — one row per issued refresh; revoked on logout, on rotation, and on user lockout

## Lifecycle (User)

```
PendingVerification → Active → (Locked → Active on cooldown | Suspended)
```

## Invariants

1. **Email is globally unique** (not per-tenant) — keeps signin able to resolve tenant from email alone
2. Signup creates a PendingVerification user *before* the OTP is sent (delivery failure → user can retry, no orphan session)
3. OTP verify consumes one attempt regardless of outcome; 5 wrong codes locks the session
4. 5 consecutive failed sign-ins lock the user for `LockCooldown` (15 min); auto-unlock when cooldown expires
5. Refresh token rotation is mandatory — using a refresh token revokes it and mints a new pair

## Endpoints (HTTP v1)

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/auth/signup/request-otp` | Create user + send signup OTP | none |
| POST | `/api/v1/auth/signup/verify-otp` | Verify signup OTP → tokens | none |
| POST | `/api/v1/auth/signin/request-otp` | Send signin OTP (uniform response) | none |
| POST | `/api/v1/auth/signin/verify-otp` | Verify signin OTP → tokens | none |
| POST | `/api/v1/auth/refresh` | Rotate refresh → new token pair | none (token in body) |
| POST | `/api/v1/auth/logout` | Revoke refresh | none (token in body) |

## JWT claim shape

Tokens carry exactly what `internal/shared/infrastructure/auth/claims.go` validates:

```json
{
  "tenant_id":    "<UUID>",
  "recruiter_id": "<UUID>",
  "roles":        ["recruiter"],
  "iss":          "hireflow",
  "sub":          "<UUID>",
  "iat":          1234,
  "nbf":          1234,
  "exp":          5678
}
```

`recruiter_id == User.ID` — we keep the claim name `recruiter_id` to match the existing middleware. When we add other user types, the claim becomes a generic `subject_id` plus a `subject_type`.

## Domain events emitted

| Event | When | Consumed by |
|---|---|---|
| `auth.UserRegistered` | `entities.User.NewUser` | Audit log |
| `auth.UserVerified` | `User.MarkVerified` | Audit log, future onboarding email |
| `auth.UserSignedIn` | `User.RecordSignIn` | Audit log, security analytics |

## Information-leak defenses

- `signin/request-otp` returns the same shape regardless of whether the email exists (no enumeration)
- `logout` is idempotent — unknown / mismatched tokens silently no-op (no enumeration)
- All credential-rejection errors collapse to `invalid_credentials` / `invalid_otp` / `invalid_refresh` at the HTTP layer

## Flows

See [`flows/`](./flows/):
- `signup.mermaid` — request-otp → verify-otp
- `signin.mermaid` — request-otp → verify-otp
- `refresh.mermaid` — rotation

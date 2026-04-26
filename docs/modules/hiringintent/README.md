# hiringintent — bounded context

Captures a recruiter's intent to hire. Owns the *why* (urgency, budget, hiring manager involvement) and the *what* (role, skills, experience, headcount, location, trust requirements). Source-of-truth for downstream contexts (`jobposting`, `sourcing`).

## Ubiquitous language

| Term | Meaning |
|---|---|
| **Hiring Intent** | A recruiter's structured commitment to fill one or more positions. Has a lifecycle. |
| **Role Spec** | The role description: title, skills, experience, headcount, location, work mode. |
| **Intent Signal** | Qualitative readiness indicator (urgency, budget approved, HM involvement) with a level (high/med/low). |
| **Trust Signal** | Required candidate verification (ID, liveness, BGV, NDA, references) toggled per intent. |
| **Recruiter** | The user who owns the intent. Belongs to a Tenant (org). |
| **Tenant** | The hiring organization. Multi-tenant boundary. |

## Aggregate

`HiringIntent` is the single aggregate root. Owns all role/signal/trust data. External contexts reference only by `IntentID`.

## Lifecycle

```
Drafted → Confirmed → (Cancelled | Closed)
```

| State | Meaning | Mutations allowed |
|---|---|---|
| `Drafted` | Created from chat extraction. Editable. | Update role, add/remove signals, confirm, cancel |
| `Confirmed` | Recruiter signed off. Frozen. Triggers `IntentConfirmed` event → drives `jobposting`. | Cancel only |
| `Cancelled` | Withdrawn before publish. Terminal. | None |
| `Closed` | Posting completed (filled or expired). Terminal. | None |

## Invariants (enforced at aggregate boundary)

1. Headcount must be > 0
2. Experience min ≤ max
3. At least one skill required to confirm
4. Cannot confirm an intent that is not `Drafted`
5. Cannot mutate role/signals after `Confirmed`
6. Tenant + Recruiter immutable after creation

## Domain events emitted

| Event | When | Consumed by |
|---|---|---|
| `IntentDrafted` | After `NewHiringIntent()` | Analytics, audit log |
| `IntentRoleUpdated` | After `UpdateRole()` | Audit log |
| `IntentConfirmed` | After `Confirm()` | `jobposting` (creates draft posting), `sourcing` (warms source adapters) |
| `IntentCancelled` | After `Cancel()` | `jobposting` (cancels draft if any), audit log |

## Queries

The application layer exposes the following read-side queries (`application/queries/`):

| Query | Purpose | HTTP |
|---|---|---|
| `GetIntent` | Fetch a single intent by ID. | `GET /intents/{id}` |
| `ListIntents` | Page through intents. Supports `status`, `recruiter_id`, free-text `q` (ILIKE on role title), and `sort` (`NEWEST` default, `URGENT` orders by priority severity). | `GET /intents` |
| `IntentSummary` | Per-status histogram for the tenant; powers the FE's filter chips with badge counts. | `GET /intents/summary` |

All queries are tenant-scoped via the JWT `tenant_id` claim and never leak rows from another tenant. The repository port lives at `domain/repositories/intent_repository.go` — see `Counts()` for the summary read path and `IntentFilter`/`ListSortOrder` for list shaping.

## Flows

See [`flows/`](./flows/):
- `create_intent.mermaid` — chat → AI extraction → draft intent
- `confirm_intent.mermaid` — review → confirm → emit event

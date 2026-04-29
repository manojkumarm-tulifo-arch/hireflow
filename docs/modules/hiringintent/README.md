# hiringintent — bounded context

Captures a recruiter's intent to hire. Owns the *why* (urgency, budget, hiring manager involvement) and the *what* (role, skills, experience, headcount, location, trust requirements). Source-of-truth for downstream contexts (`jobposting`, `sourcing`).

## Ubiquitous language

| Term | Meaning |
|---|---|
| **Hiring Intent** | A recruiter's structured commitment to fill one or more positions. Has a lifecycle. |
| **Role Spec** | The role description: title, skills, experience, headcount, location, work mode. |
| **Intent Signal** | Qualitative readiness indicator (urgency, budget approved, HM involvement) with a level (high/med/low). |
| **Trust Signal** | Required candidate verification (ID, liveness, BGV, NDA, references) toggled per intent. |
| **Reason** | Free-text rationale for the role (backfill, growth, new product). Optional. |
| **Team** | The squad / pod the hire joins (e.g. "Payments Platform"). Optional. |
| **Reports To** | Hiring manager or reporting line. Optional. |
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

## LLM-driven extraction

Recruiters draft intents through a chat conversation. The `IntentExtractor` port (`domain/services/intent_extractor.go`) abstracts the LLM call; the production adapter at `infrastructure/llm/anthropic_extractor.go` calls Claude via forced tool-use against a `propose_draft` schema, with the system prompt cached on every turn.

| Surface | Lives at | Notes |
|---|---|---|
| `ExtractIntent` command | `application/commands/extract_intent.go` | Validates input, calls the port, runs domain validation on the proposed patch (drops invalid `work_mode`/`priority`/`headcount`/years range/empty skills into `warnings` rather than failing the turn). Truncates history to the last 20 turns. |
| Port | `domain/services/intent_extractor.go` | Interface speaks DTOs only; SDK details stay behind the adapter. |
| HTTP | `POST /intents/extract` (bearer JWT) | Stateless: client sends prior `messages`, current `draft`, and `user_message`; server returns sparse `patch` + chat `reply` + `complete` flag. See `docs/api/v1/hiringintent.openapi.yaml`. |
| Provider decision | Claude (Anthropic) only for v1; sibling adapters (OpenAI etc.) can implement the same port without changing callers. |

The recruiter still calls `POST /intents` to commit the resulting draft — extraction is read-only relative to the domain.

## Flows

See [`flows/`](./flows/):
- `create_intent.mermaid` — chat → AI extraction → draft intent
- `extract_intent.mermaid` — one chat turn through the LLM extractor
- `confirm_intent.mermaid` — review → confirm → emit event

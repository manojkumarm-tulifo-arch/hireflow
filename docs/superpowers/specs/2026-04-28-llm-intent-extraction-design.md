# LLM Intent Extraction — Design

**Status:** approved (auto-mode)
**Date:** 2026-04-28
**Author:** brainstorm session, Claude Opus 4.7
**Plan item:** `plan/01-hireflow.md` §C "LLM extraction" + §F "Real LLM intent extraction"
**Related decision:** `plan/01-hireflow.md` §Decisions #3 (LLM provider)

---

## Summary

Replace the stubbed chat in `IntentCapturePage.tsx` with a real LLM-powered
extraction flow. Each turn the assistant (a) extracts new info from the
recruiter's message into a structured patch, (b) elicits one missing required
field if the draft isn't complete, and (c) returns a brief natural-language
reply. The recruiter still commits the draft via the existing
`POST /api/v1/intents` — extraction is read-only relative to the domain.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Conversation model: **patch + elicit** (B + C) | Multi-turn dialog where each turn returns both a chat reply and a sparse draft patch, and the assistant proactively asks for missing required fields one at a time. |
| D2 | Vendor: **Claude (Anthropic)**, behind a port | Tool-use gives near-bulletproof structured output. Port (`IntentExtractor`) is narrow and lives in `hiringintent`; future contexts (sourcing, interview) define their own ports and reuse the shared SDK client. |
| D3 | State: **stateless server** | FE sends `messages[]` + `draft` + `user_message` each turn. No new aggregate or table. Refresh-recovery deferred. |
| D4 | Streaming: **non-streaming** | Single JSON response. Wait is acceptable (~2-5s); FE stays a normal mutation. SSE can be added later without contract change. |

## Non-goals (Phase 0)

- Persisting chat history server-side (chat audit, training data) → out of scope.
- Multiple LLM providers active concurrently → port allows it; shipping one impl.
- Streaming UI → deferred.
- Per-tenant rate limiting / cost dashboards → flagged in §H Productionization.
- Resume-the-chat after refresh → deferred (would require D3 → option B).

## Architecture

### New files

```
internal/shared/infrastructure/llm/anthropic/
    client.go              # Thin SDK wrapper: API key, timeouts, retries.
                           # Reusable by sourcing/interview later.
    client_test.go

internal/hiringintent/
    domain/services/
        intent_extractor.go         # Port: IntentExtractor interface
    application/commands/
        extract_intent.go           # ExtractIntentHandler — orchestration + validation
        extract_intent_test.go
    application/dto/
        extract_dto.go              # ExtractInput, ExtractOutput, ChatMessage, DraftPatch
    infrastructure/llm/
        anthropic_extractor.go      # Adapter: implements IntentExtractor
        anthropic_extractor_test.go # Uses fake HTTP transport
    delivery/http/v1/
        handlers.go                 # Add Extract method (wire-up only)
        routes.go                   # Mount POST /intents/extract

web/src/api/
    extract.ts                      # FE client wrapper

docs/api/v1/
    intents-extract.yaml            # OpenAPI spec
```

### Modified files

- `web/src/features/intent/IntentCapturePage.tsx` — chat goes from stub to live mutation; remove `initialDraft`; render warnings; handle 503.
- `web/src/api/types.ts` — add `ExtractRequest`, `ExtractResponse`, `DraftPatch`, `ChatMessage`.
- `cmd/api/main.go` — instantiate `anthropic.Client`, `anthropicExtractor`, `ExtractIntentHandler`; pass to `intentHandler`.
- `internal/hiringintent/delivery/http/v1/handlers.go` — `IntentHandler` gains an `extract` field and a method.

### Boundaries

- **Port** lives in `domain/services/intent_extractor.go`. Speaks `DraftPatch` and `ChatMessage`, never SDK types.
- **Application** orchestrates: build prompt input from request, call port, run domain validation on the returned patch (drops invalid fields into `warnings`), return DTO.
- **Infrastructure** holds the Anthropic SDK call. The shared `anthropic.Client` is *not* a generic LLM service — it's an Anthropic-specific HTTP+retry wrapper. Each context defines its own port if it needs one.
- **Delivery** is a thin HTTP handler; auth + parse + call command + render.

Dependency direction: `delivery → application → domain ← infrastructure`. Standard.

## API contract

### Request

```http
POST /api/v1/intents/extract
Authorization: Bearer <jwt>
Content-Type: application/json

{
  "messages": [
    { "role": "user", "text": "Hiring 2 senior Go engineers in Bangalore, 3-7 years" },
    { "role": "assistant", "text": "Got it — what's the work mode?" }
  ],
  "draft": {
    "role_title": "",
    "skills": [],
    "min_years": 0,
    "max_years": 0,
    "headcount": 0,
    "locations": [],
    "work_mode": "",
    "priority": ""
  },
  "user_message": "Hybrid, Bangalore office 3 days a week"
}
```

- `tenant_id`, `recruiter_id` come from JWT claims, never the body.
- `messages` is prior history *excluding* the current user turn (`user_message`).
- `draft` mirrors `CreateIntentRequest` shape so the FE can pass current state directly.

### Response

```json
{
  "reply": "Hybrid, Bangalore — got it. Want to set a priority and budget before I draft?",
  "patch": {
    "work_mode": "HYBRID",
    "locations": ["Bangalore"]
  },
  "complete": false,
  "missing": ["priority"],
  "warnings": []
}
```

| Field | Meaning |
|---|---|
| `reply` | Natural-language assistant turn. FE appends to `messages`. |
| `patch` | Sparse object — only fields the LLM updated this turn. May be `{}`. |
| `complete` | LLM thinks the draft has enough fields to commit. FE enables "Create Draft Intent". |
| `missing` | Required fields still empty. FE may surface as chips. |
| `warnings` | Non-fatal validation drops, e.g. `["dropped invalid work_mode 'flexible'"]`. Rendered as soft amber note in chat. |

### Errors

Standard handler envelope (`render` style). Codes:

- `400 bad_request` — malformed body.
- `400 message_too_long` — `user_message` > 4000 chars.
- `401 unauthorized` — missing/invalid JWT (handled by middleware).
- `503 llm_unavailable` — upstream Anthropic call failed after retries / timed out.
- `500 internal_error` — anything else.

## LLM contract

### System prompt (versioned constant in `anthropic_extractor.go`)

```
You are an assistant helping a recruiter capture a structured hiring intent
through conversation. The recruiter will describe a role in their own words.

Your job each turn:
1. Read the current draft and the conversation so far.
2. Extract any new information from the recruiter's latest message.
3. Call the propose_draft tool with ONLY the fields you are updating this
   turn. Do not echo unchanged fields. Use {} if the recruiter is asking a
   question or you have nothing to update.
4. After the tool call, write a brief, friendly reply (1-2 sentences). If
   any required field is still missing, ask about ONE missing field — do
   not list everything at once.

Required fields for a complete draft: role_title, at least one required
skill, min_years, max_years, headcount, work_mode, priority.

Constraints:
- Headcount must be a positive integer.
- min_years <= max_years; both >= 0.
- work_mode is one of: ONSITE, REMOTE, HYBRID.
- priority is one of: LOW, MEDIUM, HIGH, CRITICAL.
- Never invent details the recruiter has not said. If unsure, ask.
```

### Tool definition

```json
{
  "name": "propose_draft",
  "description": "Propose changes to the hiring intent draft this turn.",
  "input_schema": {
    "type": "object",
    "properties": {
      "role_title":  { "type": "string" },
      "skills":      { "type": "array", "items": {
        "type": "object",
        "properties": {
          "name":     { "type": "string" },
          "required": { "type": "boolean" }
        },
        "required": ["name", "required"]
      }},
      "min_years": { "type": "integer", "minimum": 0 },
      "max_years": { "type": "integer", "minimum": 0 },
      "headcount": { "type": "integer", "minimum": 1 },
      "locations": { "type": "array", "items": { "type": "string" } },
      "work_mode": { "type": "string", "enum": ["ONSITE","REMOTE","HYBRID"] },
      "priority":  { "type": "string", "enum": ["LOW","MEDIUM","HIGH","CRITICAL"] },
      "complete":  { "type": "boolean" },
      "missing":   { "type": "array", "items": { "type": "string" } }
    }
  }
}
```

### Model & caching

- Model: `claude-sonnet-4-6`. Configurable via `ANTHROPIC_MODEL` env.
- `cache_control: {"type": "ephemeral"}` on the system block to cache prompt + tool def. Drops cost ~80% on warm chat traffic.
- Anthropic SDK auto-retries handle transient 5xx; one extra retry with exponential backoff.

### Server-side validation (in `ExtractIntentHandler`)

After tool result returns, before responding:

| Rule | Action |
|---|---|
| `min_years > max_years` | Drop both fields from patch; add warning. |
| `work_mode` outside enum | Drop field; add warning. |
| `priority` outside enum | Drop field; add warning. |
| `skills[].name` empty | Drop that skill entry. |
| `headcount <= 0` | Drop field; add warning. |
| `reply` empty | Substitute `"Got it."`. |

The schema should prevent most of these — validation is defense-in-depth.

## FE wiring

`IntentCapturePage.tsx` changes:

- Remove hardcoded `initialDraft`. Start with empty draft.
- `sendMessage` becomes a TanStack `useMutation` calling `intentApi.extract`.
- On success: append `reply` to `messages`; merge `patch` into `draft`; if `complete`, enable the "Create Draft Intent" button.
- `warnings` render as small `text-amber-600` chip under the assistant bubble.
- Pending state: three-dot pulse where the assistant bubble would go.
- 503 `llm_unavailable`: non-blocking banner "AI is offline — you can still edit the form". `DraftForm` (existing) remains visible and editable throughout, so the recruiter is never blocked by AI failure.

The form and chat are both always editable; chat is an accelerator, not a gate.

## Errors, guardrails, cost

| Concern | Mechanism |
|---|---|
| Anthropic call timeout | 30s on SDK call, 35s on HTTP handler. |
| Retries | SDK default + 1 extra on 5xx, exponential backoff. |
| Input token blow-up | Truncate `messages` to last 20 turns server-side; log when truncation fires. |
| Abuse / large input | Reject `user_message > 4000 chars` with `400 message_too_long`. |
| Missing API key | Fatal at startup (matches `JWT_ACCESS_SECRET` handling). |
| Logging | request_id, tenant_id, recruiter_id, token counts, latency. **Never** message content. |
| Per-tenant rate limit | Deferred to §H. With caching + Sonnet pricing, a tenant burning $1/day requires sustained traffic for hours; observability will surface real abuse. |

## Testing

| Layer | What |
|---|---|
| Domain | None — port is an interface. |
| Application | `ExtractIntentHandler` with fake `IntentExtractor`. Cover: empty patch, valid patch, invalid `work_mode` → warning, invalid `min/max` → warning, empty skill name dropped, empty `reply` → "Got it." |
| Infrastructure | `anthropicExtractor` with fake HTTP transport (`option.WithHTTPClient`). Verify request body shape (system prompt + tool def present, message mapping correct) and tool-use response decodes into port return type. |
| Delivery | Handler test using `httptest` and a fake handler. Verify auth claims used, request validation, error mapping. |
| FE | Match existing test scaffolding (none observed); add `useExtract` hook test if scaffolding exists. |
| Integration | Skip live LLM in CI. Fake-transport unit test covers the wire. |

## Open questions / follow-ups

- **OpenAPI codegen** — currently `web/src/api/types.ts` is hand-mirrored. Adding the extract types will widen the gap; codegen is already a §G item.
- **Conversation persistence** — if recruiters complain about losing sessions, upgrade to D3 option (B) — server-side `ExtractionSession` aggregate.
- **Streaming** — once chat-volume usage is real, evaluate SSE for the reply. The non-streaming response shape is forward-compatible.
- **Sourcing/interview LLM use** — they will define their own ports. The shared `anthropic.Client` is the only thing reused.

## Build sequence

1. `internal/shared/infrastructure/llm/anthropic/client.go` + test (config + thin wrapper).
2. `internal/hiringintent/domain/services/intent_extractor.go` (port + DTOs).
3. `internal/hiringintent/application/commands/extract_intent.go` + test (orchestration + validation, against a fake port).
4. `internal/hiringintent/infrastructure/llm/anthropic_extractor.go` + test (real port impl, fake HTTP transport).
5. `internal/hiringintent/delivery/http/v1/` — wire route + handler.
6. `cmd/api/main.go` — instantiate + inject.
7. `docs/api/v1/intents-extract.yaml` — OpenAPI.
8. FE: `web/src/api/types.ts` + `extract.ts` + `IntentCapturePage.tsx`.
9. Manual smoke: real key, real chat, observe logs.
10. Update `plan/01-hireflow.md` — check off §C "LLM extraction" + §F "Real LLM intent extraction".

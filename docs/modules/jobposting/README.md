# jobposting — bounded context

Owns the published JD lifecycle and source distribution for confirmed hiring intents. Postings are *created* in response to `hiringintent.IntentConfirmed` events, not via direct HTTP — recruiters then publish, amend, or close them.

## Ubiquitous language

| Term | Meaning |
|---|---|
| **Job Posting** | A draft or published JD derived from one confirmed Hiring Intent. Has a lifecycle. |
| **JD Content** | The job description payload (title, summary, responsibilities, requirements). Versioned. |
| **Source Target** | One distribution channel a posting is pushed to (LinkedIn / Career Page / Email / Internal DB). |
| **Channel** | The kind of source target: `LINKEDIN`, `CAREER_PAGE`, `EMAIL`, `INTERNAL_DB`. |

## Aggregate

`JobPosting` is the single aggregate root. Owns JD content + sources + lifecycle. Holds an opaque `intent_id` back-reference (string, not a typed VO — different bounded contexts must not share VOs).

## Lifecycle

```
Draft → Published → (Closed | Archived)
```

| State | Meaning | Mutations allowed |
|---|---|---|
| `Draft` | Created from a confirmed intent. JD editable. | Amend JD, publish, close |
| `Published` | Distributed to one or more channels. | Amend JD (bumps version), publish to more channels, close |
| `Closed` | Position filled or cancelled. Terminal. | None |
| `Archived` | Hidden but retained for analytics. Terminal. | None |

## Invariants

1. `intent_id` is required and immutable
2. JD title and summary are non-empty; version >= 1
3. Publish requires at least one channel
4. No mutations after terminal state
5. **One posting per (tenant, intent)** — enforced at DB level (`job_postings_tenant_intent_unique`) to make the IntentConfirmed consumer idempotent

## Domain events emitted

| Event | When | Consumed by |
|---|---|---|
| `JobPostingCreated` | After `NewJobPosting()` | Audit log, analytics |
| `JobPostingPublished` | After `Publish()` | `sourcing` (warms adapters for the chosen channels) |
| `JobPostingClosed` | After `Close()` | `sourcing` (stops ingestion), audit log |

## Cross-context integration

**Inbound** — subscribes to `hiringintent.IntentConfirmed`:
- Consumer: `infrastructure/subscribers/IntentConfirmedConsumer`
- Anti-corruption: `IntentReader` port at `infrastructure/subscribers/intent_confirmed.go`; production implementation at `infrastructure/clients/intent_reader.go` projects `hiringintent.IntentDTO → IntentSnapshot` and rejects non-CONFIRMED reads with `ErrIntentNotConfirmed`.
- Wiring: `cmd/api/main.go` registers the consumer against `internal/shared/infrastructure/eventbus.InMemory` for the `hiringintent.IntentConfirmed` event name. Both contexts run an outbox dispatcher that publishes into the same bus.
- Idempotent via `job_postings_tenant_intent_unique` and `jobposting_processed_events`

**Outbound** — `JobPostingCreated`/`Published`/`Closed` events are written to `job_posting_outbox` and drained by this context's own dispatcher (`infrastructure/messaging/outbox_dispatcher.go`). No subscribers yet — the dispatcher exists for parity and future-proofing.

## Flows

See [`flows/`](./flows/):
- `create_from_intent.mermaid` — IntentConfirmed → posting drafted
- `publish_posting.mermaid` — recruiter publishes to channels
- `close_posting.mermaid` — recruiter closes a posting

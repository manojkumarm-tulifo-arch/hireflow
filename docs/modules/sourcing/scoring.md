# Sourcing — Match Scoring Reference

Detailed reference for how Candidate × Intent scoring works in the `sourcing`
bounded context. Slice 3 implements stages 0–2; slice 4 layers the recruiter
lifecycle on top.

> **Where this fits:** The scoring story is the architectural choice that lets
> the platform meet the unit-economics target in the Tulifo pitch. A naive
> "ask the LLM to judge every (candidate, intent) pair" would cost ~$30K
> per intent at a million-candidate scale and take days. The two-stage funnel
> below bounds LLM cost at `top_k × intents`, not `candidates × intents` —
> which is what makes scoring economically viable.

---

## 1. The pieces

| Piece | Type | Produced when | Stored where |
|---|---|---|---|
| **Candidate profile embedding** `c⃗` | 1024-dim float vector (`pgvector`) | On `CandidateParsed` | `candidates.profile_embedding` |
| **Role embedding** `r⃗` | 1024-dim float vector | On `IntentConfirmed` (and on re-confirm — `spec_version` bumps) | `hiring_intent_embeddings.role_embedding`, keyed by `(intent_id, spec_version)` |
| **Rule match report** | Structured per-criterion pass/fail JSON | Computed on demand for each `(Candidate, Intent)` pair | `applications.rule_match` (jsonb) |
| **Embedding score** | Cosine similarity, range `[-1, 1]` (in practice `[0.3, 0.95]`) | Stage 1 of scoring | `applications.embedding_score` (numeric) |
| **LLM judgment** | `{score, evidence[], summary, concerns[]}` | Stage 2 — top-K only | `applications.llm_judgment` (jsonb) + `applications.overall_score` |

The two embeddings live in **different** tables because they have different
lifecycles. A candidate's embedding stays the same once computed (only changes
if the parsed profile schema bumps). A role's embedding is keyed by
`spec_version` — if the recruiter re-confirms the intent with a changed
`RoleSpec`, the old embedding becomes stale and a new one is computed.

---

## 2. Embedding semantics

The Voyage AI `voyage-3` model is trained so that **semantically similar text
produces vectors that point in similar directions** in 1024-dimensional space.

Two important properties:

**1. Distance is meaning.** Two profile texts that share *intent* — "Senior
Backend Engineer with 5y Go" and "Backend Lead with deep Golang background" —
produce vectors close to each other even though they share few exact words.
Conversely, an "ML Engineer" profile produces a vector far from a "Frontend
Engineer" profile, regardless of skill-keyword overlap.

**2. Direction is recoverable.** Cosine similarity (the angle between vectors)
is dimensionless. Magnitudes vary by text length; angles don't. That's why we
use cosine, not Euclidean distance.

### Serializing for the embedding

We don't send the raw `ParsedProfile` JSON. We construct a representative
single-string projection:

```
{headline} | {summary} |
Skills: {skill_names joined ", "} |
Experience: {company1} {title1} {start1}-{end1}: {description1} | …
```

Two things this projection deliberately includes:
- **Skills** — the model has strong inductive bias toward skill keywords;
  including them helps the embedding capture "what this person does."
- **Experience descriptions** — the model picks up domain context (e.g.
  "payments platform" vs "video pipeline") that pure skill lists miss.

Two things it deliberately omits:
- **PII** — `full_name`, `email`, `phone`. These add noise (and are encrypted
  on the row anyway). Embeddings are stored cleartext.
- **Education** — for v1, we trust experience signal more than degree signal.
  Education shows up in rule match if the intent specifies a degree floor.

Same serialization shape on the intent side, except built from the `RoleSpec`:

```
{role_title} |
Required skills: {required_skill_names} |
Optional skills: {optional_skill_names} |
Experience range: {min_years}-{max_years} years |
{work_mode} role in {locations joined ", "}
```

The symmetry matters: candidate vector and role vector live in the same space
because they encode the same kind of information ("who is this/what kind of
work is this").

---

## 3. Stage 1 — Coarse score (every Application)

When we need to score a `(Candidate, Intent)` pair, we compute three things in
order. The first one is a gate.

### 3.1 Rule match (the gate)

For each criterion in the `RoleSpec`, produce a pass/fail with evidence:

```json
{
  "rule_match": [
    { "criterion": "skill:Go", "required": true, "passed": true, "evidence_ref": "exp_0" },
    { "criterion": "skill:Kubernetes", "required": false, "passed": false },
    { "criterion": "experience:5-10y", "required": true, "passed": true, "actual": "6.8y" },
    { "criterion": "location:Bangalore|remote", "required": false, "passed": true }
  ]
}
```

**If any *required* criterion fails, the Application is set to status
`Excluded`. No embedding cost. No LLM cost.** This is the cheapest possible
short-circuit and the largest source of efficiency at scale — a recruiter who
specifies "5 years Go required" filters out millions of irrelevant candidates
in deterministic Go code, not in expensive LLM calls.

Criterion types in v1:

| Criterion | How it's computed |
|---|---|
| `skill:<name>` | Profile's `skills[].name` contains a case-insensitive match. Optional `years` threshold checked against `skill.years` |
| `experience:Xy-Yy` | Sum of `(exp.end - exp.start)` for all experiences yields total years; compared to range |
| `location:<csv>` | Profile's `personal.location` matches any location in the intent, OR `work_mode=remote` |
| `education:<degree>` | Any `education[].degree` matches (rare in v1) |
| `language:<name>` | Profile's `languages[]` contains the language |

### 3.2 Cosine similarity (the score)

For Applications that pass the rule gate, we compute:

```
cos(c⃗, r⃗) = (c⃗ · r⃗) / (‖c⃗‖ · ‖r⃗‖)
```

In pgvector this is one operator:

```sql
SELECT 1 - (c.profile_embedding <=> hie.role_embedding) AS embedding_score
FROM candidates c, hiring_intent_embeddings hie
WHERE c.id = $1 AND hie.intent_id = $2 AND hie.spec_version = $3;
```

(`<=>` is pgvector's cosine *distance*; `1 - <=>` gives cosine *similarity*
in `[-1, 1]`. Resume-vs-role text typically lives in `[0.3, 0.95]`.)

The score lands on `applications.embedding_score` as a `numeric(5,4)`.

### 3.3 Coarse rank (the tiebreaker)

For ranking Applications *within an intent* (e.g., to pick top-K for the LLM
judge), we combine rule signals with embedding similarity:

```
coarse_score = required_pass_rate × 100 + embedding_score × 20
```

Where `required_pass_rate` ∈ `[0, 1]` is the fraction of required criteria
passed. Examples:

| Profile | required_pass_rate | embedding_score | coarse_score |
|---|---|---|---|
| Passes 3/3 required, cos 0.85 | 1.0 | 0.85 | 100 + 17 = **117** |
| Passes 3/3 required, cos 0.55 | 1.0 | 0.55 | 100 + 11 = **111** |
| Passes 2/3 required, cos 0.95 | 0.67 | 0.95 | 67 + 19 = **86** |
| Passes 1/3 required, cos 0.92 | 0.33 | 0.92 | 33 + 18 = **51** |

The shape encodes a value judgment:
- **Rule-pass-rate is the dominant signal.** A candidate who passes all
  required skills beats one who is semantically very similar but missing a
  required skill — even if the missing skill is borderline.
- **Embedding is the tiebreaker.** Within a rule-tier, the candidate whose
  profile reads more like the role wins.

The constants `100` and `20` are tunable but live in code (`internal/sourcing/
domain/services/match_scorer.go`) for v1 — they're balanced so embedding
similarity is meaningful within a rule-tier but never enough to leap-frog a
missing required skill. If we ever want per-tenant tuning (some tenants
weight rules harder, some weight semantics harder), it becomes a tenant-level
config in slice 4+.

> **Why required-pass-rate not just a boolean "passes all required"?**
> Partial credit on required criteria preserves ordering information: a
> candidate who passes 2/3 required is ranked above one who passes 1/3, even
> though both are below top-K and won't be judged. This makes the
> "Excluded vs. just below the cutoff" call cleaner.

---

## 4. Stage 2 — LLM judge (top-K only, default K=20)

Coarse score sorts the Applications. The top K rows per intent go to Claude
via forced tool-use against a `judge_match` schema. The prompt sees:
- The parsed profile (PII-stripped — `personal.*` removed before the call)
- The `RoleSpec`
- The `rule_match` summary

The judge returns a structured verdict:

```json
{
  "score": 87,                                       // 0–100
  "evidence": [
    { "skill": "Go", "claim": "5 years",
      "support": "Senior Backend at Razorpay 2020–2025 — 4.8 years" },
    { "experience": "payments",
      "support": "Razorpay (payments) + PayU (payments)" }
  ],
  "summary": "Strong Go background with deep payments-domain experience. Two consecutive senior backend roles in payment companies suggest stable career trajectory.",
  "concerns": [
    "Career gap 2018–2019 (1 year) not explained",
    "Kubernetes claimed in skills but not visible in any role description"
  ]
}
```

The judge is doing what an experienced recruiter does:
1. Read the actual experience prose, not just skill keywords.
2. Cross-reference skill claims against the supporting work history.
3. Look at career trajectory, gaps, role progression.
4. Flag inconsistencies the structured parse can't catch.

`overall_score` on the Application is set to `llm_judgment.score`. `score_band`
is derived from it:

| Band | Threshold |
|---|---|
| `strong` | `overall_score >= 80` |
| `moderate` | `60 <= overall_score < 80` |
| `weak` | `overall_score < 60` |

Applications below top-K stay in status `Scored` but without an
`overall_score` — only the coarse rank and per-criterion rule chips show up
for them in the UI. Recruiter can manually request a judge run on any
non-top-K Application via the slice-4 rescore endpoint.

### Why K=20 by default

- A typical recruiter reviews ~10–30 candidates per intent before making
  shortlist calls. Judging the top 20 covers this with one Application's
  headroom.
- LLM cost is bounded: 20 candidates × $0.03 = $0.60 per intent per scoring
  run. At Tulifo's pre-seed scale (10 customers × 30 roles/month × 1 rescore
  per role) = ~$180/mo. Negligible.
- K is tunable via the `SOURCING_JUDGE_TOP_K` env var. A future per-tenant
  setting can override it.

---

## 5. Caching and invalidation

LLM calls are the expensive surface; we cache aggressively.

### Role embedding cache

Keyed by `(intent_id, spec_version)`. Re-confirming an unchanged intent reuses
the embedding. Modifying the `RoleSpec` and re-confirming bumps `spec_version`
and triggers a fresh embed.

### Candidate embedding cache

One per `Candidate`. Stored on `candidates.profile_embedding`. Recomputed only
when `parsed_profile.schema_version` changes (i.e., a future slice migrates
profiles to a new schema). In v1, this never happens.

### LLM judgment cache

Keyed by `(candidate_id, intent_id, intent_spec_version, profile_schema_version,
prompt_version)`. All five must match for a cached judgment to be reused. Bump
any of them and the judge re-runs:

- `intent_spec_version` — recruiter changed the role description.
- `profile_schema_version` — `ParsedProfile` schema migrated.
- `prompt_version` — we changed `judge_match.tmpl` (the prompt the judge
  reads). Stored on each judgment row so historical scores stay reproducible.

Re-confirming an intent that wasn't actually modified produces no LLM cost.

---

## 6. Score recomputation triggers

Two events trigger re-scoring; nothing else does:

### `IntentConfirmed` (fired by `hiringintent`)

The `sourcing` context's `IntentConfirmedConsumer` runs:
1. Compute or reuse role embedding for `(intent_id, current_spec_version)`.
2. For every `Candidate` in the tenant whose `parsed_profile.schema_version`
   is current, **upsert** an `Application` row.
3. Compute `rule_match` + `embedding_score` per Application.
4. Sort by coarse score, enqueue top-K into `judge_jobs` table.
5. The judge worker picks them up async and writes `llm_judgment` +
   `overall_score`.

### Explicit `rescore` (slice-4 endpoint)

`POST /api/v1/intents/{intent_id}/applications:rescore` does the same as
`IntentConfirmed` but on demand. Used by the recruiter when they:
- Add a candidate manually that wasn't auto-matched.
- Want to re-judge a non-top-K candidate.
- Suspect the model has improved since last judgment.

### Things that *don't* trigger rescoring (slice 3 + 4)

- Drafted-intent edits. Only `IntentConfirmed` matters.
- Status changes on the upload (e.g., quarantine). The upload pipeline
  doesn't feed into the scoring pipeline.
- Tenant-level config changes (since v1 has no tenant-level config that
  affects scoring).

---

## 7. End-to-end cost shape

| Step | Cost per unit | Frequency |
|---|---|---|
| Profile embed (Voyage) | ~$0.0001 / candidate | Once per candidate |
| Role embed (Voyage) | ~$0.0001 / intent | Once per `spec_version` |
| Rule match | ~free (compute in Go) | Per Application |
| Cosine similarity | ~free (pgvector) | Per Application |
| LLM judge (Claude tool-use) | ~$0.03 / (candidate, intent) | Top-K only |

For Tulifo's pre-seed target (10 customers, 1K interviews/mo):
- ~10K resumes uploaded/mo → 10K embeddings = $1/mo on embeddings
- ~300 intents/mo × 20 judge calls = $180/mo on judging
- Total scoring cost / mo ≈ **$181** at full scale

For comparison, the same workload "judge every pair" approach would be:
- 10K candidates × 300 intents = 3M judge calls = **$90,000 / mo**

The 500× cost reduction is the funnel.

---

## 8. What slice 3 actually ships

Bounded scope to keep the slice ship-ready:

- ✅ `Embedder` port + Voyage adapter + stub for tests
- ✅ `MatchScorer` (rule engine + cosine) — pure-Go, no I/O
- ✅ `LLMJudge` port + Claude adapter
- ✅ `applications` table (partition-ready by `tenant_id`)
- ✅ `hiring_intent_embeddings` table
- ✅ `judge_jobs` table (in-process queue for top-K LLM judging)
- ✅ `IntentConfirmedConsumer` (fan-out on confirm)
- ✅ `CandidateParsedConsumer` (fan-out on parse)
- ✅ Match worker (per-Application embedding + rule + cosine)
- ✅ Judge worker (top-K LLM judging)
- ✅ `GET /api/v1/intents/{id}/applications` (read-only list)

Not in slice 3 (deferred to slice 4):
- ❌ `POST .../rescore` endpoint
- ❌ Application lifecycle actions (`shortlist`/`reject`/`hire`)
- ❌ Candidate detail at scale (already shipped in slice 2)
- ❌ SSE for live score updates
- ❌ Per-tenant scoring config

---

## Appendix — Cosine similarity refresher

For two vectors `a` and `b` in `R^n`:

```
a · b = Σ aᵢ × bᵢ           (dot product, scalar)
‖a‖   = √(Σ aᵢ²)            (L2 norm, scalar)

cos(a, b) = (a · b) / (‖a‖ × ‖b‖)
```

- Range: `[-1, 1]` for arbitrary vectors. For non-negative-valued embeddings
  (most are not, but Voyage's are mixed-sign), typical range for related text
  is `[0.3, 0.95]`.
- `1.0` means identical direction (same content).
- `0.0` means orthogonal (unrelated content).
- `-1.0` means opposite direction (rare in text embeddings).

The geometry: embedding similarity is the angle between the vectors, not the
distance. Two vectors of very different lengths but the same direction have
cosine similarity 1. That's why we use cosine for text embeddings — text
length shouldn't matter; topical direction should.

In pgvector:
- `<->` is L2 (Euclidean) distance
- `<#>` is negative inner product
- `<=>` is cosine *distance* — i.e., `1 - cos(a, b)`

So `1 - (a <=> b)` gives cosine *similarity* directly, range `[-1, 1]`.

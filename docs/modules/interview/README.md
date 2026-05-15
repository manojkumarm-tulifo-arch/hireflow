# interview — bounded context

Slice 1: a new context that takes over after sourcing produces a shortlist.
Subscribes to `sourcing.ApplicationShortlisted`, creates an `InterviewProcess`
with rounds from a per-intent `LoopTemplate` (or a hardcoded default), and
asynchronously generates structured interview questions per round.

## Ubiquitous language

| Term | Meaning |
|---|---|
| **InterviewProcess** | The aggregate created for one shortlisted application. Owns its rounds. |
| **InterviewRound** | One slot in the loop (e.g., "Senior Backend, screen, sequence 1"). State machine driven. |
| **LoopTemplate** | Per-intent definition of the rounds an `InterviewProcess` inherits when created. |
| **Question** | One LLM-generated probe carrying prompt, skill_probed, why, expected_signals, model_answer, red_flags, follow_ups. |
| **Feedback** | One recruiter-entered scorecard per (round, interviewer). Append-only. |
| **RoundKind** | Fixed enum: screen, technical, system_design, behavioral, bar_raiser. |
| **FeedbackDecision** | Fixed enum: strong_yes, yes, mixed, no, strong_no. |

## Pipeline

```
ApplicationShortlisted ──► ApplicationShortlistedConsumer ──► StartInterviewProcess
                                                                       │
                                                                       ▼
                                                        InterviewProcess(rounds: Pending)
                                                                       │
                                          QuestionGenerationPool ──────┘
                                                  │
                                  per round, claim ─►
                                                  │
                          IntentReader + CandidateReader + AnthropicQuestionGenerator
                                                  │
                                                  ▼
                                    InterviewRound: QuestionsReady
                                                  │
                              recruiter conducts the round (out-of-band)
                                                  │
                                  POST /interview/rounds/{id}/feedback ─► append row
                                                  │
                                POST /interview/rounds/{id}:mark-done ─► Completed
                                                  │
                  (after all rounds Completed or Skipped)
                                                  │
                          POST /interview/processes/{id}:complete ─► Process: Completed
```

## Endpoints (slice 1)

| Method | Path | Purpose |
|---|---|---|
| PUT | `/intents/{intent_id}/loop-template` | Upsert a per-intent loop. |
| GET | `/intents/{intent_id}/loop-template` | Return the template or default. |
| GET | `/intents/{intent_id}/interview-processes` | List processes for an intent. |
| GET | `/interview/processes/{id}` | Get process + rounds + feedback summary. |
| POST | `/interview/processes/{id}:complete` | Complete the process. |
| POST | `/interview/processes/{id}:cancel` | Cancel the process. |
| POST | `/interview/rounds/{id}/feedback` | Append feedback. |
| POST | `/interview/rounds/{id}:regenerate` | Reset round → Pending for re-generation. |
| POST | `/interview/rounds/{id}:mark-done` | Round → Completed. |
| POST | `/interview/rounds/{id}:skip` | Round → Skipped. |

## Env vars

| Var | Default | Purpose |
|---|---|---|
| `INTERVIEW_QGEN_POOL` | `2` | Worker pool size for question generation. |
| `INTERVIEW_QGEN_POLL` | `1s` | Worker poll interval. |
| `ANTHROPIC_API_KEY` | — | Shared with sourcing/hiringintent. Required. |
| `ANTHROPIC_MODEL` | `claude-opus-4-7` | Used by the question generator. |

## Out of scope

See `docs/superpowers/specs/2026-05-14-interview-slice-1-question-generation-design.md` §Out of scope.

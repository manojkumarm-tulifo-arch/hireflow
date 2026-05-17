// Hand-mirrored from docs/api/v1/{hiringintent,jobposting}.openapi.yaml
// TODO: replace with `openapi-typescript` generation when stable.

export type IntentStatus = 'DRAFTED' | 'CONFIRMED' | 'CANCELLED' | 'CLOSED';
export type Priority = 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';
export type WorkMode = 'ONSITE' | 'REMOTE' | 'HYBRID';
export type SignalLevel = 'LOW' | 'MEDIUM' | 'HIGH';

export interface Skill { name: string; required: boolean }
export interface ExperienceRange { min_years: number; max_years: number }
export interface RoleSpec {
  title: string;
  skills: Skill[];
  experience: ExperienceRange;
  headcount: number;
  locations: string[];
  work_mode: WorkMode;
}
export interface IntentSignal { label: string; value: string; level: SignalLevel }
export interface TrustSignal { label: string; value: string; required: boolean }
export interface Budget { min_minor: number; max_minor: number; currency: string }

export interface Intent {
  id: string;
  tenant_id: string;
  recruiter_id: string;
  role: RoleSpec;
  priority: Priority;
  intent_signals: IntentSignal[];
  trust_signals: TrustSignal[];
  budget?: Budget;
  reason?: string;
  team?: string;
  reports_to?: string;
  status: IntentStatus;
  created_at: string;
  updated_at: string;
  confirmed_at?: string | null;
  cancelled_at?: string | null;
  cancel_reason?: string;
}

export interface CreateIntentRequest {
  role_title: string;
  skills: Skill[];
  min_years: number;
  max_years: number;
  headcount: number;
  locations: string[];
  work_mode: WorkMode;
  priority: Priority;
  budget?: Budget;
  reason?: string;
  team?: string;
  reports_to?: string;
}

// Extraction (LLM-driven intent capture). The chat is stateless on the
// server; the client passes the prior history + current draft on every turn.
export interface ExtractMessage {
  role: 'user' | 'assistant';
  text: string;
}

export interface ExtractDraft {
  role_title?: string;
  skills?: Skill[];
  min_years?: number;
  max_years?: number;
  headcount?: number;
  locations?: string[];
  work_mode?: WorkMode | '';
  priority?: Priority | '';
  budget?: Budget;
  reason?: string;
  team?: string;
  reports_to?: string;
}

export interface ExtractRequest {
  messages: ExtractMessage[];
  draft: ExtractDraft;
  user_message: string;
}

// DraftPatch fields are optional on every turn — only the fields the LLM
// updated this turn appear. Empty arrays for skills/locations also possible.
export interface DraftPatch {
  role_title?: string;
  skills?: Skill[];
  min_years?: number;
  max_years?: number;
  headcount?: number;
  locations?: string[];
  work_mode?: WorkMode;
  priority?: Priority;
  budget?: Budget;
  reason?: string;
  team?: string;
  reports_to?: string;
}

export interface ExtractResponse {
  reply: string;
  patch: DraftPatch;
  complete: boolean;
  missing?: string[];
  warnings?: string[];
}

export type IntentSortOrder = 'NEWEST' | 'URGENT';

export interface IntentListFilter {
  status?: IntentStatus;
  recruiter_id?: string;
  q?: string;
  sort?: IntentSortOrder;
  limit?: number;
  offset?: number;
}

export interface IntentStatusCounts {
  DRAFTED: number;
  CONFIRMED: number;
  CANCELLED: number;
  CLOSED: number;
  total: number;
}

export interface IntentSummary {
  counts: IntentStatusCounts;
}

// jobposting
export type PostingStatus = 'DRAFT' | 'PUBLISHED' | 'CLOSED' | 'ARCHIVED';
export type SourceChannel = 'LINKEDIN' | 'CAREER_PAGE' | 'EMAIL' | 'INTERNAL_DB';
export type SourceStatus = 'PENDING' | 'ACTIVE' | 'FAILED' | 'DISABLED';

export interface JD {
  title: string;
  summary: string;
  responsibilities: string[];
  requirements: string[];
  version: number;
}
export interface SourceTarget {
  channel: SourceChannel;
  status: SourceStatus;
  external_id?: string;
  url?: string;
  last_sync?: string | null;
}
export interface Posting {
  id: string;
  tenant_id: string;
  intent_id: string;
  jd: JD;
  sources: SourceTarget[];
  status: PostingStatus;
  created_at: string;
  updated_at: string;
  published_at?: string | null;
  closed_at?: string | null;
  close_reason?: string;
}

// Standard envelope from the Go API.
export interface Envelope<T> {
  success: boolean;
  data?: T;
  error?: { code: string; message: string };
}

// ============================================================================
// Sourcing context — slice 5
// ============================================================================

export type BatchUploadOutcomeStatus =
  | 'queued'
  | 'deduplicated'           // legacy; kept for old callers
  | 'duplicate_in_intent'    // new in slice 5
  | 'extracted_from_zip'     // new in slice 5 — ZIP parent marker
  | ''                       // empty when item rejected (see `error`)

export interface BatchUploadOutcome {
  filename: string
  status: BatchUploadOutcomeStatus
  upload_id?: string
  candidate_id?: string
  parent_filename?: string
  parent_item_id?: string
  error?: { code: string; message: string; detail?: Record<string, unknown> }
}

export interface BatchUploadResponse {
  batch_id: string
  items: BatchUploadOutcome[]
}

export interface BatchStatusItem {
  upload_id: string
  filename: string
  status: 'Pending' | 'Scanning' | 'Extracting' | 'Extracted' | 'Parsing' | 'Parsed' | 'Failed' | 'Quarantined'
  attempt: number
  last_error: string
}

export interface BatchStatusResponse {
  batch_id: string
  intent_id: string
  summary: {
    total: number
    in_flight: number
    extracted: number
    failed: number
    quarantined: number
  }
  items: BatchStatusItem[]
}

export type ApplicationStatus =
  | 'New'
  | 'Scored'
  | 'Excluded'
  | 'EmbedFailed'
  | 'JudgeFailed'
  | 'Stale'
  | 'Shortlisted'
  | 'Interviewing'
  | 'Rejected'
  | 'Hired'

export interface SkillSummary {
  name: string
  years?: number
}

export interface CandidateSummary {
  full_name: string
  headline: string
  location: string
  top_skills: SkillSummary[]
  judge_summary: string
}

export interface Application {
  id: string
  candidate_id: string
  intent_id: string
  status: ApplicationStatus
  overall_score: number | null
  score_band: 'strong' | 'moderate' | 'weak' | null
  candidate: CandidateSummary
  created_at: string
  updated_at: string
}

export interface ApplicationListResponse {
  applications: Application[]
  total: number
}

export interface CandidateDetail {
  id: string
  content_hash: string
  personal: { full_name: string; email: string; phone: string }
  location: string
  headline: string
  profile: Record<string, unknown>
  source: string
  created_at: string
}

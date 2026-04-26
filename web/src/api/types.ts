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

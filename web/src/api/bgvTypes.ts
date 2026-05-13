// Hand-mirrored from candidate-bgv/docs/api/v1/bgv.openapi.yaml.
// Kept in a separate file from types.ts because BGV is a sibling
// bounded context, not part of hireflow's own intent/posting domain —
// keeping the imports explicit makes that boundary visible.

export type BGVStatus =
  | 'INVITED'
  | 'IN_PROGRESS'
  | 'SUBMITTED'
  | 'UNDER_REVIEW'
  | 'VERIFIED'
  | 'FLAGGED';

export interface BGVCandidate {
  name: string;
  email: string;
  phone: string;
  position?: string;
  company?: string;
}

export interface BGVSubmissionListItem {
  id: string;
  tenant_id: string;
  candidate_id: string;
  invited_by: string;
  token: string;
  candidate: BGVCandidate;
  status: BGVStatus;
  created_at: string;
  updated_at: string;
  started_at?: string | null;
  submitted_at?: string | null;
  reviewed_at?: string | null;
}

export interface BGVSubmissionListPage {
  items: BGVSubmissionListItem[];
  total: number;
  limit: number;
  offset: number;
}

export interface BGVPersonalInfo {
  full_name: string;
  dob: string;
  gender: string;
  blood_group?: string;
  marital?: string;
  father_name: string;
  mother_name: string;
  nationality: string;
  languages?: string;
}

export interface BGVAddressLines {
  line1: string;
  line2?: string;
  city: string;
  state: string;
  pin: string;
  country: string;
}

export interface BGVAddress {
  current: BGVAddressLines;
  perm_same: boolean;
  permanent?: BGVAddressLines;
  duration: string;
}

export interface BGVEmergencyContact {
  name: string;
  relationship: string;
  phone: string;
  email?: string;
}

export interface BGVEmergencyContacts {
  primary: BGVEmergencyContact;
  secondary?: BGVEmergencyContact;
}

export interface BGVProfessionalReference {
  name: string;
  designation: string;
  company: string;
  phone: string;
  email: string;
  relationship: string;
}

export interface BGVReferences {
  first: BGVProfessionalReference;
  second: BGVProfessionalReference;
}

export interface BGVDigitalProfile {
  linkedin?: string;
  github?: string;
  portfolio?: string;
  memberships?: string;
}

export interface BGVDeclarations {
  bgv_consent: boolean;
  nda: boolean;
  dpdp_consent: boolean;
  code_of_conduct: boolean;
  criminal_declaration: boolean;
  signature: string;
}

export interface BGVFileRef {
  storage_key: string;
  mime_type: string;
  size_bytes: number;
  sha256: string;
}

export interface BGVDocument {
  id: string;
  label: string;
  desc: string;
  category: string;
  required: boolean;
  custom: boolean;
  status: 'PENDING' | 'CAPTURED';
  file?: BGVFileRef;
  updated_at: string;
}

export interface BGVSubmission {
  id: string;
  tenant_id: string;
  candidate_id: string;
  invited_by: string;
  token: string;
  candidate: BGVCandidate;
  status: BGVStatus;
  personal?: BGVPersonalInfo;
  address?: BGVAddress;
  emergency?: BGVEmergencyContacts;
  references?: BGVReferences;
  digital?: BGVDigitalProfile;
  declarations?: BGVDeclarations;
  documents: BGVDocument[];
  created_at: string;
  updated_at: string;
  started_at?: string | null;
  submitted_at?: string | null;
  reviewed_at?: string | null;
}

// Timeline entries carry an opaque payload (BE marks it `json.RawMessage`)
// so the FE can render different fields per `event_name` without needing
// an API change every time a new event ships. Type the known shapes here
// and fall through to `unknown` for the rest.
export interface BGVTimelineEntry {
  id: number;
  event_name: string;
  occurred_at: string;
  payload?: unknown;
}

export interface BGVTimeline {
  items: BGVTimelineEntry[];
}

// List filter — optional status + paging. Date-range and candidate
// filters exist on the BE but aren't surfaced in the v1 reviewer UI yet.
export interface BGVListFilter {
  status?: BGVStatus;
  limit?: number;
  offset?: number;
}

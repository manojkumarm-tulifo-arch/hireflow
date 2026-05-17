import { request, tokenStore } from './client';
import type {
  ApplicationListResponse,
  ApplicationStatus,
  BatchStatusResponse,
  BatchUploadResponse,
  CandidateDetail,
  Application,
} from './types';

/**
 * Upload a batch of resume files for a given intent.
 *
 * Uses raw fetch (not the `request` helper) because the payload is
 * multipart/form-data — the helper hard-codes JSON content-type when a body
 * is present.
 */
async function uploadBatch(
  intentId: string,
  files: File[],
): Promise<BatchUploadResponse> {
  const form = new FormData();
  for (const file of files) {
    form.append('resume', file, file.name);
  }

  const headers: Record<string, string> = {};
  const token = tokenStore.get();
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const res = await fetch(`/api/v1/intents/${intentId}/resumes:batch`, {
    method: 'POST',
    headers,
    body: form,
  });

  const envelope = await res.json();
  if (!res.ok || !envelope.success) {
    const code = envelope.error?.code ?? 'upload_error';
    const message = envelope.error?.message ?? `upload failed (${res.status})`;
    const { ApiError } = await import('./client');
    throw new ApiError(res.status, code, message);
  }
  return envelope.data as BatchUploadResponse;
}

/**
 * Returns an EventSource connected to the batch SSE stream.
 *
 * EventSource does not support custom headers, so the access token is passed
 * as a `token` query parameter. The server must accept this form for SSE
 * endpoints.
 */
function subscribeBatchEvents(batchId: string): EventSource {
  const token = tokenStore.get();
  const url = new URL(
    `/api/v1/resumes/batches/${batchId}/events`,
    window.location.origin,
  );
  if (token) url.searchParams.set('token', token);
  return new EventSource(url.toString());
}

export const sourcingApi = {
  /** Upload multiple resume files as a single batch for an intent. */
  uploadBatch,

  /** Poll the processing status for every item in a batch. */
  getBatchStatus: (batchId: string): Promise<BatchStatusResponse> =>
    request<BatchStatusResponse>(`/api/v1/resumes/batches/${batchId}`),

  /**
   * Subscribe to server-sent events for a batch.
   * Caller is responsible for calling `.close()` on the returned EventSource.
   */
  subscribeBatchEvents,

  /** List applications for an intent, with optional filtering and pagination. */
  listApplications: (
    intentId: string,
    filter?: { status?: ApplicationStatus; limit?: number; offset?: number },
  ): Promise<ApplicationListResponse> =>
    request<ApplicationListResponse>(`/api/v1/intents/${intentId}/applications`, {
      query: filter,
    }),

  /** Fetch full candidate profile. */
  getCandidate: (candidateId: string): Promise<CandidateDetail> =>
    request<CandidateDetail>(`/api/v1/candidates/${candidateId}`),

  /** Move an application to Shortlisted status. */
  shortlist: (applicationId: string): Promise<Application> =>
    request<Application>(`/api/v1/applications/${applicationId}:shortlist`, {
      method: 'POST',
    }),

  /** Reject an application with an optional reason. */
  reject: (applicationId: string, reason: string): Promise<Application> =>
    request<Application>(`/api/v1/applications/${applicationId}:reject`, {
      method: 'POST',
      body: { reason },
    }),

  /** Mark an application as hired. */
  hire: (applicationId: string): Promise<Application> =>
    request<Application>(`/api/v1/applications/${applicationId}:hire`, {
      method: 'POST',
    }),

  /** Retry a failed or quarantined upload item. */
  retryUpload: (uploadId: string): Promise<void> =>
    request<void>(`/api/v1/resumes/${uploadId}:retry`, { method: 'POST' }),
};

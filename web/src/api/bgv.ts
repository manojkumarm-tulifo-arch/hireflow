import { request, tokenStore, ApiError } from './client';
import type {
  BGVListFilter,
  BGVSubmission,
  BGVSubmissionListPage,
  BGVTimeline,
} from './bgvTypes';

/**
 * BGV reviewer-side API wrappers. Targets the candidate-bgv service
 * (default :8081 in dev, proxied via Vite). All calls require a
 * recruiter JWT — `request()` attaches the bearer automatically.
 */
export const bgvApi = {
  list: (filter?: BGVListFilter) =>
    request<BGVSubmissionListPage>('/api/v1/bgv', {
      query: filter as Record<string, string | number | undefined> | undefined,
    }),

  get: (id: string) => request<BGVSubmission>(`/api/v1/bgv/${id}`),

  timeline: (id: string) =>
    request<BGVTimeline>(`/api/v1/bgv/${id}/timeline`),

  reopen: (id: string, reason: string) =>
    request<BGVSubmission>(`/api/v1/bgv/${id}/reopen`, {
      method: 'POST',
      body: { reason },
    }),
};

/**
 * downloadDocument is a side-effecting helper: hits the protected file
 * endpoint with the bearer attached, follows the 302 → pre-signed URL
 * if the storage adapter signs (S3) or accepts the inline byte stream
 * if it doesn't (LocalFile), then triggers the browser save dialog.
 *
 * Why not a plain `<a href>`: we need the Authorization header on the
 * first hop. After fetch follows the cross-origin redirect to S3, the
 * browser strips the header, which is what we want — the pre-signed
 * URL is its own credential.
 */
export async function downloadDocument(
  submissionId: string,
  documentId: string,
  filenameHint: string,
): Promise<void> {
  const token = tokenStore.get();
  if (!token) throw new ApiError(401, 'no_token', 'sign in to download');

  const res = await fetch(
    `/api/v1/bgv/${submissionId}/documents/${documentId}/file`,
    { headers: { Authorization: `Bearer ${token}` } },
  );
  if (!res.ok) {
    let code = 'download_failed';
    let message = `download failed (${res.status})`;
    try {
      const env = await res.json();
      code = env?.error?.code ?? code;
      message = env?.error?.message ?? message;
    } catch {
      /* response wasn't JSON; keep defaults */
    }
    throw new ApiError(res.status, code, message);
  }

  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = sanitiseFilename(filenameHint, blob.type);
  document.body.appendChild(a);
  a.click();
  a.remove();
  // Defer revoke until after the browser has actually started the
  // download. A microtask isn't enough on some browsers; a small
  // setTimeout is the standard work-around.
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function sanitiseFilename(label: string, mime: string): string {
  const safe = label.replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '');
  if (/\.\w{1,8}$/.test(safe)) return safe; // already has an extension
  const ext = mimeToExt(mime);
  return ext ? `${safe}.${ext}` : safe;
}

function mimeToExt(mime: string): string | null {
  switch (mime) {
    case 'image/jpeg':
      return 'jpg';
    case 'image/png':
      return 'png';
    case 'image/webp':
      return 'webp';
    case 'application/pdf':
      return 'pdf';
    default:
      return null;
  }
}

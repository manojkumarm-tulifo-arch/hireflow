import { request } from './client';
import type { Posting, PostingStatus, SourceChannel } from './types';

export const postingApi = {
  list: (filter?: { status?: PostingStatus; intent_id?: string; limit?: number; offset?: number }) =>
    request<Posting[]>('/api/v1/postings', { query: filter }),

  get: (id: string) => request<Posting>(`/api/v1/postings/${id}`),

  publish: (id: string, channels: SourceChannel[]) =>
    request<Posting>(`/api/v1/postings/${id}/publish`, { method: 'POST', body: { channels } }),

  close: (id: string, reason: string) =>
    request<Posting>(`/api/v1/postings/${id}/close`, { method: 'POST', body: { reason } }),
};

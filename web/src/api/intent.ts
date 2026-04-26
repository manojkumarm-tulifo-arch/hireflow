import { request } from './client';
import type { CreateIntentRequest, Intent, IntentListFilter, IntentSummary } from './types';

export const intentApi = {
  list: (filter?: IntentListFilter) =>
    request<Intent[]>('/api/v1/intents', { query: filter as Record<string, string | number | undefined> | undefined }),

  summary: () => request<IntentSummary>('/api/v1/intents/summary'),

  get: (id: string) => request<Intent>(`/api/v1/intents/${id}`),

  create: (body: CreateIntentRequest) =>
    request<Intent>('/api/v1/intents', { method: 'POST', body }),

  confirm: (id: string) =>
    request<Intent>(`/api/v1/intents/${id}/confirm`, { method: 'POST' }),
};

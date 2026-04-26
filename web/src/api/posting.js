import { request } from './client';
export const postingApi = {
    list: (filter) => request('/api/v1/postings', { query: filter }),
    get: (id) => request(`/api/v1/postings/${id}`),
    publish: (id, channels) => request(`/api/v1/postings/${id}/publish`, { method: 'POST', body: { channels } }),
    close: (id, reason) => request(`/api/v1/postings/${id}/close`, { method: 'POST', body: { reason } }),
};

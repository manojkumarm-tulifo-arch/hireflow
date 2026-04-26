import { request } from './client';
export const intentApi = {
    list: (filter) => request('/api/v1/intents', { query: filter }),
    summary: () => request('/api/v1/intents/summary'),
    get: (id) => request(`/api/v1/intents/${id}`),
    create: (body) => request('/api/v1/intents', { method: 'POST', body }),
    confirm: (id) => request(`/api/v1/intents/${id}/confirm`, { method: 'POST' }),
};

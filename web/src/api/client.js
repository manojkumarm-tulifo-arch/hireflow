export class ApiError extends Error {
    status;
    code;
    constructor(status, code, message) {
        super(message);
        this.status = status;
        this.code = code;
        this.name = 'ApiError';
    }
}
const ACCESS_KEY = 'hireflow.token';
const REFRESH_KEY = 'hireflow.refresh';
export const tokenStore = {
    // Access token
    get: () => localStorage.getItem(ACCESS_KEY),
    set: (t) => localStorage.setItem(ACCESS_KEY, t),
    // Refresh token
    getRefresh: () => localStorage.getItem(REFRESH_KEY),
    setRefresh: (t) => localStorage.setItem(REFRESH_KEY, t),
    // Bulk
    setPair: (access, refresh) => {
        localStorage.setItem(ACCESS_KEY, access);
        localStorage.setItem(REFRESH_KEY, refresh);
    },
    clear: () => {
        localStorage.removeItem(ACCESS_KEY);
        localStorage.removeItem(REFRESH_KEY);
    },
};
// Hooks the auth context registers for refresh + sign-out — kept loose so
// client.ts doesn't import from auth/ (which would create a cycle).
let onUnauthorized = null;
export function setUnauthorizedHandler(fn) { onUnauthorized = fn; }
let refreshAccess = null;
export function setAccessRefresher(fn) {
    refreshAccess = fn;
}
let inFlightRefresh = null;
async function refreshOnce() {
    if (inFlightRefresh)
        return inFlightRefresh;
    const refresh = tokenStore.getRefresh();
    if (!refresh || !refreshAccess) {
        tokenStore.clear();
        onUnauthorized?.();
        throw new ApiError(401, 'no_refresh', 'no refresh token available');
    }
    inFlightRefresh = (async () => {
        try {
            const next = await refreshAccess(refresh);
            tokenStore.setPair(next.access, next.refresh);
        }
        catch (err) {
            tokenStore.clear();
            onUnauthorized?.();
            throw err;
        }
        finally {
            inFlightRefresh = null;
        }
    })();
    return inFlightRefresh;
}
async function fetchOnce(path, opts) {
    const url = new URL(path, window.location.origin);
    if (opts.query) {
        for (const [k, v] of Object.entries(opts.query)) {
            if (v !== undefined && v !== '')
                url.searchParams.set(k, String(v));
        }
    }
    const headers = { Accept: 'application/json' };
    if (opts.body !== undefined)
        headers['Content-Type'] = 'application/json';
    if (!opts.skipAuth) {
        const token = tokenStore.get();
        if (token)
            headers.Authorization = `Bearer ${token}`;
    }
    const res = await fetch(url.toString(), {
        method: opts.method ?? 'GET',
        headers,
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
        signal: opts.signal,
    });
    if (res.status === 204)
        return undefined;
    let envelope;
    try {
        envelope = await res.json();
    }
    catch {
        throw new ApiError(res.status, 'invalid_response', 'response was not JSON');
    }
    if (!res.ok || !envelope.success) {
        const code = envelope.error?.code ?? 'unknown_error';
        const message = envelope.error?.message ?? `request failed (${res.status})`;
        throw new ApiError(res.status, code, message);
    }
    return envelope.data;
}
export async function request(path, opts = {}) {
    try {
        return await fetchOnce(path, opts);
    }
    catch (err) {
        if (!(err instanceof ApiError) || err.status !== 401 || opts.skipRefresh)
            throw err;
        // Try one refresh + retry. If refresh fails, propagate original 401.
        try {
            await refreshOnce();
        }
        catch {
            throw err;
        }
        return fetchOnce(path, opts);
    }
}

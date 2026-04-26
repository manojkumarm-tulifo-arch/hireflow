import type { Envelope } from './types';

export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

const ACCESS_KEY = 'hireflow.token';
const REFRESH_KEY = 'hireflow.refresh';

export const tokenStore = {
  // Access token
  get: (): string | null => localStorage.getItem(ACCESS_KEY),
  set: (t: string) => localStorage.setItem(ACCESS_KEY, t),
  // Refresh token
  getRefresh: (): string | null => localStorage.getItem(REFRESH_KEY),
  setRefresh: (t: string) => localStorage.setItem(REFRESH_KEY, t),
  // Bulk
  setPair: (access: string, refresh: string) => {
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
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) { onUnauthorized = fn; }

let refreshAccess: ((refresh: string) => Promise<{ access: string; refresh: string }>) | null = null;
export function setAccessRefresher(fn: (refresh: string) => Promise<{ access: string; refresh: string }>) {
  refreshAccess = fn;
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: unknown;
  query?: Record<string, string | number | undefined>;
  signal?: AbortSignal;
  /** Skip attaching the bearer token. Used by /auth endpoints. */
  skipAuth?: boolean;
  /** Skip the refresh-on-401 retry. Used by /auth endpoints (avoids loops). */
  skipRefresh?: boolean;
}

let inFlightRefresh: Promise<void> | null = null;

async function refreshOnce(): Promise<void> {
  if (inFlightRefresh) return inFlightRefresh;
  const refresh = tokenStore.getRefresh();
  if (!refresh || !refreshAccess) {
    tokenStore.clear();
    onUnauthorized?.();
    throw new ApiError(401, 'no_refresh', 'no refresh token available');
  }
  inFlightRefresh = (async () => {
    try {
      const next = await refreshAccess!(refresh);
      tokenStore.setPair(next.access, next.refresh);
    } catch (err) {
      tokenStore.clear();
      onUnauthorized?.();
      throw err;
    } finally {
      inFlightRefresh = null;
    }
  })();
  return inFlightRefresh;
}

async function fetchOnce<T>(path: string, opts: RequestOptions): Promise<T> {
  const url = new URL(path, window.location.origin);
  if (opts.query) {
    for (const [k, v] of Object.entries(opts.query)) {
      if (v !== undefined && v !== '') url.searchParams.set(k, String(v));
    }
  }

  const headers: Record<string, string> = { Accept: 'application/json' };
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (!opts.skipAuth) {
    const token = tokenStore.get();
    if (token) headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(url.toString(), {
    method: opts.method ?? 'GET',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    signal: opts.signal,
  });

  if (res.status === 204) return undefined as T;

  let envelope: Envelope<T>;
  try {
    envelope = await res.json();
  } catch {
    throw new ApiError(res.status, 'invalid_response', 'response was not JSON');
  }

  if (!res.ok || !envelope.success) {
    const code = envelope.error?.code ?? 'unknown_error';
    const message = envelope.error?.message ?? `request failed (${res.status})`;
    throw new ApiError(res.status, code, message);
  }
  return envelope.data as T;
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  try {
    return await fetchOnce<T>(path, opts);
  } catch (err) {
    if (!(err instanceof ApiError) || err.status !== 401 || opts.skipRefresh) throw err;
    // Try one refresh + retry. If refresh fails, propagate original 401.
    try {
      await refreshOnce();
    } catch {
      throw err;
    }
    return fetchOnce<T>(path, opts);
  }
}

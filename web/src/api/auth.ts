import { request } from './client';

export interface AuthUser {
  id: string;
  tenant_id: string;
  email: string;
  name: string;
  status: 'PENDING_VERIFICATION' | 'ACTIVE' | 'LOCKED' | 'SUSPENDED';
  roles: string[];
}

export interface TokenPair {
  access_token: string;
  access_expires_at: string;
  refresh_token: string;
  refresh_expires_at: string;
  user: AuthUser;
}

export interface OTPRequestResult {
  sent: boolean;
  expires_at: string;
}

export const authApi = {
  signupRequestOTP: (body: { email: string; name: string; tenant_slug: string }) =>
    request<OTPRequestResult>('/api/v1/auth/signup/request-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),

  signupVerifyOTP: (body: { email: string; code: string }) =>
    request<TokenPair>('/api/v1/auth/signup/verify-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),

  signinRequestOTP: (body: { email: string }) =>
    request<OTPRequestResult>('/api/v1/auth/signin/request-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),

  signinVerifyOTP: (body: { email: string; code: string }) =>
    request<TokenPair>('/api/v1/auth/signin/verify-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),

  refresh: (refresh_token: string) =>
    request<TokenPair>('/api/v1/auth/refresh', { method: 'POST', body: { refresh_token }, skipAuth: true, skipRefresh: true }),

  logout: (refresh_token: string) =>
    request<void>('/api/v1/auth/logout', { method: 'POST', body: { refresh_token }, skipRefresh: true }),
};

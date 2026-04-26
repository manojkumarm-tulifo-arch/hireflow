import { request } from './client';
export const authApi = {
    signupRequestOTP: (body) => request('/api/v1/auth/signup/request-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),
    signupVerifyOTP: (body) => request('/api/v1/auth/signup/verify-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),
    signinRequestOTP: (body) => request('/api/v1/auth/signin/request-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),
    signinVerifyOTP: (body) => request('/api/v1/auth/signin/verify-otp', { method: 'POST', body, skipAuth: true, skipRefresh: true }),
    refresh: (refresh_token) => request('/api/v1/auth/refresh', { method: 'POST', body: { refresh_token }, skipAuth: true, skipRefresh: true }),
    logout: (refresh_token) => request('/api/v1/auth/logout', { method: 'POST', body: { refresh_token }, skipRefresh: true }),
};

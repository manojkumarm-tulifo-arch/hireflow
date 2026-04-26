import { jsx as _jsx } from "react/jsx-runtime";
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { setAccessRefresher, setUnauthorizedHandler, tokenStore } from '@/api/client';
import { authApi } from '@/api/auth';
const USER_KEY = 'hireflow.user';
const AuthCtx = createContext(null);
function loadUser() {
    const raw = localStorage.getItem(USER_KEY);
    if (!raw)
        return null;
    try {
        return JSON.parse(raw);
    }
    catch {
        return null;
    }
}
export function AuthProvider({ children }) {
    const [user, setUser] = useState(loadUser);
    const signInWithPair = useCallback((pair) => {
        tokenStore.setPair(pair.access_token, pair.refresh_token);
        localStorage.setItem(USER_KEY, JSON.stringify(pair.user));
        setUser(pair.user);
    }, []);
    const signOut = useCallback(async () => {
        const refresh = tokenStore.getRefresh();
        if (refresh) {
            try {
                await authApi.logout(refresh);
            }
            catch { /* best-effort */ }
        }
        tokenStore.clear();
        localStorage.removeItem(USER_KEY);
        setUser(null);
    }, []);
    // Wire the API client hooks once on mount.
    useEffect(() => {
        setAccessRefresher(async (refresh) => {
            const pair = await authApi.refresh(refresh);
            localStorage.setItem(USER_KEY, JSON.stringify(pair.user));
            setUser(pair.user);
            return { access: pair.access_token, refresh: pair.refresh_token };
        });
        setUnauthorizedHandler(() => {
            localStorage.removeItem(USER_KEY);
            setUser(null);
        });
    }, []);
    const value = useMemo(() => ({
        user,
        isAuthenticated: !!user && !!tokenStore.get(),
        signInWithPair,
        signOut,
    }), [user, signInWithPair, signOut]);
    return _jsx(AuthCtx.Provider, { value: value, children: children });
}
export function useAuth() {
    const ctx = useContext(AuthCtx);
    if (!ctx)
        throw new Error('useAuth must be used inside AuthProvider');
    return ctx;
}

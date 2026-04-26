import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { setAccessRefresher, setUnauthorizedHandler, tokenStore } from '@/api/client';
import { authApi, type AuthUser, type TokenPair } from '@/api/auth';

interface AuthState {
  user: AuthUser | null;
  isAuthenticated: boolean;
  signInWithPair: (pair: TokenPair) => void;
  signOut: () => Promise<void>;
}

const USER_KEY = 'hireflow.user';

const AuthCtx = createContext<AuthState | null>(null);

function loadUser(): AuthUser | null {
  const raw = localStorage.getItem(USER_KEY);
  if (!raw) return null;
  try { return JSON.parse(raw) as AuthUser; } catch { return null; }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(loadUser);

  const signInWithPair = useCallback((pair: TokenPair) => {
    tokenStore.setPair(pair.access_token, pair.refresh_token);
    localStorage.setItem(USER_KEY, JSON.stringify(pair.user));
    setUser(pair.user);
  }, []);

  const signOut = useCallback(async () => {
    const refresh = tokenStore.getRefresh();
    if (refresh) { try { await authApi.logout(refresh); } catch { /* best-effort */ } }
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

  const value = useMemo<AuthState>(() => ({
    user,
    isAuthenticated: !!user && !!tokenStore.get(),
    signInWithPair,
    signOut,
  }), [user, signInWithPair, signOut]);

  return <AuthCtx.Provider value={value}>{children}</AuthCtx.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthCtx);
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider');
  return ctx;
}

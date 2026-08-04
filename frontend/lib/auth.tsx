'use client';

import { createContext, useContext, useState, useEffect, ReactNode } from 'react';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || (typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : 'http://localhost:8080/api/v1');

interface AuthState {
  token: string | null;
  username: string | null;
  role: string | null;
}

interface AuthContextType extends AuthState {
  login: (username: string, password: string) => Promise<string | null>;
  logout: () => void;
  isAuthenticated: boolean;
  authHeaders: () => Record<string, string>;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [auth, setAuth] = useState<AuthState>({ token: null, username: null, role: null });
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const saved = localStorage.getItem('arteria_auth');
    if (saved) {
      try {
        const parsed = JSON.parse(saved);
        setAuth(parsed);
      } catch {
        localStorage.removeItem('arteria_auth');
      }
    }
    setLoading(false);
  }, []);

  const login = async (username: string, password: string): Promise<string | null> => {
    try {
      const res = await fetch(`${API_BASE}/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });
      const data = await res.json();

      if (!res.ok) {
        return data.error || 'Login failed';
      }

      const state: AuthState = {
        token: data.token,
        username: data.username,
        role: data.role,
      };
      setAuth(state);
      localStorage.setItem('arteria_auth', JSON.stringify(state));
      return null;
    } catch (err) {
      return 'Connection failed';
    }
  };

  const logout = () => {
    setAuth({ token: null, username: null, role: null });
    localStorage.removeItem('arteria_auth');
  };

  const authHeaders = (): Record<string, string> => {
    if (!auth.token) return {};
    return { Authorization: `Bearer ${auth.token}` };
  };

  if (loading) {
    return (
      <div className="h-screen bg-arteria-bg flex items-center justify-center">
        <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-arteria-accent to-purple-500 animate-pulse" />
      </div>
    );
  }

  return (
    <AuthContext.Provider value={{ ...auth, login, logout, isAuthenticated: !!auth.token, authHeaders }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

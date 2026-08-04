'use client';

import { AuthProvider, useAuth } from '@/lib/auth';
import LoginPage from '@/components/LoginPage';
import ForceChangePassword from '@/components/ForceChangePassword';

function AuthGateInner({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, mustChangePassword } = useAuth();

  if (!isAuthenticated) {
    return <LoginPage />;
  }

  if (mustChangePassword) {
    return <ForceChangePassword />;
  }

  return <>{children}</>;
}

export function AuthGate({ children }: { children: React.ReactNode }) {
  return (
    <AuthProvider>
      <AuthGateInner>{children}</AuthGateInner>
    </AuthProvider>
  );
}

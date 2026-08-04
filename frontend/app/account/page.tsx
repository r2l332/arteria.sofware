'use client';

import { useState, useEffect } from 'react';
import Sidebar from '@/components/Sidebar';
import { PageHeader } from '@/components/ui';
import { Key, CheckCircle2, AlertCircle, Shield } from 'lucide-react';

const API_BASE = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : 'http://localhost:8080/api/v1';

function getToken() {
  try { return JSON.parse(localStorage.getItem('arteria_auth') || '{}').token; } catch { return ''; }
}

interface PasswordPolicy {
  min_length: number;
  max_length: number;
  require_uppercase: boolean;
  require_lowercase: boolean;
  require_digit: boolean;
  require_special: boolean;
  min_special_chars: number;
}

export default function AccountPage() {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [policy, setPolicy] = useState<PasswordPolicy | null>(null);

  useEffect(() => {
    fetch(`${API_BASE}/auth/password-policy`, {
      headers: { Authorization: `Bearer ${getToken()}` },
    }).then(r => r.json()).then(setPolicy).catch(() => {});
  }, []);

  // Real-time validation
  const checks = policy ? [
    { label: `At least ${policy.min_length} characters`, pass: newPassword.length >= policy.min_length },
    { label: 'Uppercase letter', pass: /[A-Z]/.test(newPassword) },
    { label: 'Lowercase letter', pass: /[a-z]/.test(newPassword) },
    { label: 'Number', pass: /[0-9]/.test(newPassword) },
    { label: `Special character (!@#$%^&*...)`, pass: /[^A-Za-z0-9]/.test(newPassword) },
    { label: 'Passwords match', pass: newPassword === confirmPassword && newPassword.length > 0 },
  ] : [];

  const allValid = checks.every(c => c.pass);

  const changePassword = async () => {
    if (!allValid) return;
    setSaving(true);
    setError('');
    setMessage('');

    const res = await fetch(`${API_BASE}/auth/change-password`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}` },
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    }).then(r => r.json());

    if (res.status === 'password changed') {
      setMessage('Password changed successfully');
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } else {
      setError(res.error || 'Failed to change password');
    }
    setSaving(false);
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-y-auto">
        <PageHeader title="Account" description="Manage your password and security settings" />

        <div className="p-8 max-w-lg space-y-6">
          {/* Password Policy */}
          {policy && (
            <div className="card p-5">
              <div className="flex items-center gap-2 mb-3">
                <Shield size={16} className="text-arteria-accent" />
                <span className="text-sm font-medium text-white">Password Policy</span>
              </div>
              <div className="text-xs text-arteria-muted space-y-1">
                <p>Minimum {policy.min_length} characters, maximum {policy.max_length}</p>
                <p>Must include: uppercase, lowercase, digit, and at least {policy.min_special_chars} special character</p>
              </div>
            </div>
          )}

          {/* Change Password Form */}
          <div className="card p-5">
            <div className="flex items-center gap-2 mb-4">
              <Key size={16} className="text-arteria-accent" />
              <span className="text-sm font-medium text-white">Change Password</span>
            </div>

            <div className="space-y-4">
              <div>
                <label className="label">Current Password</label>
                <input type="password" value={currentPassword} onChange={e => setCurrentPassword(e.target.value)}
                  className="input" placeholder="Enter current password" />
              </div>

              <div>
                <label className="label">New Password</label>
                <input type="password" value={newPassword} onChange={e => setNewPassword(e.target.value)}
                  className="input" placeholder="Enter new password" />
              </div>

              <div>
                <label className="label">Confirm New Password</label>
                <input type="password" value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)}
                  className="input" placeholder="Confirm new password" />
              </div>

              {/* Live validation */}
              {newPassword.length > 0 && (
                <div className="space-y-1.5 pt-2">
                  {checks.map((check, i) => (
                    <div key={i} className="flex items-center gap-2 text-xs">
                      {check.pass ? (
                        <CheckCircle2 size={12} className="text-emerald-400" />
                      ) : (
                        <AlertCircle size={12} className="text-zinc-600" />
                      )}
                      <span className={check.pass ? 'text-emerald-400' : 'text-arteria-muted'}>{check.label}</span>
                    </div>
                  ))}
                </div>
              )}

              {error && (
                <div className="text-xs text-red-400 bg-red-500/10 border border-red-500/20 rounded-lg px-3 py-2">{error}</div>
              )}
              {message && (
                <div className="text-xs text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 rounded-lg px-3 py-2">{message}</div>
              )}

              <button onClick={changePassword}
                disabled={saving || !allValid || !currentPassword}
                className="btn-primary w-full justify-center disabled:opacity-50">
                {saving ? 'Changing...' : 'Change Password'}
              </button>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

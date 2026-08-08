'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { PageHeader, StatusBadge, Modal, EmptyState } from '@/components/ui';
import { Users, UserPlus, Shield } from 'lucide-react';

const API_BASE = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1';

function getToken() {
  try { return JSON.parse(localStorage.getItem('arteria_auth') || '{}').token; } catch { return ''; }
}

function authFetch(path: string, init?: RequestInit) {
  return fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}`, ...init?.headers },
  }).then(r => r.json());
}

interface User {
  user_id: string;
  username: string;
  role: string;
  is_active: boolean;
  created_at: string;
}

interface Role {
  role: string;
  permissions: string[];
}

export default function UsersPage() {
  const [users, setUsers] = useState<User[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [editUser, setEditUser] = useState<User | null>(null);
  const [form, setForm] = useState({ username: '', password: '', role: 'viewer' });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const load = () => {
    authFetch('/users').then(d => setUsers(d.users || [])).catch(() => {});
    authFetch('/roles').then(d => setRoles(d.roles || [])).catch(() => {});
  };
  useEffect(() => { load(); }, []);

  const createUser = async () => {
    setSaving(true);
    setError('');
    const res = await authFetch('/users', { method: 'POST', body: JSON.stringify(form) });
    if (res.error) { setError(res.error); setSaving(false); return; }
    setSaving(false);
    setShowCreate(false);
    setForm({ username: '', password: '', role: 'viewer' });
    load();
  };

  const updateRole = async (userId: string, role: string) => {
    await authFetch(`/users/${userId}/role`, { method: 'PUT', body: JSON.stringify({ role }) });
    load();
  };

  const toggleActive = async (userId: string, isActive: boolean) => {
    await authFetch(`/users/${userId}/role`, { method: 'PUT', body: JSON.stringify({ is_active: !isActive }) });
    load();
  };

  const deleteUser = async (userId: string) => {
    if (!confirm('Delete this user permanently?')) return;
    await authFetch(`/users/${userId}`, { method: 'DELETE' });
    load();
  };

  const roleColor: Record<string, string> = {
    super_admin: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20',
    devops: 'bg-cyan-500/10 text-cyan-400 ring-1 ring-cyan-500/20',
    admin: 'bg-purple-500/10 text-purple-400 ring-1 ring-purple-500/20',
    developer: 'bg-blue-500/10 text-blue-400 ring-1 ring-blue-500/20',
    operator: 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20',
    security: 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20',
    viewer: 'bg-zinc-500/10 text-zinc-400 ring-1 ring-zinc-500/20',
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-y-auto">
        <PageHeader title="Users & Access Control" description="Manage users, roles, and permissions">
          <button onClick={() => setShowCreate(true)} className="btn-primary">
            <UserPlus size={16} /> New User
          </button>
        </PageHeader>

        <div className="p-8 space-y-6">
          {/* Users Table */}
          <div className="card">
            <div className="card-header flex items-center gap-2">
              <Users size={16} className="text-arteria-muted" />
              <span className="text-sm font-medium text-white">Users</span>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-arteria-border">
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">User</th>
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Role</th>
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Status</th>
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Created</th>
                    <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {users.filter(u => u.username && u.role !== 'super_admin' && u.role !== 'devops').map(u => (
                    <tr key={u.user_id} className="border-b border-arteria-border/50 hover:bg-white/[0.015] transition-colors">
                      <td className="py-3 px-5">
                        <div className="flex items-center gap-3">
                          <div className="w-8 h-8 rounded-lg bg-arteria-accent/10 flex items-center justify-center text-xs font-medium text-arteria-accent">
                            {(u.username || '?')[0].toUpperCase()}
                          </div>
                          <span className="text-white font-medium">{u.username}</span>
                        </div>
                      </td>
                      <td className="py-3 px-5">
                        <select
                          value={u.role}
                          onChange={(e) => updateRole(u.user_id, e.target.value)}
                          className={`badge cursor-pointer ${roleColor[u.role] || roleColor.viewer}`}
                        >
                          {roles.filter(r => r.role !== 'super_admin' && r.role !== 'devops').map(r => <option key={r.role} value={r.role}>{r.role}</option>)}
                        </select>
                      </td>
                      <td className="py-3 px-5">
                        <button onClick={() => toggleActive(u.user_id, u.is_active)} className="cursor-pointer">
                          <StatusBadge status={u.is_active ? 'ACTIVE' : 'DISABLED'} />
                        </button>
                      </td>
                      <td className="py-3 px-5 text-xs text-arteria-muted">
                        {new Date(u.created_at).toLocaleDateString()}
                      </td>
                      <td className="py-3 px-5 text-right">
                        <button onClick={() => deleteUser(u.user_id)} className="text-xs text-red-400 hover:text-red-300">Delete</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {users.length === 0 && <EmptyState icon={Users} title="No users" description="Create the first user to get started" />}
            </div>
          </div>

          {/* Roles Reference */}
          <div className="card">
            <div className="card-header flex items-center gap-2">
              <Shield size={16} className="text-arteria-muted" />
              <span className="text-sm font-medium text-white">Roles & Permissions</span>
            </div>
            <div className="p-5 grid grid-cols-1 gap-3">
              {roles.map(r => (
                <div key={r.role} className="flex items-start gap-4">
                  <span className={`badge mt-0.5 min-w-[80px] justify-center ${roleColor[r.role] || roleColor.viewer}`}>{r.role}</span>
                  <div className="flex flex-wrap gap-1.5">
                    {r.permissions.map(p => (
                      <span key={p} className="text-2xs px-1.5 py-0.5 bg-arteria-bg border border-arteria-border rounded text-arteria-muted">{p}</span>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Create User Modal */}
        {showCreate && (
          <Modal title="Create User" onClose={() => { setShowCreate(false); setError(''); }}>
            <div className="space-y-4">
              <div>
                <label className="label">Username</label>
                <input value={form.username} onChange={e => setForm({ ...form, username: e.target.value })}
                  className="input" placeholder="e.g. dev.sarah" autoFocus />
              </div>
              <div>
                <label className="label">Password</label>
                <input type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })}
                  className="input" placeholder="Minimum 8 characters" />
              </div>
              <div>
                <label className="label">Role</label>
                <select value={form.role} onChange={e => setForm({ ...form, role: e.target.value })} className="select">
                  {roles.filter(r => r.role !== 'super_admin' && r.role !== 'devops').map(r => <option key={r.role} value={r.role}>{r.role}</option>)}
                </select>
              </div>
              {error && (
                <div className="text-xs text-red-400 bg-red-500/10 border border-red-500/20 rounded-lg px-3 py-2">{error}</div>
              )}
              <div className="flex justify-end gap-2 pt-2">
                <button onClick={() => setShowCreate(false)} className="btn-secondary">Cancel</button>
                <button onClick={createUser} disabled={saving || !form.username || !form.password}
                  className="btn-primary disabled:opacity-50">
                  {saving ? 'Creating...' : 'Create User'}
                </button>
              </div>
            </div>
          </Modal>
        )}
      </main>
    </div>
  );
}

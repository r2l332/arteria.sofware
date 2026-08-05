'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { apiFetch } from '@/lib/api';
import { Building2, Palette, Globe, Plus } from 'lucide-react';

interface Organisation {
  org_id: string;
  name: string;
  slug: string;
  custom_domain: string;
  is_active: boolean;
  created_at: string;
  org_name?: string;
  branding?: {
    logo_url: string;
    app_name: string;
    primary_color: string;
    accent_color: string;
    favicon_url: string;
  };
  support_email?: string;
}

export default function OrganisationsPage() {
  const [orgs, setOrgs] = useState<Organisation[]>([]);
  const [selected, setSelected] = useState<Organisation | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [form, setForm] = useState({ name: '', slug: '', custom_domain: '', support_email: '' });
  const [brandForm, setBrandForm] = useState({ logo_url: '', app_name: '', primary_color: '#6366f1', accent_color: '#6366f1', favicon_url: '', custom_domain: '', support_email: '' });
  const [saving, setSaving] = useState(false);

  const load = () => {
    apiFetch<{ organisations: Organisation[] }>('/organisations')
      .then(d => setOrgs(d.organisations || []))
      .catch(console.error);
  };
  useEffect(() => { load(); }, []);

  const selectOrg = async (org: Organisation) => {
    const detail = await apiFetch<Organisation>(`/organisations/${org.org_id}`);
    setSelected(detail);
    setBrandForm({
      logo_url: detail.branding?.logo_url || '',
      app_name: detail.branding?.app_name || '',
      primary_color: detail.branding?.primary_color || '#6366f1',
      accent_color: detail.branding?.accent_color || '#6366f1',
      favicon_url: detail.branding?.favicon_url || '',
      custom_domain: detail.custom_domain || '',
      support_email: detail.support_email || '',
    });
  };

  const createOrg = async () => {
    setSaving(true);
    await apiFetch('/organisations', { method: 'POST', body: JSON.stringify(form) });
    setSaving(false);
    setShowCreate(false);
    setForm({ name: '', slug: '', custom_domain: '', support_email: '' });
    load();
  };

  const saveBranding = async () => {
    if (!selected) return;
    setSaving(true);
    await apiFetch(`/organisations/${selected.org_id}/branding`, {
      method: 'PUT', body: JSON.stringify(brandForm),
    });
    setSaving(false);
    load();
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        <div className="flex items-center justify-between px-8 py-5 border-b border-arteria-border">
          <div>
            <h2 className="text-2xl font-bold text-white">Organisations</h2>
            <p className="text-xs text-arteria-muted mt-0.5">Manage tenants, branding, and custom domains</p>
          </div>
          <button onClick={() => setShowCreate(true)}
            className="px-4 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80 flex items-center gap-2">
            <Plus size={14} /> New Organisation
          </button>
        </div>

        <div className="flex-1 overflow-hidden flex">
          {/* Org List */}
          <div className="w-1/3 overflow-y-auto p-6 border-r border-arteria-border">
            {showCreate && (
              <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowCreate(false)}>
                <div className="bg-arteria-surface border border-arteria-border rounded-lg w-[450px] p-6" onClick={e => e.stopPropagation()}>
                  <h3 className="text-lg font-semibold text-white mb-4">Create Organisation</h3>
                  <div className="space-y-3">
                    <div>
                      <label className="text-xs text-arteria-muted">Organisation Name</label>
                      <input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="Hospital A"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div>
                      <label className="text-xs text-arteria-muted">Slug (URL identifier)</label>
                      <input value={form.slug} onChange={e => setForm({ ...form, slug: e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '') })} placeholder="hospital-a"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white font-mono" />
                      <p className="text-2xs text-arteria-muted mt-1">{form.slug ? `${form.slug}.arteria.cloud` : 'slug.arteria.cloud'}</p>
                    </div>
                    <div>
                      <label className="text-xs text-arteria-muted">Custom Domain (optional)</label>
                      <input value={form.custom_domain} onChange={e => setForm({ ...form, custom_domain: e.target.value })} placeholder="integration.hospital-a.com"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div>
                      <label className="text-xs text-arteria-muted">Support Email</label>
                      <input value={form.support_email} onChange={e => setForm({ ...form, support_email: e.target.value })} placeholder="support@hospital-a.com"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                  </div>
                  <div className="flex justify-end gap-2 mt-5">
                    <button onClick={() => setShowCreate(false)} className="px-4 py-2 text-sm text-arteria-muted">Cancel</button>
                    <button onClick={createOrg} disabled={saving || !form.name || !form.slug}
                      className="px-4 py-2 bg-arteria-accent text-white text-sm rounded disabled:opacity-50">
                      {saving ? 'Creating...' : 'Create'}
                    </button>
                  </div>
                </div>
              </div>
            )}

            <div className="grid gap-3">
              {orgs.map(org => (
                <div key={org.org_id} onClick={() => selectOrg(org)}
                  className={`bg-arteria-surface border rounded-lg p-4 cursor-pointer transition-colors ${
                    selected?.org_id === org.org_id ? 'border-arteria-accent' : 'border-arteria-border hover:border-arteria-accent/50'
                  }`}>
                  <div className="flex items-center gap-2 mb-1">
                    <Building2 size={14} className="text-arteria-accent" />
                    <h3 className="font-medium text-white text-sm">{org.name}</h3>
                  </div>
                  <div className="text-xs text-arteria-muted space-y-0.5">
                    <p className="font-mono">{org.slug}.arteria.cloud</p>
                    {org.custom_domain && <p><Globe size={10} className="inline mr-1" />{org.custom_domain}</p>}
                  </div>
                </div>
              ))}
              {orgs.length === 0 && (
                <p className="text-center py-8 text-arteria-muted text-sm">No organisations. Create one to start multi-tenancy.</p>
              )}
            </div>
          </div>

          {/* Branding Editor */}
          <div className="w-2/3 overflow-y-auto p-6">
            {selected ? (
              <div>
                <h3 className="text-lg font-semibold text-white mb-1">{selected.org_name || selected.name}</h3>
                <p className="text-xs text-arteria-muted mb-6">Customise the portal branding for this organisation</p>

                <div className="grid grid-cols-2 gap-6">
                  {/* Branding Fields */}
                  <div className="space-y-4">
                    <h4 className="text-sm font-medium text-white flex items-center gap-2"><Palette size={14} /> Branding</h4>
                    <div>
                      <label className="text-xs text-arteria-muted">App Name</label>
                      <input value={brandForm.app_name} onChange={e => setBrandForm({ ...brandForm, app_name: e.target.value })} placeholder="HealthConnect"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div>
                      <label className="text-xs text-arteria-muted">Logo URL</label>
                      <input value={brandForm.logo_url} onChange={e => setBrandForm({ ...brandForm, logo_url: e.target.value })} placeholder="https://..."
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div>
                      <label className="text-xs text-arteria-muted">Favicon URL</label>
                      <input value={brandForm.favicon_url} onChange={e => setBrandForm({ ...brandForm, favicon_url: e.target.value })} placeholder="https://..."
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="text-xs text-arteria-muted">Primary Color</label>
                        <div className="flex gap-2 mt-1">
                          <input type="color" value={brandForm.primary_color} onChange={e => setBrandForm({ ...brandForm, primary_color: e.target.value })}
                            className="w-10 h-10 rounded border border-arteria-border cursor-pointer" />
                          <input value={brandForm.primary_color} onChange={e => setBrandForm({ ...brandForm, primary_color: e.target.value })}
                            className="flex-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white font-mono" />
                        </div>
                      </div>
                      <div>
                        <label className="text-xs text-arteria-muted">Accent Color</label>
                        <div className="flex gap-2 mt-1">
                          <input type="color" value={brandForm.accent_color} onChange={e => setBrandForm({ ...brandForm, accent_color: e.target.value })}
                            className="w-10 h-10 rounded border border-arteria-border cursor-pointer" />
                          <input value={brandForm.accent_color} onChange={e => setBrandForm({ ...brandForm, accent_color: e.target.value })}
                            className="flex-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white font-mono" />
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Preview */}
                  <div className="space-y-4">
                    <h4 className="text-sm font-medium text-white">Live Preview</h4>
                    <div className="bg-arteria-bg border border-arteria-border rounded-lg p-4">
                      <div className="flex items-center gap-2.5 mb-3">
                        {brandForm.logo_url ? (
                          <img src={brandForm.logo_url} alt="Logo" className="w-8 h-8 rounded-lg object-contain" />
                        ) : (
                          <div className="w-8 h-8 rounded-lg flex items-center justify-center" style={{ background: `linear-gradient(135deg, ${brandForm.primary_color}, #a855f7)` }}>
                            <Building2 size={14} className="text-white" />
                          </div>
                        )}
                        <div>
                          <p className="text-sm font-semibold text-white">{brandForm.app_name || 'App Name'}</p>
                          <p className="text-2xs text-arteria-muted">Integration Engine</p>
                        </div>
                      </div>
                      <div className="h-8 rounded" style={{ backgroundColor: brandForm.primary_color, opacity: 0.2 }} />
                      <p className="text-2xs text-arteria-muted mt-2">Login button color:</p>
                      <button className="mt-1 px-4 py-1.5 text-white text-xs rounded" style={{ backgroundColor: brandForm.primary_color }}>Sign In</button>
                    </div>

                    <div>
                      <label className="text-xs text-arteria-muted">Custom Domain</label>
                      <input value={brandForm.custom_domain} onChange={e => setBrandForm({ ...brandForm, custom_domain: e.target.value })} placeholder="integration.hospital.com"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div>
                      <label className="text-xs text-arteria-muted">Support Email</label>
                      <input value={brandForm.support_email} onChange={e => setBrandForm({ ...brandForm, support_email: e.target.value })} placeholder="support@hospital.com"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                  </div>
                </div>

                <div className="mt-6 flex justify-end">
                  <button onClick={saveBranding} disabled={saving}
                    className="px-5 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80 disabled:opacity-50">
                    {saving ? 'Saving...' : 'Save Branding'}
                  </button>
                </div>
              </div>
            ) : (
              <div className="flex items-center justify-center h-full text-arteria-muted text-sm">
                Select an organisation to edit its branding
              </div>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}

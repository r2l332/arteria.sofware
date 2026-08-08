'use client';

import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import { getRoutes, getFilters, getCommPoints, createFilter, updateFilter, createRoute, updateRoute, deleteRoute, apiFetch, type Route, type Filter, type CommPoint } from '@/lib/api';
import { Plus, Trash2, Edit2, GitBranch, Radio, Cpu, Send, ChevronRight, Play, Settings2 } from 'lucide-react';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

interface RouteForm {
  name: string; description: string; source_comm_point_id: string; dest_comm_point_id: string;
  fan_out_cp_ids: string[]; source_topic: string; destination_topic: string; is_active: boolean;
}
const emptyForm: RouteForm = { name: '', description: '', source_comm_point_id: '', dest_comm_point_id: '', fan_out_cp_ids: [], source_topic: '', destination_topic: '', is_active: true };

export default function RoutesPage() {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);
  const [selectedRoute, setSelectedRoute] = useState<Route | null>(null);
  const [filters, setFilters] = useState<Filter[]>([]);
  const [editingFilter, setEditingFilter] = useState<Filter | null>(null);
  const [editorValue, setEditorValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [showRouteForm, setShowRouteForm] = useState(false);
  const [editingRouteId, setEditingRouteId] = useState<string | null>(null);
  const [routeForm, setRouteForm] = useState<RouteForm>(emptyForm);
  const [showPropsPanel, setShowPropsPanel] = useState(false);
  const [routeProperties, setRouteProperties] = useState<Record<string, string>>({});
  const [newPropKey, setNewPropKey] = useState('');
  const [newPropValue, setNewPropValue] = useState('');
  const [chainRouteId, setChainRouteId] = useState('');

  const API_BASE = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1';

  const loadRoutes = () => getRoutes().then(r => setRoutes(r.routes || [])).catch(() => {});

  useEffect(() => {
    loadRoutes();
    getCommPoints().then(r => setCommPoints(r.communication_points || [])).catch(() => {});
    const h = () => loadRoutes();
    window.addEventListener('arteria:config_change', h);
    return () => window.removeEventListener('arteria:config_change', h);
  }, []);

  const selectRoute = async (route: Route) => {
    setSelectedRoute(route);
    setEditingFilter(null);
    const f = await getFilters(route.route_id);
    setFilters(f.filters || []);
    try { const res = await fetch(`${API_BASE}/routes/${route.route_id}/properties`, { headers: { 'Authorization': `Bearer ${JSON.parse(localStorage.getItem('arteria_auth') || '{}').token}` } }); const d = await res.json(); setRouteProperties(d.properties || {}); } catch { setRouteProperties({}); }
    try { const res = await fetch(`${API_BASE}/routes/${route.route_id}/chain`, { headers: { 'Authorization': `Bearer ${JSON.parse(localStorage.getItem('arteria_auth') || '{}').token}` } }); const d = await res.json(); setChainRouteId(d.next_route_id || ''); } catch { setChainRouteId(''); }
  };

  const editFilter = (filter: Filter) => { setEditingFilter(filter); setEditorValue(filter.js_script || getDefaultScript(filter.filter_type)); };
  const newFilter = () => {
    if (!selectedRoute) return;
    const maxOrder = filters.reduce((max, f) => Math.max(max, f.execution_order), -1);
    setEditingFilter({ filter_id: '', name: 'New Filter', filter_type: 'javascript', execution_order: maxOrder + 1, js_script: getDefaultScript('javascript'), config_json: '', is_active: true });
    setEditorValue(getDefaultScript('javascript'));
  };
  const saveFilter = async () => {
    if (!selectedRoute || !editingFilter) return;
    setSaving(true);
    const payload = { name: editingFilter.name, filter_type: editingFilter.filter_type, execution_order: editingFilter.execution_order, js_script: editingFilter.filter_type === 'connector' ? '' : editorValue, config_json: editingFilter.filter_type === 'connector' ? editingFilter.config_json : editingFilter.config_json, is_active: editingFilter.is_active };
    if (editingFilter.filter_id) await updateFilter(editingFilter.filter_id, payload); else await createFilter(selectedRoute.route_id, payload);
    const f = await getFilters(selectedRoute.route_id);
    setFilters(f.filters || []);
    setEditingFilter(null);
    setSaving(false);
  };

  const openCreateRoute = () => { setRouteForm(emptyForm); setEditingRouteId(null); setShowRouteForm(true); };
  const openEditRoute = (r: Route) => { setRouteForm({ name: r.name, description: r.description, source_comm_point_id: r.source_comm_point_id, dest_comm_point_id: r.dest_comm_point_id, fan_out_cp_ids: (r as any).fan_out_cp_ids || [], source_topic: r.source_topic, destination_topic: r.destination_topic, is_active: r.is_active }); setEditingRouteId(r.route_id); setShowRouteForm(true); };
  const saveRoute = async () => { setSaving(true); if (editingRouteId) await updateRoute(editingRouteId, routeForm); else await createRoute(routeForm); setSaving(false); setShowRouteForm(false); loadRoutes(); };
  const deleteRouteById = async (id: string) => { if (!confirm('Delete this route?')) return; await deleteRoute(id); if (selectedRoute?.route_id === id) { setSelectedRoute(null); setFilters([]); } loadRoutes(); };

  const getCPName = (id: string) => commPoints.find(c => c.comm_point_id === id)?.name || id?.slice(0, 8);
  const getCPProtocol = (id: string) => commPoints.find(c => c.comm_point_id === id)?.protocol || '';

  // --- RENDER ---
  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        {/* Header */}
        <div className="px-6 py-4 border-b border-arteria-border flex items-center justify-between shrink-0">
          <div>
            <h1 className="text-xl font-bold text-white">Routes & Filters</h1>
            <p className="text-xs text-arteria-muted mt-0.5">Visual pipeline configuration</p>
          </div>
          <button onClick={openCreateRoute} className="px-4 py-2 bg-arteria-accent text-white text-sm rounded-lg hover:bg-arteria-accent/80 flex items-center gap-2">
            <Plus className="w-4 h-4" /> New Route
          </button>
        </div>

        <div className="flex-1 flex overflow-hidden">
          {/* Left: Route pipeline cards */}
          <div className="flex-1 overflow-y-auto p-6 space-y-4">
            {routes.map(r => (
              <div key={r.route_id} onClick={() => selectRoute(r)}
                className={`bg-arteria-surface border rounded-xl p-4 cursor-pointer transition-all hover:border-arteria-accent/50 ${selectedRoute?.route_id === r.route_id ? 'border-arteria-accent ring-1 ring-arteria-accent/20' : 'border-arteria-border'}`}>
                {/* Route header */}
                <div className="flex items-center justify-between mb-3">
                  <div className="flex items-center gap-3">
                    <span className={`w-2.5 h-2.5 rounded-full ${r.is_active ? 'bg-green-400' : 'bg-gray-600'}`} />
                    <h3 className="text-sm font-semibold text-white">{r.name}</h3>
                    <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-purple-900/30 text-purple-300 border border-purple-800/30">{r.source_topic}</span>
                  </div>
                  <div className="flex gap-1.5">
                    <button onClick={(e) => { e.stopPropagation(); openEditRoute(r); }} className="p-1.5 text-gray-500 hover:text-white hover:bg-white/5 rounded"><Edit2 className="w-3.5 h-3.5" /></button>
                    <button onClick={(e) => { e.stopPropagation(); deleteRouteById(r.route_id); }} className="p-1.5 text-gray-500 hover:text-red-400 hover:bg-red-500/5 rounded"><Trash2 className="w-3.5 h-3.5" /></button>
                  </div>
                </div>
                {/* Visual pipeline */}
                <div className="flex items-center gap-0 overflow-x-auto pb-1">
                  {/* Source CP */}
                  <PipeNode icon={<Radio className="w-3.5 h-3.5 text-sky-400" />} label={getCPName(r.source_comm_point_id)} sub={getCPProtocol(r.source_comm_point_id)} color="border-sky-800/50 bg-sky-950/30" />
                  <Arrow />
                  {/* Route */}
                  <PipeNode icon={<GitBranch className="w-3.5 h-3.5 text-purple-400" />} label={r.source_topic} sub={`→ ${r.destination_topic}`} color="border-purple-800/50 bg-purple-950/30" />
                  <Arrow />
                  {/* Filters (show count) */}
                  {selectedRoute?.route_id === r.route_id && filters.length > 0 ? (
                    filters.map((f, i) => (
                      <span key={f.filter_id || i} className="contents">
                        <PipeNode icon={<Cpu className="w-3.5 h-3.5 text-amber-400" />} label={f.name} sub={f.filter_type} color={`border-amber-800/50 ${editingFilter?.filter_id === f.filter_id ? 'bg-amber-900/40 ring-1 ring-amber-500/50' : 'bg-amber-950/30'}`} onClick={() => editFilter(f)} />
                        <Arrow />
                      </span>
                    ))
                  ) : (
                    <><PipeNode icon={<Cpu className="w-3.5 h-3.5 text-amber-400" />} label="Filters" sub="click to view" color="border-amber-800/50 bg-amber-950/30" /><Arrow /></>
                  )}
                  {/* Dest CP */}
                  <PipeNode icon={<Send className="w-3.5 h-3.5 text-emerald-400" />} label={getCPName(r.dest_comm_point_id)} sub={getCPProtocol(r.dest_comm_point_id)} color="border-emerald-800/50 bg-emerald-950/30" />
                </div>
                {r.description && <p className="text-[10px] text-gray-600 mt-2">{r.description}</p>}
              </div>
            ))}
            {routes.length === 0 && <div className="text-center text-gray-600 py-12">No routes configured. Click &quot;New Route&quot; to create one.</div>}
          </div>

          {/* Right: Editor panel (appears when editing a filter) */}
          {selectedRoute && (
            <div className="w-[480px] border-l border-arteria-border flex flex-col overflow-hidden bg-gray-900/30">
              {editingFilter ? (
                <>
                  <div className="px-4 py-3 border-b border-arteria-border flex items-center justify-between shrink-0">
                    <div className="flex items-center gap-3">
                      <input value={editingFilter.name} onChange={e => setEditingFilter({ ...editingFilter, name: e.target.value })} className="bg-transparent text-white text-sm font-medium border-b border-arteria-border focus:border-arteria-accent outline-none w-40" />
                      <select value={editingFilter.filter_type} onChange={e => { setEditingFilter({ ...editingFilter, filter_type: e.target.value, config_json: e.target.value === 'connector' ? getDefaultConnectorConfig() : editingFilter.config_json }); if (e.target.value !== 'connector') setEditorValue(getDefaultScript(e.target.value)); }} className="bg-arteria-bg text-[11px] text-gray-300 border border-arteria-border rounded px-2 py-1">
                        <option value="javascript">JavaScript</option><option value="conditional">Conditional</option><option value="lookup">Lookup</option><option value="python">Python</option><option value="bash">Bash</option><option value="dotnet">.NET (C#)</option><option value="connector">Connector</option>
                      </select>
                      <label className="flex items-center gap-1 text-[10px] text-gray-500"><input type="checkbox" checked={editingFilter.is_active} onChange={e => setEditingFilter({ ...editingFilter, is_active: e.target.checked })} className="rounded w-3 h-3" />Active</label>
                    </div>
                    <div className="flex gap-2">
                      <button onClick={() => setEditingFilter(null)} className="text-xs text-gray-500 hover:text-white">Cancel</button>
                      <button onClick={saveFilter} disabled={saving} className="text-xs px-3 py-1 bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">{saving ? '...' : 'Save'}</button>
                    </div>
                  </div>
                  <div className="flex-1">
                    <MonacoEditor height="100%" language={editingFilter.filter_type === 'connector' ? 'json' : editingFilter.filter_type === 'python' ? 'python' : editingFilter.filter_type === 'bash' ? 'shell' : editingFilter.filter_type === 'dotnet' ? 'csharp' : 'javascript'} theme="vs-dark"
                      value={editingFilter.filter_type === 'connector' ? (editingFilter.config_json || getDefaultConnectorConfig()) : editorValue}
                      onChange={v => { if (editingFilter.filter_type === 'connector') setEditingFilter({ ...editingFilter, config_json: v || '' }); else setEditorValue(v || ''); }}
                      options={{ minimap: { enabled: false }, fontSize: 12, lineNumbers: 'on', scrollBeyondLastLine: false, automaticLayout: true, padding: { top: 10 } }} />
                  </div>
                </>
              ) : (
                <div className="flex flex-col h-full">
                  <div className="px-4 py-3 border-b border-arteria-border flex items-center justify-between shrink-0">
                    <span className="text-sm font-semibold text-white">{selectedRoute.name}</span>
                    <div className="flex gap-2">
                      <button onClick={() => setShowPropsPanel(!showPropsPanel)} className="text-[10px] px-2 py-1 border border-arteria-border text-gray-400 rounded hover:text-white"><Settings2 className="w-3 h-3 inline mr-1" />Props</button>
                      <button onClick={newFilter} className="text-xs px-3 py-1 bg-arteria-accent text-white rounded hover:bg-arteria-accent/80"><Plus className="w-3 h-3 inline mr-1" />Filter</button>
                    </div>
                  </div>
                  {showPropsPanel && (
                    <div className="px-4 py-3 border-b border-arteria-border bg-arteria-bg/30 space-y-3">
                      <div>
                        <p className="text-[9px] text-gray-500 uppercase tracking-wider mb-1">Default Properties</p>
                        {Object.entries(routeProperties).map(([k, v]) => (
                          <div key={k} className="flex items-center gap-2 text-[10px] mb-0.5">
                            <span className="font-mono text-cyan-400">{k}</span><span className="text-gray-400">= {v}</span>
                            <button onClick={async () => { const u = { ...routeProperties }; delete u[k]; setRouteProperties(u); await apiFetch(`/routes/${selectedRoute.route_id}/properties`, { method: 'PUT', body: JSON.stringify({ properties: u }) }); }} className="text-red-400 text-[8px]">✕</button>
                          </div>
                        ))}
                        <div className="flex gap-1 mt-1">
                          <input value={newPropKey} onChange={e => setNewPropKey(e.target.value)} placeholder="key" className="px-1.5 py-0.5 bg-arteria-bg border border-arteria-border rounded text-[10px] text-white font-mono w-20" />
                          <input value={newPropValue} onChange={e => setNewPropValue(e.target.value)} placeholder="value" className="px-1.5 py-0.5 bg-arteria-bg border border-arteria-border rounded text-[10px] text-white font-mono flex-1" />
                          <button onClick={async () => { if (!newPropKey) return; const u = { ...routeProperties, [newPropKey]: newPropValue }; setRouteProperties(u); setNewPropKey(''); setNewPropValue(''); await apiFetch(`/routes/${selectedRoute.route_id}/properties`, { method: 'PUT', body: JSON.stringify({ properties: u }) }); }} className="px-2 py-0.5 bg-arteria-accent text-white text-[10px] rounded">+</button>
                        </div>
                      </div>
                      <div>
                        <p className="text-[9px] text-gray-500 uppercase tracking-wider mb-1">Chain To</p>
                        <select value={chainRouteId} onChange={async e => { setChainRouteId(e.target.value); await apiFetch(`/routes/${selectedRoute.route_id}/chain`, { method: 'PUT', body: JSON.stringify({ next_route_id: e.target.value || null }) }); }} className="w-full px-2 py-1 bg-arteria-bg border border-arteria-border rounded text-[10px] text-white">
                          <option value="">No chain</option>
                          {routes.filter(r => r.route_id !== selectedRoute.route_id).map(r => <option key={r.route_id} value={r.route_id}>{r.name}</option>)}
                        </select>
                      </div>
                    </div>
                  )}
                  <div className="flex-1 overflow-y-auto p-4">
                    <p className="text-[10px] text-gray-500 uppercase tracking-wider mb-3">Filter Chain ({filters.length} steps)</p>
                    {filters.length === 0 ? (
                      <p className="text-xs text-gray-600">No filters — messages pass through unmodified. Click &quot;+ Filter&quot; to add processing steps.</p>
                    ) : (
                      <div className="space-y-2">
                        {filters.map((f, i) => (
                          <div key={f.filter_id || i} onClick={() => editFilter(f)} className={`p-3 rounded-lg border cursor-pointer transition-all hover:border-amber-600/50 border-arteria-border bg-arteria-surface`}>
                            <div className="flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                <span className="text-[9px] text-gray-600 font-mono">#{f.execution_order}</span>
                                <Cpu className="w-3.5 h-3.5 text-amber-400" />
                                <span className="text-xs text-white font-medium">{f.name}</span>
                              </div>
                              <span className="text-[9px] px-1.5 py-0.5 rounded bg-arteria-bg border border-arteria-border text-gray-400">{f.filter_type}</span>
                            </div>
                            {!f.is_active && <span className="text-[8px] text-red-400 mt-1 block">DISABLED</span>}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Route Form Modal */}
        {showRouteForm && (
          <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowRouteForm(false)}>
            <div className="bg-arteria-surface border border-arteria-border rounded-xl w-[520px] p-6" onClick={e => e.stopPropagation()}>
              <h3 className="text-lg font-semibold text-white mb-4">{editingRouteId ? 'Edit' : 'Create'} Route</h3>
              <div className="space-y-3">
                <div><label className="text-[10px] text-gray-500 uppercase">Name</label><input value={routeForm.name} onChange={e => setRouteForm({ ...routeForm, name: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
                <div><label className="text-[10px] text-gray-500 uppercase">Description</label><input value={routeForm.description} onChange={e => setRouteForm({ ...routeForm, description: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
                <div className="grid grid-cols-2 gap-3">
                  <div><label className="text-[10px] text-gray-500 uppercase">Source CP</label><select value={routeForm.source_comm_point_id} onChange={e => setRouteForm({ ...routeForm, source_comm_point_id: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white"><option value="">Select...</option>{commPoints.filter(c => c.direction === 'INPUT').map(c => <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>)}</select></div>
                  <div><label className="text-[10px] text-gray-500 uppercase">Destination CP</label><select value={routeForm.dest_comm_point_id} onChange={e => setRouteForm({ ...routeForm, dest_comm_point_id: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white"><option value="">Select...</option>{commPoints.filter(c => c.direction === 'OUTPUT').map(c => <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>)}</select></div>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div><label className="text-[10px] text-gray-500 uppercase">Source Topic (e.g. ADT^A01, *)</label><input value={routeForm.source_topic} onChange={e => setRouteForm({ ...routeForm, source_topic: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" /></div>
                  <div><label className="text-[10px] text-gray-500 uppercase">Destination Topic</label><input value={routeForm.destination_topic} onChange={e => setRouteForm({ ...routeForm, destination_topic: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" /></div>
                </div>
                <label className="flex items-center gap-2 text-sm text-gray-300"><input type="checkbox" checked={routeForm.is_active} onChange={e => setRouteForm({ ...routeForm, is_active: e.target.checked })} className="rounded" />Active</label>
              </div>
              <div className="flex justify-end gap-2 mt-5">
                <button onClick={() => setShowRouteForm(false)} className="px-4 py-2 text-sm text-gray-400 hover:text-white">Cancel</button>
                <button onClick={saveRoute} disabled={saving || !routeForm.name || !routeForm.source_topic} className="px-4 py-2 bg-arteria-accent text-white text-sm rounded-lg hover:bg-arteria-accent/80 disabled:opacity-50">{saving ? '...' : editingRouteId ? 'Update' : 'Create'}</button>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function PipeNode({ icon, label, sub, color, onClick }: { icon: React.ReactNode; label: string; sub: string; color: string; onClick?: () => void }) {
  return (
    <div onClick={onClick} className={`shrink-0 flex items-center gap-2 px-3 py-2 rounded-lg border ${color} ${onClick ? 'cursor-pointer hover:ring-1 hover:ring-white/20' : ''}`}>
      {icon}
      <div className="min-w-0">
        <div className="text-[10px] text-white font-medium truncate max-w-[100px]">{label}</div>
        <div className="text-[8px] text-gray-500 truncate max-w-[100px]">{sub}</div>
      </div>
    </div>
  );
}

function Arrow() { return <ChevronRight className="w-4 h-4 text-gray-700 shrink-0 mx-0.5" />; }

function getDefaultScript(t: string): string {
  switch (t) {
    case 'conditional': return 'function evaluate(msg) {\n  if (!msg.patientId) return { action: "reject", reason: "Missing PID" };\n  return { action: "pass" };\n}';
    case 'python': return 'import sys, json\n\nmsg = json.load(sys.stdin)\nmsg["properties"]["filter_lang"] = "python"\nprint(json.dumps(msg))';
    case 'bash': return '#!/bin/bash\nINPUT=$(cat)\necho "$INPUT" | jq \'.properties.filter_lang = "bash"\'';
    case 'dotnet': return 'using System;\nvar input = Console.In.ReadToEnd();\nConsole.Write(input);';
    default: return 'function transform(msg) {\n  msg.properties.processed_at = new Date().toISOString();\n  return msg;\n}';
  }
}

function getDefaultConnectorConfig(): string {
  return JSON.stringify({ connector_type: "HTTP", url: "https://api.example.com/lookup", method: "POST", headers: { "Content-Type": "application/json" }, timeout_ms: 5000, body_template: "{{.RawPayload}}", response_property: "api_response", response_status_property: "api_status" }, null, 2);
}

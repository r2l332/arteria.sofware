'use client';

import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import { getRoutes, getFilters, getCommPoints, createFilter, updateFilter, createRoute, updateRoute, deleteRoute, type Route, type Filter, type CommPoint } from '@/lib/api';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

const API_BASE = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1';

interface RouteForm {
  name: string;
  description: string;
  source_comm_point_id: string;
  dest_comm_point_id: string;
  source_topic: string;
  destination_topic: string;
  is_active: boolean;
}

const emptyRouteForm: RouteForm = {
  name: '', description: '', source_comm_point_id: '', dest_comm_point_id: '',
  source_topic: '', destination_topic: '', is_active: true,
};

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
  const [routeForm, setRouteForm] = useState<RouteForm>(emptyRouteForm);

  const loadRoutes = () => getRoutes().then((r) => setRoutes(r.routes)).catch(console.error);

  useEffect(() => {
    loadRoutes();
    getCommPoints().then((r) => setCommPoints(r.communication_points)).catch(console.error);
  }, []);

  const selectRoute = async (route: Route) => {
    setSelectedRoute(route);
    setEditingFilter(null);
    const f = await getFilters(route.route_id);
    setFilters(f.filters);
  };

  const editFilter = (filter: Filter) => {
    setEditingFilter(filter);
    setEditorValue(filter.js_script || getDefaultScript(filter.filter_type));
  };

  const newFilter = () => {
    if (!selectedRoute) return;
    const maxOrder = filters.reduce((max, f) => Math.max(max, f.execution_order), -1);
    setEditingFilter({
      filter_id: '',
      name: 'New Filter',
      filter_type: 'javascript',
      execution_order: maxOrder + 1,
      js_script: getDefaultScript('javascript'),
      config_json: '',
      is_active: true,
    });
    setEditorValue(getDefaultScript('javascript'));
  };

  const saveFilter = async () => {
    if (!selectedRoute || !editingFilter) return;
    setSaving(true);
    try {
      const payload = {
        name: editingFilter.name,
        filter_type: editingFilter.filter_type,
        execution_order: editingFilter.execution_order,
        js_script: editorValue,
        config_json: editingFilter.config_json,
        is_active: editingFilter.is_active,
      };

      if (editingFilter.filter_id) {
        await updateFilter(editingFilter.filter_id, payload);
      } else {
        await createFilter(selectedRoute.route_id, payload);
      }

      const f = await getFilters(selectedRoute.route_id);
      setFilters(f.filters);
      setEditingFilter(null);
    } catch (err) {
      console.error('Save failed:', err);
    }
    setSaving(false);
  };

  const openCreateRoute = () => { setRouteForm(emptyRouteForm); setEditingRouteId(null); setShowRouteForm(true); };
  const openEditRoute = (r: Route) => {
    setRouteForm({
      name: r.name, description: r.description,
      source_comm_point_id: r.source_comm_point_id, dest_comm_point_id: r.dest_comm_point_id,
      source_topic: r.source_topic, destination_topic: r.destination_topic, is_active: r.is_active,
    });
    setEditingRouteId(r.route_id);
    setShowRouteForm(true);
  };
  const saveRoute = async () => {
    setSaving(true);
    if (editingRouteId) {
      await updateRoute(editingRouteId, routeForm);
    } else {
      await createRoute(routeForm);
    }
    setSaving(false);
    setShowRouteForm(false);
    loadRoutes();
  };
  const deleteRouteById = async (id: string) => {
    if (!confirm('Delete this route and all its filters?')) return;
    await deleteRoute(id);
    if (selectedRoute?.route_id === id) { setSelectedRoute(null); setFilters([]); }
    loadRoutes();
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex">

        {/* Route Form Modal */}
        {showRouteForm && (
          <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowRouteForm(false)}>
            <div className="bg-arteria-surface border border-arteria-border rounded-lg w-[500px] p-6" onClick={(e) => e.stopPropagation()}>
              <h3 className="text-lg font-semibold text-white mb-4">{editingRouteId ? 'Edit' : 'Create'} Route</h3>
              <div className="space-y-3">
                <div>
                  <label className="text-xs text-arteria-muted">Name</label>
                  <input value={routeForm.name} onChange={(e) => setRouteForm({ ...routeForm, name: e.target.value })}
                    className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                </div>
                <div>
                  <label className="text-xs text-arteria-muted">Description</label>
                  <input value={routeForm.description} onChange={(e) => setRouteForm({ ...routeForm, description: e.target.value })}
                    className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-xs text-arteria-muted">Source Comm Point</label>
                    <select value={routeForm.source_comm_point_id} onChange={(e) => setRouteForm({ ...routeForm, source_comm_point_id: e.target.value })}
                      className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                      <option value="">— Select —</option>
                      {commPoints.filter(c => c.direction === 'INPUT').map(c => (
                        <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>
                      ))}
                    </select>
                  </div>
                  <div>
                    <label className="text-xs text-arteria-muted">Dest Comm Point</label>
                    <select value={routeForm.dest_comm_point_id} onChange={(e) => setRouteForm({ ...routeForm, dest_comm_point_id: e.target.value })}
                      className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                      <option value="">— Select —</option>
                      {commPoints.filter(c => c.direction === 'OUTPUT').map(c => (
                        <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>
                      ))}
                    </select>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-xs text-arteria-muted">Source Topic (e.g. ADT^A01, *)</label>
                    <input value={routeForm.source_topic} onChange={(e) => setRouteForm({ ...routeForm, source_topic: e.target.value })}
                      className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white font-mono" />
                  </div>
                  <div>
                    <label className="text-xs text-arteria-muted">Destination Topic</label>
                    <input value={routeForm.destination_topic} onChange={(e) => setRouteForm({ ...routeForm, destination_topic: e.target.value })}
                      className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white font-mono" />
                  </div>
                </div>
                <label className="flex items-center gap-2 text-sm text-gray-300">
                  <input type="checkbox" checked={routeForm.is_active} onChange={(e) => setRouteForm({ ...routeForm, is_active: e.target.checked })} className="rounded" />
                  Active
                </label>
              </div>
              <div className="flex justify-end gap-2 mt-5">
                <button onClick={() => setShowRouteForm(false)} className="px-4 py-2 text-sm text-arteria-muted hover:text-white">Cancel</button>
                <button onClick={saveRoute} disabled={saving || !routeForm.name || !routeForm.source_topic}
                  className="px-4 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80 disabled:opacity-50">
                  {saving ? 'Saving...' : editingRouteId ? 'Update' : 'Create'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Route list */}
        <div className="w-72 border-r border-arteria-border overflow-y-auto">
          <div className="px-5 py-4 border-b border-arteria-border flex items-center justify-between">
            <h2 className="font-semibold text-white">Routes</h2>
            <button onClick={openCreateRoute} className="text-xs px-2 py-1 bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">+ New</button>
          </div>
          {routes.map((r) => (
            <div
              key={r.route_id}
              className={`w-full text-left px-5 py-3 border-b border-arteria-border/50 transition-colors ${
                selectedRoute?.route_id === r.route_id ? 'bg-arteria-accent/10' : 'hover:bg-white/[0.02]'
              }`}
            >
              <button onClick={() => selectRoute(r)} className="w-full text-left">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-white">{r.name}</span>
                  <span className={`w-2 h-2 rounded-full ${r.is_active ? 'bg-arteria-success' : 'bg-arteria-muted'}`} />
                </div>
                <p className="text-xs text-arteria-muted mt-0.5">{r.source_topic} → {r.destination_topic}</p>
              </button>
              <div className="flex gap-2 mt-1">
                <button onClick={() => openEditRoute(r)} className="text-[10px] text-arteria-muted hover:text-white">Edit</button>
                <button onClick={() => deleteRouteById(r.route_id)} className="text-[10px] text-red-400 hover:text-red-300">Delete</button>
              </div>
            </div>
          ))}
          {routes.length === 0 && <p className="p-5 text-sm text-arteria-muted">No routes configured</p>}
        </div>

        {/* Filter chain + editor */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {selectedRoute ? (
            <>
              <div className="px-6 py-4 border-b border-arteria-border flex items-center justify-between">
                <div>
                  <h3 className="font-semibold text-white">{selectedRoute.name}</h3>
                  <p className="text-xs text-arteria-muted">{selectedRoute.description}</p>
                </div>
                <button onClick={newFilter} className="px-3 py-1.5 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80">
                  + Add Filter
                </button>
              </div>

              {/* Filter chain */}
              <div className="px-6 py-4 border-b border-arteria-border">
                <p className="text-xs text-arteria-muted uppercase tracking-wider mb-2">Filter Chain</p>
                <div className="flex items-center gap-2 flex-wrap">
                  {filters.map((f, i) => (
                    <div key={f.filter_id || i} className="flex items-center gap-2">
                      <button
                        onClick={() => editFilter(f)}
                        className={`px-3 py-1.5 rounded text-xs font-medium border transition-colors ${
                          editingFilter?.filter_id === f.filter_id
                            ? 'border-arteria-accent bg-arteria-accent/20 text-white'
                            : 'border-arteria-border bg-arteria-surface text-gray-300 hover:border-arteria-accent/50'
                        } ${!f.is_active ? 'opacity-50' : ''}`}
                      >
                        <span className="text-arteria-muted mr-1">#{f.execution_order}</span>
                        {f.name}
                        <span className="ml-1 text-arteria-muted">({f.filter_type})</span>
                      </button>
                      {i < filters.length - 1 && <span className="text-arteria-muted">→</span>}
                    </div>
                  ))}
                  {filters.length === 0 && <span className="text-sm text-arteria-muted">No filters — messages pass through unmodified</span>}
                </div>
              </div>

              {/* Monaco Editor */}
              {editingFilter && (
                <div className="flex-1 flex flex-col overflow-hidden">
                  <div className="px-6 py-3 border-b border-arteria-border flex items-center justify-between">
                    <div className="flex items-center gap-4">
                      <input
                        value={editingFilter.name}
                        onChange={(e) => setEditingFilter({ ...editingFilter, name: e.target.value })}
                        className="bg-transparent text-white text-sm font-medium border-b border-arteria-border focus:border-arteria-accent outline-none"
                      />
                      <select
                        value={editingFilter.filter_type}
                        onChange={(e) => {
                          setEditingFilter({ ...editingFilter, filter_type: e.target.value });
                          setEditorValue(getDefaultScript(e.target.value));
                        }}
                        className="bg-arteria-bg text-sm text-gray-300 border border-arteria-border rounded px-2 py-1"
                      >
                        <option value="javascript">JavaScript Transform</option>
                        <option value="conditional">Conditional Router</option>
                        <option value="lookup">Lookup Enrichment</option>
                      </select>
                      <label className="flex items-center gap-1.5 text-sm text-arteria-muted">
                        <input
                          type="checkbox"
                          checked={editingFilter.is_active}
                          onChange={(e) => setEditingFilter({ ...editingFilter, is_active: e.target.checked })}
                          className="rounded"
                        />
                        Active
                      </label>
                    </div>
                    <div className="flex gap-2">
                      <button onClick={() => setEditingFilter(null)} className="px-3 py-1.5 text-sm text-arteria-muted hover:text-white">
                        Cancel
                      </button>
                      <button onClick={saveFilter} disabled={saving} className="px-3 py-1.5 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80 disabled:opacity-50">
                        {saving ? 'Saving...' : 'Save Filter'}
                      </button>
                    </div>
                  </div>
                  <div className="flex-1">
                    <MonacoEditor
                      height="100%"
                      language="javascript"
                      theme="vs-dark"
                      value={editorValue}
                      onChange={(v) => setEditorValue(v || '')}
                      options={{
                        minimap: { enabled: false },
                        fontSize: 13,
                        lineNumbers: 'on',
                        scrollBeyondLastLine: false,
                        automaticLayout: true,
                        padding: { top: 12 },
                      }}
                    />
                  </div>
                </div>
              )}

              {!editingFilter && (
                <div className="flex-1 flex items-center justify-center text-arteria-muted text-sm">
                  Select a filter to edit or click &quot;+ Add Filter&quot; to create one
                </div>
              )}
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-arteria-muted text-sm">
              Select a route to view its filter chain
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function getDefaultScript(filterType: string): string {
  switch (filterType) {
    case 'conditional':
      return `// Conditional filter: return { action: "pass" }, { action: "reject", reason: "..." }, or { action: "route_to", route_to: "dest" }
function evaluate(msg) {
  if (!msg.patientId) {
    return { action: "reject", reason: "Missing Patient ID" };
  }
  return { action: "pass" };
}`;
    case 'javascript':
    default:
      return `// Transform filter: modify the message and return it
function transform(msg) {
  // msg.properties, msg.patientId, msg.messageType, etc.
  msg.properties.processed_at = new Date().toISOString();
  return msg;
}`;
  }
}

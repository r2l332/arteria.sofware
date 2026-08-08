'use client';

import { useCallback, useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import { getRoutes, getFilters, getCommPoints, createFilter, updateFilter, createRoute, updateRoute, deleteRoute, apiFetch, type Route, type Filter, type CommPoint } from '@/lib/api';
import { ReactFlow, Controls, useNodesState, useEdgesState, addEdge, Node, Edge, Connection, NodeTypes, Handle, Position } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Plus, Trash2, Edit2, GitBranch, Radio, Cpu, Send, Copy, ChevronRight } from 'lucide-react';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

// --- Custom Nodes ---
function SourceNode({ data }: { data: any }) {
  const [editing, setEditing] = useState(false);
  return (
    <div className="w-[110px] rounded-lg border p-2 backdrop-blur-xl bg-slate-900/80 shadow-lg border-slate-800 hover:scale-[1.03] transition-all duration-200" onDoubleClick={() => setEditing(true)}>
      <Handle type="source" position={Position.Right} className="!bg-sky-400 !w-2 !h-2" />
      <div className="flex items-center gap-1.5">
        <div className="w-5 h-5 rounded bg-sky-500/10 flex items-center justify-center shrink-0"><Radio className="w-3 h-3 text-sky-400" /></div>
        <div className="flex-1 min-w-0">
          <div className="text-[9px] font-semibold text-white truncate">{data.label}</div>
          <div className="text-[7px] text-gray-500 font-mono truncate">{data.sub}</div>
        </div>
      </div>
      {editing && data.onChangeCP && (
        <select autoFocus className="w-full mt-1 px-1 py-0.5 bg-gray-800 border border-sky-700 rounded text-[8px] text-white" defaultValue={data.cpId}
          onChange={(e) => { data.onChangeCP(e.target.value); setEditing(false); }} onBlur={() => setEditing(false)}>
          {data.cpOptions?.map((cp: any) => <option key={cp.id} value={cp.id}>{cp.name}</option>)}
        </select>
      )}
    </div>
  );
}
function FilterNode({ data }: { data: any }) {
  return (
    <div className={`w-[110px] rounded-lg border p-2 backdrop-blur-xl shadow-lg hover:scale-[1.03] transition-all duration-200 ${data.active !== false ? 'bg-slate-900/80 border-slate-800' : 'bg-slate-900/40 border-slate-800/50 opacity-50'}`}>
      <Handle type="target" position={Position.Left} className="!bg-amber-400 !w-2 !h-2" />
      <Handle type="source" position={Position.Right} className="!bg-amber-400 !w-2 !h-2" />
      <div className="flex items-center gap-1.5">
        <div className="w-5 h-5 rounded bg-amber-500/10 flex items-center justify-center shrink-0"><Cpu className="w-3 h-3 text-amber-400" /></div>
        <div className="flex-1 min-w-0">
          <div className="text-[9px] font-semibold text-white truncate">{data.label}</div>
          <div className="text-[7px] text-gray-500 font-mono truncate">{data.sub}</div>
        </div>
      </div>
    </div>
  );
}
function DestNode({ data }: { data: any }) {
  const [editing, setEditing] = useState(false);
  return (
    <div className="w-[110px] rounded-lg border p-2 backdrop-blur-xl bg-slate-900/80 shadow-lg border-slate-800 hover:scale-[1.03] transition-all duration-200" onDoubleClick={() => setEditing(true)}>
      <Handle type="target" position={Position.Left} className="!bg-emerald-400 !w-2 !h-2" />
      <div className="flex items-center gap-1.5">
        <div className="w-5 h-5 rounded bg-emerald-500/10 flex items-center justify-center shrink-0"><Send className="w-3 h-3 text-emerald-400" /></div>
        <div className="flex-1 min-w-0">
          <div className="text-[9px] font-semibold text-white truncate">{data.label}</div>
          <div className="text-[7px] text-gray-500 font-mono truncate">{data.sub}</div>
        </div>
      </div>
      {editing && data.onChangeCP && (
        <select autoFocus className="w-full mt-1 px-1 py-0.5 bg-gray-800 border border-emerald-700 rounded text-[8px] text-white" defaultValue={data.cpId}
          onChange={(e) => { data.onChangeCP(e.target.value); setEditing(false); }} onBlur={() => setEditing(false)}>
          {data.cpOptions?.map((cp: any) => <option key={cp.id} value={cp.id}>{cp.name}</option>)}
        </select>
      )}
    </div>
  );
}

const nodeTypes: NodeTypes = { source: SourceNode, filter: FilterNode, destination: DestNode };

export default function RoutesPage() {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);
  const [selectedRoute, setSelectedRoute] = useState<Route | null>(null);
  const [filters, setFilters] = useState<Filter[]>([]);
  const [editingFilter, setEditingFilter] = useState<Filter | null>(null);
  const [editorValue, setEditorValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [showNewRoute, setShowNewRoute] = useState(false);
  const [canvasHeight, setCanvasHeight] = useState(220);
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  const loadRoutes = () => getRoutes().then(r => setRoutes(r.routes || [])).catch(() => {});
  useEffect(() => {
    loadRoutes();
    getCommPoints().then(r => setCommPoints(r.communication_points || [])).catch(() => {});
    const h = () => loadRoutes();
    window.addEventListener('arteria:config_change', h);
    return () => window.removeEventListener('arteria:config_change', h);
  }, []);

  const selectRoute = async (route: Route) => {
    setSelectedRoute(route); setEditingFilter(null);
    const f = await getFilters(route.route_id);
    setFilters((f.filters || []).sort((a: Filter, b: Filter) => a.execution_order - b.execution_order));
  };

  // Build canvas nodes/edges for the selected route
  useEffect(() => {
    if (!selectedRoute || commPoints.length === 0) { setNodes([]); setEdges([]); return; }
    const getCPName = (id: string) => commPoints.find(c => c.comm_point_id === id)?.name || id?.slice(0, 8);
    const getCPSub = (id: string) => { const cp = commPoints.find(c => c.comm_point_id === id); return cp ? `${cp.protocol} :${cp.port}` : ''; };

    const n: Node[] = [];
    const e: Edge[] = [];
    const startX = 30, y = 50, spacing = 150;

    const inputCPs = commPoints.filter(c => c.direction === 'INPUT').map(c => ({ id: c.comm_point_id, name: `${c.name} (${c.protocol}:${c.port})` }));
    const outputCPs = commPoints.filter(c => c.direction === 'OUTPUT').map(c => ({ id: c.comm_point_id, name: `${c.name} (${c.protocol}:${c.port})` }));

    const changeSourceCP = async (cpId: string) => {
      await apiFetch(`/routes/${selectedRoute.route_id}/rewire`, { method: 'PATCH', body: JSON.stringify({ source_comm_point_id: cpId }) });
      loadRoutes();
    };
    const changeDestCP = async (cpId: string) => {
      await apiFetch(`/routes/${selectedRoute.route_id}/rewire`, { method: 'PATCH', body: JSON.stringify({ dest_comm_point_id: cpId }) });
      loadRoutes();
    };

    // Source CP
    n.push({ id: 'src', type: 'source', position: { x: startX, y }, data: { label: getCPName(selectedRoute.source_comm_point_id), sub: getCPSub(selectedRoute.source_comm_point_id), cpId: selectedRoute.source_comm_point_id, cpOptions: inputCPs, onChangeCP: changeSourceCP } });

    // Filters
    filters.forEach((f, i) => {
      const x = startX + (i + 1) * spacing;
      n.push({ id: `f-${i}`, type: 'filter', position: { x, y: y + 5 }, draggable: true, data: { label: f.name, sub: f.filter_type, active: f.is_active } });
      if (i === 0) e.push({ id: 'e-src-f0', source: 'src', target: 'f-0', animated: true, style: { stroke: '#38bdf8', strokeWidth: 2 } });
      else e.push({ id: `e-f${i-1}-f${i}`, source: `f-${i-1}`, target: `f-${i}`, animated: true, style: { stroke: '#f59e0b', strokeWidth: 2 } });
    });

    // Dest CP
    const destX = startX + (filters.length + 1) * spacing;
    n.push({ id: 'dst', type: 'destination', position: { x: destX, y }, data: { label: getCPName(selectedRoute.dest_comm_point_id), sub: getCPSub(selectedRoute.dest_comm_point_id), cpId: selectedRoute.dest_comm_point_id, cpOptions: outputCPs, onChangeCP: changeDestCP } });

    if (filters.length > 0) e.push({ id: 'e-flast-dst', source: `f-${filters.length-1}`, target: 'dst', animated: true, style: { stroke: '#10b981', strokeWidth: 2 } });
    else e.push({ id: 'e-src-dst', source: 'src', target: 'dst', animated: true, style: { stroke: '#a855f7', strokeWidth: 2 } });

    setNodes(n); setEdges(e);
  }, [selectedRoute, filters, commPoints]);

  const [editingCP, setEditingCP] = useState<'source' | 'dest' | null>(null);

  const onNodeClick = useCallback((_: any, node: Node) => {
    if (node.id.startsWith('f-')) {
      const idx = parseInt(node.id.split('-')[1]);
      if (filters[idx]) { setEditingFilter(filters[idx]); setEditorValue(filters[idx].js_script || getDefaultScript(filters[idx].filter_type)); }
    } else if (node.id === 'src') {
      setEditingCP('source');
    } else if (node.id === 'dst') {
      setEditingCP('dest');
    }
  }, [filters]);

  const onConnect = useCallback((conn: Connection) => { setEdges(eds => addEdge({ ...conn, animated: true }, eds)); }, []);

  // Delete filter on Backspace/Delete key when a filter node is selected
  const onKeyDown = useCallback(async (e: KeyboardEvent) => {
    if ((e.key === 'Delete' || e.key === 'Backspace') && editingFilter && selectedRoute) {
      if (document.activeElement?.tagName === 'INPUT' || document.activeElement?.tagName === 'TEXTAREA') return;
      if (!confirm(`Delete filter "${editingFilter.name}"?`)) return;
      if (editingFilter.filter_id) {
        await apiFetch(`/routes/${selectedRoute.route_id}/filters/${editingFilter.execution_order}`, { method: 'DELETE' });
        const f = await getFilters(selectedRoute.route_id);
        setFilters((f.filters || []).sort((a: Filter, b: Filter) => a.execution_order - b.execution_order));
      }
      setEditingFilter(null);
    }
  }, [editingFilter, selectedRoute]);

  useEffect(() => {
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onKeyDown]);

  const newFilter = () => {
    if (!selectedRoute) return;
    const maxOrder = filters.reduce((max, f) => Math.max(max, f.execution_order), -1);
    setEditingFilter({ filter_id: '', name: 'New Filter', filter_type: 'javascript', execution_order: maxOrder + 1, js_script: getDefaultScript('javascript'), config_json: '', is_active: true });
    setEditorValue(getDefaultScript('javascript'));
  };

  const saveFilter = async () => {
    if (!selectedRoute || !editingFilter) return;
    setSaving(true);
    const payload = { name: editingFilter.name, filter_type: editingFilter.filter_type, execution_order: editingFilter.execution_order, js_script: editorValue, config_json: editingFilter.config_json || '', is_active: editingFilter.is_active };
    if (editingFilter.filter_id) await updateFilter(editingFilter.filter_id, payload); else await createFilter(selectedRoute.route_id, payload);
    const f = await getFilters(selectedRoute.route_id);
    setFilters((f.filters || []).sort((a: Filter, b: Filter) => a.execution_order - b.execution_order));
    setEditingFilter(null); setSaving(false);
  };

  const cloneRoute = async (r: Route) => {
    const f = await getFilters(r.route_id);
    const newRoute: any = await createRoute({ name: `${r.name} (Copy)`, description: r.description, source_comm_point_id: r.source_comm_point_id, dest_comm_point_id: r.dest_comm_point_id, source_topic: r.source_topic, destination_topic: r.destination_topic, is_active: false } as any);
    if (newRoute.route_id) {
      for (const filter of (f.filters || [])) {
        await createFilter(newRoute.route_id, { name: filter.name, filter_type: filter.filter_type, execution_order: filter.execution_order, js_script: filter.js_script, config_json: filter.config_json, is_active: filter.is_active });
      }
    }
    loadRoutes();
  };

  const deleteRouteById = async (id: string) => { if (!confirm('Delete this route and all its filters?')) return; await deleteRoute(id); if (selectedRoute?.route_id === id) { setSelectedRoute(null); setFilters([]); } loadRoutes(); };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex">
        {/* Left panel: Route list */}
        <div className="w-64 border-r border-arteria-border flex flex-col overflow-hidden shrink-0">
          <div className="px-4 py-3 border-b border-arteria-border flex items-center justify-between">
            <h2 className="text-sm font-semibold text-white">Routes</h2>
            <button onClick={() => setShowNewRoute(true)} className="p-1.5 bg-arteria-accent text-white rounded-lg hover:bg-arteria-accent/80"><Plus className="w-3.5 h-3.5" /></button>
          </div>
          <div className="flex-1 overflow-y-auto">
            {routes.map(r => (
              <div key={r.route_id} onClick={() => selectRoute(r)}
                className={`px-4 py-3 border-b border-arteria-border/30 cursor-pointer transition-colors group ${selectedRoute?.route_id === r.route_id ? 'bg-arteria-accent/10 border-l-2 border-l-arteria-accent' : 'hover:bg-white/[0.02]'}`}>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className={`w-2 h-2 rounded-full shrink-0 ${r.is_active ? 'bg-green-400' : 'bg-gray-600'}`} />
                    <span className="text-xs text-white font-medium truncate">{r.name}</span>
                  </div>
                  <div className="hidden group-hover:flex gap-1">
                    <button onClick={(e) => { e.stopPropagation(); cloneRoute(r); }} className="p-1 text-gray-500 hover:text-cyan-400" title="Clone"><Copy className="w-3 h-3" /></button>
                    <button onClick={(e) => { e.stopPropagation(); deleteRouteById(r.route_id); }} className="p-1 text-gray-500 hover:text-red-400" title="Delete"><Trash2 className="w-3 h-3" /></button>
                  </div>
                </div>
                <div className="flex items-center gap-1 mt-1">
                  <span className="text-[9px] font-mono px-1.5 py-0.5 rounded bg-purple-900/30 text-purple-300">{r.source_topic}</span>
                  <ChevronRight className="w-3 h-3 text-gray-700" />
                  <span className="text-[9px] font-mono text-gray-500">{r.destination_topic}</span>
                </div>
              </div>
            ))}
            {routes.length === 0 && <p className="p-4 text-xs text-gray-600">No routes. Click + to create one.</p>}
          </div>
        </div>

        {/* Center: Canvas + editor below */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {selectedRoute ? (
            <>
              <div className="px-4 py-2 border-b border-arteria-border flex items-center justify-between shrink-0 bg-gray-900/30">
                <div className="flex items-center gap-3">
                  <GitBranch className="w-4 h-4 text-purple-400" />
                  <span className="text-sm font-semibold text-white">{selectedRoute.name}</span>
                  <span className="text-[9px] text-gray-500">{selectedRoute.description}</span>
                </div>
                <button onClick={newFilter} className="text-xs px-3 py-1.5 bg-amber-600 text-white rounded-lg hover:bg-amber-500 flex items-center gap-1"><Plus className="w-3 h-3" />Add Filter</button>
              </div>
              <div className="shrink-0 relative overflow-x-auto border-b border-gray-800/30 bg-[radial-gradient(#1e293b_1px,transparent_1px)] [background-size:24px_24px]" style={{ height: canvasHeight }}>
                <div className="absolute inset-0 bg-gradient-to-tr from-cyan-950/10 via-transparent to-purple-950/10 pointer-events-none" />
                <ReactFlow
                  nodes={nodes} edges={edges}
                  onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
                  onConnect={onConnect} onNodeClick={onNodeClick}
                  nodeTypes={nodeTypes} fitView
                  className="!bg-transparent"
                  proOptions={{ hideAttribution: true }}
                  defaultEdgeOptions={{ type: 'smoothstep', animated: true, style: { strokeWidth: 2 } }}
                >
                  <Controls className="!bg-slate-900/80 !border-slate-700 !rounded-xl !backdrop-blur-xl [&>button]:!bg-slate-800 [&>button]:!border-slate-700 [&>button]:!text-gray-300" />
                </ReactFlow>
              </div>

              {/* Resize handle */}
              {editingFilter && (
                <div className="h-1.5 shrink-0 cursor-row-resize bg-arteria-border/50 hover:bg-arteria-accent/50 active:bg-arteria-accent transition-colors flex items-center justify-center group"
                  onMouseDown={(e) => {
                    e.preventDefault();
                    const startY = e.clientY;
                    const startH = canvasHeight;
                    const onMove = (ev: MouseEvent) => {
                      const newH = Math.max(100, Math.min(500, startH + (ev.clientY - startY)));
                      setCanvasHeight(newH);
                    };
                    const onUp = () => { document.removeEventListener('mousemove', onMove); document.removeEventListener('mouseup', onUp); };
                    document.addEventListener('mousemove', onMove);
                    document.addEventListener('mouseup', onUp);
                  }}>
                  <div className="w-8 h-0.5 rounded bg-gray-600 group-hover:bg-arteria-accent" />
                </div>
              )}

              {/* Editor panel below canvas */}
              {editingFilter ? (
                <div className="flex-1 flex flex-col overflow-hidden border-t border-arteria-border">
                  <div className="px-4 py-2 border-b border-arteria-border flex items-center justify-between shrink-0 bg-gray-900/30">
                    <div className="flex items-center gap-2">
                      <Cpu className="w-3.5 h-3.5 text-amber-400" />
                      <input value={editingFilter.name} onChange={e => setEditingFilter({ ...editingFilter, name: e.target.value })} className="bg-transparent text-white text-sm font-medium border-b border-gray-700 focus:border-arteria-accent outline-none w-32" />
                      <select value={editingFilter.filter_type} onChange={e => { setEditingFilter({ ...editingFilter, filter_type: e.target.value }); setEditorValue(getDefaultScript(e.target.value)); }} className="bg-gray-800 text-[10px] text-gray-300 border border-gray-700 rounded px-1.5 py-0.5">
                        <option value="javascript">JavaScript</option><option value="conditional">Conditional</option><option value="python">Python</option><option value="bash">Bash</option><option value="dotnet">.NET</option><option value="connector">Connector</option><option value="lookup">Lookup</option>
                      </select>
                    </div>
                    <div className="flex gap-2 items-center">
                      <label className="flex items-center gap-1 text-[10px] text-gray-400 cursor-pointer">
                        <input type="checkbox" checked={editingFilter.is_active} onChange={e => setEditingFilter({ ...editingFilter, is_active: e.target.checked })}
                          className="w-3 h-3 accent-emerald-500" />
                        Active
                      </label>
                      {editingFilter.filter_id && (
                        <button onClick={async () => {
                          if (!confirm(`Delete filter "${editingFilter.name}"?`)) return;
                          await apiFetch(`/routes/${selectedRoute!.route_id}/filters/${editingFilter.execution_order}`, { method: 'DELETE' });
                          const f = await getFilters(selectedRoute!.route_id);
                          setFilters((f.filters || []).sort((a: Filter, b: Filter) => a.execution_order - b.execution_order));
                          setEditingFilter(null);
                        }} className="text-[10px] px-2 py-0.5 text-red-400 hover:text-red-300 hover:bg-red-900/30 rounded">Delete</button>
                      )}
                      <button onClick={() => setEditingFilter(null)} className="text-[10px] text-gray-500 hover:text-white">Cancel</button>
                      <button onClick={saveFilter} disabled={saving} className="text-[10px] px-2.5 py-1 bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">{saving ? '...' : 'Save'}</button>
                    </div>
                  </div>
                  <div className="flex-1">
                    <MonacoEditor height="100%"
                      language={editingFilter.filter_type === 'python' ? 'python' : editingFilter.filter_type === 'bash' ? 'shell' : editingFilter.filter_type === 'dotnet' ? 'csharp' : editingFilter.filter_type === 'connector' ? 'json' : 'javascript'}
                      theme="vs-dark" value={editorValue} onChange={v => setEditorValue(v || '')}
                      options={{ minimap: { enabled: false }, fontSize: 12, lineNumbers: 'on', scrollBeyondLastLine: false, automaticLayout: true, padding: { top: 8 } }} />
                  </div>
                </div>
              ) : editingCP && selectedRoute ? (
                <div className="shrink-0 px-4 py-3 border-t border-arteria-border bg-gray-900/30">
                  <div className="flex items-center gap-3">
                    <span className="text-xs font-medium text-white">{editingCP === 'source' ? 'Source' : 'Destination'} CP:</span>
                    <select value={editingCP === 'source' ? selectedRoute.source_comm_point_id : selectedRoute.dest_comm_point_id}
                      onChange={async (e) => {
                        const field = editingCP === 'source' ? 'source_comm_point_id' : 'dest_comm_point_id';
                        await apiFetch(`/routes/${selectedRoute.route_id}/rewire`, { method: 'PATCH', body: JSON.stringify({ [field]: e.target.value }) });
                        loadRoutes(); setEditingCP(null);
                      }}
                      className="flex-1 px-3 py-1.5 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white">
                      {commPoints.filter(c => editingCP === 'source' ? c.direction === 'INPUT' : c.direction === 'OUTPUT').map(c => (
                        <option key={c.comm_point_id} value={c.comm_point_id}>{c.name} ({c.protocol}:{c.port})</option>
                      ))}
                    </select>
                    <button onClick={() => setEditingCP(null)} className="text-xs text-gray-500 hover:text-white">✕</button>
                  </div>
                </div>
              ) : (
                <div className="px-4 py-2 text-[10px] text-gray-600 shrink-0">
                  Click a filter node to edit • Double-click source/dest to change CP • {filters.length} filter{filters.length !== 1 ? 's' : ''} in chain
                </div>
              )}
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-gray-600 text-sm">Select a route from the left panel or create a new one</div>
          )}
        </div>

        {/* New Route Modal */}
        {showNewRoute && <NewRouteModal commPoints={commPoints} onClose={() => setShowNewRoute(false)} onCreated={loadRoutes} />}
      </main>
    </div>
  );
}

function NewRouteModal({ commPoints, onClose, onCreated }: { commPoints: CommPoint[]; onClose: () => void; onCreated: () => void }) {
  const [form, setForm] = useState({ name: '', description: '', source_comm_point_id: '', dest_comm_point_id: '', source_topic: '', destination_topic: '', is_active: true });
  const save = async () => { await createRoute(form as any); onCreated(); onClose(); };
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-arteria-surface border border-arteria-border rounded-xl w-[480px] p-6" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-4">New Route</h3>
        <div className="space-y-3">
          <div><label className="text-[10px] text-gray-500 uppercase">Name</label><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
          <div><label className="text-[10px] text-gray-500 uppercase">Description</label><input value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="text-[10px] text-gray-500 uppercase">Source CP</label><select value={form.source_comm_point_id} onChange={e => setForm({ ...form, source_comm_point_id: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white"><option value="">Select...</option>{commPoints.filter(c => c.direction === 'INPUT').map(c => <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>)}</select></div>
            <div><label className="text-[10px] text-gray-500 uppercase">Dest CP</label><select value={form.dest_comm_point_id} onChange={e => setForm({ ...form, dest_comm_point_id: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white"><option value="">Select...</option>{commPoints.filter(c => c.direction === 'OUTPUT').map(c => <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>)}</select></div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="text-[10px] text-gray-500 uppercase">Source Topic</label><input value={form.source_topic} onChange={e => setForm({ ...form, source_topic: e.target.value })} placeholder="ADT^A01 or *" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" /></div>
            <div><label className="text-[10px] text-gray-500 uppercase">Dest Topic</label><input value={form.destination_topic} onChange={e => setForm({ ...form, destination_topic: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" /></div>
          </div>
        </div>
        <div className="flex justify-end gap-2 mt-5">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-400 hover:text-white">Cancel</button>
          <button onClick={save} disabled={!form.name || !form.source_topic} className="px-4 py-2 bg-arteria-accent text-white text-sm rounded-lg hover:bg-arteria-accent/80 disabled:opacity-50">Create</button>
        </div>
      </div>
    </div>
  );
}

function getDefaultScript(t: string): string {
  switch (t) {
    case 'conditional': return 'function evaluate(msg) {\n  if (!msg.patientId) return { action: "reject", reason: "Missing PID" };\n  return { action: "pass" };\n}';
    case 'python': return 'import sys, json\n\nmsg = json.load(sys.stdin)\nmsg["properties"]["filter_lang"] = "python"\nprint(json.dumps(msg))';
    case 'bash': return '#!/bin/bash\nINPUT=$(cat)\necho "$INPUT" | jq \'.properties.filter_lang = "bash"\'';
    case 'dotnet': return 'using System;\nvar input = Console.In.ReadToEnd();\nConsole.Write(input);';
    case 'connector': return JSON.stringify({ connector_type: "HTTP", url: "https://api.example.com", method: "POST", timeout_ms: 5000, response_property: "api_response", response_status_property: "api_status" }, null, 2);
    default: return 'function transform(msg) {\n  msg.properties.processed_at = new Date().toISOString();\n  return msg;\n}';
  }
}

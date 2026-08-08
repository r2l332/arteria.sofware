'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import { getRoutes, getFilters, getCommPoints, createFilter, updateFilter, createRoute, updateRoute, deleteRoute, apiFetch, type Route, type Filter, type CommPoint } from '@/lib/api';
import { ReactFlow, Background, Controls, MiniMap, useNodesState, useEdgesState, addEdge, Node, Edge, Connection, NodeTypes, Handle, Position } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Plus, Trash2, GitBranch, Radio, Cpu, Send, Settings2 } from 'lucide-react';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

// Custom node components
function SourceNode({ data }: { data: any }) {
  return (
    <div className="px-4 py-3 rounded-xl border border-sky-700/60 bg-sky-950/80 backdrop-blur-sm shadow-xl min-w-[140px]">
      <Handle type="source" position={Position.Right} className="!bg-sky-400 !w-2.5 !h-2.5" />
      <div className="flex items-center gap-2">
        <Radio className="w-4 h-4 text-sky-400" />
        <div>
          <div className="text-xs font-semibold text-white">{data.label}</div>
          <div className="text-[9px] text-sky-300/70 font-mono">{data.protocol} :{data.port}</div>
        </div>
      </div>
    </div>
  );
}

function RouteNode({ data }: { data: any }) {
  return (
    <div className="px-4 py-3 rounded-xl border border-purple-700/60 bg-purple-950/80 backdrop-blur-sm shadow-xl min-w-[140px]">
      <Handle type="target" position={Position.Left} className="!bg-purple-400 !w-2.5 !h-2.5" />
      <Handle type="source" position={Position.Right} className="!bg-purple-400 !w-2.5 !h-2.5" />
      <div className="flex items-center gap-2">
        <GitBranch className="w-4 h-4 text-purple-400" />
        <div>
          <div className="text-xs font-semibold text-white">{data.label}</div>
          <div className="text-[9px] text-purple-300/70 font-mono">{data.topic}</div>
        </div>
      </div>
      {data.filterCount > 0 && (
        <div className="mt-1.5 text-[8px] text-amber-400 bg-amber-500/10 px-2 py-0.5 rounded-full inline-block">{data.filterCount} filters</div>
      )}
    </div>
  );
}

function FilterNode({ data }: { data: any }) {
  return (
    <div className={`px-3 py-2 rounded-lg border backdrop-blur-sm shadow-lg min-w-[120px] cursor-pointer hover:ring-1 hover:ring-amber-500/50 transition-all ${data.active ? 'border-amber-700/60 bg-amber-950/80' : 'border-gray-700 bg-gray-900/80 opacity-50'}`}
      onDoubleClick={data.onEdit}>
      <Handle type="target" position={Position.Left} className="!bg-amber-400 !w-2 !h-2" />
      <Handle type="source" position={Position.Right} className="!bg-amber-400 !w-2 !h-2" />
      <div className="flex items-center gap-2">
        <Cpu className="w-3.5 h-3.5 text-amber-400" />
        <div>
          <div className="text-[10px] font-medium text-white">{data.label}</div>
          <div className="text-[8px] text-amber-300/60">{data.filterType}</div>
        </div>
      </div>
    </div>
  );
}

function DestNode({ data }: { data: any }) {
  return (
    <div className="px-4 py-3 rounded-xl border border-emerald-700/60 bg-emerald-950/80 backdrop-blur-sm shadow-xl min-w-[140px]">
      <Handle type="target" position={Position.Left} className="!bg-emerald-400 !w-2.5 !h-2.5" />
      <div className="flex items-center gap-2">
        <Send className="w-4 h-4 text-emerald-400" />
        <div>
          <div className="text-xs font-semibold text-white">{data.label}</div>
          <div className="text-[9px] text-emerald-300/70 font-mono">{data.protocol} :{data.port}</div>
        </div>
      </div>
    </div>
  );
}

const nodeTypes: NodeTypes = {
  source: SourceNode,
  route: RouteNode,
  filter: FilterNode,
  destination: DestNode,
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
    setSelectedRoute(route);
    setEditingFilter(null);
    const f = await getFilters(route.route_id);
    setFilters((f.filters || []).sort((a: Filter, b: Filter) => a.execution_order - b.execution_order));
  };

  // Build React Flow nodes/edges from routes + CPs
  useEffect(() => {
    if (routes.length === 0 || commPoints.length === 0) return;
    const getCPName = (id: string) => commPoints.find(c => c.comm_point_id === id)?.name || id?.slice(0, 8);
    const getCPInfo = (id: string) => commPoints.find(c => c.comm_point_id === id) || { protocol: '?', port: 0 };

    const newNodes: Node[] = [];
    const newEdges: Edge[] = [];
    let y = 0;

    routes.forEach((r, ri) => {
      const srcCP = getCPInfo(r.source_comm_point_id);
      const dstCP = getCPInfo(r.dest_comm_point_id);
      const baseY = y;
      const selected = selectedRoute?.route_id === r.route_id;

      // Source CP
      newNodes.push({ id: `src-${r.route_id}`, type: 'source', position: { x: 50, y: baseY }, data: { label: getCPName(r.source_comm_point_id), protocol: (srcCP as any).protocol, port: (srcCP as any).port } });
      // Route
      newNodes.push({ id: `route-${r.route_id}`, type: 'route', position: { x: 300, y: baseY }, data: { label: r.name, topic: r.source_topic, filterCount: selected ? filters.length : 0, onSelect: () => selectRoute(r) } });
      // Dest CP
      const destX = selected && filters.length > 0 ? 300 + (filters.length + 1) * 200 : 550;
      newNodes.push({ id: `dst-${r.route_id}`, type: 'destination', position: { x: destX, y: baseY }, data: { label: getCPName(r.dest_comm_point_id), protocol: (dstCP as any).protocol, port: (dstCP as any).port } });

      // Edges
      newEdges.push({ id: `e-src-route-${r.route_id}`, source: `src-${r.route_id}`, target: `route-${r.route_id}`, animated: true, style: { stroke: '#38bdf8' } });

      if (selected && filters.length > 0) {
        // Show filter nodes between route and dest
        filters.forEach((f, fi) => {
          const fNodeId = `filter-${r.route_id}-${fi}`;
          newNodes.push({ id: fNodeId, type: 'filter', position: { x: 300 + (fi + 1) * 180, y: baseY + 5 }, draggable: true, data: { label: f.name, filterType: f.filter_type, active: f.is_active, onEdit: () => { setEditingFilter(f); setEditorValue(f.js_script || getDefaultScript(f.filter_type)); } } });
          if (fi === 0) newEdges.push({ id: `e-route-f0-${r.route_id}`, source: `route-${r.route_id}`, target: fNodeId, animated: true, style: { stroke: '#a855f7' } });
          else newEdges.push({ id: `e-f${fi - 1}-f${fi}-${r.route_id}`, source: `filter-${r.route_id}-${fi - 1}`, target: fNodeId, animated: true, style: { stroke: '#f59e0b' } });
        });
        newEdges.push({ id: `e-flast-dst-${r.route_id}`, source: `filter-${r.route_id}-${filters.length - 1}`, target: `dst-${r.route_id}`, animated: true, style: { stroke: '#10b981' } });
      } else {
        newEdges.push({ id: `e-route-dst-${r.route_id}`, source: `route-${r.route_id}`, target: `dst-${r.route_id}`, animated: true, style: { stroke: '#a855f7' } });
      }

      y += selected && filters.length > 0 ? 120 : 90;
    });

    setNodes(newNodes);
    setEdges(newEdges);
  }, [routes, commPoints, selectedRoute, filters]);

  const onNodeClick = useCallback((_: any, node: Node) => {
    if (node.type === 'route') {
      const route = routes.find(r => `route-${r.route_id}` === node.id);
      if (route) selectRoute(route);
    }
    if (node.type === 'filter') {
      const filterIdx = parseInt(node.id.split('-').pop() || '0');
      if (filters[filterIdx]) { setEditingFilter(filters[filterIdx]); setEditorValue(filters[filterIdx].js_script || ''); }
    }
  }, [routes, filters]);

  const onConnect = useCallback((connection: Connection) => {
    setEdges(eds => addEdge({ ...connection, animated: true }, eds));
  }, []);

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
    setEditingFilter(null);
    setSaving(false);
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        {/* Header */}
        <div className="px-5 py-3 border-b border-arteria-border flex items-center justify-between shrink-0">
          <div>
            <h1 className="text-lg font-bold text-white">Routes & Filters</h1>
            <p className="text-[10px] text-arteria-muted">Click a route to expand filters. Double-click a filter to edit. Drag nodes to rearrange.</p>
          </div>
          <div className="flex gap-2">
            {selectedRoute && <button onClick={newFilter} className="text-xs px-3 py-1.5 bg-amber-600 text-white rounded-lg hover:bg-amber-500 flex items-center gap-1"><Plus className="w-3 h-3" />Filter</button>}
            <button onClick={() => setShowRouteForm(true)} className="text-xs px-3 py-1.5 bg-arteria-accent text-white rounded-lg hover:bg-arteria-accent/80 flex items-center gap-1"><Plus className="w-3 h-3" />Route</button>
          </div>
        </div>

        <div className="flex-1 flex overflow-hidden">
          {/* Canvas */}
          <div className="flex-1">
            <ReactFlow
              nodes={nodes} edges={edges}
              onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
              onConnect={onConnect} onNodeClick={onNodeClick}
              nodeTypes={nodeTypes}
              fitView
              className="bg-gray-950"
              defaultEdgeOptions={{ type: 'smoothstep' }}
            >
              <Background color="#1e293b" gap={20} size={1} />
              <Controls className="!bg-gray-900 !border-gray-700 !rounded-lg [&>button]:!bg-gray-800 [&>button]:!border-gray-700 [&>button]:!text-gray-300" />
              <MiniMap className="!bg-gray-900 !border-gray-700" nodeColor={(n) => ({ source: '#38bdf8', route: '#a855f7', filter: '#f59e0b', destination: '#10b981' }[n.type || ''] || '#6b7280')} />
            </ReactFlow>
          </div>

          {/* Right panel: Filter editor */}
          {editingFilter && (
            <div className="w-[420px] border-l border-arteria-border flex flex-col overflow-hidden bg-gray-900/50">
              <div className="px-4 py-3 border-b border-arteria-border flex items-center justify-between shrink-0">
                <div className="flex items-center gap-2">
                  <input value={editingFilter.name} onChange={e => setEditingFilter({ ...editingFilter, name: e.target.value })} className="bg-transparent text-white text-sm font-medium border-b border-gray-700 focus:border-arteria-accent outline-none w-32" />
                  <select value={editingFilter.filter_type} onChange={e => { setEditingFilter({ ...editingFilter, filter_type: e.target.value }); setEditorValue(getDefaultScript(e.target.value)); }} className="bg-gray-800 text-[10px] text-gray-300 border border-gray-700 rounded px-1.5 py-0.5">
                    <option value="javascript">JavaScript</option><option value="conditional">Conditional</option><option value="python">Python</option><option value="bash">Bash</option><option value="dotnet">.NET</option><option value="connector">Connector</option><option value="lookup">Lookup</option>
                  </select>
                </div>
                <div className="flex gap-2">
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
          )}
        </div>

        {/* Route Form Modal */}
        {showRouteForm && <RouteFormModal commPoints={commPoints} onClose={() => setShowRouteForm(false)} onSave={loadRoutes} />}
      </main>
    </div>
  );
}

function RouteFormModal({ commPoints, onClose, onSave }: { commPoints: CommPoint[]; onClose: () => void; onSave: () => void }) {
  const [form, setForm] = useState({ name: '', description: '', source_comm_point_id: '', dest_comm_point_id: '', source_topic: '', destination_topic: '', is_active: true });
  const save = async () => { await createRoute(form as any); onSave(); onClose(); };
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-arteria-surface border border-arteria-border rounded-xl w-[480px] p-6" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-4">New Route</h3>
        <div className="space-y-3">
          <div><label className="text-[10px] text-gray-500 uppercase">Name</label><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="text-[10px] text-gray-500 uppercase">Source CP</label><select value={form.source_comm_point_id} onChange={e => setForm({ ...form, source_comm_point_id: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white"><option value="">Select...</option>{commPoints.filter(c => c.direction === 'INPUT').map(c => <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>)}</select></div>
            <div><label className="text-[10px] text-gray-500 uppercase">Dest CP</label><select value={form.dest_comm_point_id} onChange={e => setForm({ ...form, dest_comm_point_id: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white"><option value="">Select...</option>{commPoints.filter(c => c.direction === 'OUTPUT').map(c => <option key={c.comm_point_id} value={c.comm_point_id}>{c.name}</option>)}</select></div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div><label className="text-[10px] text-gray-500 uppercase">Source Topic</label><input value={form.source_topic} onChange={e => setForm({ ...form, source_topic: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" placeholder="ADT^A01 or *" /></div>
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
    case 'connector': return JSON.stringify({ connector_type: "HTTP", url: "https://api.example.com/lookup", method: "POST", timeout_ms: 5000, response_property: "api_response", response_status_property: "api_status" }, null, 2);
    default: return 'function transform(msg) {\n  msg.properties.processed_at = new Date().toISOString();\n  return msg;\n}';
  }
}

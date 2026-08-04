'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080/api/v1';

interface TunnelNode {
  node_id: string;
  name: string;
  site_name: string;
  enrollment_token: string;
  status: string;
  agent_version: string;
  last_seen: string;
}

interface TunnelMapping {
  local_port: number;
  direction: string;
  target_host: string;
  target_port: number;
  comm_point_id: string;
  protocol: string;
  is_active: boolean;
}

export default function TunnelPage() {
  const [nodes, setNodes] = useState<TunnelNode[]>([]);
  const [selectedNode, setSelectedNode] = useState<TunnelNode | null>(null);
  const [mappings, setMappings] = useState<TunnelMapping[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [showMapping, setShowMapping] = useState(false);
  const [form, setForm] = useState({ name: '', site_name: '' });
  const [mapForm, setMapForm] = useState({
    local_port: 2575, direction: 'INBOUND', target_host: 'ingestion',
    target_port: 2575, comm_point_id: '', protocol: 'MLLP', is_active: true,
  });
  const [enrollInfo, setEnrollInfo] = useState<{ token: string; nodeId: string } | null>(null);

  const load = () => fetch(`${API_BASE}/tunnel/nodes`).then(r => r.json()).then(d => setNodes(d.nodes || []));
  useEffect(() => { load(); }, []);

  const selectNode = async (node: TunnelNode) => {
    setSelectedNode(node);
    const res = await fetch(`${API_BASE}/tunnel/nodes/${node.node_id}/mappings`);
    const data = await res.json();
    setMappings(data.mappings || []);
  };

  const createNode = async () => {
    const res = await fetch(`${API_BASE}/tunnel/nodes`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form),
    });
    const data = await res.json();
    setEnrollInfo({ token: data.enrollment_token, nodeId: data.node_id });
    setShowCreate(false);
    load();
  };

  const deleteNode = async (id: string) => {
    if (!confirm('Delete this tunnel node?')) return;
    await fetch(`${API_BASE}/tunnel/nodes/${id}`, { method: 'DELETE' });
    if (selectedNode?.node_id === id) { setSelectedNode(null); setMappings([]); }
    load();
  };

  const createMapping = async () => {
    if (!selectedNode) return;
    await fetch(`${API_BASE}/tunnel/nodes/${selectedNode.node_id}/mappings`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(mapForm),
    });
    setShowMapping(false);
    selectNode(selectedNode);
  };

  const statusColor: Record<string, string> = {
    PENDING: 'bg-yellow-500/20 text-yellow-400',
    ENROLLED: 'bg-blue-500/20 text-blue-400',
    CONNECTED: 'bg-green-500/20 text-green-400',
    DISCONNECTED: 'bg-red-500/20 text-red-400',
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        <div className="flex items-center justify-between px-8 py-5 border-b border-arteria-border">
          <div>
            <h2 className="text-2xl font-bold text-white">Tunnel Mesh</h2>
            <p className="text-xs text-arteria-muted mt-0.5">Encrypted tunnels to remote sites — no VPN required</p>
          </div>
          <button onClick={() => { setShowCreate(true); setEnrollInfo(null); }}
            className="px-4 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80">
            + New Tunnel Node
          </button>
        </div>

        {/* Enrollment Info Banner */}
        {enrollInfo && (
          <div className="mx-8 mt-4 bg-arteria-surface border border-arteria-accent/50 rounded-lg p-4">
            <p className="text-sm text-white font-medium mb-2">Node Created — Enrollment Instructions</p>
            <p className="text-xs text-arteria-muted mb-3">Run the following command on the remote site to enroll the agent:</p>
            <div className="bg-arteria-bg rounded p-3 font-mono text-xs text-green-400 select-all">
              arteria-agent enroll {enrollInfo.token} --broker &lt;broker-address&gt;:9443
            </div>
            <button onClick={() => setEnrollInfo(null)} className="mt-2 text-xs text-arteria-muted hover:text-white">Dismiss</button>
          </div>
        )}

        <div className="flex-1 overflow-hidden flex">
          {/* Node List */}
          <div className="w-1/2 overflow-y-auto p-6 border-r border-arteria-border">
            {/* Create Modal */}
            {showCreate && (
              <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowCreate(false)}>
                <div className="bg-arteria-surface border border-arteria-border rounded-lg w-[400px] p-6" onClick={e => e.stopPropagation()}>
                  <h3 className="text-lg font-semibold text-white mb-4">Create Tunnel Node</h3>
                  <div className="space-y-3">
                    <div>
                      <label className="text-xs text-arteria-muted">Node Name</label>
                      <input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })}
                        placeholder="e.g., Hospital A Agent"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div>
                      <label className="text-xs text-arteria-muted">Site Name</label>
                      <input value={form.site_name} onChange={e => setForm({ ...form, site_name: e.target.value })}
                        placeholder="e.g., Hospital A, Lab Downtown"
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                  </div>
                  <div className="flex justify-end gap-2 mt-5">
                    <button onClick={() => setShowCreate(false)} className="px-4 py-2 text-sm text-arteria-muted">Cancel</button>
                    <button onClick={createNode} disabled={!form.name}
                      className="px-4 py-2 bg-arteria-accent text-white text-sm rounded disabled:opacity-50">Create</button>
                  </div>
                </div>
              </div>
            )}

            <div className="grid gap-3">
              {nodes.map(node => (
                <div key={node.node_id}
                  onClick={() => selectNode(node)}
                  className={`bg-arteria-surface border rounded-lg p-4 cursor-pointer transition-colors ${
                    selectedNode?.node_id === node.node_id ? 'border-arteria-accent' : 'border-arteria-border hover:border-arteria-accent/50'
                  }`}>
                  <div className="flex items-center justify-between mb-2">
                    <h3 className="font-medium text-white text-sm">{node.name}</h3>
                    <div className="flex items-center gap-2">
                      <span className={`px-2 py-0.5 rounded text-[10px] font-medium ${statusColor[node.status] || 'bg-gray-500/20 text-gray-400'}`}>
                        {node.status}
                      </span>
                      <button onClick={e => { e.stopPropagation(); deleteNode(node.node_id); }}
                        className="text-[10px] text-red-400 hover:text-red-300">Del</button>
                    </div>
                  </div>
                  <div className="text-xs text-arteria-muted space-y-0.5">
                    <p>Site: {node.site_name}</p>
                    {node.agent_version && <p>Agent: v{node.agent_version}</p>}
                    {node.last_seen && <p>Last seen: {new Date(node.last_seen).toLocaleString()}</p>}
                  </div>
                </div>
              ))}
              {nodes.length === 0 && (
                <p className="text-center py-8 text-arteria-muted">No tunnel nodes. Create one to get started.</p>
              )}
            </div>
          </div>

          {/* Mappings Panel */}
          <div className="w-1/2 overflow-y-auto flex flex-col">
            {selectedNode ? (
              <>
                <div className="px-5 py-4 border-b border-arteria-border flex items-center justify-between">
                  <div>
                    <h3 className="font-semibold text-white">{selectedNode.name} — Port Mappings</h3>
                    <p className="text-xs text-arteria-muted">Configure which comm points are tunneled through this node</p>
                  </div>
                  <button onClick={() => setShowMapping(true)}
                    className="px-3 py-1.5 bg-arteria-accent text-white text-xs rounded">+ Add Mapping</button>
                </div>

                {/* Mapping Modal */}
                {showMapping && (
                  <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowMapping(false)}>
                    <div className="bg-arteria-surface border border-arteria-border rounded-lg w-[450px] p-6" onClick={e => e.stopPropagation()}>
                      <h3 className="text-lg font-semibold text-white mb-4">Add Port Mapping</h3>
                      <div className="space-y-3">
                        <div className="grid grid-cols-2 gap-3">
                          <div>
                            <label className="text-xs text-arteria-muted">Direction</label>
                            <select value={mapForm.direction} onChange={e => setMapForm({ ...mapForm, direction: e.target.value })}
                              className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                              <option value="INBOUND">INBOUND (site → Arteria)</option>
                              <option value="OUTBOUND">OUTBOUND (Arteria → site)</option>
                            </select>
                          </div>
                          <div>
                            <label className="text-xs text-arteria-muted">Protocol</label>
                            <select value={mapForm.protocol} onChange={e => setMapForm({ ...mapForm, protocol: e.target.value })}
                              className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                              <option value="MLLP">MLLP</option>
                              <option value="TCP">TCP</option>
                              <option value="HTTP">HTTP</option>
                            </select>
                          </div>
                        </div>
                        <div className="grid grid-cols-2 gap-3">
                          <div>
                            <label className="text-xs text-arteria-muted">Local Port (at site)</label>
                            <input type="number" value={mapForm.local_port} onChange={e => setMapForm({ ...mapForm, local_port: parseInt(e.target.value) || 0 })}
                              className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                          </div>
                          <div>
                            <label className="text-xs text-arteria-muted">Target Port (Arteria)</label>
                            <input type="number" value={mapForm.target_port} onChange={e => setMapForm({ ...mapForm, target_port: parseInt(e.target.value) || 0 })}
                              className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                          </div>
                        </div>
                        <div>
                          <label className="text-xs text-arteria-muted">Target Host</label>
                          <input value={mapForm.target_host} onChange={e => setMapForm({ ...mapForm, target_host: e.target.value })}
                            placeholder="e.g., ingestion (Docker service name)"
                            className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                        </div>
                      </div>
                      <div className="flex justify-end gap-2 mt-5">
                        <button onClick={() => setShowMapping(false)} className="px-4 py-2 text-sm text-arteria-muted">Cancel</button>
                        <button onClick={createMapping} className="px-4 py-2 bg-arteria-accent text-white text-sm rounded">Create</button>
                      </div>
                    </div>
                  </div>
                )}

                <div className="p-5 space-y-3">
                  {mappings.map((m, i) => (
                    <div key={i} className="bg-arteria-surface border border-arteria-border rounded-lg p-4">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          <span className={`px-2 py-0.5 rounded text-[10px] font-medium ${
                            m.direction === 'INBOUND' ? 'bg-blue-500/20 text-blue-400' : 'bg-purple-500/20 text-purple-400'
                          }`}>{m.direction}</span>
                          <span className="text-xs font-mono text-white">{m.protocol}</span>
                        </div>
                        <span className={`w-2 h-2 rounded-full ${m.is_active ? 'bg-arteria-success' : 'bg-arteria-muted'}`} />
                      </div>
                      <div className="text-xs text-arteria-muted font-mono">
                        {m.direction === 'INBOUND'
                          ? `Site :${m.local_port} ═══TLS═══▶ Arteria ${m.target_host}:${m.target_port}`
                          : `Arteria ═══TLS═══▶ Site :${m.local_port} → ${m.target_host}:${m.target_port}`
                        }
                      </div>
                    </div>
                  ))}
                  {mappings.length === 0 && (
                    <p className="text-center py-8 text-arteria-muted">No port mappings configured for this node</p>
                  )}

                  {/* Enrollment info for this node */}
                  {selectedNode.status === 'PENDING' && (
                    <div className="bg-yellow-950/30 border border-yellow-900/50 rounded-lg p-4 mt-4">
                      <p className="text-sm text-yellow-400 font-medium mb-2">Awaiting Enrollment</p>
                      <p className="text-xs text-arteria-muted mb-2">Run this command on the remote site:</p>
                      <code className="block bg-arteria-bg rounded p-2 text-xs text-green-400 font-mono select-all">
                        arteria-agent enroll {selectedNode.enrollment_token}
                      </code>
                    </div>
                  )}
                </div>
              </>
            ) : (
              <div className="flex-1 flex items-center justify-center text-arteria-muted text-sm">
                Select a tunnel node to view its port mappings
              </div>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}

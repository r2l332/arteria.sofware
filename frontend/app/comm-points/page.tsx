'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { getCommPoints, createCommPoint, updateCommPoint, deleteCommPoint, getTunnelNodes, type CommPoint } from '@/lib/api';
import { Radio, Plus, Trash2, Edit2, ChevronDown, ChevronRight, Shield } from 'lucide-react';

interface TunnelNode { node_id: string; name: string; site_name: string; status: string; }

interface CPForm {
  name: string; direction: string; protocol: string; host: string; port: number;
  is_active: boolean; max_retries: number; retry_delay_ms: number; timeout_ms: number;
  tunnel_enabled: boolean; tunnel_node_id: string; tunnel_local_port: number;
}

const emptyForm: CPForm = { name: '', direction: 'INPUT', protocol: 'MLLP', host: '0.0.0.0', port: 2575, is_active: true, max_retries: 0, retry_delay_ms: 0, timeout_ms: 30000, tunnel_enabled: false, tunnel_node_id: '', tunnel_local_port: 0 };

export default function CommPointsPage() {
  const [cps, setCps] = useState<CommPoint[]>([]);
  const [nodes, setNodes] = useState<TunnelNode[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [form, setForm] = useState<CPForm>(emptyForm);
  const [expandedInputs, setExpandedInputs] = useState(true);
  const [expandedOutputs, setExpandedOutputs] = useState(true);

  const load = () => {
    getCommPoints().then(r => setCps(r.communication_points || [])).catch(() => {});
    getTunnelNodes().then(r => setNodes(r.nodes || [])).catch(() => {});
  };

  useEffect(() => {
    load();
    const h = () => load();
    window.addEventListener('arteria:config_change', h);
    return () => window.removeEventListener('arteria:config_change', h);
  }, []);

  const inputs = cps.filter(c => c.direction === 'INPUT');
  const outputs = cps.filter(c => c.direction === 'OUTPUT');

  const openCreate = (dir: string) => { setForm({ ...emptyForm, direction: dir }); setEditId(null); setShowForm(true); };
  const openEdit = (cp: CommPoint) => {
    setForm({ name: cp.name, direction: cp.direction, protocol: cp.protocol, host: cp.host || '', port: cp.port, is_active: cp.is_active, max_retries: cp.max_retries || 0, retry_delay_ms: cp.retry_delay_ms || 0, timeout_ms: cp.timeout_ms || 30000, tunnel_enabled: !!(cp as any).tunnel_enabled, tunnel_node_id: (cp as any).tunnel_node_id || '', tunnel_local_port: (cp as any).tunnel_local_port || 0 });
    setEditId(cp.comm_point_id); setShowForm(true);
  };
  const save = async () => {
    if (editId) await updateCommPoint(editId, form); else await createCommPoint(form);
    setShowForm(false); load();
  };
  const remove = async (id: string) => { if (!confirm('Delete this communication point?')) return; await deleteCommPoint(id); load(); };

  const getNodeName = (nodeId: string) => nodes.find(n => n.node_id === nodeId)?.name || '';

  const CPRow = ({ cp }: { cp: CommPoint }) => (
    <tr className="border-t border-arteria-border/30 hover:bg-white/[0.02] group">
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <span className={`w-2 h-2 rounded-full ${cp.is_active ? 'bg-green-400' : 'bg-gray-600'}`} />
          <span className="text-sm text-white font-medium">{cp.name}</span>
        </div>
      </td>
      <td className="px-4 py-3">
        <span className="text-xs font-mono px-2 py-0.5 rounded bg-arteria-bg border border-arteria-border text-gray-300">{cp.protocol}</span>
      </td>
      <td className="px-4 py-3 text-xs text-gray-400 font-mono">{cp.host || '—'}:{cp.port}</td>
      <td className="px-4 py-3">
        {(cp as any).tunnel_enabled ? (
          <div className="flex items-center gap-1.5 text-xs">
            <Shield className="w-3 h-3 text-cyan-400" />
            <span className="text-cyan-400">{getNodeName((cp as any).tunnel_node_id) || 'Capillary'}</span>
            <span className="text-gray-600">:{(cp as any).tunnel_local_port}</span>
          </div>
        ) : <span className="text-xs text-gray-600">Direct</span>}
      </td>
      <td className="px-4 py-3">
        <span className={`text-[10px] px-1.5 py-0.5 rounded ${cp.is_active ? 'bg-green-900/30 text-green-400' : 'bg-gray-800 text-gray-500'}`}>
          {cp.is_active ? 'Active' : 'Disabled'}
        </span>
      </td>
      <td className="px-4 py-3 text-right">
        <div className="opacity-0 group-hover:opacity-100 flex gap-2 justify-end">
          <button onClick={() => openEdit(cp)} className="text-gray-400 hover:text-white"><Edit2 className="w-3.5 h-3.5" /></button>
          <button onClick={() => remove(cp.comm_point_id)} className="text-gray-400 hover:text-red-400"><Trash2 className="w-3.5 h-3.5" /></button>
        </div>
      </td>
    </tr>
  );

  const CPTable = ({ items, title, direction, expanded, toggle }: { items: CommPoint[]; title: string; direction: string; expanded: boolean; toggle: () => void }) => (
    <div className="bg-arteria-surface border border-arteria-border rounded-xl overflow-hidden">
      <div className="flex items-center justify-between px-5 py-3 border-b border-arteria-border cursor-pointer hover:bg-white/[0.01]" onClick={toggle}>
        <div className="flex items-center gap-3">
          {expanded ? <ChevronDown className="w-4 h-4 text-gray-500" /> : <ChevronRight className="w-4 h-4 text-gray-500" />}
          <h3 className="text-sm font-semibold text-white">{title}</h3>
          <span className="text-xs text-gray-500 bg-arteria-bg px-2 py-0.5 rounded-full">{items.length}</span>
        </div>
        <button onClick={(e) => { e.stopPropagation(); openCreate(direction); }} className="text-xs px-3 py-1.5 bg-arteria-accent text-white rounded-lg hover:bg-arteria-accent/80 flex items-center gap-1">
          <Plus className="w-3 h-3" /> Add
        </button>
      </div>
      {expanded && (
        <table className="w-full">
          <thead><tr className="text-[10px] text-gray-500 uppercase tracking-wider">
            <th className="text-left px-4 py-2">Name</th>
            <th className="text-left px-4 py-2">Protocol</th>
            <th className="text-left px-4 py-2">Address</th>
            <th className="text-left px-4 py-2">Tunnel</th>
            <th className="text-left px-4 py-2">Status</th>
            <th className="text-right px-4 py-2">Actions</th>
          </tr></thead>
          <tbody>{items.map(cp => <CPRow key={cp.comm_point_id} cp={cp} />)}</tbody>
        </table>
      )}
    </div>
  );

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-y-auto">
        <div className="px-8 py-6">
          <div className="flex items-center justify-between mb-6">
            <div>
              <h1 className="text-xl font-bold text-white">Communication Points</h1>
              <p className="text-xs text-arteria-muted mt-1">Inbound and outbound message endpoints</p>
            </div>
            <div className="flex items-center gap-2 text-xs text-gray-500">
              <Radio className="w-4 h-4" />
              <span>{inputs.length} input, {outputs.length} output</span>
            </div>
          </div>

          <div className="space-y-4">
            <CPTable items={inputs} title="Input Communication Points" direction="INPUT" expanded={expandedInputs} toggle={() => setExpandedInputs(!expandedInputs)} />
            <CPTable items={outputs} title="Output Communication Points" direction="OUTPUT" expanded={expandedOutputs} toggle={() => setExpandedOutputs(!expandedOutputs)} />
          </div>
        </div>

        {/* Form Modal */}
        {showForm && (
          <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowForm(false)}>
            <div className="bg-arteria-surface border border-arteria-border rounded-xl w-[520px] p-6" onClick={e => e.stopPropagation()}>
              <h3 className="text-lg font-semibold text-white mb-4">{editId ? 'Edit' : 'Create'} Communication Point</h3>
              <div className="space-y-3">
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-[10px] text-gray-500 uppercase">Name</label>
                    <input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" />
                  </div>
                  <div>
                    <label className="text-[10px] text-gray-500 uppercase">Direction</label>
                    <select value={form.direction} onChange={e => setForm({ ...form, direction: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white">
                      <option value="INPUT">INPUT</option><option value="OUTPUT">OUTPUT</option>
                    </select>
                  </div>
                </div>
                <div className="grid grid-cols-3 gap-3">
                  <div>
                    <label className="text-[10px] text-gray-500 uppercase">Protocol</label>
                    <select value={form.protocol} onChange={e => setForm({ ...form, protocol: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white">
                      {['MLLP','TCP','HTTP','REST','WEBHOOK','S3','AZURE_BLOB','SQS','SNS','AZURE_EVENT_HUB','AZURE_SERVICE_BUS','DISCARD'].map(p => <option key={p} value={p}>{p}</option>)}
                    </select>
                  </div>
                  <div>
                    <label className="text-[10px] text-gray-500 uppercase">Host</label>
                    <input value={form.host} onChange={e => setForm({ ...form, host: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" />
                  </div>
                  <div>
                    <label className="text-[10px] text-gray-500 uppercase">Port</label>
                    <input type="number" value={form.port} onChange={e => setForm({ ...form, port: parseInt(e.target.value) || 0 })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" />
                  </div>
                </div>
                <div className="grid grid-cols-3 gap-3">
                  <div><label className="text-[10px] text-gray-500 uppercase">Retries</label><input type="number" value={form.max_retries} onChange={e => setForm({ ...form, max_retries: parseInt(e.target.value) || 0 })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
                  <div><label className="text-[10px] text-gray-500 uppercase">Retry Delay (ms)</label><input type="number" value={form.retry_delay_ms} onChange={e => setForm({ ...form, retry_delay_ms: parseInt(e.target.value) || 0 })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
                  <div><label className="text-[10px] text-gray-500 uppercase">Timeout (ms)</label><input type="number" value={form.timeout_ms} onChange={e => setForm({ ...form, timeout_ms: parseInt(e.target.value) || 0 })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white" /></div>
                </div>
                <div className="border-t border-arteria-border pt-3">
                  <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
                    <input type="checkbox" checked={form.tunnel_enabled} onChange={e => setForm({ ...form, tunnel_enabled: e.target.checked })} className="rounded" />
                    Route via Capillary (encrypted tunnel)
                  </label>
                  {form.tunnel_enabled && (
                    <div className="grid grid-cols-2 gap-3 mt-2">
                      <div>
                        <label className="text-[10px] text-gray-500 uppercase">Capillary Node</label>
                        <select value={form.tunnel_node_id} onChange={e => setForm({ ...form, tunnel_node_id: e.target.value })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white">
                          <option value="">Select node...</option>
                          {nodes.map(n => <option key={n.node_id} value={n.node_id}>{n.name} ({n.site_name})</option>)}
                        </select>
                      </div>
                      <div>
                        <label className="text-[10px] text-gray-500 uppercase">Local Port</label>
                        <input type="number" value={form.tunnel_local_port} onChange={e => setForm({ ...form, tunnel_local_port: parseInt(e.target.value) || 0 })} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded-lg text-sm text-white font-mono" />
                      </div>
                    </div>
                  )}
                </div>
                <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
                  <input type="checkbox" checked={form.is_active} onChange={e => setForm({ ...form, is_active: e.target.checked })} className="rounded" /> Active
                </label>
              </div>
              <div className="flex justify-end gap-2 mt-5">
                <button onClick={() => setShowForm(false)} className="px-4 py-2 text-sm text-gray-400 hover:text-white">Cancel</button>
                <button onClick={save} disabled={!form.name || !form.protocol} className="px-4 py-2 bg-arteria-accent text-white text-sm rounded-lg hover:bg-arteria-accent/80 disabled:opacity-50">
                  {editId ? 'Update' : 'Create'}
                </button>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

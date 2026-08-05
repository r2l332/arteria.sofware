'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { getCommPoints, createCommPoint, updateCommPoint, deleteCommPoint, getCPLogs, getTunnelNodes, type CommPoint, type CPLogResponse } from '@/lib/api';

const API_BASE = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1';

interface CommPointForm {
  name: string;
  direction: string;
  protocol: string;
  host: string;
  port: number;
  is_active: boolean;
  max_retries: number;
  retry_delay_ms: number;
  timeout_ms: number;
  tunnel_enabled: boolean;
  tunnel_node_id: string;
  tunnel_local_port: number;
}

interface TunnelNode {
  node_id: string;
  name: string;
  site_name: string;
  status: string;
}

const emptyForm: CommPointForm = {
  name: '', direction: 'INPUT', protocol: 'MLLP', host: '0.0.0.0',
  port: 2575, is_active: true, max_retries: 3, retry_delay_ms: 1000, timeout_ms: 30000,
  tunnel_enabled: false, tunnel_node_id: '', tunnel_local_port: 2575,
};

export default function CommPointsPage() {
  const [points, setPoints] = useState<CommPoint[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<CommPointForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [selectedCP, setSelectedCP] = useState<string | null>(null);
  const [cpLogs, setCPLogs] = useState<CPLogResponse | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [tunnelNodes, setTunnelNodes] = useState<TunnelNode[]>([]);

  const load = () => {
    getCommPoints().then((r) => setPoints(r.communication_points)).catch(console.error);
    getTunnelNodes().then(d => setTunnelNodes(d.nodes || [])).catch(() => {});
  };
  useEffect(() => { load(); }, []);

  // Auto-refresh logs
  useEffect(() => {
    if (!selectedCP || !autoRefresh) return;
    const refresh = () => getCPLogs(selectedCP).then(setCPLogs).catch(console.error);
    refresh();
    const interval = setInterval(refresh, 2000);
    return () => clearInterval(interval);
  }, [selectedCP, autoRefresh]);

  const openCreate = () => { setForm(emptyForm); setEditingId(null); setShowForm(true); };
  const openEdit = (cp: CommPoint) => {
    setForm({
      name: cp.name, direction: cp.direction, protocol: cp.protocol,
      host: cp.host, port: cp.port, is_active: cp.is_active,
      max_retries: cp.max_retries, retry_delay_ms: cp.retry_delay_ms, timeout_ms: cp.timeout_ms,
      tunnel_enabled: (cp as any).tunnel_enabled || false,
      tunnel_node_id: (cp as any).tunnel_node_id || '',
      tunnel_local_port: (cp as any).tunnel_local_port || 2575,
    });
    setEditingId(cp.comm_point_id);
    setShowForm(true);
  };

  const save = async () => {
    setSaving(true);
    if (editingId) {
      await updateCommPoint(editingId, form);
    } else {
      await createCommPoint(form);
    }
    setSaving(false);
    setShowForm(false);
    load();
  };

  const remove = async (id: string) => {
    if (!confirm('Delete this communication point?')) return;
    await deleteCommPoint(id);
    load();
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        <div className="flex items-center justify-between px-8 py-5 border-b border-arteria-border">
          <h2 className="text-2xl font-bold text-white">Communication Points</h2>
          <button onClick={openCreate} className="px-4 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80">
            + New Comm Point
          </button>
        </div>

        <div className="flex-1 overflow-hidden flex">
          {/* CP List */}
          <div className="w-1/2 overflow-y-auto p-6 border-r border-arteria-border">
            {/* Form Modal */}
            {showForm && (
              <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowForm(false)}>
                <div className="bg-arteria-surface border border-arteria-border rounded-lg w-[500px] p-6" onClick={(e) => e.stopPropagation()}>
                  <h3 className="text-lg font-semibold text-white mb-4">{editingId ? 'Edit' : 'Create'} Communication Point</h3>
                  <div className="space-y-3">
                    <div>
                      <label className="text-xs text-arteria-muted">Name</label>
                      <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })}
                        className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                    </div>
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="text-xs text-arteria-muted">Direction</label>
                        <select value={form.direction} onChange={(e) => setForm({ ...form, direction: e.target.value })}
                          className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                          <option value="INPUT">INPUT</option>
                          <option value="OUTPUT">OUTPUT</option>
                        </select>
                      </div>
                      <div>
                        <label className="text-xs text-arteria-muted">Protocol / Connector</label>
                        <select value={form.protocol} onChange={(e) => setForm({ ...form, protocol: e.target.value })}
                          className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                          <optgroup label="Traditional">
                            <option value="MLLP">MLLP (HL7 TCP)</option>
                            <option value="HTTP">HTTP</option>
                            <option value="TCP">TCP</option>
                            <option value="REST">REST API</option>
                          </optgroup>
                          <optgroup label="Cloud Storage">
                            <option value="S3">AWS S3 (Archive)</option>
                            <option value="AZURE_BLOB">Azure Blob Storage</option>
                          </optgroup>
                          <optgroup label="Event / Queue">
                            <option value="SQS">AWS SQS</option>
                            <option value="SNS">AWS SNS</option>
                            <option value="AZURE_EVENT_HUB">Azure Event Hub</option>
                            <option value="AZURE_SERVICE_BUS">Azure Service Bus</option>
                          </optgroup>
                          <optgroup label="HTTP">
                            <option value="WEBHOOK">Webhook (HTTP POST)</option>
                          </optgroup>
                          <optgroup label="Testing">
                            <option value="DISCARD">Discard (Null Sink)</option>
                          </optgroup>
                        </select>
                      </div>
                    </div>

                    {/* Traditional TCP config */}
                    {['MLLP', 'HTTP', 'TCP', 'REST'].includes(form.protocol) && (<>
                    <div className="grid grid-cols-2 gap-3">
                      <div>
                        <label className="text-xs text-arteria-muted">Host</label>
                        <input value={form.host} onChange={(e) => setForm({ ...form, host: e.target.value })}
                          className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                      </div>
                      <div>
                        <label className="text-xs text-arteria-muted">Port</label>
                        <input type="number" value={form.port} onChange={(e) => setForm({ ...form, port: parseInt(e.target.value) || 0 })}
                          className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                      </div>
                    </div>
                    <div className="grid grid-cols-3 gap-3">
                      <div>
                        <label className="text-xs text-arteria-muted">Max Retries</label>
                        <input type="number" value={form.max_retries} onChange={(e) => setForm({ ...form, max_retries: parseInt(e.target.value) || 0 })}
                          className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                      </div>
                      <div>
                        <label className="text-xs text-arteria-muted">Retry Delay (ms)</label>
                        <input type="number" value={form.retry_delay_ms} onChange={(e) => setForm({ ...form, retry_delay_ms: parseInt(e.target.value) || 0 })}
                          className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                      </div>
                      <div>
                        <label className="text-xs text-arteria-muted">Timeout (ms)</label>
                        <input type="number" value={form.timeout_ms} onChange={(e) => setForm({ ...form, timeout_ms: parseInt(e.target.value) || 0 })}
                          className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                      </div>
                    </div>
                    </>)}

                    {/* S3 Config */}
                    {form.protocol === 'S3' && (
                    <div className="space-y-3 p-3 bg-arteria-bg/50 rounded-lg border border-arteria-border">
                      <p className="text-2xs text-arteria-muted uppercase tracking-wider font-medium">AWS S3 Configuration</p>
                      <div className="grid grid-cols-2 gap-3">
                        <div><label className="text-xs text-arteria-muted">Bucket Name</label>
                          <input placeholder="my-hl7-archive" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                        <div><label className="text-xs text-arteria-muted">Region</label>
                          <input placeholder="us-east-1" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      </div>
                      <div><label className="text-xs text-arteria-muted">Prefix (folder path)</label>
                        <input placeholder="hl7/inbound/" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      <div className="grid grid-cols-2 gap-3">
                        <div><label className="text-xs text-arteria-muted">Access Key ID</label>
                          <input placeholder="AKIA..." className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                        <div><label className="text-xs text-arteria-muted">Secret Access Key</label>
                          <input type="password" placeholder="••••••••" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      </div>
                    </div>
                    )}

                    {/* Azure Blob Config */}
                    {form.protocol === 'AZURE_BLOB' && (
                    <div className="space-y-3 p-3 bg-arteria-bg/50 rounded-lg border border-arteria-border">
                      <p className="text-2xs text-arteria-muted uppercase tracking-wider font-medium">Azure Blob Storage Configuration</p>
                      <div><label className="text-xs text-arteria-muted">Connection String</label>
                        <input placeholder="DefaultEndpointsProtocol=https;AccountName=..." className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      <div className="grid grid-cols-2 gap-3">
                        <div><label className="text-xs text-arteria-muted">Container Name</label>
                          <input placeholder="hl7-archive" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                        <div><label className="text-xs text-arteria-muted">Prefix</label>
                          <input placeholder="inbound/" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      </div>
                    </div>
                    )}

                    {/* SQS Config */}
                    {form.protocol === 'SQS' && (
                    <div className="space-y-3 p-3 bg-arteria-bg/50 rounded-lg border border-arteria-border">
                      <p className="text-2xs text-arteria-muted uppercase tracking-wider font-medium">AWS SQS Configuration</p>
                      <div><label className="text-xs text-arteria-muted">Queue URL</label>
                        <input placeholder="https://sqs.us-east-1.amazonaws.com/123456/my-queue" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      <div className="grid grid-cols-2 gap-3">
                        <div><label className="text-xs text-arteria-muted">Access Key ID</label>
                          <input placeholder="AKIA..." className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                        <div><label className="text-xs text-arteria-muted">Secret Access Key</label>
                          <input type="password" placeholder="••••••••" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      </div>
                    </div>
                    )}

                    {/* SNS Config */}
                    {form.protocol === 'SNS' && (
                    <div className="space-y-3 p-3 bg-arteria-bg/50 rounded-lg border border-arteria-border">
                      <p className="text-2xs text-arteria-muted uppercase tracking-wider font-medium">AWS SNS Configuration</p>
                      <div><label className="text-xs text-arteria-muted">Topic ARN</label>
                        <input placeholder="arn:aws:sns:us-east-1:123456:my-topic" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      <div className="grid grid-cols-2 gap-3">
                        <div><label className="text-xs text-arteria-muted">Access Key ID</label>
                          <input placeholder="AKIA..." className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                        <div><label className="text-xs text-arteria-muted">Secret Access Key</label>
                          <input type="password" placeholder="••••••••" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      </div>
                    </div>
                    )}

                    {/* Azure Event Hub Config */}
                    {form.protocol === 'AZURE_EVENT_HUB' && (
                    <div className="space-y-3 p-3 bg-arteria-bg/50 rounded-lg border border-arteria-border">
                      <p className="text-2xs text-arteria-muted uppercase tracking-wider font-medium">Azure Event Hub Configuration</p>
                      <div><label className="text-xs text-arteria-muted">Connection String</label>
                        <input placeholder="Endpoint=sb://..." className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      <div><label className="text-xs text-arteria-muted">Event Hub Name</label>
                        <input placeholder="hl7-events" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                    </div>
                    )}

                    {/* Azure Service Bus Config */}
                    {form.protocol === 'AZURE_SERVICE_BUS' && (
                    <div className="space-y-3 p-3 bg-arteria-bg/50 rounded-lg border border-arteria-border">
                      <p className="text-2xs text-arteria-muted uppercase tracking-wider font-medium">Azure Service Bus Configuration</p>
                      <div><label className="text-xs text-arteria-muted">Connection String</label>
                        <input placeholder="Endpoint=sb://..." className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      <div><label className="text-xs text-arteria-muted">Namespace / Queue Name</label>
                        <input placeholder="hl7-queue" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                    </div>
                    )}

                    {/* Webhook Config */}
                    {form.protocol === 'WEBHOOK' && (
                    <div className="space-y-3 p-3 bg-arteria-bg/50 rounded-lg border border-arteria-border">
                      <p className="text-2xs text-arteria-muted uppercase tracking-wider font-medium">Webhook Configuration</p>
                      <div><label className="text-xs text-arteria-muted">URL</label>
                        <input placeholder="https://api.example.com/hl7/inbound" className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      <div className="grid grid-cols-2 gap-3">
                        <div><label className="text-xs text-arteria-muted">Method</label>
                          <select className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                            <option value="POST">POST</option>
                            <option value="PUT">PUT</option>
                          </select></div>
                        <div><label className="text-xs text-arteria-muted">Max Retries</label>
                          <input type="number" defaultValue={3} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" /></div>
                      </div>
                      <div><label className="text-xs text-arteria-muted">Headers (one per line: Key: Value)</label>
                        <textarea rows={3} placeholder={"Authorization: Bearer token\nX-Custom: value"} className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white font-mono" /></div>
                    </div>
                    )}

                    <label className="flex items-center gap-2 text-sm text-gray-300">
                      <input type="checkbox" checked={form.is_active} onChange={(e) => setForm({ ...form, is_active: e.target.checked })} className="rounded" />
                      Active
                    </label>

                    {/* Tunnel Configuration */}
                    <div className="border-t border-arteria-border pt-3 mt-1">
                      <label className="flex items-center gap-2 text-sm text-gray-300 mb-2">
                        <input type="checkbox" checked={form.tunnel_enabled} onChange={(e) => setForm({ ...form, tunnel_enabled: e.target.checked })} className="rounded" />
                        <span className="flex items-center gap-1">
                          <span className="text-cyan-400">⛓</span> Route via Capillary (Encrypted Tunnel)
                        </span>
                      </label>
                      {form.tunnel_enabled && (
                        <div className="grid grid-cols-2 gap-3 pl-6">
                          <div>
                            <label className="text-xs text-arteria-muted">Capillary Node</label>
                            <select value={form.tunnel_node_id} onChange={(e) => setForm({ ...form, tunnel_node_id: e.target.value })}
                              className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white">
                              <option value="">— Select node —</option>
                              {tunnelNodes.map(n => (
                                <option key={n.node_id} value={n.node_id}>{n.name} ({n.site_name})</option>
                              ))}
                            </select>
                          </div>
                          <div>
                            <label className="text-xs text-arteria-muted">Local Port (at site)</label>
                            <input type="number" value={form.tunnel_local_port} onChange={(e) => setForm({ ...form, tunnel_local_port: parseInt(e.target.value) || 0 })}
                              className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                  <div className="flex justify-end gap-2 mt-5">
                    <button onClick={() => setShowForm(false)} className="px-4 py-2 text-sm text-arteria-muted hover:text-white">Cancel</button>
                    <button onClick={save} disabled={saving || !form.name} className="px-4 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80 disabled:opacity-50">
                      {saving ? 'Saving...' : editingId ? 'Update' : 'Create'}
                    </button>
                  </div>
                </div>
              </div>
            )}

            <div className="grid gap-3">
              {points.map((cp) => (
                <div
                  key={cp.comm_point_id}
                  onClick={() => setSelectedCP(cp.comm_point_id)}
                  className={`bg-arteria-surface border rounded-lg p-4 cursor-pointer transition-colors ${
                    selectedCP === cp.comm_point_id ? 'border-arteria-accent' : 'border-arteria-border hover:border-arteria-accent/50'
                  }`}
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <span className={`w-2 h-2 rounded-full ${cp.is_active ? 'bg-arteria-success' : 'bg-arteria-muted'}`} />
                      <h3 className="font-medium text-white text-sm">{cp.name}</h3>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                        cp.direction === 'INPUT' ? 'bg-blue-500/20 text-blue-400' : 'bg-purple-500/20 text-purple-400'
                      }`}>{cp.direction}</span>
                      <button onClick={(e) => { e.stopPropagation(); openEdit(cp); }} className="text-[10px] text-arteria-muted hover:text-white">Edit</button>
                      <button onClick={(e) => { e.stopPropagation(); remove(cp.comm_point_id); }} className="text-[10px] text-red-400 hover:text-red-300">Del</button>
                    </div>
                  </div>
                  <div className="flex gap-4 text-xs text-arteria-muted">
                    <span className="font-mono">{cp.protocol}</span>
                    <span className="font-mono">{cp.host}:{cp.port}</span>
                    <span>Retries: {cp.max_retries}</span>
                    {(cp as any).tunnel_enabled && (
                      <span className={`flex items-center gap-1 ${
                        (cp as any).capillary_status === 'connected' ? 'text-cyan-400' : 'text-yellow-400'
                      }`}>
                        ⛓ {(cp as any).capillary_name
                          ? `${(cp as any).capillary_name} (${(cp as any).capillary_site || 'unknown site'})`
                          : 'Capillary'}
                        {(cp as any).capillary_status && (
                          <span className={`ml-1 w-1.5 h-1.5 rounded-full inline-block ${
                            (cp as any).capillary_status === 'connected' ? 'bg-green-400' : 'bg-yellow-400'
                          }`} />
                        )}
                      </span>
                    )}
                  </div>
                </div>
              ))}
              {points.length === 0 && (
                <p className="text-center py-8 text-arteria-muted">No communication points configured</p>
              )}
            </div>
          </div>

          {/* CP Log Viewer */}
          <div className="w-1/2 flex flex-col overflow-hidden">
            {selectedCP && cpLogs ? (
              <>
                <div className="px-5 py-4 border-b border-arteria-border">
                  <div className="flex items-center justify-between">
                    <div>
                      <h3 className="font-semibold text-white">{cpLogs.name}</h3>
                      <div className="flex gap-4 mt-1 text-xs">
                        <span className="text-arteria-success">{cpLogs.received} received</span>
                        <span className="text-arteria-error">{cpLogs.errors} errors</span>
                        <span className="text-arteria-muted">{cpLogs.log_count} log entries</span>
                      </div>
                    </div>
                    <label className="flex items-center gap-1.5 text-xs text-arteria-muted">
                      <input type="checkbox" checked={autoRefresh} onChange={(e) => setAutoRefresh(e.target.checked)} className="rounded" />
                      Auto-refresh
                    </label>
                  </div>
                </div>
                <div className="flex-1 overflow-y-auto p-4 font-mono text-xs bg-arteria-bg">
                  {cpLogs.logs.map((entry, i) => (
                    <div key={i} className={`py-1 px-2 border-b border-arteria-border/30 flex gap-3 ${
                      entry.level === 'ERROR' ? 'bg-red-950/20' : ''
                    }`}>
                      <span className="text-arteria-muted shrink-0 w-24">
                        {new Date(entry.timestamp).toLocaleTimeString([], { hour12: false, fractionalSecondDigits: 3 } as Intl.DateTimeFormatOptions)}
                      </span>
                      <span className={`shrink-0 w-12 ${
                        entry.level === 'ERROR' ? 'text-arteria-error' :
                        entry.level === 'WARN' ? 'text-arteria-warning' :
                        entry.level === 'DEBUG' ? 'text-cyan-400' : 'text-arteria-success'
                      }`}>{entry.level}</span>
                      <span className="text-gray-300">{entry.message}</span>
                      {entry.message_id && <span className="text-arteria-muted truncate">{entry.message_id.slice(0, 8)}</span>}
                      {entry.size_bytes ? <span className="text-arteria-muted">{entry.size_bytes}B</span> : null}
                      {entry.error && <span className="text-arteria-error">{entry.error}</span>}
                    </div>
                  ))}
                  {cpLogs.logs.length === 0 && (
                    <p className="text-center py-8 text-arteria-muted">No log entries yet</p>
                  )}
                </div>
              </>
            ) : (
              <div className="flex-1 flex items-center justify-center text-arteria-muted text-sm">
                Select a communication point to view its logs
              </div>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}

'use client';

import { useEffect, useState, useCallback } from 'react';
import Sidebar from '@/components/Sidebar';

const API = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1';

interface HealthComponent {
  name: string;
  status: string;
  latency_ms?: number;
  details?: string;
}

interface NATSStats {
  account?: { memory: number; storage: number; streams: number; consumers: number };
  streams?: Array<{ name: string; subjects: string[]; messages: number; bytes: number; consumers: number; first_seq: number; last_seq: number; storage: string }>;
}

interface ConsumerInfo {
  name: string;
  stream: string;
  pending: number;
  ack_pending: number;
  delivered: number;
  redelivered: number;
  waiting: number;
}

interface DLQSummary {
  count: number;
  error_types: Record<string, number>;
  oldest: string;
  newest: string;
}

interface Overview {
  messages: { total: number; errors: number; error_rate: number };
  routes: { total: number; active: number };
  comm_points: { total: number; active: number; input: number; output: number };
  nats: { stream_msgs: number; stream_bytes: number; pending: number };
  processing: Record<string, number> | null;
}

export default function PlatformPage() {
  const [token, setToken] = useState('');
  const [activeTab, setActiveTab] = useState<'overview' | 'health' | 'nats' | 'logs' | 'dlq' | 'audit'>('overview');
  const [health, setHealth] = useState<Record<string, HealthComponent>>({});
  const [resources, setResources] = useState<Record<string, { used: number; total: number; unit: string; percent: number }>>({});
  const [nats, setNats] = useState<NATSStats>({});
  const [consumers, setConsumers] = useState<ConsumerInfo[]>([]);
  const [dlq, setDlq] = useState<DLQSummary>({ count: 0, error_types: {}, oldest: '', newest: '' });
  const [overview, setOverview] = useState<Overview | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [logService, setLogService] = useState('processing');
  const [audit, setAudit] = useState<Array<Record<string, string>>>([]);
  const [loading, setLoading] = useState(true);
  const [actionMsg, setActionMsg] = useState('');

  const headers = useCallback(() => {
    const t = typeof window !== 'undefined' ? localStorage.getItem('token') || token : token;
    return { 'Authorization': `Bearer ${t}`, 'Content-Type': 'application/json' };
  }, [token]);

  const apiFetch = useCallback(async (path: string) => {
    try {
      const res = await fetch(`${API}${path}`, { headers: headers() });
      if (!res.ok) return null;
      return await res.json();
    } catch { return null; }
  }, [headers]);

  useEffect(() => {
    const t = localStorage.getItem('token') || '';
    setToken(t);
  }, []);

  useEffect(() => {
    if (!token) return;
    loadTab(activeTab);
  }, [activeTab, token]);

  const loadTab = async (tab: string) => {
    setLoading(true);
    switch (tab) {
      case 'overview': {
        const o = await apiFetch('/platform/overview');
        setOverview(o);
        const d = await apiFetch('/platform/dlq/summary');
        setDlq(d || { count: 0, error_types: {}, oldest: '', newest: '' });
        break;
      }
      case 'health': {
        const h = await apiFetch('/platform/health');
        setHealth(h?.components || {});
        setResources(h?.resources || {});
        break;
      }
      case 'nats': {
        const n = await apiFetch('/platform/nats-stats');
        setNats(n || {});
        const c = await apiFetch('/platform/nats/consumers');
        setConsumers(c?.consumers || []);
        break;
      }
      case 'logs': {
        const l = await apiFetch(`/platform/logs/${logService}?lines=200`);
        setLogs(l?.logs || []);
        break;
      }
      case 'dlq': {
        const d = await apiFetch('/platform/dlq/summary');
        setDlq(d || { count: 0, error_types: {}, oldest: '', newest: '' });
        break;
      }
      case 'audit': {
        const a = await apiFetch('/audit-log?limit=50');
        setAudit(a?.entries || []);
        break;
      }
    }
    setLoading(false);
  };

  const retryAllDLQ = async () => {
    const res = await fetch(`${API}/platform/dlq/retry-all`, { method: 'POST', headers: headers(), body: JSON.stringify({ limit: 100 }) });
    const data = await res.json();
    setActionMsg(`Retried ${data.retried} messages (${data.failed} failed)`);
    loadTab('dlq');
  };

  const dropAllDLQ = async () => {
    if (!confirm('Drop ALL DLQ messages? This cannot be undone.')) return;
    const res = await fetch(`${API}/platform/dlq/drop-all`, { method: 'POST', headers: headers(), body: JSON.stringify({ reason: 'Bulk drop from admin panel' }) });
    const data = await res.json();
    setActionMsg(`Dropped ${data.dropped} messages`);
    loadTab('dlq');
  };

  const purgeSubject = async (subject: string) => {
    if (!confirm(`Purge all messages on ${subject}?`)) return;
    const res = await fetch(`${API}/platform/nats/purge`, { method: 'POST', headers: headers(), body: JSON.stringify({ subject }) });
    const data = await res.json();
    setActionMsg(`Purged: ${data.status || data.error}`);
    loadTab('nats');
  };

  const refreshHealth = () => loadTab('health');

  const tabs = [
    { id: 'overview' as const, label: 'Overview' },
    { id: 'health' as const, label: 'System Health' },
    { id: 'nats' as const, label: 'NATS / Queues' },
    { id: 'dlq' as const, label: 'Dead Letter Queue' },
    { id: 'logs' as const, label: 'Service Logs' },
    { id: 'audit' as const, label: 'Audit Trail' },
  ];

  const statusColor = (s: string) => {
    if (s === 'healthy' || s === 'ok' || s === 'connected') return 'bg-green-500';
    if (s === 'degraded' || s === 'slow') return 'bg-yellow-500';
    return 'bg-red-500';
  };

  const levelColor = (l: string) => {
    if (l === 'ERROR' || l === 'FATAL') return 'text-red-400';
    if (l === 'WARN') return 'text-yellow-400';
    if (l === 'DEBUG' || l === 'TRACE') return 'text-gray-500';
    return 'text-green-400';
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        <div className="px-6 py-4 border-b border-arteria-border">
          <h1 className="text-xl font-bold text-white">Platform Administration</h1>
          <p className="text-xs text-arteria-muted mt-1">Infrastructure health, logs, audit trail, and configuration management</p>
        </div>

        {/* Tab bar */}
        <div className="px-6 py-2 border-b border-arteria-border flex gap-1">
          {tabs.map(t => (
            <button key={t.id} onClick={() => setActiveTab(t.id)}
              className={`px-4 py-2 text-xs rounded-t font-medium transition-colors ${activeTab === t.id ? 'bg-arteria-surface text-white border-b-2 border-arteria-accent' : 'text-arteria-muted hover:text-white'}`}>
              {t.label}
            </button>
          ))}
        </div>

        <div className="flex-1 overflow-y-auto p-6">
          {loading && <div className="text-arteria-muted text-sm animate-pulse">Loading...</div>}
          {actionMsg && <div className="mb-4 px-4 py-2 bg-blue-900/30 border border-blue-700 rounded text-sm text-blue-200">{actionMsg}</div>}

          {/* OVERVIEW TAB */}
          {activeTab === 'overview' && !loading && overview && (
            <div className="space-y-6">
              <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
                <StatCard label="Total Messages" value={overview.messages.total.toLocaleString()} sub={`${overview.messages.error_rate.toFixed(1)}% error rate`} />
                <StatCard label="Active Routes" value={`${overview.routes.active}/${overview.routes.total}`} sub={`${overview.comm_points.input} input, ${overview.comm_points.output} output CPs`} />
                <StatCard label="NATS Stream" value={overview.nats.stream_msgs.toLocaleString()} sub={`${(overview.nats.stream_bytes / 1024 / 1024).toFixed(1)} MB, ${overview.nats.pending} pending`} />
                <StatCard label="DLQ Errors" value={dlq.count.toString()} sub={dlq.count > 0 ? `Oldest: ${dlq.oldest?.slice(0, 16) || 'N/A'}` : 'All clear'} alert={dlq.count > 0} />
              </div>

              {overview.processing && (
                <div>
                  <h3 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Processing Metrics (Live)</h3>
                  <div className="grid grid-cols-3 lg:grid-cols-6 gap-3">
                    {Object.entries(overview.processing).filter(([k]) => !k.startsWith('comm_')).map(([k, v]) => (
                      <div key={k} className="bg-arteria-surface border border-arteria-border rounded p-3 text-center">
                        <div className="text-lg font-bold text-white">{typeof v === 'number' ? (v > 1000 ? `${(v/1000).toFixed(1)}k` : v.toFixed(v % 1 === 0 ? 0 : 1)) : String(v)}</div>
                        <div className="text-[10px] text-arteria-muted mt-0.5">{k.replace(/_/g, ' ')}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {dlq.count > 0 && (
                <div className="bg-red-900/20 border border-red-800/50 rounded-lg p-4">
                  <h3 className="text-sm font-semibold text-red-300 mb-2">Dead Letter Queue ({dlq.count} messages)</h3>
                  <div className="flex gap-4 text-xs text-gray-400">
                    {Object.entries(dlq.error_types).map(([t, c]) => <span key={t}>{t}: {c}</span>)}
                  </div>
                  <div className="flex gap-2 mt-3">
                    <button onClick={retryAllDLQ} className="px-3 py-1 text-xs bg-yellow-600 text-white rounded hover:bg-yellow-500">Retry All</button>
                    <button onClick={dropAllDLQ} className="px-3 py-1 text-xs bg-red-700 text-white rounded hover:bg-red-600">Drop All</button>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* HEALTH TAB */}
          {activeTab === 'health' && !loading && (
            <div className="space-y-6">
              <div className="flex justify-between items-center">
                <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Component Health</h2>
                <button onClick={refreshHealth} className="px-3 py-1 text-xs bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">Refresh</button>
              </div>
              <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
                {Object.entries(health).map(([name, c]) => (
                  <div key={name} className="bg-arteria-surface border border-arteria-border rounded-lg p-4">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm text-white font-medium">{name.replace(/_/g, ' ')}</span>
                      <span className={`w-2.5 h-2.5 rounded-full ${statusColor((c as HealthComponent).status)}`} />
                    </div>
                    <div className="text-xs text-arteria-muted">
                      <span className="uppercase">{(c as HealthComponent).status}</span>
                      {(c as HealthComponent).latency_ms !== undefined && <span className="ml-2">{(c as HealthComponent).latency_ms}ms</span>}
                    </div>
                    {(c as HealthComponent).details && <p className="text-[10px] text-gray-600 mt-1">{(c as HealthComponent).details}</p>}
                  </div>
                ))}
              </div>

              {Object.keys(resources).length > 0 && (
                <div>
                  <h3 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">System Resources (API Process)</h3>
                  <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
                    {Object.entries(resources).map(([name, r]) => (
                      <div key={name} className="bg-arteria-surface border border-arteria-border rounded-lg p-4">
                        <div className="text-xs text-arteria-muted mb-1">{name.replace(/_/g, ' ')}</div>
                        <div className="text-lg font-bold text-white">{r.used}{r.unit && ` ${r.unit}`}</div>
                        <div className="w-full bg-arteria-bg rounded-full h-1.5 mt-2">
                          <div className={`h-1.5 rounded-full ${r.percent > 80 ? 'bg-red-500' : r.percent > 50 ? 'bg-yellow-500' : 'bg-green-500'}`} style={{ width: `${Math.min(r.percent, 100)}%` }} />
                        </div>
                        <div className="text-[10px] text-gray-600 mt-1">{r.percent.toFixed(0)}% of {r.total}{r.unit && ` ${r.unit}`}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}

          {/* NATS TAB */}
          {activeTab === 'nats' && !loading && (
            <div className="space-y-6">
              {nats.account && (
                <div>
                  <h3 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">JetStream Account</h3>
                  <div className="grid grid-cols-4 gap-4">
                    <StatCard label="Memory" value={`${(nats.account.memory / 1024 / 1024).toFixed(1)} MB`} />
                    <StatCard label="Storage" value={`${(nats.account.storage / 1024 / 1024).toFixed(1)} MB`} />
                    <StatCard label="Streams" value={String(nats.account.streams)} />
                    <StatCard label="Consumers" value={String(nats.account.consumers)} />
                  </div>
                </div>
              )}

              {nats.streams && nats.streams.length > 0 && (
                <div>
                  <h3 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Streams</h3>
                  <div className="bg-arteria-surface border border-arteria-border rounded-lg overflow-hidden">
                    <table className="w-full text-xs">
                      <thead className="bg-arteria-bg"><tr>
                        <th className="text-left px-4 py-2 text-arteria-muted">Name</th>
                        <th className="text-left px-4 py-2 text-arteria-muted">Subjects</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Messages</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Size</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Consumers</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Actions</th>
                      </tr></thead>
                      <tbody>
                        {nats.streams.map((s, i) => (
                          <tr key={i} className="border-t border-arteria-border/30">
                            <td className="px-4 py-2 text-white font-mono">{s.name}</td>
                            <td className="px-4 py-2 text-gray-400 font-mono text-[10px]">{s.subjects?.join(', ')}</td>
                            <td className="px-4 py-2 text-right text-white">{s.messages.toLocaleString()}</td>
                            <td className="px-4 py-2 text-right text-gray-400">{(s.bytes / 1024).toFixed(1)} KB</td>
                            <td className="px-4 py-2 text-right text-gray-300">{s.consumers}</td>
                            <td className="px-4 py-2 text-right">
                              <button onClick={() => purgeSubject(s.subjects?.[0] || s.name)} className="text-[10px] text-red-400 hover:text-red-300">Purge</button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}

              {consumers.length > 0 && (
                <div>
                  <h3 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Consumers (Queue Lag)</h3>
                  <div className="bg-arteria-surface border border-arteria-border rounded-lg overflow-hidden">
                    <table className="w-full text-xs">
                      <thead className="bg-arteria-bg"><tr>
                        <th className="text-left px-4 py-2 text-arteria-muted">Consumer</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Pending</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Ack Pending</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Delivered</th>
                        <th className="text-right px-4 py-2 text-arteria-muted">Redelivered</th>
                      </tr></thead>
                      <tbody>
                        {consumers.map((c, i) => (
                          <tr key={i} className="border-t border-arteria-border/30">
                            <td className="px-4 py-2 text-white font-mono">{c.name}</td>
                            <td className={`px-4 py-2 text-right font-bold ${c.pending > 100 ? 'text-red-400' : c.pending > 10 ? 'text-yellow-400' : 'text-green-400'}`}>{c.pending}</td>
                            <td className="px-4 py-2 text-right text-gray-300">{c.ack_pending}</td>
                            <td className="px-4 py-2 text-right text-gray-400">{c.delivered.toLocaleString()}</td>
                            <td className="px-4 py-2 text-right text-gray-400">{c.redelivered}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* DLQ TAB */}
          {activeTab === 'dlq' && !loading && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Dead Letter Queue Management</h2>
                <div className="flex gap-2">
                  <button onClick={retryAllDLQ} className="px-3 py-1.5 text-xs bg-yellow-600 text-white rounded hover:bg-yellow-500">Retry All ({dlq.count})</button>
                  <button onClick={dropAllDLQ} className="px-3 py-1.5 text-xs bg-red-700 text-white rounded hover:bg-red-600">Drop All</button>
                  <button onClick={() => loadTab('dlq')} className="px-3 py-1.5 text-xs bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">Refresh</button>
                </div>
              </div>
              <div className="grid grid-cols-3 gap-4">
                <StatCard label="Total Errors" value={String(dlq.count)} alert={dlq.count > 0} />
                <StatCard label="Oldest" value={dlq.oldest ? dlq.oldest.slice(0, 16) : 'N/A'} />
                <StatCard label="Newest" value={dlq.newest ? dlq.newest.slice(0, 16) : 'N/A'} />
              </div>
              {Object.keys(dlq.error_types).length > 0 && (
                <div className="bg-arteria-surface border border-arteria-border rounded-lg p-4">
                  <h3 className="text-xs text-arteria-muted uppercase mb-2">Error Type Breakdown</h3>
                  <div className="space-y-2">
                    {Object.entries(dlq.error_types).map(([type, count]) => (
                      <div key={type} className="flex items-center justify-between">
                        <span className="text-sm text-white">{type}</span>
                        <span className="text-sm font-bold text-red-400">{count}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {dlq.count === 0 && <p className="text-center text-arteria-muted py-8">No errors in DLQ — all systems operational</p>}
            </div>
          )}

          {/* LOGS TAB */}
          {activeTab === 'logs' && !loading && (
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Service Logs</h2>
                <select value={logService} onChange={(e) => setLogService(e.target.value)}
                  className="bg-arteria-bg border border-arteria-border rounded px-2 py-1 text-xs text-white">
                  {['api', 'ingestion', 'processing', 'egress', 'broker'].map(s => (
                    <option key={s} value={s}>{s}</option>
                  ))}
                </select>
                <button onClick={() => loadTab('logs')} className="px-3 py-1 text-xs bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">Load</button>
              </div>
              <div className="bg-black border border-arteria-border rounded-lg overflow-hidden">
                <div className="max-h-[600px] overflow-y-auto font-mono text-[11px] p-3 space-y-0.5">
                  {logs.map((line, i) => {
                    const isErr = line.includes('"ERROR"') || line.includes('"FATAL"');
                    const isWarn = line.includes('"WARN"');
                    return (
                      <div key={i} className={`${isErr ? 'text-red-400' : isWarn ? 'text-yellow-400' : 'text-gray-400'} hover:bg-white/5 px-1`}>
                        {line}
                      </div>
                    );
                  })}
                  {logs.length === 0 && <p className="text-arteria-muted p-4">No logs available for {logService}. Check the volume mount.</p>}
                </div>
              </div>
              <p className="text-[10px] text-gray-600">{logs.length} lines shown</p>
            </div>
          )}

          {/* AUDIT TAB */}
          {activeTab === 'audit' && !loading && (
            <div className="space-y-4">
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Security Audit Trail</h2>
              <div className="bg-arteria-surface border border-arteria-border rounded-lg overflow-hidden">
                <table className="w-full text-xs">
                  <thead className="bg-arteria-bg">
                    <tr>
                      <th className="text-left px-4 py-2 text-arteria-muted">Time</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">User</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Action</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Resource</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">IP</th>
                    </tr>
                  </thead>
                  <tbody>
                    {audit.map((a, i) => (
                      <tr key={i} className="border-t border-arteria-border/30 hover:bg-white/[0.02]">
                        <td className="px-4 py-2 text-gray-400 font-mono">{a.timestamp?.slice(0, 19)}</td>
                        <td className="px-4 py-2 text-white">{a.username}</td>
                        <td className="px-4 py-2">
                          <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${a.action?.includes('SUCCESS') ? 'bg-green-900/50 text-green-300' : a.action?.includes('FAIL') ? 'bg-red-900/50 text-red-300' : 'bg-blue-900/50 text-blue-300'}`}>
                            {a.action}
                          </span>
                        </td>
                        <td className="px-4 py-2 text-gray-400 font-mono">{a.resource}</td>
                        <td className="px-4 py-2 text-gray-500 font-mono">{a.client_ip}</td>
                      </tr>
                    ))}
                    {audit.length === 0 && <tr><td colSpan={5} className="px-4 py-8 text-center text-arteria-muted">No audit entries</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function StatCard({ label, value, sub, alert }: { label: string; value: string; sub?: string; alert?: boolean }) {
  return (
    <div className={`bg-arteria-surface border rounded-lg p-4 ${alert ? 'border-red-700/50' : 'border-arteria-border'}`}>
      <div className="text-xs text-arteria-muted">{label}</div>
      <div className={`text-2xl font-bold mt-1 ${alert ? 'text-red-400' : 'text-white'}`}>{value}</div>
      {sub && <div className="text-[10px] text-gray-600 mt-1">{sub}</div>}
    </div>
  );
}

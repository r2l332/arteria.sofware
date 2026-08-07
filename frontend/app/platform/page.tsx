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
  streams?: number;
  consumers?: number;
  messages?: number;
  bytes?: number;
  pending?: number;
}

interface ConnectionEntry {
  source_ip: string;
  timestamp: string;
  duration_ms: number;
  protocol: string;
  bytes_in: number;
  bytes_out: number;
}

interface AuditEntry {
  username: string;
  action: string;
  path: string;
  target_id: string;
  ip: string;
  user_agent: string;
  timestamp: string;
}

interface ConfigChange {
  change_type: string;
  entity_id: string;
  entity_name: string;
  changed_by: string;
  changed_at: string;
  details: string;
}

interface ServiceLog {
  timestamp: string;
  level: string;
  message: string;
  fields?: Record<string, string>;
}

export default function PlatformPage() {
  const [token, setToken] = useState('');
  const [activeTab, setActiveTab] = useState<'health' | 'nats' | 'logs' | 'audit' | 'connections' | 'config'>('health');
  const [health, setHealth] = useState<HealthComponent[]>([]);
  const [nats, setNats] = useState<NATSStats>({});
  const [tunnelStats, setTunnelStats] = useState<Record<string, unknown>>({});
  const [logs, setLogs] = useState<ServiceLog[]>([]);
  const [logService, setLogService] = useState('processing');
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [connections, setConnections] = useState<ConnectionEntry[]>([]);
  const [configHistory, setConfigHistory] = useState<ConfigChange[]>([]);
  const [loading, setLoading] = useState(true);

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
      case 'health': {
        const h = await apiFetch('/platform/health');
        setHealth(h?.components || []);
        const n = await apiFetch('/platform/nats-stats');
        setNats(n || {});
        const t = await apiFetch('/platform/tunnel-stats');
        setTunnelStats(t || {});
        break;
      }
      case 'nats': {
        const n = await apiFetch('/platform/nats-stats');
        setNats(n || {});
        break;
      }
      case 'logs': {
        const l = await apiFetch(`/platform/logs/${logService}`);
        setLogs(l?.logs || []);
        break;
      }
      case 'audit': {
        const a = await apiFetch('/audit-log?limit=50');
        setAudit(a?.entries || []);
        break;
      }
      case 'connections': {
        const c = await apiFetch('/platform/connections');
        setConnections(c?.connections || []);
        break;
      }
      case 'config': {
        const ch = await apiFetch('/config/history');
        setConfigHistory(ch?.changes || []);
        break;
      }
    }
    setLoading(false);
  };

  const refreshHealth = () => loadTab('health');

  const tabs = [
    { id: 'health' as const, label: 'System Health' },
    { id: 'nats' as const, label: 'NATS / Queues' },
    { id: 'logs' as const, label: 'Service Logs' },
    { id: 'audit' as const, label: 'Audit Trail' },
    { id: 'connections' as const, label: 'Connections' },
    { id: 'config' as const, label: 'Config History' },
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
          {loading && <div className="text-arteria-muted text-sm">Loading...</div>}

          {/* HEALTH TAB */}
          {activeTab === 'health' && !loading && (
            <div className="space-y-6">
              <div className="flex justify-between items-center">
                <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Component Health</h2>
                <button onClick={refreshHealth} className="px-3 py-1 text-xs bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">Refresh</button>
              </div>
              <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
                {health.map((c, i) => (
                  <div key={i} className="bg-arteria-surface border border-arteria-border rounded-lg p-4">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm text-white font-medium">{c.name}</span>
                      <span className={`w-2.5 h-2.5 rounded-full ${statusColor(c.status)}`} />
                    </div>
                    <div className="text-xs text-arteria-muted">
                      <span className="uppercase">{c.status}</span>
                      {c.latency_ms !== undefined && <span className="ml-2">{c.latency_ms}ms</span>}
                    </div>
                    {c.details && <p className="text-[10px] text-gray-600 mt-1">{c.details}</p>}
                  </div>
                ))}
                {health.length === 0 && <p className="text-sm text-arteria-muted col-span-3">No health data available. Check API permissions.</p>}
              </div>

              {/* NATS summary */}
              <div>
                <h2 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">NATS JetStream</h2>
                <div className="grid grid-cols-4 gap-4">
                  {[
                    { label: 'Streams', value: nats.streams ?? '-' },
                    { label: 'Consumers', value: nats.consumers ?? '-' },
                    { label: 'Messages', value: nats.messages?.toLocaleString() ?? '-' },
                    { label: 'Pending', value: nats.pending ?? 0 },
                  ].map(m => (
                    <div key={m.label} className="bg-arteria-surface border border-arteria-border rounded-lg p-4 text-center">
                      <div className="text-2xl font-bold text-white">{m.value}</div>
                      <div className="text-xs text-arteria-muted mt-1">{m.label}</div>
                    </div>
                  ))}
                </div>
              </div>

              {/* Tunnel summary */}
              <div>
                <h2 className="text-sm font-semibold text-white uppercase tracking-wider mb-3">Aorta Mesh</h2>
                <div className="grid grid-cols-3 gap-4">
                  {Object.entries(tunnelStats).filter(([k]) => typeof tunnelStats[k] !== 'object').map(([k, v]) => (
                    <div key={k} className="bg-arteria-surface border border-arteria-border rounded-lg p-4 text-center">
                      <div className="text-2xl font-bold text-white">{String(v)}</div>
                      <div className="text-xs text-arteria-muted mt-1">{k.replace(/_/g, ' ')}</div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}

          {/* NATS TAB */}
          {activeTab === 'nats' && !loading && (
            <div className="space-y-4">
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">NATS JetStream Detail</h2>
              <pre className="bg-arteria-surface border border-arteria-border rounded-lg p-4 text-xs text-gray-300 overflow-x-auto font-mono">
                {JSON.stringify(nats, null, 2)}
              </pre>
            </div>
          )}

          {/* LOGS TAB */}
          {activeTab === 'logs' && !loading && (
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Service Logs</h2>
                <select value={logService} onChange={(e) => { setLogService(e.target.value); }}
                  className="bg-arteria-bg border border-arteria-border rounded px-2 py-1 text-xs text-white">
                  {['api', 'ingestion', 'processing', 'egress', 'tunnel-broker'].map(s => (
                    <option key={s} value={s}>{s}</option>
                  ))}
                </select>
                <button onClick={() => loadTab('logs')} className="px-3 py-1 text-xs bg-arteria-accent text-white rounded hover:bg-arteria-accent/80">Load</button>
              </div>
              <div className="bg-arteria-surface border border-arteria-border rounded-lg overflow-hidden">
                <div className="max-h-[600px] overflow-y-auto font-mono text-xs p-2 space-y-0.5">
                  {logs.map((l, i) => (
                    <div key={i} className={`${levelColor(l.level)} flex gap-2`}>
                      <span className="text-gray-600 min-w-[180px]">{l.timestamp?.slice(0, 23)}</span>
                      <span className="min-w-[50px] font-bold">{l.level}</span>
                      <span className="text-gray-300">{l.message}</span>
                    </div>
                  ))}
                  {logs.length === 0 && <p className="text-arteria-muted p-4">No logs available for {logService}</p>}
                </div>
              </div>
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
                      <th className="text-left px-4 py-2 text-arteria-muted">Path</th>
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
                        <td className="px-4 py-2 text-gray-400 font-mono">{a.path}</td>
                        <td className="px-4 py-2 text-gray-500 font-mono">{a.ip}</td>
                      </tr>
                    ))}
                    {audit.length === 0 && <tr><td colSpan={5} className="px-4 py-8 text-center text-arteria-muted">No audit entries</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* CONNECTIONS TAB */}
          {activeTab === 'connections' && !loading && (
            <div className="space-y-4">
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Connection History</h2>
              <div className="bg-arteria-surface border border-arteria-border rounded-lg overflow-hidden">
                <table className="w-full text-xs">
                  <thead className="bg-arteria-bg">
                    <tr>
                      <th className="text-left px-4 py-2 text-arteria-muted">Time</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Source IP</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Protocol</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Duration</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Bytes In</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Bytes Out</th>
                    </tr>
                  </thead>
                  <tbody>
                    {connections.map((c, i) => (
                      <tr key={i} className="border-t border-arteria-border/30 hover:bg-white/[0.02]">
                        <td className="px-4 py-2 text-gray-400 font-mono">{c.timestamp?.slice(0, 19)}</td>
                        <td className="px-4 py-2 text-white font-mono">{c.source_ip}</td>
                        <td className="px-4 py-2 text-cyan-400">{c.protocol}</td>
                        <td className="px-4 py-2 text-gray-300">{c.duration_ms}ms</td>
                        <td className="px-4 py-2 text-gray-400">{c.bytes_in?.toLocaleString()}</td>
                        <td className="px-4 py-2 text-gray-400">{c.bytes_out?.toLocaleString()}</td>
                      </tr>
                    ))}
                    {connections.length === 0 && <tr><td colSpan={6} className="px-4 py-8 text-center text-arteria-muted">No connection history</td></tr>}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* CONFIG HISTORY TAB */}
          {activeTab === 'config' && !loading && (
            <div className="space-y-4">
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">Configuration Change History</h2>
              <div className="bg-arteria-surface border border-arteria-border rounded-lg overflow-hidden">
                <table className="w-full text-xs">
                  <thead className="bg-arteria-bg">
                    <tr>
                      <th className="text-left px-4 py-2 text-arteria-muted">Time</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Type</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Entity</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Changed By</th>
                      <th className="text-left px-4 py-2 text-arteria-muted">Details</th>
                    </tr>
                  </thead>
                  <tbody>
                    {configHistory.map((c, i) => (
                      <tr key={i} className="border-t border-arteria-border/30 hover:bg-white/[0.02]">
                        <td className="px-4 py-2 text-gray-400 font-mono">{c.changed_at?.slice(0, 19)}</td>
                        <td className="px-4 py-2">
                          <span className="px-1.5 py-0.5 bg-purple-900/50 text-purple-300 rounded text-[10px]">{c.change_type}</span>
                        </td>
                        <td className="px-4 py-2 text-white">{c.entity_name || c.entity_id?.slice(0, 8)}</td>
                        <td className="px-4 py-2 text-gray-300">{c.changed_by}</td>
                        <td className="px-4 py-2 text-gray-500 truncate max-w-[300px]">{c.details}</td>
                      </tr>
                    ))}
                    {configHistory.length === 0 && <tr><td colSpan={5} className="px-4 py-8 text-center text-arteria-muted">No config changes recorded</td></tr>}
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

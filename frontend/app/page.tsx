'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { StatCard, StatusBadge, PageHeader, EmptyState } from '@/components/ui';
import { getStats, getMessages, getErrors, getMetrics, apiFetch, type Stats, type Message, type ErrorMessage, type LiveMetrics } from '@/lib/api';
import { useAuth } from '@/lib/auth';
import { MessageSquare, GitBranch, AlertTriangle, Activity, ArrowUpRight, ArrowDownRight, Zap, Database, Server, Shield, Building2 } from 'lucide-react';

interface PlatformHealth {
  status: string;
  components: Record<string, { status: string; latency_ms?: number; details?: string }>;
  tunnel_nodes?: { total: number; connected: number };
  orgs?: { total: number };
  users?: { total: number; online: number };
}

interface OrgUsage {
  org_id: string;
  name: string;
  slug: string;
  total_messages: number;
  users: number;
  comm_points: number;
  messages_24h?: number;
  messages_7d?: number;
  messages_30d?: number;
  tunnel_nodes?: number;
}

interface PlatformUsage {
  total_messages: number;
  organisations: OrgUsage[];
}

export default function DashboardPage() {
  const { role } = useAuth();
  const isPlatform = role === 'super_admin' || role === 'devops';
  const [stats, setStats] = useState<Stats | null>(null);
  const [metrics, setMetrics] = useState<LiveMetrics | null>(null);
  const [recentMessages, setRecentMessages] = useState<Message[]>([]);
  const [recentErrors, setRecentErrors] = useState<ErrorMessage[]>([]);
  const [health, setHealth] = useState<PlatformHealth | null>(null);
  const [usage, setUsage] = useState<PlatformUsage | null>(null);

  useEffect(() => {
    const load = async () => {
      getStats().then(setStats).catch(() => {});
      getMetrics().then(setMetrics).catch(() => {});

      if (isPlatform) {
        apiFetch<PlatformHealth>('/platform/health').then(setHealth).catch(() => {});
        apiFetch<PlatformUsage>('/platform/usage').then(setUsage).catch(() => {});
      } else {
        getMessages(10).then(m => setRecentMessages(m.messages)).catch(() => {});
        getErrors(5).then(e => setRecentErrors(e.errors)).catch(() => {});
      }
    };
    load();
    const interval = setInterval(load, 5000);
    return () => clearInterval(interval);
  }, [isPlatform]);

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-y-auto">
        <PageHeader title={isPlatform ? "Platform Overview" : "Dashboard"} description={isPlatform ? "System health and organisation metrics" : "Real-time system overview"} />

        <div className="p-8 space-y-6">
          {/* Stats Cards */}
          {isPlatform ? (
            <div className="grid grid-cols-4 gap-4">
              <StatCard label="Total Messages" value={stats?.total_messages ?? '—'} icon={MessageSquare} subtitle="All orgs" />
              <StatCard label="Organisations" value={health?.orgs?.total ?? '—'} icon={Building2} subtitle="Active" />
              <StatCard label="Users" value={health?.users?.total ?? '—'} icon={Activity} subtitle={`${health?.users?.online ?? 0} online`} />
              <StatCard label="Capillary Nodes" value={health?.tunnel_nodes?.total ?? '—'} icon={Shield} subtitle={`${health?.tunnel_nodes?.connected ?? 0} connected`} />
            </div>
          ) : (
            <div className="grid grid-cols-3 gap-4">
              <StatCard label="Total Messages" value={stats?.total_messages ?? '—'} icon={MessageSquare} subtitle="All time" />
              <StatCard label="Active Routes" value={stats?.total_routes ?? '—'} icon={GitBranch} subtitle="Configured" />
              <StatCard label="Errors" value={stats?.total_errors ?? '—'} icon={AlertTriangle} subtitle="Total failures" />
            </div>
          )}

          {/* Platform Health (super_admin only) */}
          {isPlatform && health && (
            <div className="card">
              <div className="card-header flex items-center gap-2">
                <Server size={16} className="text-arteria-muted" />
                <span className="text-sm font-medium text-white">Platform Health</span>
                <span className={`ml-auto badge ${health.status === 'healthy' ? 'bg-green-500/10 text-green-400' : 'bg-red-500/10 text-red-400'}`}>
                  {health.status}
                </span>
              </div>
              <div className="p-5 grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
                {Object.entries(health.components || {}).map(([name, comp]) => (
                  <div key={name} className="bg-arteria-bg rounded-lg p-3 border border-arteria-border">
                    <div className="flex items-center gap-2 mb-1">
                      <div className={`w-2 h-2 rounded-full ${comp.status === 'up' ? 'bg-green-400' : 'bg-red-400'}`} />
                      <span className="text-xs font-medium text-white capitalize">{name}</span>
                    </div>
                    {comp.latency_ms !== undefined && (
                      <p className="text-2xs text-arteria-muted">{comp.latency_ms}ms latency</p>
                    )}
                    {comp.details && <p className="text-2xs text-arteria-muted">{comp.details}</p>}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Per-Org Usage (billing metrics) */}
          {isPlatform && usage && usage.organisations.length > 0 && (
            <div className="card">
              <div className="card-header flex items-center gap-2">
                <Building2 size={16} className="text-arteria-muted" />
                <span className="text-sm font-medium text-white">Organisation Usage</span>
                <span className="ml-auto text-2xs text-arteria-muted">{usage.total_messages.toLocaleString()} total messages</span>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-arteria-border">
                      <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Organisation</th>
                      <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">24h</th>
                      <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">7 days</th>
                      <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">30 days</th>
                      <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">All Time</th>
                      <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Users</th>
                      <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">CPs</th>
                      <th className="text-right py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Capillaries</th>
                    </tr>
                  </thead>
                  <tbody>
                    {usage.organisations.map(org => (
                      <tr key={org.org_id} className="border-b border-arteria-border/50 hover:bg-white/[0.015]">
                        <td className="py-3 px-5">
                          <div>
                            <span className="text-white font-medium">{org.name}</span>
                            <span className="text-2xs text-arteria-muted ml-2 font-mono">{org.slug}</span>
                          </div>
                        </td>
                        <td className="py-3 px-5 text-right text-white font-mono text-xs">{(org.messages_24h ?? 0).toLocaleString()}</td>
                        <td className="py-3 px-5 text-right text-white font-mono text-xs">{(org.messages_7d ?? 0).toLocaleString()}</td>
                        <td className="py-3 px-5 text-right text-white font-mono text-xs">{(org.messages_30d ?? 0).toLocaleString()}</td>
                        <td className="py-3 px-5 text-right text-arteria-accent font-mono text-xs font-medium">{org.total_messages.toLocaleString()}</td>
                        <td className="py-3 px-5 text-right text-xs text-arteria-muted">{org.users}</td>
                        <td className="py-3 px-5 text-right text-xs text-arteria-muted">{org.comm_points}</td>
                        <td className="py-3 px-5 text-right text-xs text-arteria-muted">{org.tunnel_nodes ?? 0}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Live Throughput */}
          {metrics && (
            <div className="grid grid-cols-2 gap-4">
              {metrics.ingestion && (
                <div className="card">
                  <div className="card-header flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="w-6 h-6 rounded-md bg-blue-500/10 flex items-center justify-center">
                        <ArrowDownRight size={14} className="text-blue-400" />
                      </div>
                      <span className="text-xs font-medium text-arteria-text-secondary">Ingestion</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <div className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse-slow" />
                      <span className="text-2xs text-arteria-muted">Live</span>
                    </div>
                  </div>
                  <div className="p-5">
                    <div className="grid grid-cols-3 gap-6">
                      <div>
                        <p className="text-2xl font-semibold text-white tabular-nums">{metrics.ingestion.received}</p>
                        <p className="text-2xs text-arteria-muted mt-0.5">Received</p>
                      </div>
                      <div>
                        <p className="text-2xl font-semibold text-emerald-400 tabular-nums">{metrics.ingestion.processed}</p>
                        <p className="text-2xs text-arteria-muted mt-0.5">Published</p>
                      </div>
                      <div>
                        <p className="text-2xl font-semibold text-arteria-accent tabular-nums">{Math.round(metrics.ingestion.msgs_per_minute)}</p>
                        <p className="text-2xs text-arteria-muted mt-0.5">msgs/min</p>
                      </div>
                    </div>
                    <div className="mt-4 pt-3 border-t border-arteria-border flex items-center gap-4 text-2xs text-arteria-muted">
                      <span className="flex items-center gap-1"><Database size={10} /> {(metrics.ingestion.bytes_in / 1024).toFixed(1)} KB</span>
                      <span className="flex items-center gap-1"><Activity size={10} /> {Math.round(metrics.ingestion.uptime_seconds)}s uptime</span>
                    </div>
                  </div>
                </div>
              )}
              {metrics.processing && (
                <div className="card">
                  <div className="card-header flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <div className="w-6 h-6 rounded-md bg-purple-500/10 flex items-center justify-center">
                        <Zap size={14} className="text-purple-400" />
                      </div>
                      <span className="text-xs font-medium text-arteria-text-secondary">Processing</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <div className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse-slow" />
                      <span className="text-2xs text-arteria-muted">Live</span>
                    </div>
                  </div>
                  <div className="p-5">
                    <div className="grid grid-cols-4 gap-4">
                      <div>
                        <p className="text-2xl font-semibold text-white tabular-nums">{metrics.processing.received}</p>
                        <p className="text-2xs text-arteria-muted mt-0.5">Received</p>
                      </div>
                      <div>
                        <p className="text-2xl font-semibold text-emerald-400 tabular-nums">{metrics.processing.routed}</p>
                        <p className="text-2xs text-arteria-muted mt-0.5">Routed</p>
                      </div>
                      <div>
                        <p className="text-2xl font-semibold text-red-400 tabular-nums">{metrics.processing.errors}</p>
                        <p className="text-2xs text-arteria-muted mt-0.5">Errors</p>
                      </div>
                      <div>
                        <p className="text-2xl font-semibold text-arteria-accent tabular-nums">{Math.round(metrics.processing.msgs_per_minute)}</p>
                        <p className="text-2xs text-arteria-muted mt-0.5">msgs/min</p>
                      </div>
                    </div>
                    <div className="mt-4 pt-3 border-t border-arteria-border flex items-center gap-4 text-2xs text-arteria-muted">
                      <span>{metrics.processing.rejected} rejected</span>
                      <span>{metrics.processing.dlq} DLQ</span>
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Recent Messages */}
          <div className="card">
            <div className="card-header flex items-center justify-between">
              <h3 className="text-sm font-medium text-white">Recent Messages</h3>
              <a href="/messages" className="text-2xs text-arteria-accent hover:text-arteria-accent-hover flex items-center gap-1">
                View all <ArrowUpRight size={10} />
              </a>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-arteria-border">
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Type</th>
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Patient</th>
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Facility</th>
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Status</th>
                    <th className="text-left py-3 px-5 text-arteria-muted text-2xs font-medium uppercase tracking-wider">Time</th>
                  </tr>
                </thead>
                <tbody>
                  {recentMessages.map((m) => (
                    <tr key={m.message_id} className="border-b border-arteria-border/50 hover:bg-white/[0.015] transition-colors duration-100">
                      <td className="py-3 px-5 font-mono text-xs text-arteria-text-secondary">{m.message_type}^{m.trigger_event}</td>
                      <td className="py-3 px-5 text-xs text-arteria-text-secondary">{m.patient_id || '—'}</td>
                      <td className="py-3 px-5 text-xs text-arteria-text-secondary">{m.sending_facility || '—'}</td>
                      <td className="py-3 px-5"><StatusBadge status={m.status} /></td>
                      <td className="py-3 px-5 text-xs text-arteria-muted">{new Date(m.created_at).toLocaleTimeString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {recentMessages.length === 0 && (
                <EmptyState icon={MessageSquare} title="No messages yet" description="Send an HL7 message to get started" />
              )}
            </div>
          </div>

          {/* Recent Errors */}
          {recentErrors.length > 0 && (
            <div className="card border-red-500/20">
              <div className="card-header flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <AlertTriangle size={14} className="text-red-400" />
                  <h3 className="text-sm font-medium text-red-400">Recent Errors</h3>
                </div>
                <a href="/errors" className="text-2xs text-arteria-accent hover:text-arteria-accent-hover flex items-center gap-1">
                  View all <ArrowUpRight size={10} />
                </a>
              </div>
              <div className="divide-y divide-arteria-border/50">
                {recentErrors.map((e) => (
                  <div key={e.message_id} className="px-5 py-3 flex items-center justify-between">
                    <div className="flex items-center gap-3">
                      <span className="font-mono text-2xs text-arteria-muted">{e.message_id.slice(0, 8)}</span>
                      <StatusBadge status={e.error_type} />
                    </div>
                    <span className="text-xs text-arteria-muted truncate max-w-sm ml-4">{e.error_details}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

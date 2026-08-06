'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { StatusBadge } from '@/components/ui';
import { getMessages, getMessage, getRoutes, getCommPoints, type Message, type MessageDetail, type Route, type CommPoint } from '@/lib/api';

export default function MessagesPage() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [selected, setSelected] = useState<MessageDetail | null>(null);
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);

  useEffect(() => {
    getMessages(100).then((r) => setMessages(r.messages || [])).catch(console.error);
    getRoutes().then((r) => setRoutes(r.routes || [])).catch(() => {});
    getCommPoints().then((r) => setCommPoints(r.communication_points || [])).catch(() => {});
  }, []);

  const cpMap = new Map(commPoints.map(cp => [cp.comm_point_id, cp]));

  const findRoute = (msgType: string, trigger: string) => {
    const topic = `${msgType}^${trigger}`;
    return routes.find(r => r.source_topic === topic) || routes.find(r => r.source_topic === '*');
  };

  const openDetail = async (id: string) => {
    const detail = await getMessage(id);
    setSelected(detail);
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-y-auto p-8">
        <h2 className="text-2xl font-bold text-white mb-6">Message Log</h2>

        <div className="bg-arteria-surface border border-arteria-border rounded-lg">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-arteria-border">
                  <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">ID</th>
                  <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Type</th>
                  <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Patient</th>
                  <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Facility</th>
                  <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Route</th>
                  <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Status</th>
                  <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Time</th>
                </tr>
              </thead>
              <tbody>
                {messages.map((m) => {
                  const route = findRoute(m.message_type, m.trigger_event);
                  return (
                  <tr
                    key={m.message_id}
                    onClick={() => openDetail(m.message_id)}
                    className="border-b border-arteria-border/50 hover:bg-white/[0.02] cursor-pointer transition-colors"
                  >
                    <td className="py-3 px-4 font-mono text-xs text-arteria-accent">{m.message_id.slice(0, 8)}…</td>
                    <td className="py-3 px-4 font-mono text-xs">{m.message_type}^{m.trigger_event}</td>
                    <td className="py-3 px-4">{m.patient_id || '—'}</td>
                    <td className="py-3 px-4">{m.sending_facility || '—'}</td>
                    <td className="py-3 px-4 text-xs text-purple-400">{route?.name || '—'}</td>
                    <td className="py-3 px-4"><StatusBadge status={m.status} /></td>
                    <td className="py-3 px-4 text-arteria-muted text-xs">{new Date(m.created_at).toLocaleString()}</td>
                  </tr>
                  );
                })}
              </tbody>
            </table>
            {messages.length === 0 && (
              <p className="text-center py-8 text-arteria-muted">No messages</p>
            )}
          </div>
        </div>

        {/* Detail Panel */}
        {selected && (
          <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setSelected(null)}>
            <div className="bg-arteria-surface border border-arteria-border rounded-lg w-[800px] max-h-[80vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
              <div className="flex items-center justify-between px-6 py-4 border-b border-arteria-border">
                <h3 className="font-semibold text-white">Message Detail</h3>
                <button onClick={() => setSelected(null)} className="text-arteria-muted hover:text-white">✕</button>
              </div>
              <div className="p-6 space-y-4">
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div><span className="text-arteria-muted">ID:</span> <span className="font-mono text-xs">{selected.message_id}</span></div>
                  <div><span className="text-arteria-muted">Status:</span> <StatusBadge status={selected.status} /></div>
                  <div><span className="text-arteria-muted">Type:</span> {selected.message_type}^{selected.trigger_event}</div>
                  <div><span className="text-arteria-muted">Patient:</span> {selected.patient_id || '—'}</div>
                  <div><span className="text-arteria-muted">Facility:</span> {selected.sending_facility || '—'}</div>
                  <div><span className="text-arteria-muted">Retries:</span> {selected.retry_count}</div>
                </div>
                {/* Route & CP context */}
                <RouteContext route={findRoute(selected.message_type, selected.trigger_event)} cpMap={cpMap} />
                {selected.error_details && (
                  <div>
                    <p className="text-sm text-arteria-error font-medium mb-1">Error</p>
                    <pre className="bg-red-950/30 p-3 rounded text-xs text-red-300 overflow-x-auto">{selected.error_details}</pre>
                  </div>
                )}
                <div>
                  <p className="text-sm text-arteria-muted font-medium mb-1">Raw Payload</p>
                  <pre className="bg-arteria-bg p-3 rounded text-xs text-green-300 overflow-x-auto whitespace-pre-wrap">{selected.raw_payload}</pre>
                </div>
                {selected.transformed_payload && (
                  <div>
                    <p className="text-sm text-arteria-muted font-medium mb-1">Transformed Payload</p>
                    <pre className="bg-arteria-bg p-3 rounded text-xs text-cyan-300 overflow-x-auto whitespace-pre-wrap">{selected.transformed_payload}</pre>
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function RouteContext({ route, cpMap }: { route?: Route; cpMap: Map<string, CommPoint> }) {
  if (!route) return null;
  const srcCP = cpMap.get(route.source_comm_point_id);
  const dstCP = cpMap.get(route.dest_comm_point_id);
  return (
    <div className="bg-purple-950/20 border border-purple-900/30 rounded-lg p-3 grid grid-cols-3 gap-3 text-xs">
      <div>
        <span className="text-purple-400 font-medium">Route</span>
        <p className="text-white mt-0.5">{route.name}</p>
        <p className="text-gray-500 font-mono">{route.source_topic} → {route.destination_topic}</p>
      </div>
      <div>
        <span className="text-blue-400 font-medium">Input CP</span>
        <p className="text-white mt-0.5">{srcCP?.name || '—'}</p>
        <p className="text-gray-500 font-mono">{srcCP ? `${srcCP.protocol} :${srcCP.port}` : ''}</p>
      </div>
      <div>
        <span className="text-emerald-400 font-medium">Output CP</span>
        <p className="text-white mt-0.5">{dstCP?.name || '—'}</p>
        <p className="text-gray-500 font-mono">{dstCP ? `${dstCP.protocol} :${dstCP.port}` : ''}</p>
      </div>
    </div>
  );
}

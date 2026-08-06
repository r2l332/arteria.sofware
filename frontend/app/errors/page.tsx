'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { getErrors, getMessage, getRoutes, getCommPoints, type ErrorMessage, type MessageDetail, type Route, type CommPoint } from '@/lib/api';
import { X, AlertTriangle, RefreshCw } from 'lucide-react';

export default function ErrorsPage() {
  const [errors, setErrors] = useState<ErrorMessage[]>([]);
  const [selected, setSelected] = useState<ErrorMessage | null>(null);
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);
  const [detail, setDetail] = useState<MessageDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);

  const load = () => getErrors(100).then((r) => setErrors(r.errors || [])).catch(console.error);
  useEffect(() => {
    load();
    getRoutes().then((r) => setRoutes(r.routes || [])).catch(() => {});
    getCommPoints().then((r) => setCommPoints(r.communication_points || [])).catch(() => {});
  }, []);

  const cpMap = new Map(commPoints.map(cp => [cp.comm_point_id, cp]));

  const findRouteFromError = (errorDetails: string) => {
    // Try to extract route name from error like "filter chain error on route <name>: ..."
    const match = errorDetails.match(/on route ([^:]+):/);
    if (match) return routes.find(r => r.name === match[1].trim());
    return undefined;
  };

  const openError = async (err: ErrorMessage) => {
    setSelected(err);
    setDetail(null);
    setLoadingDetail(true);
    try {
      const msg = await getMessage(err.message_id);
      setDetail(msg);
    } catch (e) {
      console.error('Failed to load message detail:', e);
    }
    setLoadingDetail(false);
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-y-auto p-8">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-2xl font-bold text-white">Errors / Dead Letter Queue</h2>
          <button onClick={load} className="flex items-center gap-1.5 text-xs text-gray-400 hover:text-white bg-gray-800 px-3 py-1.5 rounded-lg border border-gray-700">
            <RefreshCw className="w-3 h-3" /> Refresh
          </button>
        </div>

        <div className="bg-arteria-surface border border-arteria-border rounded-lg">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-arteria-border">
                <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Message ID</th>
                <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Error Type</th>
                <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Details</th>
                <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Retries</th>
                <th className="text-left py-3 px-4 text-arteria-muted text-xs uppercase">Time</th>
              </tr>
            </thead>
            <tbody>
              {errors.map((e) => (
                <tr
                  key={e.message_id}
                  onClick={() => openError(e)}
                  className="border-b border-arteria-border/50 cursor-pointer hover:bg-gray-800/50 transition-colors"
                >
                  <td className="py-3 px-4 font-mono text-xs text-arteria-accent">{e.message_id.slice(0, 8)}…</td>
                  <td className="py-3 px-4">
                    <span className="bg-red-500/20 text-red-400 px-2 py-0.5 rounded text-xs">{e.error_type}</span>
                  </td>
                  <td className="py-3 px-4 text-gray-400 text-xs truncate max-w-md">{e.error_details}</td>
                  <td className="py-3 px-4">{e.retry_count}/{e.max_retries}</td>
                  <td className="py-3 px-4 text-arteria-muted text-xs">{new Date(e.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {errors.length === 0 && (
            <p className="text-center py-8 text-arteria-muted">No errors — all systems operational</p>
          )}
        </div>

        {/* Error detail drawer */}
        {selected && (
          <div className="fixed inset-0 z-50 flex">
            <div className="absolute inset-0 bg-black/50" onClick={() => setSelected(null)} />
            <div className="absolute right-0 top-0 bottom-0 w-[600px] bg-gray-900 border-l border-gray-800 overflow-y-auto shadow-2xl">
              <div className="sticky top-0 bg-gray-900 border-b border-gray-800 px-6 py-4 flex items-center justify-between z-10">
                <div className="flex items-center gap-3">
                  <AlertTriangle className="w-5 h-5 text-red-400" />
                  <div>
                    <h3 className="text-sm font-bold text-white">Error Detail</h3>
                    <p className="text-xs text-gray-500 font-mono">{selected.message_id}</p>
                  </div>
                </div>
                <button onClick={() => setSelected(null)} className="text-gray-500 hover:text-white"><X className="w-5 h-5" /></button>
              </div>

              <div className="p-6 space-y-5">
                {/* Error summary */}
                <div className="grid grid-cols-2 gap-3">
                  <DetailCard label="Error Type" value={selected.error_type} highlight />
                  <DetailCard label="Retries" value={`${selected.retry_count} / ${selected.max_retries}`} />
                  <DetailCard label="Time" value={new Date(selected.created_at).toLocaleString()} />
                  <DetailCard label="Message ID" value={selected.message_id} mono />
                </div>

                {/* Route & CP context */}
                {(() => {
                  const route = findRouteFromError(selected.error_details);
                  if (!route) return null;
                  const srcCP = cpMap.get(route.source_comm_point_id);
                  const dstCP = cpMap.get(route.dest_comm_point_id);
                  return (
                    <div className="bg-purple-950/20 border border-purple-900/30 rounded-lg p-3 grid grid-cols-3 gap-3 text-xs">
                      <div>
                        <span className="text-purple-400 font-medium">Route</span>
                        <p className="text-white mt-0.5">{route.name}</p>
                      </div>
                      <div>
                        <span className="text-blue-400 font-medium">Input CP</span>
                        <p className="text-white mt-0.5">{srcCP?.name || '—'}</p>
                      </div>
                      <div>
                        <span className="text-emerald-400 font-medium">Output CP</span>
                        <p className="text-white mt-0.5">{dstCP?.name || '—'}</p>
                      </div>
                    </div>
                  );
                })()}

                {/* Error details */}
                <div>
                  <h4 className="text-xs text-gray-500 uppercase tracking-wider mb-2">Error Details</h4>
                  <pre className="text-xs bg-red-950/30 border border-red-900/30 rounded-lg p-4 text-red-300 font-mono whitespace-pre-wrap break-all">
                    {selected.error_details}
                  </pre>
                </div>

                {/* Message content (if loaded) */}
                {loadingDetail && (
                  <div className="flex items-center justify-center py-8">
                    <div className="animate-spin rounded-full h-5 w-5 border-t-2 border-cyan-400" />
                  </div>
                )}
                {detail && (
                  <>
                    <div className="grid grid-cols-2 gap-3">
                      <DetailCard label="Message Type" value={`${detail.message_type}^${detail.trigger_event}`} />
                      <DetailCard label="Patient ID" value={detail.patient_id || '—'} />
                      <DetailCard label="Facility" value={detail.sending_facility || '—'} />
                      <DetailCard label="Status" value={detail.status} highlight />
                    </div>

                    {detail.raw_payload && (
                      <div>
                        <h4 className="text-xs text-gray-500 uppercase tracking-wider mb-2">Raw Payload</h4>
                        <pre className="text-xs bg-gray-950 border border-gray-800 rounded-lg p-4 text-gray-300 font-mono whitespace-pre-wrap break-all max-h-60 overflow-auto">
                          {detail.raw_payload}
                        </pre>
                      </div>
                    )}

                    {detail.transformed_payload && (
                      <div>
                        <h4 className="text-xs text-gray-500 uppercase tracking-wider mb-2">Transformed Payload</h4>
                        <pre className="text-xs bg-gray-950 border border-gray-800 rounded-lg p-4 text-emerald-300/80 font-mono whitespace-pre-wrap break-all max-h-60 overflow-auto">
                          {formatJson(detail.transformed_payload)}
                        </pre>
                      </div>
                    )}

                    {detail.properties && (
                      <div>
                        <h4 className="text-xs text-gray-500 uppercase tracking-wider mb-2">Properties</h4>
                        <pre className="text-xs bg-gray-950 border border-gray-800 rounded-lg p-4 text-cyan-300/80 font-mono whitespace-pre-wrap max-h-40 overflow-auto">
                          {formatJson(detail.properties)}
                        </pre>
                      </div>
                    )}
                  </>
                )}
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function DetailCard({ label, value, mono, highlight }: { label: string; value: string; mono?: boolean; highlight?: boolean }) {
  return (
    <div className="bg-gray-950 rounded-lg border border-gray-800 p-3">
      <div className="text-[9px] text-gray-500 uppercase tracking-wider">{label}</div>
      <div className={`text-xs mt-1 truncate ${mono ? 'font-mono' : ''} ${highlight ? 'text-red-400 font-semibold' : 'text-gray-200'}`}>{value}</div>
    </div>
  );
}

function formatJson(s: string): string {
  try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; }
}

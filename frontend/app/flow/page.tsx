'use client';

import { useEffect, useState, useRef } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import {
  getRoutes, getFilters, getCommPoints, getRouteRecent, getMessageTrace,
  dropMessage, retryMessage, flushRoute, testFilter, connectFlowWebSocket,
  type Route, type Filter, type CommPoint, type StreamEvent, type TraceStep,
} from '@/lib/api';
import { Radio, GitBranch, Cpu, Send, Activity, Trash2, RotateCcw, X, GripVertical } from 'lucide-react';
import { clsx } from 'clsx';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

interface LiveMessage {
  message_id: string; message_type: string; trigger_event: string;
  patient_id: string; sending_facility: string; status: string;
  stage: string; timestamp: string;
}

interface FlowNode {
  id: string; type: 'source' | 'route' | 'filter' | 'destination';
  label: string; sublabel: string; active: boolean; count: number;
  x: number; y: number; data?: any;
}

export default function FlowPage() {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);
  const [selectedRouteId, setSelectedRouteId] = useState('');
  const [filters, setFilters] = useState<Filter[]>([]);
  const [liveMessages, setLiveMessages] = useState<LiveMessage[]>([]);
  const [metrics, setMetrics] = useState<any>(null);
  const [traceSteps, setTraceSteps] = useState<TraceStep[]>([]);
  const [tracedMsg, setTracedMsg] = useState<any>(null);
  const [testingFilter, setTestingFilter] = useState<Filter | null>(null);
  const [testPayload, setTestPayload] = useState('');
  const [testResult, setTestResult] = useState('');
  const [recentMsgs, setRecentMsgs] = useState<any[]>([]);
  const wsRef = useRef<WebSocket | null>(null);
  const counts = useRef({ received: 0, routed: 0, errors: 0 });

  useEffect(() => {
    Promise.all([
      getRoutes().catch(() => ({ routes: [], count: 0 })),
      getCommPoints().catch(() => ({ communication_points: [], count: 0 })),
    ]).then(([r, c]) => {
      setRoutes(r.routes || []);
      setCommPoints(c.communication_points || []);
      if (r.routes?.length > 0) setSelectedRouteId(r.routes[0].route_id);
    });
  }, []);

  useEffect(() => {
    if (!selectedRouteId) return;
    getFilters(selectedRouteId).then(r => setFilters((r.filters || []).sort((a, b) => a.execution_order - b.execution_order))).catch(() => setFilters([]));
    getRouteRecent(selectedRouteId, 8).then(r => setRecentMsgs(r.messages || [])).catch(() => {});
  }, [selectedRouteId]);

  // WebSocket — real-time push, no polling
  useEffect(() => {
    const ws = connectFlowWebSocket((event: StreamEvent) => {
      if (event.type === 'message' || event.type === 'error') {
        const msg = event.data as LiveMessage;
        msg.timestamp = event.timestamp;
        setLiveMessages(prev => [msg, ...prev].slice(0, 30));
        if (msg.stage === 'received') counts.current.received++;
        if (msg.stage === 'routed') counts.current.routed++;
        if (msg.stage === 'error') counts.current.errors++;
      }
      if (event.type === 'metric') setMetrics(event.data);
    });
    wsRef.current = ws;
    return () => { ws?.close(); };
  }, []);

  const openTrace = async (msgId: string) => {
    const r = await getMessageTrace(msgId).catch(() => null);
    if (r) { setTracedMsg(r.message); setTraceSteps(r.steps); }
  };

  const moveFilterUp = async (idx: number) => {
    if (idx <= 0) return;
    const newOrder = filters.map((f, i) => ({ filter_id: f.filter_id, position: i }));
    [newOrder[idx], newOrder[idx - 1]] = [newOrder[idx - 1], newOrder[idx]];
    const reordered = newOrder.map((o, i) => ({ ...o, position: i }));
    const { reorderFilters } = await import('@/lib/api');
    await reorderFilters(selectedRouteId, reordered);
    const resp = await getFilters(selectedRouteId);
    setFilters((resp.filters || []).sort((a: Filter, b: Filter) => a.execution_order - b.execution_order));
  };

  const moveFilterDown = async (idx: number) => {
    if (idx >= filters.length - 1) return;
    const newOrder = filters.map((f, i) => ({ filter_id: f.filter_id, position: i }));
    [newOrder[idx], newOrder[idx + 1]] = [newOrder[idx + 1], newOrder[idx]];
    const reordered = newOrder.map((o, i) => ({ ...o, position: i }));
    const { reorderFilters } = await import('@/lib/api');
    await reorderFilters(selectedRouteId, reordered);
    const resp = await getFilters(selectedRouteId);
    setFilters((resp.filters || []).sort((a: Filter, b: Filter) => a.execution_order - b.execution_order));
  };

  const selectedRoute = routes.find(r => r.route_id === selectedRouteId);
  const cpMap = new Map(commPoints.map(cp => [cp.comm_point_id, cp]));
  const srcCP = selectedRoute ? cpMap.get(selectedRoute.source_comm_point_id) : null;
  const dstCP = selectedRoute ? cpMap.get(selectedRoute.dest_comm_point_id) : null;

  // Build flow nodes — wider spacing for previous layout feel
  const nodes: FlowNode[] = [];
  const colW = 260;
  let col = 0;
  const yMid = 170;
  const xOff = 120;

  if (srcCP) { nodes.push({ id: 'src', type: 'source', label: srcCP.name, sublabel: `${srcCP.protocol} :${srcCP.port}`, active: srcCP.is_active, count: counts.current.received, x: xOff + col * colW, y: yMid, data: srcCP }); col++; }
  if (selectedRoute) { nodes.push({ id: 'route', type: 'route', label: selectedRoute.name, sublabel: selectedRoute.source_topic, active: selectedRoute.is_active, count: counts.current.received, x: xOff + col * colW, y: yMid + 50, data: selectedRoute }); col++; }
  filters.forEach((f, i) => { nodes.push({ id: `f-${i}`, type: 'filter', label: f.name, sublabel: f.filter_type, active: f.is_active, count: counts.current.routed, x: xOff + col * colW, y: yMid + (i % 2 === 0 ? -10 : 30), data: f }); col++; });
  if (dstCP) { nodes.push({ id: 'dst', type: 'destination', label: dstCP.name, sublabel: `${dstCP.protocol} :${dstCP.port}`, active: dstCP.is_active, count: counts.current.routed, x: xOff + col * colW, y: yMid, data: dstCP }); }

  const canvasW = Math.max(1000, (col + 2) * colW);
  const canvasH = 240;

  return (
    <div className="flex h-screen bg-gray-950">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-800/50 shrink-0">
          <div className="flex items-center gap-4">
            <h1 className="text-lg font-bold text-white">Message Flow</h1>
            <select value={selectedRouteId} onChange={(e) => setSelectedRouteId(e.target.value)}
              className="bg-gray-800 border border-gray-700 text-gray-200 text-xs rounded-lg px-2.5 py-1.5 focus:border-cyan-500 outline-none">
              {routes.map(r => <option key={r.route_id} value={r.route_id}>{r.name}</option>)}
            </select>
          </div>
          <div className="flex items-center gap-3">
            <button onClick={() => flushRoute(selectedRouteId, 'Manual flush')} className="text-[10px] bg-red-900/30 border border-red-800/50 text-red-400 px-2.5 py-1 rounded hover:bg-red-900/50">Flush</button>
            {metrics && <span className="text-[10px] text-cyan-300 font-mono bg-gray-800/80 px-2 py-1 rounded border border-gray-700/50">{(metrics.msgs_per_second || 0).toFixed(1)} msg/s</span>}
            <WsBadge ws={wsRef.current} />
          </div>
        </div>

        {/* Flow canvas — Upwind style */}
        <div className={clsx('shrink-0 relative overflow-x-auto border-b border-gray-800/30', 'bg-[radial-gradient(#1e293b_1px,transparent_1px)] [background-size:24px_24px]')} style={{ height: 420 }}>
          <div className="absolute inset-0 bg-gradient-to-tr from-cyan-950/15 via-transparent to-purple-950/15 pointer-events-none" />
          {/* Stats chips */}
          <div className="absolute top-3 left-4 flex gap-2 z-10">
            <Chip label="IN" value={counts.current.received} color="text-sky-400" />
            <Chip label="OUT" value={counts.current.routed} color="text-emerald-400" />
            {counts.current.errors > 0 && <Chip label="ERR" value={counts.current.errors} color="text-red-400" />}
          </div>

          <div className="relative h-full" style={{ minWidth: canvasW }}>
            {/* SVG edges */}
            <svg className="absolute inset-0 w-full h-full pointer-events-none">
              <defs><filter id="neon"><feGaussianBlur stdDeviation="2.5" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter></defs>
              {nodes.map((n, i) => {
                if (i === 0) return null;
                const p = nodes[i - 1];
                const x1 = p.x + 80, y1 = p.y + 35;
                const x2 = n.x, y2 = n.y + 35;
                const cx1 = x1 + (x2 - x1) * 0.4, cx2 = x1 + (x2 - x1) * 0.6;
                const d = `M${x1},${y1} C${cx1},${y1} ${cx2},${y2} ${x2},${y2}`;
                return <g key={`e${i}`}>
                  <path d={d} fill="none" stroke="#1e3a5f" strokeWidth="3" strokeLinecap="round" />
                  <path d={d} fill="none" stroke="#38bdf8" strokeWidth="2" strokeDasharray="9 5" opacity="0.7" filter="url(#neon)">
                    <animate attributeName="stroke-dashoffset" values="28;0" dur={`${1.1 + i * 0.15}s`} repeatCount="indefinite" />
                  </path>
                  <circle r="3" fill="#38bdf8" filter="url(#neon)"><animateMotion dur={`${2 + i * 0.25}s`} repeatCount="indefinite" path={d} /></circle>
                  <circle r="1.5" fill="white" opacity="0.9"><animateMotion dur={`${2 + i * 0.25}s`} repeatCount="indefinite" path={d} /></circle>
                </g>;
              })}
            </svg>
            {/* Nodes — glassmorphic cards */}
            {nodes.map((n, idx) => {
              const filterIdx = n.type === 'filter' ? filters.findIndex(f => f.filter_id === n.data?.filter_id) : -1;
              return (
              <div key={n.id}
                className={clsx('absolute w-[160px] rounded-xl border p-3 transition-all duration-200 hover:scale-[1.04] backdrop-blur-xl bg-slate-900/80 shadow-2xl border-slate-800',
                  !n.active && 'opacity-40 grayscale')}
                style={{ left: n.x, top: n.y }}
              >
                <div className="flex items-center gap-2 mb-2">
                  <div className={clsx('w-7 h-7 rounded-lg flex items-center justify-center', getBg(n.type))}>
                    <NIcon type={n.type} className={clsx('w-4 h-4', getColor(n.type))} />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="text-[10px] font-semibold text-white truncate">{n.label}</div>
                    <div className="text-[8px] text-gray-500 font-mono truncate">{n.sublabel}</div>
                  </div>
                  {/* Reorder arrows for filters */}
                  {n.type === 'filter' && filters.length > 1 && (
                    <div className="flex flex-col gap-0.5">
                      {filterIdx > 0 && <button onClick={() => moveFilterUp(filterIdx)} className="text-[9px] text-cyan-400 hover:text-cyan-300 bg-cyan-500/10 rounded px-1 leading-tight">◀</button>}
                      {filterIdx < filters.length - 1 && <button onClick={() => moveFilterDown(filterIdx)} className="text-[9px] text-cyan-400 hover:text-cyan-300 bg-cyan-500/10 rounded px-1 leading-tight">▶</button>}
                    </div>
                  )}
                </div>
                <div className="flex items-center justify-between">
                  <div className={clsx('text-center flex-1 py-1 rounded-lg', getBg(n.type))}>
                    <span className={clsx('text-sm font-bold font-mono tabular-nums', getColor(n.type))}>{n.count.toLocaleString()}</span>
                    <span className="text-[7px] text-gray-500 ml-1">msgs</span>
                  </div>
                  {n.type === 'filter' && (
                    <button onClick={() => { setTestingFilter(n.data); setTestPayload(JSON.stringify({ messageId: '', rawPayload: '', properties: {} }, null, 2)); }}
                      className="ml-2 text-[8px] text-amber-400 bg-amber-500/10 px-1.5 py-0.5 rounded hover:bg-amber-500/20">Test</button>
                  )}
                </div>
              </div>
              );
            })}
          </div>
        </div>

        {/* Bottom: Live stream + Inspector */}
        <div className="flex-1 flex overflow-hidden min-h-0">
          {/* Live stream */}
          <div className="flex-1 flex flex-col overflow-hidden border-r border-gray-800/30">
            <div className="px-4 py-2 border-b border-gray-800/30 flex items-center justify-between shrink-0">
              <span className="text-[10px] text-gray-500 uppercase tracking-widest">Live Stream</span>
              <span className="text-[9px] text-gray-600">{liveMessages.length} events</span>
            </div>
            <div className="flex-1 overflow-y-auto">
              {liveMessages.length === 0 ? (
                <div className="flex items-center justify-center h-full text-gray-600 text-xs"><Activity className="w-4 h-4 mr-2 animate-pulse" />Waiting for messages...</div>
              ) : liveMessages.map((msg, i) => (
                <div key={`${msg.message_id}-${i}`} onClick={() => openTrace(msg.message_id)}
                  className="group px-4 py-1.5 border-b border-gray-800/15 hover:bg-gray-800/30 cursor-pointer flex items-center gap-2 text-[11px]">
                  <span className={clsx('w-1.5 h-1.5 rounded-full shrink-0', msg.stage === 'received' ? 'bg-cyan-400' : msg.stage === 'routed' ? 'bg-emerald-400' : 'bg-red-400')} />
                  <span className="text-gray-400 font-mono w-14 shrink-0 text-[9px]">{msg.message_type}^{msg.trigger_event}</span>
                  <span className="text-gray-300 truncate flex-1">{msg.patient_id || msg.sending_facility || msg.message_id?.slice(0, 8)}</span>
                  <span className="text-gray-600 font-mono text-[8px] shrink-0">{new Date(msg.timestamp).toLocaleTimeString()}</span>
                  <div className="hidden group-hover:flex gap-1">
                    <button onClick={(e) => { e.stopPropagation(); dropMessage(msg.message_id, 'Dropped from flow'); }} className="p-0.5"><Trash2 className="w-3 h-3 text-red-400" /></button>
                    <button onClick={(e) => { e.stopPropagation(); retryMessage(msg.message_id); }} className="p-0.5"><RotateCcw className="w-3 h-3 text-amber-400" /></button>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Right panel */}
          <div className="w-[360px] flex flex-col overflow-hidden bg-gray-900/40">
            {testingFilter ? (
              <FilterTestPanel filter={testingFilter} payload={testPayload} result={testResult}
                onPayloadChange={setTestPayload} onClose={() => setTestingFilter(null)}
                onRun={async () => { const r = await testFilter(testingFilter.filter_id, testPayload).catch(e => ({ error: e.message })); setTestResult(JSON.stringify(r, null, 2)); }} />
            ) : traceSteps.length > 0 ? (
              <TracePanel steps={traceSteps} msg={tracedMsg} onClose={() => { setTraceSteps([]); setTracedMsg(null); }} />
            ) : (
              <RecentPanel messages={recentMsgs} onSelect={openTrace} />
            )}
          </div>
        </div>
      </main>
    </div>
  );
}

// --- Sub-components ---

function FilterTestPanel({ filter, payload, result, onPayloadChange, onClose, onRun }: any) {
  return (
    <div className="flex flex-col h-full">
      <div className="px-4 py-2 border-b border-gray-800/30 flex items-center justify-between shrink-0">
        <span className="text-xs font-semibold text-white">Test: {filter.name}</span>
        <button onClick={onClose}><X className="w-3.5 h-3.5 text-gray-500 hover:text-white" /></button>
      </div>
      <div className="flex-1 min-h-0 flex flex-col">
        <div className="h-2/5 border-b border-gray-800/30"><MonacoEditor height="100%" language="json" theme="vs-dark" value={payload} onChange={(v: any) => onPayloadChange(v || '')} options={{ minimap: { enabled: false }, fontSize: 10, lineNumbers: 'off', scrollBeyondLastLine: false }} /></div>
        <div className="px-3 py-1.5 border-b border-gray-800/30 shrink-0"><button onClick={onRun} className="text-[10px] bg-emerald-600 text-white px-3 py-1 rounded hover:bg-emerald-500">Run Filter</button></div>
        <div className="flex-1"><MonacoEditor height="100%" language="json" theme="vs-dark" value={result} options={{ readOnly: true, minimap: { enabled: false }, fontSize: 10, lineNumbers: 'off', scrollBeyondLastLine: false }} /></div>
      </div>
    </div>
  );
}

function TracePanel({ steps, msg, onClose }: { steps: TraceStep[]; msg: any; onClose: () => void }) {
  return (
    <div className="flex flex-col h-full">
      <div className="px-4 py-2 border-b border-gray-800/30 flex items-center justify-between shrink-0">
        <span className="text-xs font-semibold text-white">Message Trace</span>
        <button onClick={onClose}><X className="w-3.5 h-3.5 text-gray-500 hover:text-white" /></button>
      </div>
      <div className="flex-1 overflow-y-auto px-3 py-2 space-y-2">
        {steps.map((s, i) => (
          <div key={i} className={clsx('rounded-lg border p-2.5 text-[10px]', s.error ? 'border-red-800/40 bg-red-950/20' : 'border-gray-800/40 bg-gray-900/40')}>
            <div className="flex items-center justify-between mb-1">
              <span className="font-semibold text-white">{s.component}</span>
              {s.duration_ms > 0 && <span className="text-gray-500 font-mono">{s.duration_ms}ms</span>}
            </div>
            <span className={clsx('text-[9px] px-1.5 py-0.5 rounded', s.error ? 'bg-red-500/20 text-red-400' : 'bg-cyan-500/10 text-cyan-400')}>{s.stage}</span>
            {s.error && <pre className="text-red-300 text-[9px] mt-1 whitespace-pre-wrap">{s.error}</pre>}
            {s.input && <pre className="text-gray-400 text-[9px] mt-1 max-h-16 overflow-auto whitespace-pre-wrap border-l-2 border-gray-700 pl-2">{s.input}</pre>}
            {s.output && <pre className="text-emerald-300/70 text-[9px] mt-1 max-h-16 overflow-auto whitespace-pre-wrap border-l-2 border-emerald-700/50 pl-2">{s.output}</pre>}
          </div>
        ))}
      </div>
    </div>
  );
}

function RecentPanel({ messages, onSelect }: { messages: any[]; onSelect: (id: string) => void }) {
  return (
    <div className="flex flex-col h-full">
      <div className="px-4 py-2 border-b border-gray-800/30 shrink-0"><span className="text-[10px] text-gray-500 uppercase tracking-widest">Recent Messages</span></div>
      <div className="flex-1 overflow-y-auto">
        {messages.map((m, i) => (
          <div key={i} onClick={() => onSelect(m.message_id)} className="px-4 py-2 border-b border-gray-800/15 hover:bg-gray-800/30 cursor-pointer text-[11px]">
            <div className="flex justify-between"><span className="text-gray-300">{m.patient_id || m.message_id?.slice(0, 8)}</span><span className="text-gray-600 text-[9px]">{m.status}</span></div>
            <span className="text-gray-500 font-mono text-[9px]">{m.message_type}^{m.trigger_event} • {m.sending_facility}</span>
          </div>
        ))}
        {messages.length === 0 && <div className="px-4 py-8 text-center text-gray-600 text-xs">No messages yet</div>}
      </div>
    </div>
  );
}

function Chip({ label, value, color }: { label: string; value: number; color: string }) {
  return <div className="bg-slate-900/80 backdrop-blur border border-slate-800 rounded px-2 py-1 flex items-center gap-1.5"><span className={clsx('text-[10px] font-bold font-mono tabular-nums', color)}>{value.toLocaleString()}</span><span className="text-[7px] text-gray-500 uppercase">{label}</span></div>;
}

function WsBadge({ ws }: { ws: WebSocket | null }) {
  const [ok, setOk] = useState(false);
  useEffect(() => { if (!ws) return; const iv = setInterval(() => setOk(ws.readyState === WebSocket.OPEN), 2000); return () => clearInterval(iv); }, [ws]);
  return <span className={clsx('text-[8px] px-1.5 py-0.5 rounded flex items-center gap-1', ok ? 'bg-green-500/10 text-green-400' : 'bg-gray-800 text-gray-500')}><span className={clsx('w-1.5 h-1.5 rounded-full', ok ? 'bg-green-400' : 'bg-gray-500')} />{ok ? 'LIVE' : '...'}</span>;
}

function NIcon({ type, className }: { type: string; className?: string }) {
  switch (type) { case 'source': return <Radio className={className}/>; case 'route': return <GitBranch className={className}/>; case 'filter': return <Cpu className={className}/>; case 'destination': return <Send className={className}/>; default: return null; }
}

function getBg(t: string) { return { source: 'bg-sky-500/10', route: 'bg-purple-500/10', filter: 'bg-amber-500/10', destination: 'bg-emerald-500/10' }[t] || 'bg-gray-500/10'; }
function getColor(t: string) { return { source: 'text-sky-400', route: 'text-purple-400', filter: 'text-amber-400', destination: 'text-emerald-400' }[t] || 'text-gray-400'; }

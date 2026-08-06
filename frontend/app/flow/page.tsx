'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import {
  getRoutes, getFilters, getCommPoints, getRouteRecent, getMessageTrace,
  dropMessage, retryMessage, holdMessage, releaseMessage, flushRoute, testFilter,
  connectFlowWebSocket,
  type Route, type Filter, type CommPoint, type StreamEvent, type TraceStep,
} from '@/lib/api';
import { Radio, GitBranch, Cpu, Send, Zap, Activity, Trash2, RotateCcw, Pause, Play, X, Eye } from 'lucide-react';
import { clsx } from 'clsx';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

// --- Types ---
interface FlowNode {
  id: string;
  type: 'source' | 'route' | 'filter' | 'destination';
  label: string;
  sublabel: string;
  active: boolean;
  count: number;
  x: number;
  y: number;
  data?: any;
}

interface LiveMessage {
  message_id: string;
  message_type: string;
  trigger_event: string;
  patient_id: string;
  sending_facility: string;
  status: string;
  stage: string;
  timestamp: string;
  size_bytes?: number;
}

// --- Component ---
export default function FlowPage() {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);
  const [selectedRouteId, setSelectedRouteId] = useState('');
  const [filters, setFilters] = useState<Filter[]>([]);
  const [liveMessages, setLiveMessages] = useState<LiveMessage[]>([]);
  const [metrics, setMetrics] = useState<any>(null);
  const [inspectedMsg, setInspectedMsg] = useState<any>(null);
  const [traceSteps, setTraceSteps] = useState<TraceStep[]>([]);
  const [filterTestResult, setFilterTestResult] = useState<string>('');
  const [testingFilter, setTestingFilter] = useState<Filter | null>(null);
  const [testPayload, setTestPayload] = useState('');
  const [recentMsgs, setRecentMsgs] = useState<any[]>([]);
  const wsRef = useRef<WebSocket | null>(null);
  const msgCountRef = useRef({ received: 0, routed: 0, errors: 0 });

  // Load initial data
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
    getFilters(selectedRouteId)
      .then(r => setFilters((r.filters || []).sort((a, b) => a.execution_order - b.execution_order)))
      .catch(() => setFilters([]));
    getRouteRecent(selectedRouteId, 5).then(r => setRecentMsgs(r.messages || [])).catch(() => {});
  }, [selectedRouteId]);

  // WebSocket connection — replaces HTTP polling entirely
  useEffect(() => {
    const ws = connectFlowWebSocket((event: StreamEvent) => {
      if (event.type === 'message' || event.type === 'error') {
        const msg = event.data as LiveMessage;
        msg.timestamp = event.timestamp;
        setLiveMessages(prev => [msg, ...prev].slice(0, 50));
        if (msg.stage === 'received') msgCountRef.current.received++;
        if (msg.stage === 'routed') msgCountRef.current.routed++;
        if (msg.stage === 'error') msgCountRef.current.errors++;
      }
      if (event.type === 'metric') {
        setMetrics(event.data);
      }
    });
    wsRef.current = ws;
    return () => { ws?.close(); };
  }, []);

  // Message trace
  const openTrace = async (msgId: string) => {
    const result = await getMessageTrace(msgId).catch(() => null);
    if (result) {
      setInspectedMsg(result.message);
      setTraceSteps(result.steps);
    }
  };

  // Message control actions
  const handleDrop = async (msgId: string) => {
    await dropMessage(msgId, 'Manually dropped from flow view');
    setLiveMessages(prev => prev.filter(m => m.message_id !== msgId));
  };

  const handleRetry = async (msgId: string) => {
    await retryMessage(msgId);
  };

  const handleFlush = async () => {
    if (!selectedRouteId) return;
    await flushRoute(selectedRouteId, 'Flushed from flow view');
  };

  // Filter test
  const runFilterTest = async () => {
    if (!testingFilter) return;
    const result = await testFilter(testingFilter.filter_id, testPayload).catch(e => ({ error: e.message }));
    setFilterTestResult(JSON.stringify(result, null, 2));
  };

  // Layout
  const selectedRoute = routes.find(r => r.route_id === selectedRouteId);
  const cpMap = new Map(commPoints.map(cp => [cp.comm_point_id, cp]));
  const srcCP = selectedRoute ? cpMap.get(selectedRoute.source_comm_point_id) : null;
  const dstCP = selectedRoute ? cpMap.get(selectedRoute.dest_comm_point_id) : null;

  const nodes: FlowNode[] = [];
  const colWidth = 240;
  const canvasH = 200;
  const yMid = canvasH / 2;
  let col = 0;
  const totalCols = 3 + filters.length;
  const xOff = colWidth * 0.5;

  if (srcCP) {
    nodes.push({ id: 'src', type: 'source', label: srcCP.name, sublabel: `${srcCP.protocol} :${srcCP.port}`, active: srcCP.is_active, count: msgCountRef.current.received, x: xOff + col * colWidth, y: yMid, data: srcCP });
    col++;
  }
  if (selectedRoute) {
    nodes.push({ id: 'route', type: 'route', label: selectedRoute.name, sublabel: selectedRoute.source_topic, active: selectedRoute.is_active, count: msgCountRef.current.received, x: xOff + col * colWidth, y: yMid, data: selectedRoute });
    col++;
  }
  filters.forEach((f, i) => {
    nodes.push({ id: `f-${i}`, type: 'filter', label: f.name, sublabel: f.filter_type, active: f.is_active, count: msgCountRef.current.routed, x: xOff + col * colWidth, y: yMid, data: f });
    col++;
  });
  if (dstCP) {
    nodes.push({ id: 'dst', type: 'destination', label: dstCP.name, sublabel: `${dstCP.protocol} :${dstCP.port}`, active: dstCP.is_active, count: msgCountRef.current.routed, x: xOff + col * colWidth, y: yMid, data: dstCP });
  }

  const canvasW = Math.max(900, (totalCols + 1) * colWidth);

  return (
    <div className="flex h-screen bg-gray-950">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-800/50 shrink-0">
          <div className="flex items-center gap-4">
            <h1 className="text-lg font-bold text-white">Message Flow</h1>
            <select value={selectedRouteId} onChange={(e) => setSelectedRouteId(e.target.value)}
              className="bg-gray-800 border border-gray-700 text-gray-200 text-xs rounded-lg px-2 py-1 focus:border-cyan-500 outline-none">
              {routes.map(r => <option key={r.route_id} value={r.route_id}>{r.name}</option>)}
            </select>
          </div>
          <div className="flex items-center gap-3">
            <button onClick={handleFlush} className="text-xs bg-red-900/30 border border-red-800/50 text-red-400 px-2 py-1 rounded hover:bg-red-900/50 transition-colors">Flush Queue</button>
            {metrics && <div className="flex items-center gap-1.5 bg-gray-800/80 rounded px-2 py-1 border border-gray-700/50">
              <Activity className="w-3 h-3 text-cyan-400 animate-pulse" />
              <span className="text-[10px] text-cyan-300 font-mono tabular-nums">{(metrics.msgs_per_second || 0).toFixed(1)} msg/s</span>
            </div>}
            <WsStatus ws={wsRef.current} />
          </div>
        </div>

        {/* Flow diagram — compact top section */}
        <div className={clsx('shrink-0 relative h-[180px] overflow-x-auto border-b border-gray-800/30', 'bg-[radial-gradient(#1e293b_1px,transparent_1px)] [background-size:20px_20px]')}>
          <div className="absolute inset-0 bg-gradient-to-r from-cyan-950/10 via-transparent to-purple-950/10 pointer-events-none" />
          <div className="relative flex items-center justify-center h-full" style={{ minWidth: canvasW }}>
            {/* SVG edges */}
            <svg className="absolute inset-0 w-full h-full pointer-events-none">
              <defs><filter id="gl"><feGaussianBlur stdDeviation="2" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter></defs>
              {nodes.map((n, i) => {
                if (i === 0) return null;
                const prev = nodes[i - 1];
                const x1 = prev.x + 70, y1 = prev.y;
                const x2 = n.x - 70, y2 = n.y;
                const cx1 = x1 + 40, cx2 = x2 - 40;
                const d = `M${x1},${y1} C${cx1},${y1} ${cx2},${y2} ${x2},${y2}`;
                return <g key={`e${i}`}>
                  <path d={d} fill="none" stroke="#1e3a5f" strokeWidth="2.5" strokeLinecap="round"/>
                  <path d={d} fill="none" stroke="#38bdf8" strokeWidth="1.5" strokeDasharray="8 5" opacity="0.7" filter="url(#gl)">
                    <animate attributeName="stroke-dashoffset" values="26;0" dur="1s" repeatCount="indefinite"/>
                  </path>
                  <circle r="3" fill="#38bdf8" filter="url(#gl)"><animateMotion dur={`${1.8+i*0.2}s`} repeatCount="indefinite" path={d}/></circle>
                </g>;
              })}
            </svg>
            {/* Nodes */}
            {nodes.map(n => <FlowNodeCard key={n.id} node={n} onTestFilter={n.type === 'filter' ? () => { setTestingFilter(n.data); setTestPayload(recentMsgs[0]?.raw_payload || '{}'); } : undefined} />)}
          </div>
        </div>

        {/* Bottom section: live stream + trace/test */}
        <div className="flex-1 flex overflow-hidden">
          {/* Live message stream */}
          <div className="flex-1 flex flex-col overflow-hidden border-r border-gray-800/30">
            <div className="px-4 py-2 border-b border-gray-800/30 flex items-center justify-between">
              <span className="text-[10px] text-gray-500 uppercase tracking-widest">Live Stream</span>
              <div className="flex gap-2 text-[9px] font-mono">
                <span className="text-cyan-400">↓{msgCountRef.current.received}</span>
                <span className="text-emerald-400">↑{msgCountRef.current.routed}</span>
                {msgCountRef.current.errors > 0 && <span className="text-red-400">⚠{msgCountRef.current.errors}</span>}
              </div>
            </div>
            <div className="flex-1 overflow-y-auto">
              {liveMessages.length === 0 ? (
                <div className="flex items-center justify-center h-full text-gray-600 text-xs">Waiting for messages...</div>
              ) : (
                liveMessages.map((msg, i) => (
                  <div key={`${msg.message_id}-${i}`} className="px-4 py-2 border-b border-gray-800/20 hover:bg-gray-800/30 cursor-pointer flex items-center gap-3 text-xs" onClick={() => openTrace(msg.message_id)}>
                    <StageDot stage={msg.stage} />
                    <span className="text-gray-500 font-mono w-16 shrink-0">{msg.message_type}^{msg.trigger_event}</span>
                    <span className="text-gray-300 truncate flex-1">{msg.patient_id || msg.sending_facility || msg.message_id.slice(0, 8)}</span>
                    <span className="text-gray-600 font-mono text-[9px]">{new Date(msg.timestamp).toLocaleTimeString()}</span>
                    {/* Control buttons */}
                    <div className="flex gap-1 opacity-0 group-hover:opacity-100">
                      <button onClick={(e) => { e.stopPropagation(); handleDrop(msg.message_id); }} title="Drop"><Trash2 className="w-3 h-3 text-red-400 hover:text-red-300" /></button>
                      <button onClick={(e) => { e.stopPropagation(); handleRetry(msg.message_id); }} title="Retry"><RotateCcw className="w-3 h-3 text-amber-400 hover:text-amber-300" /></button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>

          {/* Right panel: trace or filter test */}
          <div className="w-[380px] flex flex-col overflow-hidden bg-gray-900/50">
            {testingFilter ? (
              // Filter test mode
              <div className="flex flex-col h-full">
                <div className="px-4 py-2 border-b border-gray-800/30 flex items-center justify-between">
                  <span className="text-xs font-semibold text-white">Test: {testingFilter.name}</span>
                  <button onClick={() => setTestingFilter(null)} className="text-gray-500 hover:text-white"><X className="w-3.5 h-3.5"/></button>
                </div>
                <div className="flex-1 min-h-0 flex flex-col">
                  <div className="h-1/2 border-b border-gray-800/30">
                    <MonacoEditor height="100%" language="json" theme="vs-dark" value={testPayload} onChange={(v) => setTestPayload(v || '')}
                      options={{ minimap: { enabled: false }, fontSize: 10, lineNumbers: 'off', scrollBeyondLastLine: false, padding: { top: 4 } }} />
                  </div>
                  <div className="px-3 py-1.5 border-b border-gray-800/30">
                    <button onClick={runFilterTest} className="text-[10px] bg-emerald-600 text-white px-3 py-1 rounded hover:bg-emerald-500 transition-colors">Run Filter</button>
                  </div>
                  <div className="flex-1 overflow-auto">
                    <MonacoEditor height="100%" language="json" theme="vs-dark" value={filterTestResult}
                      options={{ readOnly: true, minimap: { enabled: false }, fontSize: 10, lineNumbers: 'off', scrollBeyondLastLine: false, padding: { top: 4 } }} />
                  </div>
                </div>
              </div>
            ) : traceSteps.length > 0 ? (
              // Trace view
              <div className="flex flex-col h-full">
                <div className="px-4 py-2 border-b border-gray-800/30 flex items-center justify-between">
                  <span className="text-xs font-semibold text-white">Message Trace</span>
                  <button onClick={() => { setTraceSteps([]); setInspectedMsg(null); }} className="text-gray-500 hover:text-white"><X className="w-3.5 h-3.5"/></button>
                </div>
                <div className="flex-1 overflow-y-auto px-4 py-3 space-y-2">
                  {traceSteps.map((step, i) => (
                    <div key={i} className={clsx('rounded-lg border p-3 text-xs', step.error ? 'border-red-800/50 bg-red-950/20' : 'border-gray-800/50 bg-gray-900/50')}>
                      <div className="flex items-center justify-between mb-1">
                        <div className="flex items-center gap-2">
                          <StageDot stage={step.stage} />
                          <span className="font-semibold text-white">{step.component}</span>
                        </div>
                        {step.duration_ms > 0 && <span className="text-gray-500 font-mono">{step.duration_ms}ms</span>}
                      </div>
                      {step.error && <pre className="text-red-300 text-[10px] mt-1 whitespace-pre-wrap">{step.error}</pre>}
                      {step.input && <div className="mt-1"><span className="text-gray-500">IN:</span><pre className="text-gray-400 text-[9px] mt-0.5 max-h-20 overflow-auto whitespace-pre-wrap">{step.input}</pre></div>}
                      {step.output && <div className="mt-1"><span className="text-gray-500">OUT:</span><pre className="text-emerald-300/70 text-[9px] mt-0.5 max-h-20 overflow-auto whitespace-pre-wrap">{step.output}</pre></div>}
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              // Recent messages for this route
              <div className="flex flex-col h-full">
                <div className="px-4 py-2 border-b border-gray-800/30">
                  <span className="text-[10px] text-gray-500 uppercase tracking-widest">Recent Messages</span>
                </div>
                <div className="flex-1 overflow-y-auto">
                  {recentMsgs.map((msg, i) => (
                    <div key={i} className="px-4 py-2 border-b border-gray-800/20 text-xs hover:bg-gray-800/30 cursor-pointer" onClick={() => openTrace(msg.message_id)}>
                      <div className="flex justify-between">
                        <span className="text-gray-300">{msg.patient_id || msg.message_id?.slice(0, 8)}</span>
                        <span className="text-gray-600">{msg.status}</span>
                      </div>
                      <div className="text-gray-500 font-mono text-[9px]">{msg.message_type}^{msg.trigger_event} • {msg.sending_facility}</div>
                    </div>
                  ))}
                  {recentMsgs.length === 0 && <div className="px-4 py-8 text-center text-gray-600 text-xs">No messages yet</div>}
                </div>
              </div>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}

// --- Sub-components ---

function FlowNodeCard({ node, onTestFilter }: { node: FlowNode; onTestFilter?: () => void }) {
  const theme = getTheme(node.type);
  return (
    <div className={clsx('absolute w-[140px] rounded-lg border p-2.5 backdrop-blur-xl bg-slate-900/80 shadow-xl transition-all hover:scale-105', `border-slate-800 hover:${theme.border}`)}
      style={{ left: node.x - 70, top: node.y - 40 }}>
      <div className="flex items-center gap-2 mb-1.5">
        <div className={clsx('w-6 h-6 rounded flex items-center justify-center', theme.iconBg)}>
          <NodeIcon type={node.type} className={clsx('w-3.5 h-3.5', theme.color)} />
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-[9px] font-semibold text-white truncate">{node.label}</div>
          <div className="text-[8px] text-gray-500 font-mono truncate">{node.sublabel}</div>
        </div>
        <span className={clsx('w-1.5 h-1.5 rounded-full', node.active ? 'bg-green-400' : 'bg-red-400')} />
      </div>
      <div className="flex items-center justify-between">
        <span className={clsx('text-xs font-bold font-mono tabular-nums', theme.color)}>{node.count.toLocaleString()}</span>
        {onTestFilter && <button onClick={onTestFilter} className="text-[8px] text-amber-400 hover:text-amber-300 bg-amber-500/10 px-1.5 py-0.5 rounded">Test</button>}
      </div>
    </div>
  );
}

function StageDot({ stage }: { stage: string }) {
  const colors: Record<string, string> = { received: 'bg-cyan-400', routed: 'bg-emerald-400', delivered: 'bg-green-400', error: 'bg-red-400', dropped: 'bg-gray-400' };
  return <span className={clsx('w-2 h-2 rounded-full shrink-0', colors[stage] || 'bg-gray-500')} />;
}

function WsStatus({ ws }: { ws: WebSocket | null }) {
  const [connected, setConnected] = useState(false);
  useEffect(() => {
    if (!ws) return;
    const check = () => setConnected(ws.readyState === WebSocket.OPEN);
    const iv = setInterval(check, 1000);
    check();
    return () => clearInterval(iv);
  }, [ws]);
  return (
    <div className={clsx('flex items-center gap-1 text-[9px] px-2 py-0.5 rounded', connected ? 'text-green-400 bg-green-500/10' : 'text-gray-500 bg-gray-800')}>
      <span className={clsx('w-1.5 h-1.5 rounded-full', connected ? 'bg-green-400' : 'bg-gray-500')} />
      {connected ? 'LIVE' : 'CONNECTING'}
    </div>
  );
}

function NodeIcon({ type, className }: { type: string; className?: string }) {
  switch (type) {
    case 'source': return <Radio className={className} />;
    case 'route': return <GitBranch className={className} />;
    case 'filter': return <Cpu className={className} />;
    case 'destination': return <Send className={className} />;
    default: return <Zap className={className} />;
  }
}

function getTheme(type: string) {
  switch (type) {
    case 'source': return { iconBg: 'bg-sky-500/10', color: 'text-sky-400', border: 'border-sky-500/50' };
    case 'route': return { iconBg: 'bg-purple-500/10', color: 'text-purple-400', border: 'border-purple-500/50' };
    case 'filter': return { iconBg: 'bg-amber-500/10', color: 'text-amber-400', border: 'border-amber-500/50' };
    case 'destination': return { iconBg: 'bg-emerald-500/10', color: 'text-emerald-400', border: 'border-emerald-500/50' };
    default: return { iconBg: 'bg-gray-500/10', color: 'text-gray-400', border: 'border-gray-500/50' };
  }
}

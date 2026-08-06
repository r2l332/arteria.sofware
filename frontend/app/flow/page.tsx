'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import { getRoutes, getFilters, getCommPoints, getMetrics, getCPMetrics, type Route, type Filter, type CommPoint, type LiveMetrics, type CPMetricSnapshot } from '@/lib/api';
import { Radio, GitBranch, Cpu, Send, Zap, Activity, Server, Globe, Filter as FilterIcon, X } from 'lucide-react';
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
  errors: number;
  x: number;
  y: number;
  data?: any;
}

interface FlowEdge {
  from: string;
  to: string;
}

interface FlowMetrics {
  received: number;
  transformed: number;
  delivered: number;
  errors: number;
  msgsPerSec: number;
}

// --- Component ---

export default function FlowPage() {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);
  const [selectedRouteId, setSelectedRouteId] = useState('');
  const [filters, setFilters] = useState<Filter[]>([]);
  const [metrics, setMetrics] = useState<LiveMetrics | null>(null);
  const [cpMetrics, setCpMetrics] = useState<Record<string, CPMetricSnapshot>>({});
  const [inspectedNode, setInspectedNode] = useState<FlowNode | null>(null);
  const [tick, setTick] = useState(0);

  const loadData = useCallback(async () => {
    const [r, c] = await Promise.all([
      getRoutes().catch(() => ({ routes: [], count: 0 })),
      getCommPoints().catch(() => ({ communication_points: [], count: 0 })),
    ]);
    setRoutes(r.routes || []);
    setCommPoints(c.communication_points || []);
    if (!selectedRouteId && r.routes?.length > 0) setSelectedRouteId(r.routes[0].route_id);
  }, [selectedRouteId]);

  useEffect(() => { loadData(); }, [loadData]);

  useEffect(() => {
    if (!selectedRouteId) return;
    getFilters(selectedRouteId)
      .then(r => setFilters((r.filters || []).sort((a, b) => a.execution_order - b.execution_order)))
      .catch(() => setFilters([]));
  }, [selectedRouteId]);

  // 2s real-time polling
  useEffect(() => {
    const poll = () => {
      getMetrics().then(setMetrics).catch(() => {});
      getCPMetrics().then(r => setCpMetrics(r.comm_points || {})).catch(() => {});
      setTick(t => t + 1);
    };
    poll();
    const iv = setInterval(poll, 2000);
    return () => clearInterval(iv);
  }, []);

  const selectedRoute = routes.find(r => r.route_id === selectedRouteId);
  const cpMap = new Map(commPoints.map(cp => [cp.comm_point_id, cp]));
  const srcCP = selectedRoute ? cpMap.get(selectedRoute.source_comm_point_id) : null;
  const dstCP = selectedRoute ? cpMap.get(selectedRoute.dest_comm_point_id) : null;

  const procReceived = metrics?.processing?.received || 0;
  const procRouted = metrics?.processing?.routed || 0;
  const procErrors = metrics?.processing?.errors || 0;
  const msgsPerSec = metrics?.ingestion?.msgs_per_second || 0;

  // --- Build topology layout (left-to-right organic flow) ---
  const nodes: FlowNode[] = [];
  const edges: FlowEdge[] = [];

  const totalCols = 2 + filters.length;
  const colWidth = 280;
  const canvasW = (totalCols + 1) * colWidth;
  const canvasH = 360;
  const yMid = canvasH / 2;
  const xOffset = colWidth / 2;
  let col = 0;

  // Source node
  if (srcCP) {
    const m = cpMetrics[srcCP.comm_point_id];
    const x = xOffset + col * colWidth;
    nodes.push({ id: 'src', type: 'source', label: srcCP.name, sublabel: `${srcCP.protocol} :${srcCP.port}`, active: srcCP.is_active, count: m?.received || procReceived, errors: m?.errors || 0, x, y: yMid - 50, data: srcCP });
    col++;
  }

  // Route node (below and left of filters)
  if (selectedRoute) {
    const x = xOffset + 0.5 * colWidth;
    nodes.push({ id: 'route', type: 'route', label: selectedRoute.name, sublabel: selectedRoute.source_topic, active: selectedRoute.is_active, count: procReceived, errors: procErrors, x, y: yMid + 90, data: selectedRoute });
    if (srcCP) edges.push({ from: 'src', to: 'route' });
  }

  // Filter nodes
  filters.forEach((f, i) => {
    const x = xOffset + col * colWidth;
    const yOffset = (i % 2 === 0 ? -20 : 20);
    const id = `filter-${i}`;
    nodes.push({ id, type: 'filter', label: f.name, sublabel: f.filter_type, active: f.is_active, count: procRouted, errors: 0, x, y: yMid + yOffset, data: f });
    if (i === 0) {
      edges.push({ from: 'route', to: id });
    } else {
      edges.push({ from: `filter-${i - 1}`, to: id });
    }
    col++;
  });

  // Destination node
  if (dstCP) {
    const m = cpMetrics[dstCP.comm_point_id];
    const x = xOffset + col * colWidth;
    const id = 'dest';
    nodes.push({ id, type: 'destination', label: dstCP.name, sublabel: `${dstCP.protocol} :${dstCP.port}`, active: dstCP.is_active, count: m?.sent || procRouted, errors: m?.errors || 0, x, y: yMid, data: dstCP });
    const lastFilter = filters.length > 0 ? `filter-${filters.length - 1}` : 'route';
    edges.push({ from: lastFilter, to: id });
  }

  const flowMetrics: FlowMetrics = {
    received: nodes.find(n => n.type === 'source')?.count || 0,
    transformed: procRouted,
    delivered: nodes.find(n => n.type === 'destination')?.count || 0,
    errors: procErrors,
    msgsPerSec,
  };

  const nodeMap = new Map(nodes.map(n => [n.id, n]));

  return (
    <div className="flex h-screen bg-gray-950">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-800/50">
          <div>
            <h1 className="text-xl font-bold text-white tracking-tight">Message Flow</h1>
            <p className="text-xs text-gray-500">Real-time traffic topology</p>
          </div>
          <div className="flex items-center gap-3">
            <select
              value={selectedRouteId}
              onChange={(e) => { setSelectedRouteId(e.target.value); setInspectedNode(null); }}
              className="bg-gray-800 border border-gray-700 text-gray-200 text-sm rounded-lg px-3 py-1.5 focus:border-cyan-500 outline-none"
            >
              {routes.map(r => <option key={r.route_id} value={r.route_id}>{r.name}</option>)}
            </select>
            <LiveBadge value={flowMetrics.msgsPerSec} />
          </div>
        </div>

        {!selectedRoute ? (
          <div className="flex-1 flex items-center justify-center text-gray-600">Select a route</div>
        ) : (
          <div className="flex-1 flex flex-col overflow-auto">
            {/* Flow canvas — top half */}
            <div className={clsx('relative flex items-center justify-center h-[45vh] min-h-[280px] shrink-0 border-b border-gray-800/30 overflow-x-auto', 'bg-[radial-gradient(#1e293b_1px,transparent_1px)] [background-size:24px_24px]')}>
              {/* Ambient glow */}
              <div className="absolute inset-0 bg-gradient-to-tr from-cyan-950/20 via-transparent to-purple-950/20 pointer-events-none" />

              {/* Stats bar */}
              <div className="absolute top-4 left-4 flex gap-2 z-10">
                <MetricChip label="IN" value={flowMetrics.received} color="text-sky-400" />
                <MetricChip label="TRANSFORMED" value={flowMetrics.transformed} color="text-amber-400" />
                <MetricChip label="OUT" value={flowMetrics.delivered} color="text-emerald-400" />
                {flowMetrics.errors > 0 && <MetricChip label="ERR" value={flowMetrics.errors} color="text-red-400" />}
              </div>

              {/* Flow container — centred */}
              <div className="relative" style={{ width: canvasW, height: canvasH }}>
                {/* SVG edges */}
                <svg className="absolute inset-0 w-full h-full pointer-events-none">
                  <defs>
                    <filter id="glow"><feGaussianBlur stdDeviation="3" result="blur" /><feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge></filter>
                  </defs>
                {edges.map((edge, i) => {
                  const from = nodeMap.get(edge.from);
                  const to = nodeMap.get(edge.to);
                  if (!from || !to) return null;
                  const x1 = from.x + 80, y1 = from.y + 32;
                  const x2 = to.x - 10, y2 = to.y + 32;
                  const cpx1 = x1 + (x2 - x1) * 0.4, cpx2 = x1 + (x2 - x1) * 0.6;
                  const d = `M ${x1} ${y1} C ${cpx1} ${y1}, ${cpx2} ${y2}, ${x2} ${y2}`;
                  return (
                    <g key={`e-${i}`}>
                      <path d={d} fill="none" stroke="#1e3a5f" strokeWidth="3" strokeLinecap="round" opacity="0.5" />
                      <path d={d} fill="none" stroke="#38bdf8" strokeWidth="2" strokeDasharray="10 6" strokeLinecap="round" opacity="0.7" filter="url(#glow)">
                        <animate attributeName="stroke-dashoffset" values="32;0" dur={`${1.2 + i * 0.2}s`} repeatCount="indefinite" />
                      </path>
                      {/* Particle */}
                      <circle r="3.5" fill="#38bdf8" filter="url(#glow)">
                        <animateMotion dur={`${2 + i * 0.3}s`} repeatCount="indefinite" path={d} />
                      </circle>
                      <circle r="1.5" fill="white" opacity="0.9">
                        <animateMotion dur={`${2 + i * 0.3}s`} repeatCount="indefinite" path={d} />
                      </circle>
                    </g>
                  );
                })}
                </svg>

                {/* Nodes */}
                {nodes.map(node => (
                  <NodeCard
                    key={node.id}
                    node={node}
                    isSelected={inspectedNode?.id === node.id}
                    onClick={() => setInspectedNode(node)}
                  />
                ))}
              </div>
            </div>

            {/* Step details — bottom half */}
            <div className="flex-1 min-h-0 overflow-auto px-6 py-4">
              {inspectedNode ? (
                <InspectorPanel node={inspectedNode} onClose={() => setInspectedNode(null)} />
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
                  {nodes.map(node => (
                    <StepSummaryCard key={node.id} node={node} onClick={() => setInspectedNode(node)} />
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

// --- Sub-components ---

function NodeCard({ node, isSelected, onClick }: { node: FlowNode; isSelected: boolean; onClick: () => void }) {
  const theme = getNodeTheme(node.type);
  return (
    <div
      onClick={onClick}
      className={clsx(
        'absolute w-[160px] rounded-xl border p-3 cursor-pointer transition-all duration-200',
        'backdrop-blur-xl bg-slate-900/80 shadow-2xl',
        isSelected ? 'border-cyan-500/70 shadow-cyan-500/20' : `border-slate-800 hover:${theme.hoverBorder}`,
        !node.active && 'opacity-40 grayscale'
      )}
      style={{ left: node.x - 80, top: node.y }}
    >
      {/* Header */}
      <div className="flex items-center gap-2 mb-2">
        <div className={clsx('w-7 h-7 rounded-lg flex items-center justify-center', theme.iconBg)}>
          <NodeIcon type={node.type} className={clsx('w-4 h-4', theme.iconColor)} />
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-[10px] font-semibold text-white truncate">{node.label}</div>
          <div className="text-[9px] text-gray-500 font-mono truncate">{node.sublabel}</div>
        </div>
        {/* Status ping */}
        <span className="relative flex h-2 w-2">
          {node.active && <span className={clsx('animate-ping absolute h-full w-full rounded-full opacity-75', theme.pingColor)} />}
          <span className={clsx('relative rounded-full h-2 w-2', node.active ? theme.dotColor : 'bg-red-500')} />
        </span>
      </div>
      {/* Metrics */}
      <div className={clsx('text-center py-1 rounded-lg', theme.metricBg)}>
        <div className={clsx('text-sm font-bold font-mono tabular-nums', theme.iconColor)}>{node.count.toLocaleString()}</div>
        <div className="text-[8px] text-gray-500 uppercase tracking-wider">messages</div>
      </div>
      {node.errors > 0 && (
        <div className="mt-1 text-center text-[9px] text-red-400 font-mono">⚠ {node.errors} errors</div>
      )}
    </div>
  );
}

function InspectorPanel({ node, onClose }: { node: FlowNode; onClose: () => void }) {
  const theme = getNodeTheme(node.type);
  return (
    <div className="bg-slate-900/80 backdrop-blur border border-slate-800 rounded-xl overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-5 py-3 border-b border-gray-800/30">
        <div className="flex items-center gap-3">
          <div className={clsx('w-8 h-8 rounded-lg flex items-center justify-center', theme.iconBg)}>
            <NodeIcon type={node.type} className={clsx('w-4 h-4', theme.iconColor)} />
          </div>
          <div>
            <div className="text-sm font-semibold text-white">{node.label}</div>
            <div className="text-[10px] text-gray-500">{getTypeName(node.type)}</div>
          </div>
        </div>
        <button onClick={onClose} className="text-gray-500 hover:text-white"><X className="w-4 h-4" /></button>
      </div>

      <div className="flex flex-col lg:flex-row">
        {/* Stats */}
        <div className="px-5 py-3 space-y-2 lg:w-64 lg:border-r lg:border-gray-800/30">
          <InspectorRow label="Status" value={node.active ? 'HEALTHY' : 'INACTIVE'} badge={node.active ? 'bg-green-500/20 text-green-400' : 'bg-red-500/20 text-red-400'} />
          <InspectorRow label="Messages" value={node.count.toLocaleString()} mono />
          <InspectorRow label="Protocol" value={node.sublabel} mono />
          {node.errors > 0 && <InspectorRow label="Errors" value={node.errors.toLocaleString()} badge="bg-red-500/20 text-red-400" />}
        </div>

        {/* Script preview */}
        {node.type === 'filter' && node.data?.js_script && (
          <div className="flex-1 min-h-0 border-t lg:border-t-0 border-gray-800/30">
            <div className="px-4 pt-2 pb-1 text-[9px] text-gray-500 uppercase tracking-widest">Transform Script</div>
            <div className="h-48">
              <MonacoEditor
                height="100%"
                language={node.data.filter_type === 'python' ? 'python' : 'javascript'}
                theme="vs-dark"
                value={node.data.js_script}
                options={{ readOnly: true, minimap: { enabled: false }, fontSize: 11, lineNumbers: 'off', scrollBeyondLastLine: false, padding: { top: 8 } }}
              />
            </div>
          </div>
        )}
        {node.type === 'route' && node.data?.description && (
          <div className="flex-1 px-5 py-3">
            <div className="text-[9px] text-gray-500 uppercase tracking-widest mb-1">Description</div>
            <p className="text-xs text-gray-300">{node.data.description}</p>
          </div>
        )}
      </div>
    </div>
  );
}

function StepSummaryCard({ node, onClick }: { node: FlowNode; onClick: () => void }) {
  const theme = getNodeTheme(node.type);
  return (
    <div
      onClick={onClick}
      className="bg-slate-900/60 backdrop-blur border border-slate-800 rounded-lg p-3 cursor-pointer hover:border-cyan-500/30 transition-colors"
    >
      <div className="flex items-center gap-2 mb-2">
        <div className={clsx('w-6 h-6 rounded flex items-center justify-center', theme.iconBg)}>
          <NodeIcon type={node.type} className={clsx('w-3.5 h-3.5', theme.iconColor)} />
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-[10px] font-semibold text-white truncate">{node.label}</div>
          <div className="text-[9px] text-gray-500">{getTypeName(node.type)}</div>
        </div>
        <span className={clsx('text-[9px] px-1.5 py-0.5 rounded-full font-medium', node.active ? 'bg-green-500/20 text-green-400' : 'bg-red-500/20 text-red-400')}>
          {node.active ? 'OK' : 'OFF'}
        </span>
      </div>
      <div className="flex items-center justify-between">
        <span className="text-[9px] text-gray-500 font-mono">{node.sublabel}</span>
        <span className={clsx('text-xs font-bold font-mono tabular-nums', theme.iconColor)}>{node.count.toLocaleString()}</span>
      </div>
    </div>
  );
}

function InspectorRow({ label, value, mono, badge }: { label: string; value: string; mono?: boolean; badge?: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-[10px] text-gray-500">{label}</span>
      {badge ? (
        <span className={clsx('text-[10px] font-semibold px-2 py-0.5 rounded-full', badge)}>{value}</span>
      ) : (
        <span className={clsx('text-[11px] text-gray-200', mono && 'font-mono')}>{value}</span>
      )}
    </div>
  );
}

function LiveBadge({ value }: { value: number }) {
  return (
    <div className="flex items-center gap-2 bg-gray-800/80 backdrop-blur rounded-lg px-3 py-1.5 border border-gray-700/50">
      <span className="relative flex h-2 w-2">
        <span className="animate-ping absolute h-full w-full rounded-full bg-cyan-400 opacity-75" />
        <span className="relative rounded-full h-2 w-2 bg-cyan-500" />
      </span>
      <span className="text-xs text-cyan-300 font-mono tabular-nums">{value.toFixed(1)} msg/s</span>
    </div>
  );
}

function MetricChip({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="bg-slate-900/80 backdrop-blur border border-slate-800 rounded-lg px-2.5 py-1.5 flex items-center gap-1.5">
      <span className={clsx('text-xs font-bold font-mono tabular-nums', color)}>{value.toLocaleString()}</span>
      <span className="text-[8px] text-gray-500 uppercase">{label}</span>
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

function getNodeTheme(type: string) {
  switch (type) {
    case 'source': return { iconBg: 'bg-sky-500/10', iconColor: 'text-sky-400', hoverBorder: 'border-sky-500/50', pingColor: 'bg-sky-400', dotColor: 'bg-sky-500', metricBg: 'bg-sky-500/5' };
    case 'route': return { iconBg: 'bg-purple-500/10', iconColor: 'text-purple-400', hoverBorder: 'border-purple-500/50', pingColor: 'bg-purple-400', dotColor: 'bg-purple-500', metricBg: 'bg-purple-500/5' };
    case 'filter': return { iconBg: 'bg-amber-500/10', iconColor: 'text-amber-400', hoverBorder: 'border-amber-500/50', pingColor: 'bg-amber-400', dotColor: 'bg-amber-500', metricBg: 'bg-amber-500/5' };
    case 'destination': return { iconBg: 'bg-emerald-500/10', iconColor: 'text-emerald-400', hoverBorder: 'border-emerald-500/50', pingColor: 'bg-emerald-400', dotColor: 'bg-emerald-500', metricBg: 'bg-emerald-500/5' };
    default: return { iconBg: 'bg-gray-500/10', iconColor: 'text-gray-400', hoverBorder: 'border-gray-500/50', pingColor: 'bg-gray-400', dotColor: 'bg-gray-500', metricBg: 'bg-gray-500/5' };
  }
}

function getTypeName(type: string): string {
  switch (type) {
    case 'source': return 'Input Comm Point';
    case 'destination': return 'Output Comm Point';
    case 'route': return 'Route';
    case 'filter': return 'Transform Filter';
    default: return type;
  }
}

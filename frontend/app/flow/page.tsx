'use client';

import { useEffect, useState, useCallback, useRef } from 'react';
import Sidebar from '@/components/Sidebar';
import { getRoutes, getFilters, getCommPoints, getMetrics, getCPMetrics, type Route, type Filter, type CommPoint, type LiveMetrics, type CPMetricSnapshot } from '@/lib/api';
import { Radio, GitBranch, Cog, Send, Zap, Activity } from 'lucide-react';

interface StageNode {
  id: string;
  type: 'input' | 'route' | 'filter' | 'output';
  label: string;
  sublabel: string;
  active: boolean;
  count: number;
  errors: number;
  data?: any;
}

export default function FlowPage() {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [commPoints, setCommPoints] = useState<CommPoint[]>([]);
  const [selectedRouteId, setSelectedRouteId] = useState<string>('');
  const [filters, setFilters] = useState<Filter[]>([]);
  const [metrics, setMetrics] = useState<LiveMetrics | null>(null);
  const [cpMetrics, setCpMetrics] = useState<Record<string, CPMetricSnapshot>>({});
  const [selectedStage, setSelectedStage] = useState<StageNode | null>(null);
  const [tick, setTick] = useState(0);
  const prevCountRef = useRef<Record<string, number>>({});

  const loadData = useCallback(async () => {
    const [routeResp, cpResp] = await Promise.all([
      getRoutes().catch(() => ({ routes: [], count: 0 })),
      getCommPoints().catch(() => ({ communication_points: [], count: 0 })),
    ]);
    setRoutes(routeResp.routes || []);
    setCommPoints(cpResp.communication_points || []);
    if (!selectedRouteId && routeResp.routes?.length > 0) {
      setSelectedRouteId(routeResp.routes[0].route_id);
    }
  }, [selectedRouteId]);

  useEffect(() => { loadData(); }, [loadData]);

  useEffect(() => {
    if (!selectedRouteId) return;
    getFilters(selectedRouteId)
      .then(r => setFilters((r.filters || []).sort((a, b) => a.execution_order - b.execution_order)))
      .catch(() => setFilters([]));
  }, [selectedRouteId]);

  // Real-time polling every 2s
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

  // Build stages
  const stages: StageNode[] = [];
  if (srcCP) {
    const m = cpMetrics[srcCP.comm_point_id];
    stages.push({ id: 'input', type: 'input', label: srcCP.name, sublabel: `${srcCP.protocol} :${srcCP.port}`, active: srcCP.is_active, count: m?.received || procReceived, errors: m?.errors || 0, data: srcCP });
  }
  if (selectedRoute) {
    stages.push({ id: 'route', type: 'route', label: selectedRoute.name, sublabel: `${selectedRoute.source_topic}`, active: selectedRoute.is_active, count: procReceived, errors: procErrors, data: selectedRoute });
  }
  filters.forEach((f, i) => {
    stages.push({ id: `filter-${i}`, type: 'filter', label: f.name, sublabel: f.filter_type, active: f.is_active, count: procRouted, errors: 0, data: f });
  });
  if (dstCP) {
    const m = cpMetrics[dstCP.comm_point_id];
    stages.push({ id: 'output', type: 'output', label: dstCP.name, sublabel: `${dstCP.protocol} :${dstCP.port}`, active: dstCP.is_active, count: m?.sent || procRouted, errors: m?.errors || 0, data: dstCP });
  }

  // Detect count changes for flash animation
  const flashNodes = new Set<string>();
  stages.forEach(s => {
    const prev = prevCountRef.current[s.id] || 0;
    if (s.count > prev) flashNodes.add(s.id);
  });
  useEffect(() => {
    const map: Record<string, number> = {};
    stages.forEach(s => { map[s.id] = s.count; });
    prevCountRef.current = map;
  });

  // Radial layout: route in centre, others arranged around it
  const cx = 400, cy = 300;
  const radius = 200;
  const routeIdx = stages.findIndex(s => s.type === 'route');
  const otherStages = stages.filter(s => s.type !== 'route');
  const angleStep = (2 * Math.PI) / Math.max(otherStages.length, 1);
  const startAngle = -Math.PI / 2;

  const nodePositions: Record<string, { x: number; y: number }> = {};
  if (routeIdx >= 0) {
    nodePositions[stages[routeIdx].id] = { x: cx, y: cy };
  }
  otherStages.forEach((s, i) => {
    const angle = startAngle + i * angleStep;
    nodePositions[s.id] = { x: cx + radius * Math.cos(angle), y: cy + radius * Math.sin(angle) };
  });

  // Edges: input→route, route→filters, filters→output (chain)
  const edges: { from: string; to: string }[] = [];
  const inputStage = stages.find(s => s.type === 'input');
  const routeStage = stages.find(s => s.type === 'route');
  const outputStage = stages.find(s => s.type === 'output');
  const filterStages = stages.filter(s => s.type === 'filter');

  if (inputStage && routeStage) edges.push({ from: inputStage.id, to: routeStage.id });
  if (routeStage && filterStages.length > 0) {
    edges.push({ from: routeStage.id, to: filterStages[0].id });
    for (let i = 0; i < filterStages.length - 1; i++) {
      edges.push({ from: filterStages[i].id, to: filterStages[i + 1].id });
    }
    edges.push({ from: filterStages[filterStages.length - 1].id, to: outputStage?.id || '' });
  } else if (routeStage && outputStage) {
    edges.push({ from: routeStage.id, to: outputStage.id });
  }

  const svgW = 800, svgH = 600;
  const nodeR = 52;

  return (
    <div className="flex h-screen bg-gray-950">
      <Sidebar />
      <main className="flex-1 overflow-auto p-6">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-white tracking-tight">Message Flow</h1>
            <p className="text-sm text-gray-500">Real-time message pipeline</p>
          </div>
          <div className="flex items-center gap-3">
            <select
              value={selectedRouteId}
              onChange={(e) => { setSelectedRouteId(e.target.value); setSelectedStage(null); }}
              className="bg-gray-800 border border-gray-700 text-gray-200 text-sm rounded-lg px-3 py-2 focus:border-cyan-500 outline-none min-w-[200px]"
            >
              {routes.map(r => <option key={r.route_id} value={r.route_id}>{r.name}</option>)}
            </select>
            {metrics?.ingestion && (
              <div className="flex items-center gap-2 bg-gray-800/80 rounded-lg px-3 py-2 border border-gray-700/50">
                <Activity className="w-3.5 h-3.5 text-cyan-400 animate-pulse" />
                <span className="text-xs text-gray-300 font-mono">{metrics.ingestion.msgs_per_second?.toFixed(1) || '0'} msg/s</span>
              </div>
            )}
          </div>
        </div>

        {!selectedRoute ? (
          <div className="flex items-center justify-center h-96 text-gray-600">Select a route</div>
        ) : (
          <div className="flex gap-5">
            {/* Radial flow diagram */}
            <div className="flex-1 relative bg-gradient-to-br from-gray-900 via-[#0a0f1a] to-gray-950 rounded-2xl border border-gray-800/40 overflow-hidden shadow-2xl">
              {/* Subtle grid */}
              <div className="absolute inset-0 opacity-[0.04]" style={{ backgroundImage: 'radial-gradient(circle, #38bdf8 1px, transparent 1px)', backgroundSize: '30px 30px' }} />
              {/* Outer ring decoration */}
              <svg className="absolute inset-0 w-full h-full pointer-events-none" viewBox={`0 0 ${svgW} ${svgH}`}>
                <circle cx={cx} cy={cy} r={radius + 30} fill="none" stroke="#1e293b" strokeWidth="1" strokeDasharray="4 8" opacity="0.5" />
                <circle cx={cx} cy={cy} r={radius - 30} fill="none" stroke="#1e293b" strokeWidth="1" strokeDasharray="2 6" opacity="0.3" />
              </svg>

              <svg viewBox={`0 0 ${svgW} ${svgH}`} className="w-full h-full min-h-[500px]" style={{ filter: 'drop-shadow(0 0 40px rgba(56,189,248,0.03))' }}>
                <defs>
                  <radialGradient id="centreGlow">
                    <stop offset="0%" stopColor="#7c3aed" stopOpacity="0.15" />
                    <stop offset="100%" stopColor="#7c3aed" stopOpacity="0" />
                  </radialGradient>
                  <filter id="neonGlow">
                    <feGaussianBlur stdDeviation="4" result="blur" />
                    <feMerge><feMergeNode in="blur" /><feMergeNode in="SourceGraphic" /></feMerge>
                  </filter>
                </defs>

                {/* Centre glow */}
                <circle cx={cx} cy={cy} r="90" fill="url(#centreGlow)" />

                {/* Edges with animated particles */}
                {edges.map((edge, i) => {
                  const from = nodePositions[edge.from];
                  const to = nodePositions[edge.to];
                  if (!from || !to) return null;
                  const dx = to.x - from.x, dy = to.y - from.y;
                  const dist = Math.sqrt(dx * dx + dy * dy);
                  const nx = dx / dist, ny = dy / dist;
                  const x1 = from.x + nx * nodeR, y1 = from.y + ny * nodeR;
                  const x2 = to.x - nx * nodeR, y2 = to.y - ny * nodeR;

                  return (
                    <g key={`edge-${i}`}>
                      {/* Track */}
                      <line x1={x1} y1={y1} x2={x2} y2={y2} stroke="#1e293b" strokeWidth="3" strokeLinecap="round" />
                      {/* Animated flow */}
                      <line x1={x1} y1={y1} x2={x2} y2={y2} stroke="#38bdf8" strokeWidth="2" strokeDasharray="8 6" strokeLinecap="round" opacity="0.6">
                        <animate attributeName="stroke-dashoffset" values="28;0" dur="1.2s" repeatCount="indefinite" />
                      </line>
                      {/* Particle */}
                      <circle r="4" fill="#38bdf8" opacity="0.9" filter="url(#neonGlow)">
                        <animateMotion dur={`${1.5 + i * 0.3}s`} repeatCount="indefinite" path={`M${x1},${y1} L${x2},${y2}`} />
                      </circle>
                      <circle r="2" fill="white" opacity="0.8">
                        <animateMotion dur={`${1.5 + i * 0.3}s`} repeatCount="indefinite" path={`M${x1},${y1} L${x2},${y2}`} />
                      </circle>
                    </g>
                  );
                })}

                {/* Nodes */}
                {stages.map((stage) => {
                  const pos = nodePositions[stage.id];
                  if (!pos) return null;
                  const isFlashing = flashNodes.has(stage.id);
                  const isSelected = selectedStage?.id === stage.id;
                  const { fill, stroke, iconColor } = getNodeColors(stage.type);

                  return (
                    <g key={stage.id} onClick={() => setSelectedStage(stage)} className="cursor-pointer" style={{ transition: 'transform 0.2s' }}>
                      {/* Pulse ring on data change */}
                      {isFlashing && (
                        <circle cx={pos.x} cy={pos.y} r={nodeR + 8} fill="none" stroke={stroke} strokeWidth="2" opacity="0.6">
                          <animate attributeName="r" values={`${nodeR};${nodeR + 20}`} dur="0.8s" repeatCount="1" />
                          <animate attributeName="opacity" values="0.6;0" dur="0.8s" repeatCount="1" />
                        </circle>
                      )}
                      {/* Selection ring */}
                      {isSelected && <circle cx={pos.x} cy={pos.y} r={nodeR + 4} fill="none" stroke="#38bdf8" strokeWidth="2" opacity="0.8" />}
                      {/* Node circle */}
                      <circle cx={pos.x} cy={pos.y} r={nodeR} fill={fill} stroke={stroke} strokeWidth={isSelected ? 2 : 1.5} opacity={stage.active ? 1 : 0.4} />
                      {/* Status dot */}
                      <circle cx={pos.x + nodeR * 0.6} cy={pos.y - nodeR * 0.6} r="5" fill={stage.active ? '#4ade80' : '#ef4444'} stroke="#0f172a" strokeWidth="2" />
                      {/* Icon placeholder (text fallback) */}
                      <text x={pos.x} y={pos.y - 10} textAnchor="middle" fontSize="18" fill={iconColor}>{getIconChar(stage.type)}</text>
                      {/* Label */}
                      <text x={pos.x} y={pos.y + 12} textAnchor="middle" fontSize="9" fill="#e2e8f0" fontWeight="600">
                        {stage.label.length > 18 ? stage.label.slice(0, 18) + '…' : stage.label}
                      </text>
                      {/* Count - this ticks up live */}
                      <text x={pos.x} y={pos.y + 26} textAnchor="middle" fontSize="10" fill="#38bdf8" fontFamily="monospace" fontWeight="bold">
                        {formatNum(stage.count)}
                      </text>
                      {/* Sublabel */}
                      <text x={pos.x} y={pos.y + 38} textAnchor="middle" fontSize="8" fill="#64748b" fontFamily="monospace">
                        {stage.sublabel.length > 20 ? stage.sublabel.slice(0, 20) + '…' : stage.sublabel}
                      </text>
                    </g>
                  );
                })}
              </svg>
            </div>

            {/* Right panel: stats + details */}
            <div className="w-72 flex flex-col gap-4">
              {/* Live counters */}
              <div className="bg-gray-900 rounded-xl border border-gray-800/50 p-4 space-y-3">
                <h3 className="text-[10px] text-gray-500 uppercase tracking-widest">Pipeline Throughput</h3>
                <CounterRow icon={<Radio className="w-4 h-4 text-blue-400" />} label="Received" value={stages.find(s => s.type === 'input')?.count || 0} color="text-blue-400" />
                <CounterRow icon={<Cog className="w-4 h-4 text-amber-400" />} label={`Transformed (${filters.length})`} value={procRouted} color="text-amber-400" />
                <CounterRow icon={<Send className="w-4 h-4 text-emerald-400" />} label="Delivered" value={stages.find(s => s.type === 'output')?.count || 0} color="text-emerald-400" />
                {procErrors > 0 && <CounterRow icon={<Zap className="w-4 h-4 text-red-400" />} label="Errors" value={procErrors} color="text-red-400" />}
              </div>

              {/* Selected node details */}
              {selectedStage && (
                <div className="bg-gray-900 rounded-xl border border-gray-800/50 p-4 flex-1 overflow-auto">
                  <div className="flex items-center justify-between mb-3">
                    <h3 className="text-xs font-bold text-white">{selectedStage.label}</h3>
                    <button onClick={() => setSelectedStage(null)} className="text-gray-600 hover:text-white text-sm">✕</button>
                  </div>
                  <div className="space-y-2 text-xs">
                    <div className="flex justify-between"><span className="text-gray-500">Type</span><span className="text-gray-300">{getTypeName(selectedStage.type)}</span></div>
                    <div className="flex justify-between"><span className="text-gray-500">Status</span><span className={selectedStage.active ? 'text-green-400' : 'text-red-400'}>{selectedStage.active ? 'Active' : 'Inactive'}</span></div>
                    <div className="flex justify-between"><span className="text-gray-500">Messages</span><span className="text-cyan-400 font-mono">{selectedStage.count.toLocaleString()}</span></div>
                    {selectedStage.errors > 0 && <div className="flex justify-between"><span className="text-gray-500">Errors</span><span className="text-red-400 font-mono">{selectedStage.errors}</span></div>}
                    <div className="flex justify-between"><span className="text-gray-500">Detail</span><span className="text-gray-400 font-mono text-[10px]">{selectedStage.sublabel}</span></div>
                  </div>
                  {selectedStage.type === 'filter' && selectedStage.data?.js_script && (
                    <pre className="mt-3 text-[10px] bg-gray-950 border border-gray-800/50 rounded-lg p-2 overflow-auto max-h-44 text-emerald-300/80 font-mono leading-relaxed whitespace-pre-wrap">
                      {selectedStage.data.js_script.slice(0, 400)}{selectedStage.data.js_script.length > 400 ? '\n…' : ''}
                    </pre>
                  )}
                  {selectedStage.type === 'route' && selectedStage.data?.description && (
                    <p className="mt-3 text-[10px] text-gray-400 italic">{selectedStage.data.description}</p>
                  )}
                </div>
              )}
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function CounterRow({ icon, label, value, color }: { icon: React.ReactNode; label: string; value: number; color: string }) {
  return (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        {icon}
        <span className="text-[11px] text-gray-400">{label}</span>
      </div>
      <span className={`text-sm font-bold font-mono ${color} tabular-nums`}>{value.toLocaleString()}</span>
    </div>
  );
}

function getNodeColors(type: string) {
  switch (type) {
    case 'input': return { fill: '#0c2d48', stroke: '#3b82f6', iconColor: '#60a5fa' };
    case 'route': return { fill: '#1e1045', stroke: '#8b5cf6', iconColor: '#a78bfa' };
    case 'filter': return { fill: '#1c1508', stroke: '#f59e0b', iconColor: '#fbbf24' };
    case 'output': return { fill: '#052e16', stroke: '#10b981', iconColor: '#34d399' };
    default: return { fill: '#1e293b', stroke: '#475569', iconColor: '#94a3b8' };
  }
}

function getIconChar(type: string): string {
  switch (type) {
    case 'input': return '◉';
    case 'route': return '⑂';
    case 'filter': return '⚙';
    case 'output': return '◎';
    default: return '•';
  }
}

function getTypeName(type: string): string {
  switch (type) {
    case 'input': return 'Input Comm Point';
    case 'output': return 'Output Comm Point';
    case 'route': return 'Route';
    case 'filter': return 'Transform Filter';
    default: return type;
  }
}

function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k';
  return n.toString();
}

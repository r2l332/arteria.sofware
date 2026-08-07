'use client';

import { clsx } from 'clsx';
import { TrendingUp, TrendingDown, Minus } from 'lucide-react';

export function StatusBadge({ status }: { status: string }) {
  const styles: Record<string, string> = {
    RECEIVED: 'bg-blue-500/10 text-blue-400 ring-1 ring-blue-500/20',
    FILTERING: 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20',
    FILTERED: 'bg-cyan-500/10 text-cyan-400 ring-1 ring-cyan-500/20',
    ROUTED: 'bg-indigo-500/10 text-indigo-400 ring-1 ring-indigo-500/20',
    DELIVERED: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20',
    ERROR: 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20',
    DLQ: 'bg-red-600/10 text-red-500 ring-1 ring-red-600/20',
    PROCESSED: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20',
    CONNECTED: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20',
    DISCONNECTED: 'bg-red-500/10 text-red-400 ring-1 ring-red-500/20',
    ACTIVE: 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20',
    DISABLED: 'bg-gray-500/10 text-gray-400 ring-1 ring-gray-500/20',
    PENDING: 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20',
    ENROLLED: 'bg-blue-500/10 text-blue-400 ring-1 ring-blue-500/20',
  };

  return (
    <span className={clsx('badge', styles[status] || 'bg-zinc-500/10 text-zinc-400 ring-1 ring-zinc-500/20')}>
      {status}
    </span>
  );
}

export function StatCard({
  label,
  value,
  subtitle,
  trend,
  icon: Icon,
}: {
  label: string;
  value: string | number;
  subtitle?: string;
  trend?: 'up' | 'down' | 'neutral';
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  icon?: any;
}) {
  return (
    <div className="card p-5">
      <div className="flex items-start justify-between mb-3">
        <p className="text-xs font-medium text-arteria-text-secondary uppercase tracking-wider">{label}</p>
        {Icon && (
          <div className="w-8 h-8 rounded-lg bg-arteria-accent/10 flex items-center justify-center">
            <Icon size={16} className="text-arteria-accent" />
          </div>
        )}
      </div>
      <div className="flex items-end gap-2">
        <p className="text-2xl font-semibold text-white tabular-nums">{value}</p>
        {trend && (
          <span className={clsx('flex items-center gap-0.5 text-xs font-medium mb-0.5', {
            'text-emerald-400': trend === 'up',
            'text-red-400': trend === 'down',
            'text-zinc-500': trend === 'neutral',
          })}>
            {trend === 'up' && <TrendingUp size={12} />}
            {trend === 'down' && <TrendingDown size={12} />}
            {trend === 'neutral' && <Minus size={12} />}
          </span>
        )}
      </div>
      {subtitle && <p className="text-xs text-arteria-muted mt-1">{subtitle}</p>}
    </div>
  );
}

export function PageHeader({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between px-8 py-5 border-b border-arteria-border bg-arteria-surface/50">
      <div>
        <h2 className="text-lg font-semibold text-white">{title}</h2>
        {description && <p className="text-xs text-arteria-muted mt-0.5">{description}</p>}
      </div>
      {children && <div className="flex items-center gap-2">{children}</div>}
    </div>
  );
}

export function EmptyState({
  icon: Icon,
  title,
  description,
}: {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  icon: any;
  title: string;
  description?: string;
}) {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-center">
      <div className="w-12 h-12 rounded-xl bg-arteria-card border border-arteria-border flex items-center justify-center mb-4">
        <Icon size={20} className="text-arteria-muted" />
      </div>
      <p className="text-sm font-medium text-arteria-text-secondary">{title}</p>
      {description && <p className="text-xs text-arteria-muted mt-1 max-w-xs">{description}</p>}
    </div>
  );
}

export function Modal({
  title,
  onClose,
  children,
  width = 'max-w-lg',
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  width?: string;
}) {
  return (
    <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center z-50" onClick={onClose}>
      <div className={clsx('bg-arteria-card border border-arteria-border rounded-xl shadow-2xl w-full', width)} onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between px-6 py-4 border-b border-arteria-border">
          <h3 className="text-sm font-semibold text-white">{title}</h3>
          <button onClick={onClose} className="text-arteria-muted hover:text-white transition-colors">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M18 6L6 18M6 6l12 12"/></svg>
          </button>
        </div>
        <div className="p-6">{children}</div>
      </div>
    </div>
  );
}

export function DataTable({ columns, rows }: { columns: string[]; rows: Record<string, React.ReactNode>[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-arteria-border">
            {columns.map((col) => (
              <th key={col} className="text-left py-3 px-4 text-arteria-muted font-medium text-2xs uppercase tracking-wider">
                {col}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i} className="border-b border-arteria-border/50 hover:bg-white/[0.02] transition-colors duration-100">
              {columns.map((col) => (
                <td key={col} className="py-3 px-4 text-arteria-text-secondary">
                  {row[col]}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

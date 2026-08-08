'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useEffect, useState } from 'react';
import {
  LayoutDashboard,
  MessageSquare,
  GitBranch,
  Radio,
  Shield,
  Code2,
  AlertTriangle,
  ChevronRight,
  Heart,
  Scan,
  LogOut,
  Users,
  Settings,
  Key,
  Mail,
  Building2,
  Activity,
  Server,
} from 'lucide-react';
import { clsx } from 'clsx';
import { useAuth } from '@/lib/auth';
import { apiFetch } from '@/lib/api';
import { useBranding } from '@/lib/branding';
import { useTheme } from '@/lib/theme';

const API_BASE = typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : '/api/v1';

interface NavItem {
  href: string;
  label: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  icon: any;
  module?: string;
  section: 'core' | 'tools';
}

const allNav: NavItem[] = [
  { href: '/', label: 'Dashboard', icon: LayoutDashboard, section: 'core' },
  { href: '/flow', label: 'Message Flow', icon: Activity, section: 'core' },
  { href: '/messages', label: 'Messages', icon: MessageSquare, section: 'core' },
  { href: '/routes', label: 'Routes & Filters', icon: GitBranch, module: 'routes-js', section: 'core' },
  { href: '/comm-points', label: 'Comm Points', icon: Radio, section: 'core' },
  { href: '/tunnel', label: 'Aorta Mesh', icon: Shield, module: 'tunnel', section: 'core' },
  { href: '/patient-journey', label: 'Patient Journey', icon: Heart, section: 'core' },
  { href: '/fhir', label: 'FHIR Resources', icon: Heart, module: 'fhir', section: 'core' },
  { href: '/dicom', label: 'DICOM Nodes', icon: Scan, module: 'dicom', section: 'core' },
  { href: '/ai-filters', label: 'AI Filters', icon: Code2, section: 'tools' },
  { href: '/playground', label: 'JS Playground', icon: Code2, module: 'routes-js', section: 'tools' },
  { href: '/messages-internal', label: 'Team Messages', icon: Mail, section: 'tools' },
  { href: '/errors', label: 'Errors / DLQ', icon: AlertTriangle, section: 'tools' },
  { href: '/settings', label: 'Settings', icon: Settings, section: 'tools' },
  { href: '/users', label: 'Users & Access', icon: Users, module: 'rbac', section: 'tools' },
  { href: '/organisations', label: 'Organisations', icon: Building2, module: 'platform', section: 'tools' },
  { href: '/platform', label: 'Platform Admin', icon: Server, module: 'super_admin_only', section: 'tools' },
];

export default function Sidebar() {
  const pathname = usePathname();
  const { username, role, logout } = useAuth();
  const branding = useBranding();
  const [modules, setModules] = useState<Record<string, boolean>>({ hl7: true, tunnel: true, 'routes-js': true });
  const [onlineCount, setOnlineCount] = useState(0);

  useEffect(() => {
    fetch(`${API_BASE}/config/modules`)
      .then(r => r.json())
      .then(d => setModules(d.modules || {}))
      .catch(() => {});
  }, []);

  useEffect(() => {
    const fetchOnline = () => {
      apiFetch<{ count: number }>('/users/online')
        .then(d => setOnlineCount(d.count))
        .catch(() => {});
    };
    fetchOnline();
    const interval = setInterval(fetchOnline, 15000);
    return () => clearInterval(interval);
  }, []);

  const isPlatformRole = role === 'super_admin' || role === 'devops';

  const visibleNav = allNav.filter(item => {
    // super_admin/devops see platform admin + management pages
    if (isPlatformRole) {
      const platformPages = ['/platform', '/organisations', '/users', '/settings', '/tunnel', '/messages-internal'];
      return platformPages.includes(item.href);
    }
    if (!item.module) return true;
    if (item.module === 'super_admin_only') return false;
    if (item.module === 'rbac') return role === 'admin' || role === 'security';
    if (item.module === 'platform') return role === 'admin';
    return modules[item.module];
  });
  const coreItems = visibleNav.filter(i => i.section === 'core');
  const toolItems = visibleNav.filter(i => i.section === 'tools');
  const protocols = [modules.hl7 && 'HL7v2', modules.fhir && 'FHIR', modules.dicom && 'DICOM'].filter(Boolean);

  return (
    <aside className="w-60 shrink-0 border-r border-arteria-border bg-arteria-surface flex flex-col">
      {/* Brand */}
      <div className="px-5 py-5">
        <div className="flex items-center gap-2.5">
          {branding.logo_url ? (
            <img src={branding.logo_url} alt={branding.app_name} className="w-8 h-8 rounded-lg object-contain" />
          ) : (
            <div className="w-8 h-8 rounded-lg flex items-center justify-center" style={{ background: `linear-gradient(135deg, ${branding.primary_color}, #a855f7)` }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 2L2 7l10 5 10-5-10-5z" />
                <path d="M2 17l10 5 10-5" />
                <path d="M2 12l10 5 10-5" />
              </svg>
            </div>
          )}
          <div>
            <h1 className="text-sm font-semibold text-white tracking-tight">{branding.app_name}</h1>
            <p className="text-2xs text-arteria-muted">{branding.subtitle}</p>
          </div>
        </div>
      </div>

      {/* Protocol badges */}
      <div className="px-5 pb-3 flex gap-1.5 flex-wrap">
        {protocols.map(p => (
          <span key={p as string} className="badge bg-arteria-accent/10 text-arteria-accent ring-1 ring-arteria-accent/20 text-2xs">{p as string}</span>
        ))}
      </div>

      {/* Navigation */}
      <nav className="flex-1 px-3 py-2 space-y-0.5 overflow-y-auto">
        <p className="px-3 pt-2 pb-1.5 text-2xs font-medium text-arteria-muted uppercase tracking-wider">Platform</p>
        {coreItems.map((item) => {
          const active = pathname === item.href || (item.href !== '/' && pathname.startsWith(item.href));
          const Icon = item.icon;
          return (
            <Link
              key={item.href}
              href={item.href}
              className={clsx(
                'flex items-center gap-3 px-3 py-2 rounded-lg text-[13px] font-medium transition-all duration-150 group',
                active
                  ? 'bg-arteria-accent/10 text-white'
                  : 'text-arteria-text-secondary hover:bg-white/[0.03] hover:text-white'
              )}
            >
              <Icon
                size={16}
                className={clsx(
                  'shrink-0 transition-colors',
                  active ? 'text-arteria-accent' : 'text-arteria-muted group-hover:text-arteria-text-secondary'
                )}
              />
              <span className="flex-1">{item.label}</span>
              {active && <ChevronRight size={14} className="text-arteria-accent/50" />}
            </Link>
          );
        })}
        {toolItems.length > 0 && (
          <>
            <p className="px-3 pt-4 pb-1.5 text-2xs font-medium text-arteria-muted uppercase tracking-wider">Tools</p>
            {toolItems.map((item) => {
              const active = pathname === item.href || (item.href !== '/' && pathname.startsWith(item.href));
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={clsx(
                    'flex items-center gap-3 px-3 py-2 rounded-lg text-[13px] font-medium transition-all duration-150 group',
                    active
                      ? 'bg-arteria-accent/10 text-white'
                      : 'text-arteria-text-secondary hover:bg-white/[0.03] hover:text-white'
                  )}
                >
                  <Icon
                    size={16}
                    className={clsx(
                      'shrink-0 transition-colors',
                      active ? 'text-arteria-accent' : 'text-arteria-muted group-hover:text-arteria-text-secondary'
                    )}
                  />
                  <span className="flex-1">{item.label}</span>
                  {active && <ChevronRight size={14} className="text-arteria-accent/50" />}
                </Link>
              );
            })}
          </>
        )}
      </nav>

      {/* Footer */}
      <div className="px-4 py-3 border-t border-arteria-border space-y-2">
        <div className="flex items-center gap-2 px-1">
          <div className="w-6 h-6 rounded-md bg-arteria-accent/10 flex items-center justify-center text-2xs font-medium text-arteria-accent">
            {(username || 'A')[0].toUpperCase()}
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-2xs font-medium text-white truncate">{username}</p>
            <p className="text-2xs text-arteria-muted">{isPlatformRole ? '⚡ Platform' : role}</p>
          </div>
          <ThemeToggle />
          <Link href="/account" className="p-1 text-arteria-muted hover:text-arteria-accent transition-colors" title="Change password">
            <Key size={12} />
          </Link>
          <button onClick={logout} className="p-1 text-arteria-muted hover:text-red-400 transition-colors" title="Sign out">
            <LogOut size={14} />
          </button>
        </div>
        <div className="flex items-center justify-between px-1">
          <div className="flex items-center gap-1.5">
            <div className="w-1.5 h-1.5 rounded-full bg-arteria-success animate-pulse-slow" />
            <span className="text-2xs text-arteria-muted">{onlineCount} online</span>
          </div>
          <span className="text-2xs text-arteria-muted">v0.1.0</span>
        </div>
      </div>
    </aside>
  );
}

function ThemeToggle() {
  const { theme, toggle } = useTheme();
  return (
    <button onClick={toggle} className="p-1 text-arteria-muted hover:text-arteria-accent transition-colors" title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`}>
      {theme === 'dark' ? (
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
      ) : (
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
      )}
    </button>
  );
}

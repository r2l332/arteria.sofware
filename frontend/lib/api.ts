const API_BASE = process.env.NEXT_PUBLIC_API_URL || (typeof window !== 'undefined' ? `${window.location.origin}/api/v1` : 'http://localhost:8080/api/v1');

function getAuthToken(): string | null {
  if (typeof window === 'undefined') return null;
  try {
    const saved = localStorage.getItem('arteria_auth');
    if (saved) return JSON.parse(saved).token;
  } catch {}
  return null;
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getAuthToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(init?.headers as Record<string, string> || {}),
  };

  const res = await fetch(`${API_BASE}${path}`, { ...init, headers });

  if (res.status === 401) {
    // Token expired — clear auth and redirect to login
    localStorage.removeItem('arteria_auth');
    window.location.reload();
    throw new Error('Session expired');
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `API error ${res.status}`);
  }
  return res.json();
}

// --- Messages ---
export const getMessages = (limit = 50) =>
  apiFetch<{ messages: Message[]; count: number }>(`/messages?limit=${limit}`);

export const getMessage = (id: string) =>
  apiFetch<MessageDetail>(`/messages/${id}`);

export const getMessagesByStatus = (status: string, limit = 50) =>
  apiFetch<{ messages: Message[]; count: number }>(`/messages/status/${status}?limit=${limit}`);

// --- Routes ---
export const getRoutes = () =>
  apiFetch<{ routes: Route[]; count: number }>('/routes');

export const createRoute = (data: RoutePayload) =>
  apiFetch<{ route_id: string }>('/routes', { method: 'POST', body: JSON.stringify(data) });

export const updateRoute = (id: string, data: RoutePayload) =>
  apiFetch<{ status: string }>(`/routes/${id}`, { method: 'PUT', body: JSON.stringify(data) });

export const deleteRoute = (id: string) =>
  apiFetch<{ status: string }>(`/routes/${id}`, { method: 'DELETE' });

// --- Filters ---
export const getFilters = (routeId: string) =>
  apiFetch<{ filters: Filter[]; count: number }>(`/routes/${routeId}/filters`);

export const createFilter = (routeId: string, data: FilterPayload) =>
  apiFetch<{ filter_id: string }>(`/routes/${routeId}/filters`, { method: 'POST', body: JSON.stringify(data) });

export const updateFilter = (filterId: string, data: FilterPayload) =>
  apiFetch<{ status: string }>(`/filters/${filterId}`, { method: 'PUT', body: JSON.stringify(data) });

// --- Communication Points ---
export const getCommPoints = () =>
  apiFetch<{ communication_points: CommPoint[]; count: number }>('/comm-points');

export const createCommPoint = (data: any) =>
  apiFetch<{ comm_point_id: string }>('/comm-points', { method: 'POST', body: JSON.stringify(data) });

export const updateCommPoint = (id: string, data: any) =>
  apiFetch<{ status: string }>(`/comm-points/${id}`, { method: 'PUT', body: JSON.stringify(data) });

export const deleteCommPoint = (id: string) =>
  apiFetch<{ status: string }>(`/comm-points/${id}`, { method: 'DELETE' });

// --- Tunnel Nodes ---
export const getTunnelNodes = () =>
  apiFetch<{ nodes: any[]; count: number }>('/tunnel/nodes');

export const createTunnelNode = (data: any) =>
  apiFetch<{ node_id: string; enrollment_token: string }>('/tunnel/nodes', { method: 'POST', body: JSON.stringify(data) });

export const deleteTunnelNode = (id: string) =>
  apiFetch<{ status: string }>(`/tunnel/nodes/${id}`, { method: 'DELETE' });

export const getTunnelMappings = (nodeId: string) =>
  apiFetch<{ mappings: any[] }>(`/tunnel/nodes/${nodeId}/mappings`);

export const createTunnelMapping = (nodeId: string, data: any) =>
  apiFetch<{ status: string }>(`/tunnel/nodes/${nodeId}/mappings`, { method: 'POST', body: JSON.stringify(data) });

// --- Lookup Tables ---
export const getLookupTables = () =>
  apiFetch<{ lookup_tables: LookupTable[]; count: number }>('/lookups');

// --- Errors ---
export const getErrors = (limit = 50) =>
  apiFetch<{ errors: ErrorMessage[]; count: number }>(`/errors?limit=${limit}`);

// --- Stats ---
export const getStats = () =>
  apiFetch<Stats>('/stats');

// --- Live Metrics ---
export const getMetrics = () =>
  apiFetch<LiveMetrics>('/metrics');

export const getCPMetrics = () =>
  apiFetch<{ comm_points: Record<string, CPMetricSnapshot>; count: number }>('/metrics/comm-points');

export const getCPLogs = (id: string) =>
  apiFetch<CPLogResponse>(`/metrics/comm-points/${id}/logs`);

// --- Types ---
export interface Message {
  message_id: string;
  patient_id: string;
  message_type: string;
  trigger_event: string;
  sending_facility: string;
  status: string;
  created_at: string;
}

export interface MessageDetail extends Message {
  raw_payload: string;
  transformed_payload: string;
  properties: string;
  error_details: string;
  updated_at: string;
  retry_count: number;
}

export interface Route {
  route_id: string;
  name: string;
  description: string;
  source_comm_point_id: string;
  dest_comm_point_id: string;
  source_topic: string;
  destination_topic: string;
  is_active: boolean;
}

export interface RoutePayload {
  name: string;
  description?: string;
  source_comm_point_id?: string;
  dest_comm_point_id?: string;
  source_topic: string;
  destination_topic: string;
  is_active: boolean;
}

export interface Filter {
  filter_id: string;
  name: string;
  filter_type: string;
  execution_order: number;
  js_script: string;
  config_json: string;
  is_active: boolean;
}

export interface FilterPayload {
  name: string;
  filter_type: string;
  execution_order: number;
  js_script: string;
  config_json?: string;
  is_active: boolean;
}

export interface CommPoint {
  comm_point_id: string;
  name: string;
  direction: string;
  protocol: string;
  host: string;
  port: number;
  is_active: boolean;
  max_retries: number;
  retry_delay_ms: number;
  timeout_ms: number;
}

export interface LookupTable {
  table_id: string;
  name: string;
  description: string;
}

export interface ErrorMessage {
  message_id: string;
  error_type: string;
  error_details: string;
  retry_count: number;
  max_retries: number;
  created_at: string;
}

export interface Stats {
  total_messages: number;
  total_routes: number;
  total_errors: number;
}

export interface ServiceMetrics {
  received: number;
  processed: number;
  routed: number;
  errors: number;
  rejected: number;
  dlq: number;
  bytes_in: number;
  uptime_seconds: number;
  msgs_per_second: number;
  msgs_per_minute: number;
}

export interface LiveMetrics {
  ingestion?: ServiceMetrics;
  processing?: ServiceMetrics;
}

export interface CPMetricSnapshot {
  id: string;
  name: string;
  direction: string;
  received: number;
  sent: number;
  errors: number;
  bytes_in: number;
  bytes_out: number;
  last_seen?: string;
  logs?: CPLogEntryData[];
}

export interface CPLogEntryData {
  timestamp: string;
  level: string;
  message: string;
  message_id?: string;
  error?: string;
  size_bytes?: number;
}

export interface CPLogResponse {
  comm_point_id: string;
  name: string;
  direction: string;
  received: number;
  errors: number;
  logs: CPLogEntryData[];
  log_count: number;
}

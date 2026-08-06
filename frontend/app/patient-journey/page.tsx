'use client';

import { useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { apiFetch } from '@/lib/api';
import { Search, User, Activity, AlertCircle, ArrowRight, Clock, Building2 } from 'lucide-react';
import { clsx } from 'clsx';

interface JourneyEvent {
  message_id: string;
  message_type: string;
  trigger_event: string;
  sending_facility: string;
  status: string;
  created_at: string;
  event_type: string;
  summary: string;
  error?: string;
  has_payload?: boolean;
}

export default function PatientJourneyPage() {
  const [patientId, setPatientId] = useState('');
  const [events, setEvents] = useState<JourneyEvent[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);

  const search = async () => {
    if (!patientId.trim()) return;
    setLoading(true);
    setSearched(true);
    try {
      const data = await apiFetch<{ events: JourneyEvent[]; count: number }>(`/patients/${patientId.trim()}/journey`);
      setEvents(data.events || []);
    } catch { setEvents([]); }
    setLoading(false);
  };

  return (
    <div className="flex h-screen bg-gray-950">
      <Sidebar />
      <main className="flex-1 overflow-y-auto p-6">
        <div className="max-w-4xl mx-auto">
          {/* Header */}
          <div className="mb-8">
            <h1 className="text-2xl font-bold text-white flex items-center gap-3">
              <User className="w-6 h-6 text-cyan-400" />
              Patient Journey
            </h1>
            <p className="text-sm text-gray-500 mt-1">Track every message for a patient across all routes and systems</p>
          </div>

          {/* Search */}
          <div className="flex gap-3 mb-8">
            <div className="flex-1 relative">
              <Search className="absolute left-3 top-2.5 w-4 h-4 text-gray-500" />
              <input
                type="text"
                value={patientId}
                onChange={(e) => setPatientId(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && search()}
                placeholder="Enter Patient ID / MRN (e.g. MRN12345)"
                className="w-full bg-gray-800 border border-gray-700 rounded-lg pl-10 pr-4 py-2.5 text-sm text-white placeholder-gray-500 focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 outline-none"
              />
            </div>
            <button onClick={search} disabled={loading}
              className="bg-cyan-600 hover:bg-cyan-500 text-white px-6 py-2.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-50">
              {loading ? 'Searching...' : 'Search'}
            </button>
          </div>

          {/* Timeline */}
          {searched && events.length === 0 && !loading && (
            <div className="text-center py-16 text-gray-500">
              <User className="w-12 h-12 mx-auto mb-3 opacity-30" />
              <p>No events found for patient <span className="font-mono text-gray-300">{patientId}</span></p>
            </div>
          )}

          {events.length > 0 && (
            <div className="relative">
              {/* Timeline line */}
              <div className="absolute left-6 top-0 bottom-0 w-0.5 bg-gradient-to-b from-cyan-500 via-purple-500 to-emerald-500 opacity-30" />

              <div className="space-y-4">
                {events.map((event, i) => (
                  <div key={event.message_id} className="relative flex gap-4 pl-4">
                    {/* Timeline dot */}
                    <div className={clsx('relative z-10 w-5 h-5 rounded-full border-2 mt-4 shrink-0',
                      event.status === 'ERROR' ? 'bg-red-500/20 border-red-500' :
                      event.event_type === 'admission' ? 'bg-cyan-500/20 border-cyan-500' :
                      event.event_type === 'discharge' ? 'bg-emerald-500/20 border-emerald-500' :
                      event.event_type === 'order' ? 'bg-amber-500/20 border-amber-500' :
                      event.event_type === 'result' ? 'bg-purple-500/20 border-purple-500' :
                      'bg-gray-500/20 border-gray-600'
                    )} />

                    {/* Event card */}
                    <div className={clsx('flex-1 bg-gray-900/80 backdrop-blur border rounded-xl p-4 transition-all hover:scale-[1.01]',
                      event.status === 'ERROR' ? 'border-red-800/50' : 'border-gray-800/50')}>
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          <EventIcon type={event.event_type} />
                          <span className="text-sm font-semibold text-white">{event.summary}</span>
                        </div>
                        <span className={clsx('text-[9px] px-2 py-0.5 rounded-full font-medium',
                          event.status === 'ROUTED' ? 'bg-emerald-500/20 text-emerald-400' :
                          event.status === 'ERROR' ? 'bg-red-500/20 text-red-400' :
                          event.status === 'DELIVERED' ? 'bg-green-500/20 text-green-400' :
                          'bg-gray-500/20 text-gray-400'
                        )}>{event.status}</span>
                      </div>
                      <div className="flex items-center gap-4 text-[11px] text-gray-500">
                        <span className="flex items-center gap-1"><Clock className="w-3 h-3" />{new Date(event.created_at).toLocaleString()}</span>
                        <span className="flex items-center gap-1"><Building2 className="w-3 h-3" />{event.sending_facility}</span>
                        <span className="font-mono text-gray-600">{event.message_type}^{event.trigger_event}</span>
                        <span className="font-mono text-gray-700">{event.message_id.slice(0, 8)}</span>
                      </div>
                      {event.error && <p className="mt-2 text-[10px] text-red-400 bg-red-950/30 rounded p-2 font-mono">{event.error}</p>}
                    </div>
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

function EventIcon({ type }: { type: string }) {
  switch (type) {
    case 'admission': return <Activity className="w-4 h-4 text-cyan-400" />;
    case 'discharge': return <ArrowRight className="w-4 h-4 text-emerald-400" />;
    case 'order': return <Clock className="w-4 h-4 text-amber-400" />;
    case 'result': return <AlertCircle className="w-4 h-4 text-purple-400" />;
    default: return <Activity className="w-4 h-4 text-gray-400" />;
  }
}

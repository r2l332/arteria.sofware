'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { getErrors, type ErrorMessage } from '@/lib/api';

export default function ErrorsPage() {
  const [errors, setErrors] = useState<ErrorMessage[]>([]);

  useEffect(() => {
    getErrors(100).then((r) => setErrors(r.errors)).catch(console.error);
  }, []);

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-y-auto p-8">
        <h2 className="text-2xl font-bold text-white mb-6">Errors / Dead Letter Queue</h2>
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
                <tr key={e.message_id} className="border-b border-arteria-border/50">
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
      </main>
    </div>
  );
}

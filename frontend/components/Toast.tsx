'use client';

import { useEffect, useState, useCallback } from 'react';

interface ToastItem {
  id: number;
  message: string;
  type: 'success' | 'error' | 'info';
}

let addToastFn: ((msg: string, type: ToastItem['type']) => void) | null = null;

export function toast(message: string, type: ToastItem['type'] = 'info') {
  if (addToastFn) addToastFn(message, type);
}

export function ToastContainer() {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  let counter = 0;

  const add = useCallback((message: string, type: ToastItem['type']) => {
    const id = Date.now() + (counter++);
    setToasts(prev => [...prev, { id, message, type }]);
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 4000);
  }, []);

  useEffect(() => { addToastFn = add; return () => { addToastFn = null; }; }, [add]);

  const colors = {
    success: 'bg-emerald-900/90 border-emerald-700 text-emerald-200',
    error: 'bg-red-900/90 border-red-700 text-red-200',
    info: 'bg-cyan-900/90 border-cyan-700 text-cyan-200',
  };

  return (
    <div className="fixed top-4 right-4 z-[9999] flex flex-col gap-2 pointer-events-none">
      {toasts.map(t => (
        <div key={t.id}
          className={`pointer-events-auto px-4 py-3 rounded-lg border backdrop-blur-xl shadow-2xl text-sm font-medium animate-slide-in ${colors[t.type]}`}
          onClick={() => setToasts(prev => prev.filter(x => x.id !== t.id))}>
          {t.message}
        </div>
      ))}
    </div>
  );
}

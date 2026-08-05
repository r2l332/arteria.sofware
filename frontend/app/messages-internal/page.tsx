'use client';

import { useEffect, useState } from 'react';
import Sidebar from '@/components/Sidebar';
import { apiFetch } from '@/lib/api';
import { useAuth } from '@/lib/auth';
import { Send, Mail, MailOpen, Inbox, SendHorizonal } from 'lucide-react';

interface InternalMessage {
  message_id: string;
  from: string;
  to: string;
  subject: string;
  body: string;
  is_read: boolean;
  created_at: string;
}

export default function InternalMessagesPage() {
  const { username } = useAuth();
  const [tab, setTab] = useState<'inbox' | 'sent'>('inbox');
  const [messages, setMessages] = useState<InternalMessage[]>([]);
  const [selected, setSelected] = useState<InternalMessage | null>(null);
  const [showCompose, setShowCompose] = useState(false);
  const [to, setTo] = useState('');
  const [subject, setSubject] = useState('');
  const [body, setBody] = useState('');
  const [sending, setSending] = useState(false);

  const loadMessages = () => {
    const endpoint = tab === 'inbox' ? '/messages/internal/inbox' : '/messages/internal/sent';
    apiFetch<{ messages: InternalMessage[] }>(endpoint)
      .then(d => setMessages(d.messages || []))
      .catch(console.error);
  };

  useEffect(() => { loadMessages(); }, [tab]);

  const send = async () => {
    if (!to || !subject) return;
    setSending(true);
    await apiFetch('/messages/internal', {
      method: 'POST',
      body: JSON.stringify({ to, subject, body }),
    });
    setSending(false);
    setShowCompose(false);
    setTo(''); setSubject(''); setBody('');
    loadMessages();
  };

  const markRead = async (msg: InternalMessage) => {
    if (!msg.is_read) {
      await apiFetch(`/messages/internal/${msg.message_id}/read`, { method: 'PUT' });
    }
    setSelected(msg);
    loadMessages();
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        <div className="flex items-center justify-between px-8 py-5 border-b border-arteria-border">
          <div>
            <h2 className="text-2xl font-bold text-white">Messages</h2>
            <p className="text-xs text-arteria-muted mt-0.5">Internal messaging between team members</p>
          </div>
          <button onClick={() => setShowCompose(true)}
            className="px-4 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80 flex items-center gap-2">
            <Send size={14} /> Compose
          </button>
        </div>

        {/* Tabs */}
        <div className="px-8 pt-3 flex gap-4 border-b border-arteria-border">
          <button onClick={() => setTab('inbox')}
            className={`pb-2 px-1 text-sm font-medium border-b-2 transition-colors flex items-center gap-1.5 ${
              tab === 'inbox' ? 'border-arteria-accent text-white' : 'border-transparent text-arteria-muted hover:text-white'
            }`}><Inbox size={14} /> Inbox</button>
          <button onClick={() => setTab('sent')}
            className={`pb-2 px-1 text-sm font-medium border-b-2 transition-colors flex items-center gap-1.5 ${
              tab === 'sent' ? 'border-arteria-accent text-white' : 'border-transparent text-arteria-muted hover:text-white'
            }`}><SendHorizonal size={14} /> Sent</button>
        </div>

        <div className="flex-1 overflow-hidden flex">
          {/* Message List */}
          <div className="w-1/2 overflow-y-auto border-r border-arteria-border">
            {messages.length === 0 && (
              <p className="text-center py-12 text-arteria-muted text-sm">No messages</p>
            )}
            {messages.map(msg => (
              <div key={msg.message_id} onClick={() => markRead(msg)}
                className={`px-6 py-4 border-b border-arteria-border cursor-pointer transition-colors hover:bg-white/[0.02] ${
                  selected?.message_id === msg.message_id ? 'bg-arteria-accent/5' : ''
                } ${!msg.is_read && tab === 'inbox' ? 'border-l-2 border-l-arteria-accent' : ''}`}>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-xs font-medium text-white flex items-center gap-1.5">
                    {!msg.is_read && tab === 'inbox' ? <Mail size={12} className="text-arteria-accent" /> : <MailOpen size={12} className="text-arteria-muted" />}
                    {tab === 'inbox' ? msg.from : msg.to}
                  </span>
                  <span className="text-2xs text-arteria-muted">{new Date(msg.created_at).toLocaleString()}</span>
                </div>
                <p className="text-sm text-white truncate">{msg.subject}</p>
                <p className="text-xs text-arteria-muted truncate mt-0.5">{msg.body}</p>
              </div>
            ))}
          </div>

          {/* Message Detail */}
          <div className="w-1/2 overflow-y-auto">
            {selected ? (
              <div className="p-6">
                <div className="mb-4">
                  <h3 className="text-lg font-semibold text-white">{selected.subject}</h3>
                  <div className="flex gap-4 mt-2 text-xs text-arteria-muted">
                    <span>From: <strong className="text-white">{selected.from}</strong></span>
                    <span>To: <strong className="text-white">{selected.to}</strong></span>
                    <span>{new Date(selected.created_at).toLocaleString()}</span>
                  </div>
                </div>
                <div className="bg-arteria-surface border border-arteria-border rounded-lg p-4 text-sm text-gray-300 whitespace-pre-wrap">
                  {selected.body || '(no content)'}
                </div>
              </div>
            ) : (
              <div className="flex-1 flex items-center justify-center h-full text-arteria-muted text-sm">
                Select a message to read
              </div>
            )}
          </div>
        </div>

        {/* Compose Modal */}
        {showCompose && (
          <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowCompose(false)}>
            <div className="bg-arteria-surface border border-arteria-border rounded-lg w-[500px] p-6" onClick={e => e.stopPropagation()}>
              <h3 className="text-lg font-semibold text-white mb-4">New Message</h3>
              <div className="space-y-3">
                <div>
                  <label className="text-xs text-arteria-muted">To (username)</label>
                  <input value={to} onChange={e => setTo(e.target.value)} placeholder="e.g., darren.markham"
                    className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                </div>
                <div>
                  <label className="text-xs text-arteria-muted">Subject</label>
                  <input value={subject} onChange={e => setSubject(e.target.value)} placeholder="Subject"
                    className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white" />
                </div>
                <div>
                  <label className="text-xs text-arteria-muted">Message</label>
                  <textarea value={body} onChange={e => setBody(e.target.value)} rows={5} placeholder="Type your message..."
                    className="w-full mt-1 px-3 py-2 bg-arteria-bg border border-arteria-border rounded text-sm text-white resize-none" />
                </div>
              </div>
              <div className="flex justify-end gap-2 mt-5">
                <button onClick={() => setShowCompose(false)} className="px-4 py-2 text-sm text-arteria-muted hover:text-white">Cancel</button>
                <button onClick={send} disabled={sending || !to || !subject}
                  className="px-4 py-2 bg-arteria-accent text-white text-sm rounded hover:bg-arteria-accent/80 disabled:opacity-50 flex items-center gap-2">
                  <Send size={14} /> {sending ? 'Sending...' : 'Send'}
                </button>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

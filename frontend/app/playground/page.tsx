'use client';

import { useState } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

const API_BASE = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080/api/v1';

const DEFAULT_SCRIPT = `// Write your JS filter and click "Run" to test it
// The message object is available as 'msg'
function transform(msg) {
  msg.properties.processed_at = new Date().toISOString();
  msg.properties.facility_code = msg.sendingFacility.substring(0, 3);
  return msg;
}`;

const DEFAULT_PAYLOAD = JSON.stringify({
  messageId: "test-0001",
  messageType: "ADT",
  triggerEvent: "A01",
  sendingFacility: "HOSPITAL_A",
  patientId: "PAT12345",
  rawPayload: "MSH|^~\\&|SRC|HOSPITAL_A|DST|FAC|202608040800||ADT^A01|123|P|2.3\rPID|||PAT12345||Doe^John||19800101|M",
  properties: {}
}, null, 2);

const CONDITIONAL_TEMPLATE = `// Conditional filter — return pass, reject, or route_to
function evaluate(msg) {
  if (!msg.patientId || msg.patientId === "") {
    return { action: "reject", reason: "Missing Patient ID" };
  }
  if (msg.sendingFacility.startsWith("LAB")) {
    return { action: "route_to", route_to: "lab_results" };
  }
  return { action: "pass" };
}

// Wrap for the V8 engine
function transform(msg) { return evaluate(msg); }`;

export default function PlaygroundPage() {
  const [script, setScript] = useState(DEFAULT_SCRIPT);
  const [payload, setPayload] = useState(DEFAULT_PAYLOAD);
  const [output, setOutput] = useState<string>('');
  const [error, setError] = useState<string>('');
  const [running, setRunning] = useState(false);
  const [execTime, setExecTime] = useState<number | null>(null);

  const run = async () => {
    setRunning(true);
    setError('');
    setOutput('');
    const start = performance.now();

    try {
      const res = await fetch(`${API_BASE}/playground/execute`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ script, filter_type: 'javascript', payload }),
      });
      const data = await res.json();
      const elapsed = performance.now() - start;
      setExecTime(elapsed);

      if (data.success) {
        try {
          setOutput(JSON.stringify(JSON.parse(data.output), null, 2));
        } catch {
          setOutput(data.output);
        }
      } else {
        setError(data.error || 'Unknown error');
      }
    } catch (err) {
      setError(String(err));
    }
    setRunning(false);
  };

  return (
    <div className="flex h-screen bg-arteria-bg">
      <Sidebar />
      <main className="flex-1 overflow-hidden flex flex-col">
        {/* Toolbar */}
        <div className="px-6 py-4 border-b border-arteria-border flex items-center justify-between">
          <div>
            <h2 className="text-xl font-bold text-white">JS Filter Playground</h2>
            <p className="text-xs text-arteria-muted mt-0.5">Test your JavaScript filters against sample payloads before deploying</p>
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={() => { setScript(DEFAULT_SCRIPT); setPayload(DEFAULT_PAYLOAD); }}
              className="px-3 py-1.5 text-xs text-arteria-muted hover:text-white border border-arteria-border rounded"
            >
              Transform Template
            </button>
            <button
              onClick={() => { setScript(CONDITIONAL_TEMPLATE); }}
              className="px-3 py-1.5 text-xs text-arteria-muted hover:text-white border border-arteria-border rounded"
            >
              Conditional Template
            </button>
            <button
              onClick={run}
              disabled={running}
              className="px-5 py-2 bg-arteria-success text-white text-sm font-medium rounded hover:bg-arteria-success/80 disabled:opacity-50"
            >
              {running ? 'Running...' : '▶ Run'}
            </button>
          </div>
        </div>

        {/* Editor + payload + output */}
        <div className="flex-1 overflow-hidden flex">
          {/* Script Editor */}
          <div className="flex-1 flex flex-col border-r border-arteria-border">
            <div className="px-4 py-2 border-b border-arteria-border">
              <span className="text-xs text-arteria-muted">JavaScript Filter</span>
            </div>
            <div className="flex-1">
              <MonacoEditor
                height="100%"
                language="javascript"
                theme="vs-dark"
                value={script}
                onChange={(v) => setScript(v || '')}
                options={{
                  minimap: { enabled: false },
                  fontSize: 13,
                  scrollBeyondLastLine: false,
                  automaticLayout: true,
                  padding: { top: 8 },
                }}
              />
            </div>
          </div>

          {/* Input / Output */}
          <div className="w-[420px] flex flex-col">
            {/* Input Payload */}
            <div className="flex-1 flex flex-col border-b border-arteria-border">
              <div className="px-4 py-2 border-b border-arteria-border">
                <span className="text-xs text-arteria-muted">Input Payload (JSON)</span>
              </div>
              <div className="flex-1">
                <MonacoEditor
                  height="100%"
                  language="json"
                  theme="vs-dark"
                  value={payload}
                  onChange={(v) => setPayload(v || '')}
                  options={{
                    minimap: { enabled: false },
                    fontSize: 12,
                    scrollBeyondLastLine: false,
                    automaticLayout: true,
                    padding: { top: 8 },
                    lineNumbers: 'off',
                  }}
                />
              </div>
            </div>

            {/* Output */}
            <div className="flex-1 flex flex-col">
              <div className="px-4 py-2 border-b border-arteria-border flex items-center justify-between">
                <span className="text-xs text-arteria-muted">Output</span>
                {execTime !== null && (
                  <span className="text-[10px] text-arteria-muted">{execTime.toFixed(1)}ms</span>
                )}
              </div>
              <div className="flex-1 overflow-auto p-4 font-mono text-xs">
                {error && (
                  <div className="bg-red-950/30 border border-red-900/50 rounded p-3 text-red-400">
                    <p className="font-medium mb-1">Error</p>
                    <pre className="whitespace-pre-wrap">{error}</pre>
                  </div>
                )}
                {output && (
                  <pre className="text-green-400 whitespace-pre-wrap">{output}</pre>
                )}
                {!error && !output && (
                  <p className="text-arteria-muted">Click ▶ Run to execute your filter</p>
                )}
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

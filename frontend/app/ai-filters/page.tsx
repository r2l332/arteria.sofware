'use client';

import { useState } from 'react';
import dynamic from 'next/dynamic';
import Sidebar from '@/components/Sidebar';
import { apiFetch } from '@/lib/api';
import { Sparkles, Play, Copy, Check, Zap } from 'lucide-react';
import { clsx } from 'clsx';

const MonacoEditor = dynamic(() => import('@monaco-editor/react'), { ssr: false });

export default function AIFiltersPage() {
  const [description, setDescription] = useState('');
  const [sampleInput, setSampleInput] = useState('');
  const [language, setLanguage] = useState('python');
  const [generatedScript, setGeneratedScript] = useState('');
  const [generating, setGenerating] = useState(false);
  const [copied, setCopied] = useState(false);
  const [testResult, setTestResult] = useState('');

  const generate = async () => {
    if (!description.trim()) return;
    setGenerating(true);
    try {
      const resp = await apiFetch<{ script: string }>('/ai/generate-filter', {
        method: 'POST',
        body: JSON.stringify({ description, sample_input: sampleInput, language }),
      });
      setGeneratedScript(resp.script || '');
    } catch (e: any) {
      setGeneratedScript(`# Error: ${e.message}`);
    }
    setGenerating(false);
  };

  const copyToClipboard = () => {
    navigator.clipboard.writeText(generatedScript);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const testScript = async () => {
    if (!generatedScript || !sampleInput) return;
    try {
      // Use a temp test via the processing service
      setTestResult('Testing...');
      // For now just show the input → script mapping
      setTestResult(JSON.stringify({ status: 'ready', note: 'Deploy this filter to a route to test with live messages' }, null, 2));
    } catch (e: any) {
      setTestResult(`Error: ${e.message}`);
    }
  };

  return (
    <div className="flex h-screen bg-gray-950">
      <Sidebar />
      <main className="flex-1 overflow-y-auto p-6">
        <div className="max-w-6xl mx-auto">
          {/* Header */}
          <div className="mb-6">
            <h1 className="text-2xl font-bold text-white flex items-center gap-3">
              <Sparkles className="w-6 h-6 text-amber-400" />
              AI Filter Generator
            </h1>
            <p className="text-sm text-gray-500 mt-1">Describe what you want in plain English — get a working filter instantly</p>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Input panel */}
            <div className="space-y-4">
              {/* Description */}
              <div>
                <label className="text-xs text-gray-400 uppercase tracking-wider block mb-2">What should this filter do?</label>
                <textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="e.g. Extract the patient name and ID from the HL7 message and output as JSON"
                  rows={3}
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-sm text-white placeholder-gray-500 focus:border-amber-500 outline-none resize-none"
                />
              </div>

              {/* Quick prompts */}
              <div className="flex flex-wrap gap-2">
                {[
                  'Extract patient demographics as JSON',
                  'Convert HL7 to structured JSON',
                  'Reject messages without patient ID',
                  'Add timestamp and routing metadata',
                  'Route by message type (ADT vs ORM)',
                ].map(prompt => (
                  <button key={prompt} onClick={() => setDescription(prompt)}
                    className="text-[10px] bg-gray-800 border border-gray-700 text-gray-400 px-2 py-1 rounded hover:border-amber-500/50 hover:text-amber-400 transition-colors">
                    {prompt}
                  </button>
                ))}
              </div>

              {/* Sample input */}
              <div>
                <label className="text-xs text-gray-400 uppercase tracking-wider block mb-2">Sample HL7 Message (optional)</label>
                <textarea
                  value={sampleInput}
                  onChange={(e) => setSampleInput(e.target.value)}
                  placeholder="MSH|^~\&|EMR|HOSP|...\rPID|||MRN123||Smith^John||..."
                  rows={4}
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-xs text-green-300 font-mono placeholder-gray-600 focus:border-amber-500 outline-none resize-none"
                />
              </div>

              {/* Options */}
              <div className="flex items-center gap-4">
                <div>
                  <label className="text-[10px] text-gray-500 uppercase block mb-1">Language</label>
                  <select value={language} onChange={(e) => setLanguage(e.target.value)}
                    className="bg-gray-800 border border-gray-700 text-gray-200 text-xs rounded px-2 py-1.5 outline-none">
                    <option value="python">Python</option>
                    <option value="javascript">JavaScript</option>
                  </select>
                </div>
                <button onClick={generate} disabled={generating || !description.trim()}
                  className="flex items-center gap-2 bg-gradient-to-r from-amber-600 to-orange-600 hover:from-amber-500 hover:to-orange-500 text-white px-5 py-2 rounded-lg text-sm font-medium transition-all disabled:opacity-40 mt-4">
                  <Sparkles className="w-4 h-4" />
                  {generating ? 'Generating...' : 'Generate Filter'}
                </button>
              </div>
            </div>

            {/* Output panel */}
            <div className="flex flex-col">
              <div className="flex items-center justify-between mb-2">
                <label className="text-xs text-gray-400 uppercase tracking-wider">Generated Filter</label>
                <div className="flex gap-2">
                  {generatedScript && (
                    <>
                      <button onClick={copyToClipboard} className="flex items-center gap-1 text-[10px] text-gray-400 hover:text-white bg-gray-800 px-2 py-1 rounded border border-gray-700">
                        {copied ? <Check className="w-3 h-3 text-green-400" /> : <Copy className="w-3 h-3" />}
                        {copied ? 'Copied' : 'Copy'}
                      </button>
                      <button onClick={testScript} className="flex items-center gap-1 text-[10px] text-emerald-400 hover:text-emerald-300 bg-emerald-500/10 px-2 py-1 rounded border border-emerald-500/30">
                        <Play className="w-3 h-3" /> Test
                      </button>
                    </>
                  )}
                </div>
              </div>
              <div className="flex-1 min-h-[300px] rounded-lg overflow-hidden border border-gray-800">
                <MonacoEditor
                  height="300px"
                  language={language}
                  theme="vs-dark"
                  value={generatedScript || '# Click "Generate Filter" to create a filter from your description'}
                  options={{ minimap: { enabled: false }, fontSize: 12, scrollBeyondLastLine: false, padding: { top: 12 } }}
                />
              </div>
              {testResult && (
                <pre className="mt-3 text-xs bg-gray-900 border border-gray-800 rounded-lg p-3 text-cyan-300 font-mono overflow-auto max-h-32">{testResult}</pre>
              )}

              {/* Deploy hint */}
              {generatedScript && (
                <div className="mt-4 bg-amber-500/5 border border-amber-500/20 rounded-lg p-3 flex items-start gap-2">
                  <Zap className="w-4 h-4 text-amber-400 shrink-0 mt-0.5" />
                  <div className="text-xs text-gray-400">
                    <span className="text-amber-400 font-medium">Ready to deploy.</span> Copy this script and paste it into a filter on your route via <span className="text-white">Routes & Filters</span>. Or use the Message Flow page to test it against live messages.
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}

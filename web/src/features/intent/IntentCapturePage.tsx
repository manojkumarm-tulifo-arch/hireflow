import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Bot, Send, Sparkles } from 'lucide-react';
import { intentApi } from '@/api/intent';
import type { CreateIntentRequest, Intent } from '@/api/types';
import { Card, Button, Spinner, Input } from '@/components/ui/primitives';
import { IntentCard } from './IntentCard';

// Demo extraction: we don't have an LLM hooked up yet, so the recruiter
// fills a small form on the right that doubles as the chat-extracted
// "structured intent". When the auth backend + LLM service land, replace
// the form with a real chat → extract → propose flow.
const initialDraft: CreateIntentRequest = {
  role_title: 'Senior Backend Engineer',
  skills: [
    { name: 'Go', required: true },
    { name: 'Postgres', required: true },
    { name: 'Kubernetes', required: false },
  ],
  min_years: 3,
  max_years: 7,
  headcount: 2,
  locations: ['Bangalore'],
  work_mode: 'HYBRID',
  priority: 'HIGH',
};

export function IntentCapturePage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [draft, setDraft] = useState(initialDraft);
  const [createdIntent, setCreatedIntent] = useState<Intent | null>(null);
  const [chatInput, setChatInput] = useState('');
  const [messages, setMessages] = useState<Array<{ role: 'ai' | 'user'; text: string }>>([
    { role: 'ai', text: "Hi! Tell me about the role you'd like to fill — title, must-have skills, headcount, timeline. I'll structure it on the right." },
  ]);

  const createMutation = useMutation({
    mutationFn: (body: CreateIntentRequest) => intentApi.create(body),
    onSuccess: (intent) => {
      setCreatedIntent(intent);
      setMessages((prev) => [...prev, { role: 'ai', text: 'Drafted! Review on the right and click Confirm Intent when ready.' }]);
      qc.invalidateQueries({ queryKey: ['intents'] });
    },
  });

  const confirmMutation = useMutation({
    mutationFn: (id: string) => intentApi.confirm(id),
    onSuccess: (intent) => {
      qc.invalidateQueries({ queryKey: ['intents'] });
      qc.invalidateQueries({ queryKey: ['postings'] });
      navigate(`/intents/${intent.id}`);
    },
  });

  const sendMessage = () => {
    if (!chatInput.trim()) return;
    setMessages((prev) => [
      ...prev,
      { role: 'user', text: chatInput.trim() },
      { role: 'ai', text: "I've updated the draft on the right. Edit the fields directly, or tell me what to change." },
    ]);
    setChatInput('');
  };

  return (
    <div className="grid grid-cols-1 lg:grid-cols-[1fr_420px] h-screen">
      {/* Chat */}
      <div className="flex flex-col border-r border-line bg-white">
        <header className="px-6 py-4 border-b border-line flex items-center gap-3">
          <div className="w-9 h-9 rounded-lg bg-accent flex items-center justify-center">
            <Bot className="w-5 h-5 text-white" />
          </div>
          <div>
            <h2 className="text-sm font-bold text-ink">AI Recruitment Assistant</h2>
            <div className="flex items-center gap-1.5 text-[11px] text-green-600">
              <span className="w-1.5 h-1.5 rounded-full bg-green-500" /> Active
            </div>
          </div>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-6 space-y-4">
          {messages.map((m, i) => (
            <div key={i} className={m.role === 'user' ? 'flex justify-end' : 'flex'}>
              <div className="max-w-[80%]">
                {m.role === 'ai' && (
                  <div className="flex items-center gap-1.5 mb-1">
                    <Sparkles className="w-3 h-3 text-accent" />
                    <span className="text-[10px] font-bold text-ink-mute uppercase tracking-wider">AI Assistant</span>
                  </div>
                )}
                <div className={
                  m.role === 'user'
                    ? 'rounded-xl px-4 py-2.5 bg-accent text-white text-sm'
                    : 'rounded-xl px-4 py-2.5 bg-line-soft text-ink text-sm'
                }>
                  {m.text}
                </div>
              </div>
            </div>
          ))}
        </div>

        <div className="px-6 py-4 border-t border-line">
          <div className="flex items-center gap-2 bg-line-soft border border-line rounded-lg px-3 h-11">
            <input
              value={chatInput}
              onChange={(e) => setChatInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') sendMessage(); }}
              placeholder="Describe the role or ask anything..."
              className="flex-1 bg-transparent text-sm focus:outline-none"
            />
            <button
              onClick={sendMessage}
              disabled={!chatInput.trim()}
              className="w-7 h-7 rounded-md bg-accent text-white flex items-center justify-center disabled:opacity-30"
            >
              <Send className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>
      </div>

      {/* Draft / Intent panel */}
      <div className="overflow-y-auto px-6 py-6 bg-line-soft">
        {createdIntent ? (
          <IntentCard
            intent={createdIntent}
            onConfirm={() => confirmMutation.mutate(createdIntent.id)}
            onEdit={() => setCreatedIntent(null)}
            confirming={confirmMutation.isPending}
          />
        ) : (
          <Card className="p-5 space-y-4">
            <h3 className="text-sm font-bold text-ink">Draft from chat</h3>
            <DraftForm draft={draft} onChange={setDraft} />
            <Button
              onClick={() => createMutation.mutate(draft)}
              disabled={createMutation.isPending || !draft.role_title.trim()}
              className="w-full"
            >
              {createMutation.isPending ? <Spinner /> : <Sparkles className="w-4 h-4" />}
              Create Draft Intent
            </Button>
            {createMutation.isError && (
              <p className="text-xs text-red-600">{(createMutation.error as Error).message}</p>
            )}
          </Card>
        )}
      </div>
    </div>
  );
}

function DraftForm({ draft, onChange }: { draft: CreateIntentRequest; onChange: (d: CreateIntentRequest) => void }) {
  return (
    <div className="space-y-3">
      <Field label="Role title">
        <Input value={draft.role_title} onChange={(e) => onChange({ ...draft, role_title: e.target.value })} />
      </Field>
      <Field label="Required skills (comma-separated)">
        <Input
          value={draft.skills.map((s) => s.name).join(', ')}
          onChange={(e) =>
            onChange({
              ...draft,
              skills: e.target.value.split(',').map((n, i) => ({ name: n.trim(), required: i < 2 })).filter((s) => s.name),
            })
          }
        />
      </Field>
      <div className="grid grid-cols-2 gap-2">
        <Field label="Min years">
          <Input type="number" value={draft.min_years} onChange={(e) => onChange({ ...draft, min_years: Number(e.target.value) })} />
        </Field>
        <Field label="Max years">
          <Input type="number" value={draft.max_years} onChange={(e) => onChange({ ...draft, max_years: Number(e.target.value) })} />
        </Field>
        <Field label="Headcount">
          <Input type="number" value={draft.headcount} onChange={(e) => onChange({ ...draft, headcount: Number(e.target.value) })} />
        </Field>
        <Field label="Work mode">
          <select
            value={draft.work_mode}
            onChange={(e) => onChange({ ...draft, work_mode: e.target.value as CreateIntentRequest['work_mode'] })}
            className="w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent"
          >
            <option value="ONSITE">On-site</option>
            <option value="REMOTE">Remote</option>
            <option value="HYBRID">Hybrid</option>
          </select>
        </Field>
      </div>
      <Field label="Priority">
        <select
          value={draft.priority}
          onChange={(e) => onChange({ ...draft, priority: e.target.value as CreateIntentRequest['priority'] })}
          className="w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent"
        >
          <option value="LOW">Low</option>
          <option value="MEDIUM">Medium</option>
          <option value="HIGH">High</option>
          <option value="CRITICAL">Critical</option>
        </select>
      </Field>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-ink-sub mb-1">{label}</label>
      {children}
    </div>
  );
}

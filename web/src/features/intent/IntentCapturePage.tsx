import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Bot, Send, Sparkles } from 'lucide-react';
import { intentApi } from '@/api/intent';
import { ApiError } from '@/api/client';
import type {
  CreateIntentRequest,
  DraftPatch,
  ExtractMessage,
  ExtractResponse,
  Intent,
  Priority,
  WorkMode,
} from '@/api/types';
import { Card, Button, Spinner, Input } from '@/components/ui/primitives';
import { IntentCard } from './IntentCard';

const emptyDraft: CreateIntentRequest = {
  role_title: '',
  skills: [],
  min_years: 0,
  max_years: 0,
  headcount: 0,
  locations: [],
  work_mode: 'HYBRID',
  priority: 'MEDIUM',
  reason: '',
  team: '',
  reports_to: '',
};

const greeting = "Hi! Tell me about the role you'd like to fill — title, must-have skills, headcount, timeline. I'll structure it on the right.";

// extractErrorMessage maps the API error code to recruiter-facing copy.
// Each case is a real failure mode reported by the backend's respondExtractError;
// the default catches both unmapped codes and non-API errors (network, etc).
function extractErrorMessage(err: unknown): { text: string; offline: boolean } {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'llm_billing':
        return { text: "AI workspace is out of credits — an admin needs to top up at console.anthropic.com. You can still edit the form on the right.", offline: true };
      case 'llm_auth_error':
        return { text: "AI service authentication is misconfigured. The form on the right still works.", offline: true };
      case 'llm_permission_error':
        return { text: "AI workspace can't access the configured model. The form on the right still works.", offline: true };
      case 'llm_rate_limited':
        return { text: "AI is rate-limited right now — wait a moment and try sending again.", offline: false };
      case 'llm_overloaded':
        return { text: "AI is overloaded right now — try again in a moment.", offline: false };
      case 'llm_timeout':
        return { text: "AI didn't respond in time — try again.", offline: false };
      case 'llm_response_error':
        return { text: "AI returned an unexpected response — try again or edit the form on the right.", offline: false };
      case 'llm_unavailable':
        return { text: "AI is offline right now — you can still edit the form on the right.", offline: true };
      case 'message_too_long':
        return { text: "That message is too long — please keep it under 4000 characters.", offline: false };
      case 'user_message_required':
        return { text: "Please enter a message.", offline: false };
      default:
        return { text: `Something went wrong: ${err.message}`, offline: false };
    }
  }
  return { text: `Something went wrong: ${(err as Error).message}`, offline: false };
}

// applyPatch merges the LLM's sparse patch onto the draft. Only fields
// present in the patch overwrite the draft; everything else is left alone.
function applyPatch(draft: CreateIntentRequest, patch: DraftPatch): CreateIntentRequest {
  return {
    role_title: patch.role_title ?? draft.role_title,
    skills:     patch.skills     ?? draft.skills,
    min_years:  patch.min_years  ?? draft.min_years,
    max_years:  patch.max_years  ?? draft.max_years,
    headcount:  patch.headcount  ?? draft.headcount,
    locations:  patch.locations  ?? draft.locations,
    work_mode:  (patch.work_mode as WorkMode) ?? draft.work_mode,
    priority:   (patch.priority  as Priority) ?? draft.priority,
    budget:     patch.budget     ?? draft.budget,
    reason:     patch.reason     ?? draft.reason,
    team:       patch.team       ?? draft.team,
    reports_to: patch.reports_to ?? draft.reports_to,
  };
}

export function IntentCapturePage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [draft, setDraft] = useState<CreateIntentRequest>(emptyDraft);
  const [createdIntent, setCreatedIntent] = useState<Intent | null>(null);
  const [chatInput, setChatInput] = useState('');
  const [messages, setMessages] = useState<ExtractMessage[]>([{ role: 'assistant', text: greeting }]);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [llmOffline, setLlmOffline] = useState(false);
  const [complete, setComplete] = useState(false);

  const extractMutation = useMutation({
    mutationFn: (userMessage: string) =>
      intentApi.extract({
        // Server expects history excluding the current user turn — strip the
        // greeting (it's a UI-only welcome, not an LLM-relevant turn).
        messages: messages.filter((m) => m.text !== greeting),
        draft: {
          role_title: draft.role_title || undefined,
          skills: draft.skills.length ? draft.skills : undefined,
          min_years: draft.min_years || undefined,
          max_years: draft.max_years || undefined,
          headcount: draft.headcount || undefined,
          locations: draft.locations.length ? draft.locations : undefined,
          work_mode: draft.work_mode,
          priority: draft.priority,
          budget: draft.budget,
          reason: draft.reason || undefined,
          team: draft.team || undefined,
          reports_to: draft.reports_to || undefined,
        },
        user_message: userMessage,
      }),
    onSuccess: (resp: ExtractResponse) => {
      setLlmOffline(false);
      setMessages((prev) => [...prev, { role: 'assistant', text: resp.reply }]);
      setDraft((prev) => applyPatch(prev, resp.patch));
      setWarnings(resp.warnings ?? []);
      setComplete(resp.complete);
    },
    onError: (err) => {
      const { text, offline } = extractErrorMessage(err);
      setLlmOffline(offline);
      setMessages((prev) => [...prev, { role: 'assistant', text }]);
    },
  });

  const createMutation = useMutation({
    mutationFn: (body: CreateIntentRequest) => intentApi.create(body),
    onSuccess: (intent) => {
      setCreatedIntent(intent);
      setMessages((prev) => [...prev, { role: 'assistant', text: 'Drafted! Review on the right and click Confirm Intent when ready.' }]);
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
    const text = chatInput.trim();
    if (!text || extractMutation.isPending) return;
    setMessages((prev) => [...prev, { role: 'user', text }]);
    setChatInput('');
    extractMutation.mutate(text);
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
            <div className={`flex items-center gap-1.5 text-[11px] ${llmOffline ? 'text-amber-600' : 'text-green-600'}`}>
              <span className={`w-1.5 h-1.5 rounded-full ${llmOffline ? 'bg-amber-500' : 'bg-green-500'}`} />
              {llmOffline ? 'Offline — form still editable' : 'Active'}
            </div>
          </div>
        </header>

        <div className="flex-1 overflow-y-auto px-6 py-6 space-y-4">
          {messages.map((m, i) => (
            <div key={i} className={m.role === 'user' ? 'flex justify-end' : 'flex'}>
              <div className="max-w-[80%]">
                {m.role === 'assistant' && (
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
          {extractMutation.isPending && (
            <div className="flex">
              <div className="rounded-xl px-4 py-2.5 bg-line-soft text-ink text-sm">
                <span className="inline-flex gap-1">
                  <span className="w-1.5 h-1.5 rounded-full bg-ink-mute animate-pulse" />
                  <span className="w-1.5 h-1.5 rounded-full bg-ink-mute animate-pulse [animation-delay:150ms]" />
                  <span className="w-1.5 h-1.5 rounded-full bg-ink-mute animate-pulse [animation-delay:300ms]" />
                </span>
              </div>
            </div>
          )}
          {warnings.length > 0 && (
            <div className="text-[11px] text-amber-600 px-1">
              {warnings.map((w, i) => <div key={i}>· {w}</div>)}
            </div>
          )}
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
              disabled={!chatInput.trim() || extractMutation.isPending}
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
            <h3 className="text-sm font-bold text-ink">
              Draft from chat {complete && <span className="text-[11px] text-green-600 font-normal">· ready to create</span>}
            </h3>
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
      <BudgetField draft={draft} onChange={onChange} />
      <Field label="Reason for hire">
        <Input
          placeholder="e.g. Backfill, growth, new product line..."
          value={draft.reason ?? ''}
          onChange={(e) => onChange({ ...draft, reason: e.target.value })}
        />
      </Field>
      <div className="grid grid-cols-2 gap-2">
        <Field label="Team">
          <Input
            placeholder="Payments Platform"
            value={draft.team ?? ''}
            onChange={(e) => onChange({ ...draft, team: e.target.value })}
          />
        </Field>
        <Field label="Reports to">
          <Input
            placeholder="Aisha Khan"
            value={draft.reports_to ?? ''}
            onChange={(e) => onChange({ ...draft, reports_to: e.target.value })}
          />
        </Field>
      </div>
    </div>
  );
}

// SUPPORTED_CURRENCIES drives both the dropdown and the unit shown next to
// the amount inputs. Each entry knows the multiplier from its display unit
// to the minor unit on the wire (paise/cents/etc). INR uses lakhs because
// that's how recruiters quote — 60 LPA, not 60,00,000.
const SUPPORTED_CURRENCIES = [
  { code: 'INR', symbol: '₹', unitLabel: 'LPA',  toMinor: 100_00_000 }, // 1 lakh = 100,000 rupees = 10,000,000 paise
  { code: 'USD', symbol: '$', unitLabel: 'k/yr', toMinor: 100_000 },    // 1k = 1000 dollars = 100,000 cents
  { code: 'EUR', symbol: '€', unitLabel: 'k/yr', toMinor: 100_000 },
  { code: 'GBP', symbol: '£', unitLabel: 'k/yr', toMinor: 100_000 },
] as const;

type CurrencyCode = (typeof SUPPORTED_CURRENCIES)[number]['code'];

function unitFor(code: string) {
  return SUPPORTED_CURRENCIES.find((c) => c.code === code) ?? SUPPORTED_CURRENCIES[0];
}

function BudgetField({ draft, onChange }: { draft: CreateIntentRequest; onChange: (d: CreateIntentRequest) => void }) {
  const currency = (draft.budget?.currency as CurrencyCode) ?? 'INR';
  const unit = unitFor(currency);
  const minDisplay = draft.budget ? draft.budget.min_minor / unit.toMinor : 0;
  const maxDisplay = draft.budget ? draft.budget.max_minor / unit.toMinor : 0;

  const update = (next: { min?: number; max?: number; code?: string }) => {
    const code = next.code ?? currency;
    const nextUnit = unitFor(code);
    const min = next.min ?? minDisplay;
    const max = next.max ?? maxDisplay;
    if (!min && !max) {
      onChange({ ...draft, budget: undefined });
      return;
    }
    onChange({
      ...draft,
      budget: {
        min_minor: Math.round(min * nextUnit.toMinor),
        max_minor: Math.round(max * nextUnit.toMinor),
        currency: code,
      },
    });
  };

  return (
    <div className="pt-1">
      <label className="block text-[10px] font-bold uppercase tracking-wider text-ink-sub mb-1">
        Budget <span className="font-normal lowercase text-ink-mute">(optional)</span>
      </label>
      <div className="grid grid-cols-[1fr_1fr_auto] gap-2 items-center">
        <Input
          type="number"
          min={0}
          step="0.5"
          placeholder={`Min (${unit.unitLabel})`}
          value={minDisplay || ''}
          onChange={(e) => update({ min: Number(e.target.value) })}
        />
        <Input
          type="number"
          min={0}
          step="0.5"
          placeholder={`Max (${unit.unitLabel})`}
          value={maxDisplay || ''}
          onChange={(e) => update({ max: Number(e.target.value) })}
        />
        <select
          value={currency}
          onChange={(e) => update({ code: e.target.value })}
          className="h-10 px-2 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent"
        >
          {SUPPORTED_CURRENCIES.map((c) => (
            <option key={c.code} value={c.code}>{c.symbol} {c.code}</option>
          ))}
        </select>
      </div>
      <p className="text-[10px] text-ink-mute mt-1">
        {unit.code === 'INR' ? 'Lakhs Per Annum (e.g. 40 = ₹40,00,000/year)' : 'Thousands per year'}
      </p>
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

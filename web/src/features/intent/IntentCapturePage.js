import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Bot, Send, Sparkles } from 'lucide-react';
import { intentApi } from '@/api/intent';
import { Card, Button, Spinner, Input } from '@/components/ui/primitives';
import { IntentCard } from './IntentCard';
// Demo extraction: we don't have an LLM hooked up yet, so the recruiter
// fills a small form on the right that doubles as the chat-extracted
// "structured intent". When the auth backend + LLM service land, replace
// the form with a real chat → extract → propose flow.
const initialDraft = {
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
    const [createdIntent, setCreatedIntent] = useState(null);
    const [chatInput, setChatInput] = useState('');
    const [messages, setMessages] = useState([
        { role: 'ai', text: "Hi! Tell me about the role you'd like to fill — title, must-have skills, headcount, timeline. I'll structure it on the right." },
    ]);
    const createMutation = useMutation({
        mutationFn: (body) => intentApi.create(body),
        onSuccess: (intent) => {
            setCreatedIntent(intent);
            setMessages((prev) => [...prev, { role: 'ai', text: 'Drafted! Review on the right and click Confirm Intent when ready.' }]);
            qc.invalidateQueries({ queryKey: ['intents'] });
        },
    });
    const confirmMutation = useMutation({
        mutationFn: (id) => intentApi.confirm(id),
        onSuccess: (intent) => {
            qc.invalidateQueries({ queryKey: ['intents'] });
            qc.invalidateQueries({ queryKey: ['postings'] });
            navigate(`/intents/${intent.id}`);
        },
    });
    const sendMessage = () => {
        if (!chatInput.trim())
            return;
        setMessages((prev) => [
            ...prev,
            { role: 'user', text: chatInput.trim() },
            { role: 'ai', text: "I've updated the draft on the right. Edit the fields directly, or tell me what to change." },
        ]);
        setChatInput('');
    };
    return (_jsxs("div", { className: "grid grid-cols-1 lg:grid-cols-[1fr_420px] h-screen", children: [_jsxs("div", { className: "flex flex-col border-r border-line bg-white", children: [_jsxs("header", { className: "px-6 py-4 border-b border-line flex items-center gap-3", children: [_jsx("div", { className: "w-9 h-9 rounded-lg bg-accent flex items-center justify-center", children: _jsx(Bot, { className: "w-5 h-5 text-white" }) }), _jsxs("div", { children: [_jsx("h2", { className: "text-sm font-bold text-ink", children: "AI Recruitment Assistant" }), _jsxs("div", { className: "flex items-center gap-1.5 text-[11px] text-green-600", children: [_jsx("span", { className: "w-1.5 h-1.5 rounded-full bg-green-500" }), " Active"] })] })] }), _jsx("div", { className: "flex-1 overflow-y-auto px-6 py-6 space-y-4", children: messages.map((m, i) => (_jsx("div", { className: m.role === 'user' ? 'flex justify-end' : 'flex', children: _jsxs("div", { className: "max-w-[80%]", children: [m.role === 'ai' && (_jsxs("div", { className: "flex items-center gap-1.5 mb-1", children: [_jsx(Sparkles, { className: "w-3 h-3 text-accent" }), _jsx("span", { className: "text-[10px] font-bold text-ink-mute uppercase tracking-wider", children: "AI Assistant" })] })), _jsx("div", { className: m.role === 'user'
                                            ? 'rounded-xl px-4 py-2.5 bg-accent text-white text-sm'
                                            : 'rounded-xl px-4 py-2.5 bg-line-soft text-ink text-sm', children: m.text })] }) }, i))) }), _jsx("div", { className: "px-6 py-4 border-t border-line", children: _jsxs("div", { className: "flex items-center gap-2 bg-line-soft border border-line rounded-lg px-3 h-11", children: [_jsx("input", { value: chatInput, onChange: (e) => setChatInput(e.target.value), onKeyDown: (e) => { if (e.key === 'Enter')
                                        sendMessage(); }, placeholder: "Describe the role or ask anything...", className: "flex-1 bg-transparent text-sm focus:outline-none" }), _jsx("button", { onClick: sendMessage, disabled: !chatInput.trim(), className: "w-7 h-7 rounded-md bg-accent text-white flex items-center justify-center disabled:opacity-30", children: _jsx(Send, { className: "w-3.5 h-3.5" }) })] }) })] }), _jsx("div", { className: "overflow-y-auto px-6 py-6 bg-line-soft", children: createdIntent ? (_jsx(IntentCard, { intent: createdIntent, onConfirm: () => confirmMutation.mutate(createdIntent.id), onEdit: () => setCreatedIntent(null), confirming: confirmMutation.isPending })) : (_jsxs(Card, { className: "p-5 space-y-4", children: [_jsx("h3", { className: "text-sm font-bold text-ink", children: "Draft from chat" }), _jsx(DraftForm, { draft: draft, onChange: setDraft }), _jsxs(Button, { onClick: () => createMutation.mutate(draft), disabled: createMutation.isPending || !draft.role_title.trim(), className: "w-full", children: [createMutation.isPending ? _jsx(Spinner, {}) : _jsx(Sparkles, { className: "w-4 h-4" }), "Create Draft Intent"] }), createMutation.isError && (_jsx("p", { className: "text-xs text-red-600", children: createMutation.error.message }))] })) })] }));
}
function DraftForm({ draft, onChange }) {
    return (_jsxs("div", { className: "space-y-3", children: [_jsx(Field, { label: "Role title", children: _jsx(Input, { value: draft.role_title, onChange: (e) => onChange({ ...draft, role_title: e.target.value }) }) }), _jsx(Field, { label: "Required skills (comma-separated)", children: _jsx(Input, { value: draft.skills.map((s) => s.name).join(', '), onChange: (e) => onChange({
                        ...draft,
                        skills: e.target.value.split(',').map((n, i) => ({ name: n.trim(), required: i < 2 })).filter((s) => s.name),
                    }) }) }), _jsxs("div", { className: "grid grid-cols-2 gap-2", children: [_jsx(Field, { label: "Min years", children: _jsx(Input, { type: "number", value: draft.min_years, onChange: (e) => onChange({ ...draft, min_years: Number(e.target.value) }) }) }), _jsx(Field, { label: "Max years", children: _jsx(Input, { type: "number", value: draft.max_years, onChange: (e) => onChange({ ...draft, max_years: Number(e.target.value) }) }) }), _jsx(Field, { label: "Headcount", children: _jsx(Input, { type: "number", value: draft.headcount, onChange: (e) => onChange({ ...draft, headcount: Number(e.target.value) }) }) }), _jsx(Field, { label: "Work mode", children: _jsxs("select", { value: draft.work_mode, onChange: (e) => onChange({ ...draft, work_mode: e.target.value }), className: "w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent", children: [_jsx("option", { value: "ONSITE", children: "On-site" }), _jsx("option", { value: "REMOTE", children: "Remote" }), _jsx("option", { value: "HYBRID", children: "Hybrid" })] }) })] }), _jsx(Field, { label: "Priority", children: _jsxs("select", { value: draft.priority, onChange: (e) => onChange({ ...draft, priority: e.target.value }), className: "w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent", children: [_jsx("option", { value: "LOW", children: "Low" }), _jsx("option", { value: "MEDIUM", children: "Medium" }), _jsx("option", { value: "HIGH", children: "High" }), _jsx("option", { value: "CRITICAL", children: "Critical" })] }) })] }));
}
function Field({ label, children }) {
    return (_jsxs("div", { children: [_jsx("label", { className: "block text-[10px] font-bold uppercase tracking-wider text-ink-sub mb-1", children: label }), children] }));
}

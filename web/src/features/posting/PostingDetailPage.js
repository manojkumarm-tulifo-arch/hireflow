import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Linkedin, Globe, Mail, Database, XCircle } from 'lucide-react';
import { postingApi } from '@/api/posting';
import { Button, Card, Spinner } from '@/components/ui/primitives';
import { PostingStatusBadge } from '@/components/ui/StatusBadge';
const CHANNELS = [
    { id: 'LINKEDIN', label: 'LinkedIn', icon: Linkedin },
    { id: 'CAREER_PAGE', label: 'Career Page', icon: Globe },
    { id: 'EMAIL', label: 'Email Inbox', icon: Mail },
    { id: 'INTERNAL_DB', label: 'Internal DB', icon: Database },
];
export function PostingDetailPage() {
    const { id = '' } = useParams();
    const qc = useQueryClient();
    const [selected, setSelected] = useState(new Set());
    const [closeReason, setCloseReason] = useState('');
    const { data: posting, isLoading } = useQuery({
        queryKey: ['posting', id],
        queryFn: () => postingApi.get(id),
        enabled: !!id,
    });
    const publishMutation = useMutation({
        mutationFn: (channels) => postingApi.publish(id, channels),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['posting', id] });
            qc.invalidateQueries({ queryKey: ['postings'] });
            setSelected(new Set());
        },
    });
    const closeMutation = useMutation({
        mutationFn: () => postingApi.close(id, closeReason),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['posting', id] });
            qc.invalidateQueries({ queryKey: ['postings'] });
        },
    });
    if (isLoading)
        return _jsx("div", { className: "p-12 flex justify-center", children: _jsx(Spinner, {}) });
    if (!posting)
        return _jsx("div", { className: "p-8", children: "Not found." });
    const isTerminal = posting.status === 'CLOSED' || posting.status === 'ARCHIVED';
    const existingChannels = new Set(posting.sources.map((s) => s.channel));
    const toggle = (c) => {
        const copy = new Set(selected);
        if (copy.has(c))
            copy.delete(c);
        else
            copy.add(c);
        setSelected(copy);
    };
    return (_jsxs("div", { className: "px-8 py-6 max-w-3xl space-y-6", children: [_jsxs(Link, { to: "/postings", className: "inline-flex items-center gap-1 text-xs text-ink-sub hover:text-ink", children: [_jsx(ArrowLeft, { className: "w-3.5 h-3.5" }), " Back to postings"] }), _jsxs(Card, { className: "p-6 space-y-4", children: [_jsxs("div", { className: "flex items-start justify-between", children: [_jsxs("div", { children: [_jsx("h1", { className: "text-xl font-bold text-ink", children: posting.jd.title }), _jsxs("p", { className: "text-xs text-ink-mute mt-1", children: ["JD v", posting.jd.version, " \u00B7 intent ", posting.intent_id] })] }), _jsx(PostingStatusBadge, { status: posting.status })] }), _jsx("p", { className: "text-sm text-ink-sub", children: posting.jd.summary }), posting.jd.responsibilities.length > 0 && (_jsx(Section, { title: "Responsibilities", items: posting.jd.responsibilities })), posting.jd.requirements.length > 0 && (_jsx(Section, { title: "Requirements", items: posting.jd.requirements }))] }), _jsxs(Card, { className: "p-6 space-y-4", children: [_jsx("h2", { className: "text-sm font-bold text-ink", children: "Source Distribution" }), posting.sources.length > 0 && (_jsx("div", { className: "space-y-1.5", children: posting.sources.map((s) => {
                            const Icon = CHANNELS.find((c) => c.id === s.channel)?.icon ?? Globe;
                            return (_jsxs("div", { className: "flex items-center justify-between px-3 py-2 rounded bg-line-soft", children: [_jsxs("div", { className: "flex items-center gap-2", children: [_jsx(Icon, { className: "w-4 h-4 text-accent" }), _jsx("span", { className: "text-sm font-semibold", children: s.channel })] }), _jsx("span", { className: "text-[11px] font-bold uppercase tracking-wider text-ink-sub", children: s.status })] }, s.channel));
                        }) })), !isTerminal && (_jsxs("div", { className: "space-y-3 pt-2 border-t border-line", children: [_jsx("p", { className: "text-xs text-ink-sub", children: "Add new channels to distribute to:" }), _jsx("div", { className: "grid grid-cols-2 gap-2", children: CHANNELS.map(({ id: c, label, icon: Icon }) => {
                                    const already = existingChannels.has(c);
                                    const isOn = selected.has(c);
                                    return (_jsxs("button", { disabled: already, onClick: () => toggle(c), className: `flex items-center gap-2 px-3 py-2 rounded-md border text-sm font-semibold transition-colors ${already
                                            ? 'border-line bg-line-soft text-ink-mute cursor-not-allowed'
                                            : isOn
                                                ? 'border-accent bg-accent-soft text-accent'
                                                : 'border-line bg-white text-ink hover:border-accent'}`, children: [_jsx(Icon, { className: "w-4 h-4" }), label, already && _jsx("span", { className: "ml-auto text-[10px]", children: "added" })] }, c));
                                }) }), _jsxs(Button, { onClick: () => publishMutation.mutate(Array.from(selected)), disabled: selected.size === 0 || publishMutation.isPending, className: "w-full", children: [publishMutation.isPending ? _jsx(Spinner, {}) : null, "Publish to ", selected.size, " channel", selected.size === 1 ? '' : 's'] }), publishMutation.isError && (_jsx("p", { className: "text-xs text-red-600", children: publishMutation.error.message }))] }))] }), !isTerminal && (_jsxs(Card, { className: "p-6 space-y-3", children: [_jsx("h2", { className: "text-sm font-bold text-ink", children: "Close Posting" }), _jsx("input", { value: closeReason, onChange: (e) => setCloseReason(e.target.value), placeholder: "Reason (e.g., position filled)", className: "w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent" }), _jsxs(Button, { variant: "danger", onClick: () => closeMutation.mutate(), disabled: !closeReason.trim() || closeMutation.isPending, className: "w-full", children: [closeMutation.isPending ? _jsx(Spinner, {}) : _jsx(XCircle, { className: "w-4 h-4" }), "Close Posting"] })] })), posting.close_reason && (_jsxs(Card, { className: "p-4 text-sm text-ink-sub", children: [_jsx("span", { className: "font-semibold text-ink", children: "Closed:" }), " ", posting.close_reason] }))] }));
}
function Section({ title, items }) {
    return (_jsxs("div", { children: [_jsx("h3", { className: "text-[10px] font-bold uppercase tracking-widest text-ink-sub mb-1.5", children: title }), _jsx("ul", { className: "list-disc list-inside text-sm text-ink space-y-0.5", children: items.map((item, i) => _jsx("li", { children: item }, i)) })] }));
}

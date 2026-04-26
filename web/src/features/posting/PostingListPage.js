import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight } from 'lucide-react';
import { postingApi } from '@/api/posting';
import { Card, EmptyState, Spinner } from '@/components/ui/primitives';
import { PostingStatusBadge } from '@/components/ui/StatusBadge';
const STATUSES = ['ALL', 'DRAFT', 'PUBLISHED', 'CLOSED', 'ARCHIVED'];
export function PostingListPage() {
    const [filter, setFilter] = useState('ALL');
    const { data, isLoading, error } = useQuery({
        queryKey: ['postings', filter],
        queryFn: () => postingApi.list(filter === 'ALL' ? undefined : { status: filter }),
    });
    return (_jsxs("div", { className: "px-8 py-6 max-w-5xl space-y-6", children: [_jsxs("div", { children: [_jsx("h1", { className: "text-2xl font-bold text-ink", children: "Job Postings" }), _jsx("p", { className: "text-sm text-ink-sub mt-0.5", children: "Drafted automatically when intents are confirmed. Publish to one or more channels to start sourcing." })] }), _jsx("div", { className: "flex gap-1.5", children: STATUSES.map((s) => (_jsx("button", { onClick: () => setFilter(s), className: `px-3 py-1.5 rounded-full text-xs font-bold uppercase tracking-wider ${filter === s ? 'bg-accent text-white' : 'bg-white border border-line text-ink-sub hover:text-ink'}`, children: s }, s))) }), isLoading && _jsx("div", { className: "flex justify-center py-12", children: _jsx(Spinner, {}) }), error && _jsx(Card, { className: "p-4 text-sm text-red-600", children: error.message }), data && data.length === 0 && (_jsx(EmptyState, { title: "No postings yet", hint: "Confirm a hiring intent to draft a posting automatically." })), data && data.length > 0 && (_jsx("div", { className: "space-y-2", children: data.map((p) => (_jsx(Link, { to: `/postings/${p.id}`, children: _jsxs(Card, { className: "px-5 py-4 flex items-center justify-between hover:border-accent transition-colors", children: [_jsxs("div", { children: [_jsxs("div", { className: "flex items-center gap-3", children: [_jsx("h3", { className: "text-sm font-bold text-ink", children: p.jd.title }), _jsx(PostingStatusBadge, { status: p.status }), _jsxs("span", { className: "text-[11px] text-ink-mute font-mono", children: ["v", p.jd.version] })] }), _jsx("p", { className: "text-xs text-ink-sub mt-1", children: p.sources.length === 0
                                            ? 'Not yet distributed'
                                            : `${p.sources.length} source${p.sources.length === 1 ? '' : 's'}: ${p.sources.map((s) => s.channel).join(', ')}` })] }), _jsx(ChevronRight, { className: "w-4 h-4 text-ink-mute" })] }) }, p.id))) }))] }));
}

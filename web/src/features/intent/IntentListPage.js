import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowDownAZ, BriefcaseBusiness, ChevronRight, Flame, GraduationCap, Hash, Loader2, MapPin, Plus, Search, Sparkles, Target, Zap, } from 'lucide-react';
import { intentApi } from '@/api/intent';
import { Button, EmptyState } from '@/components/ui/primitives';
import { IntentStatusBadge } from '@/components/ui/StatusBadge';
import { cn } from '@/lib/cn';
const STATUS_KEYS = ['ALL', 'DRAFTED', 'CONFIRMED', 'CANCELLED', 'CLOSED'];
const STATUS_COPY = {
    ALL: 'All',
    DRAFTED: 'Drafted',
    CONFIRMED: 'Confirmed',
    CANCELLED: 'Cancelled',
    CLOSED: 'Closed',
};
const SORT_OPTIONS = [
    { value: 'NEWEST', label: 'Newest', icon: ArrowDownAZ },
    { value: 'URGENT', label: 'Urgent first', icon: Zap },
];
export function IntentListPage() {
    const [statusFilter, setStatusFilter] = useState('ALL');
    const [search, setSearch] = useState('');
    const [sort, setSort] = useState('NEWEST');
    const summaryQuery = useQuery({
        queryKey: ['intents', 'summary'],
        queryFn: () => intentApi.summary(),
        staleTime: 15_000,
    });
    const listQuery = useQuery({
        queryKey: ['intents', { statusFilter, search, sort }],
        queryFn: () => intentApi.list({
            status: statusFilter === 'ALL' ? undefined : statusFilter,
            q: search.trim() || undefined,
            sort,
        }),
    });
    const counts = summaryQuery.data?.counts;
    return (_jsxs("div", { className: "px-8 py-6 space-y-5 max-w-6xl", children: [_jsxs("header", { className: "flex items-start justify-between gap-4", children: [_jsxs("div", { children: [_jsx("h1", { className: "font-display text-3xl font-bold text-ink", children: "Hiring Intents" }), _jsx("p", { className: "text-sm text-ink-sub mt-1", children: "Every role your team is hiring for \u2014 drafts, confirmed, and historical." })] }), _jsx(Link, { to: "/intents/new", children: _jsxs(Button, { children: [_jsx(Plus, { className: "w-4 h-4" }), " New Intent"] }) })] }), _jsx(SummaryStrip, { counts: counts, loading: summaryQuery.isLoading }), _jsxs("div", { className: "flex flex-wrap items-center gap-2", children: [_jsx(FilterChips, { counts: counts, active: statusFilter, onChange: setStatusFilter }), _jsx("div", { className: "flex-1" }), _jsx(SearchInput, { value: search, onChange: setSearch }), _jsx(SortPicker, { value: sort, onChange: setSort })] }), _jsx(ResultGrid, { query: listQuery })] }));
}
function SummaryStrip({ counts, loading }) {
    const tiles = useMemo(() => [
        { label: 'Total', value: counts?.total, icon: Sparkles, tone: 'text-accent bg-accent-soft' },
        { label: 'Drafted', value: counts?.DRAFTED, icon: Target, tone: 'text-amber-700 bg-amber-50' },
        { label: 'Confirmed', value: counts?.CONFIRMED, icon: BriefcaseBusiness, tone: 'text-green-700 bg-green-50' },
        { label: 'Closed', value: counts?.CLOSED, icon: Hash, tone: 'text-ink-sub bg-line-soft' },
    ], [counts]);
    return (_jsx("div", { className: "grid grid-cols-2 sm:grid-cols-4 gap-2.5", children: tiles.map((t) => {
            const Icon = t.icon;
            return (_jsxs("div", { className: "rounded-[12px] bg-white border border-line p-3.5 flex items-center gap-3", children: [_jsx("div", { className: cn('w-9 h-9 rounded-[10px] flex items-center justify-center', t.tone), children: _jsx(Icon, { className: "w-4 h-4" }) }), _jsxs("div", { children: [_jsx("div", { className: "text-[10px] font-bold uppercase tracking-wider text-ink-sub", children: t.label }), _jsx("div", { className: "text-xl font-bold text-ink leading-none mt-0.5", children: loading ? _jsx("span", { className: "text-ink-mute", children: "\u2014" }) : (t.value ?? 0) })] })] }, t.label));
        }) }));
}
function FilterChips({ counts, active, onChange, }) {
    return (_jsx("div", { className: "flex flex-wrap gap-1.5", children: STATUS_KEYS.map((s) => {
            const n = !counts ? null : (s === 'ALL' ? counts.total : counts[s]);
            const isActive = active === s;
            return (_jsxs("button", { onClick: () => onChange(s), className: cn('h-8 px-3 inline-flex items-center gap-1.5 rounded-full text-[11px] font-bold uppercase tracking-wider transition-colors border', isActive
                    ? 'bg-accent text-white border-accent'
                    : 'bg-white border-line text-ink-sub hover:text-ink hover:border-ink-mute'), children: [STATUS_COPY[s], n != null && (_jsx("span", { className: cn('min-w-[18px] h-[18px] px-1 rounded-full inline-flex items-center justify-center text-[10px]', isActive ? 'bg-white/20 text-white' : 'bg-line-soft text-ink-sub'), children: n }))] }, s));
        }) }));
}
function SearchInput({ value, onChange }) {
    return (_jsxs("div", { className: "relative", children: [_jsx(Search, { className: "w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-ink-mute pointer-events-none" }), _jsx("input", { value: value, onChange: (e) => onChange(e.target.value), placeholder: "Search role title\u2026", className: "h-9 w-64 pl-9 pr-3 rounded-[10px] bg-white border border-line text-sm text-ink placeholder:text-ink-mute focus:outline-none focus:border-accent transition-colors" })] }));
}
function SortPicker({ value, onChange }) {
    return (_jsx("div", { className: "inline-flex p-1 rounded-[10px] bg-white border border-line", children: SORT_OPTIONS.map((opt) => {
            const Icon = opt.icon;
            const isActive = value === opt.value;
            return (_jsxs("button", { onClick: () => onChange(opt.value), className: cn('h-7 px-2.5 inline-flex items-center gap-1.5 rounded-[7px] text-[11px] font-bold transition-colors', isActive ? 'bg-accent text-white' : 'text-ink-sub hover:text-ink'), children: [_jsx(Icon, { className: "w-3 h-3" }), opt.label] }, opt.value));
        }) }));
}
function ResultGrid({ query }) {
    if (query.isLoading) {
        return (_jsx("div", { className: "py-16 flex items-center justify-center text-ink-sub", children: _jsx(Loader2, { className: "w-5 h-5 animate-spin" }) }));
    }
    if (query.error) {
        return (_jsx("div", { className: "rounded-[10px] border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700", children: query.error.message }));
    }
    const intents = query.data ?? [];
    if (intents.length === 0) {
        return (_jsx(EmptyState, { title: "No intents match these filters", hint: "Try clearing the search or status filter, or capture a new intent." }));
    }
    return (_jsx("div", { className: "grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3", children: intents.map((i) => _jsx(IntentCardRow, { intent: i }, i.id)) }));
}
const priorityPill = {
    LOW: 'bg-line-soft text-ink-sub',
    MEDIUM: 'bg-blue-50 text-blue-700',
    HIGH: 'bg-amber-50 text-amber-700',
    CRITICAL: 'bg-red-50 text-red-700',
};
function IntentCardRow({ intent }) {
    const role = intent.role;
    const skills = role.skills.slice(0, 4);
    const overflow = role.skills.length - skills.length;
    return (_jsx(Link, { to: `/intents/${intent.id}`, className: "group", children: _jsxs("div", { className: "rounded-[12px] bg-white border border-line shadow-sm p-4 h-full flex flex-col gap-3 transition-colors group-hover:border-accent", children: [_jsxs("div", { className: "flex items-start justify-between gap-2", children: [_jsxs("div", { className: "min-w-0", children: [_jsx("h3", { className: "text-[15px] font-bold text-ink leading-tight truncate", title: role.title, children: role.title }), _jsx("p", { className: "text-[11px] text-ink-mute mt-0.5", children: relativeTime(intent.created_at) })] }), _jsx(IntentStatusBadge, { status: intent.status })] }), skills.length > 0 && (_jsxs("div", { className: "flex flex-wrap gap-1", children: [skills.map((s) => (_jsx("span", { className: cn('text-[10px] font-bold px-1.5 py-0.5 rounded', s.required ? 'bg-accent-soft text-accent' : 'bg-line-soft text-ink-sub'), children: s.name }, s.name))), overflow > 0 && (_jsxs("span", { className: "text-[10px] font-bold px-1.5 py-0.5 rounded bg-line-soft text-ink-sub", children: ["+", overflow] }))] })), _jsxs("dl", { className: "grid grid-cols-2 gap-y-1.5 text-[12px] text-ink-sub", children: [_jsx(Fact, { icon: _jsx(GraduationCap, { className: "w-3.5 h-3.5" }), value: `${role.experience.min_years}-${role.experience.max_years} yrs` }), _jsx(Fact, { icon: _jsx(Hash, { className: "w-3.5 h-3.5" }), value: `${role.headcount} ${role.headcount === 1 ? 'position' : 'positions'}` }), _jsx(Fact, { icon: _jsx(MapPin, { className: "w-3.5 h-3.5" }), value: role.locations.length ? role.locations.join(', ') : role.work_mode }), _jsx(Fact, { icon: _jsx(BriefcaseBusiness, { className: "w-3.5 h-3.5" }), value: role.work_mode })] }), _jsxs("div", { className: "mt-auto pt-2 flex items-center justify-between border-t border-line", children: [_jsxs("span", { className: cn('text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full inline-flex items-center gap-1', priorityPill[intent.priority]), children: [_jsx(Flame, { className: "w-2.5 h-2.5" }), " ", intent.priority] }), _jsxs("span", { className: "text-[11px] text-ink-mute inline-flex items-center gap-1 group-hover:text-accent", children: ["View ", _jsx(ChevronRight, { className: "w-3.5 h-3.5" })] })] })] }) }));
}
function Fact({ icon, value }) {
    return (_jsxs("div", { className: "flex items-center gap-1.5 truncate", children: [_jsx("span", { className: "text-ink-mute flex-shrink-0", children: icon }), _jsx("span", { className: "font-semibold truncate", title: value, children: value })] }));
}
function relativeTime(iso) {
    const then = new Date(iso).getTime();
    const diffMs = Date.now() - then;
    const mins = Math.round(diffMs / 60_000);
    if (mins < 1)
        return 'just now';
    if (mins < 60)
        return `${mins}m ago`;
    const hrs = Math.round(mins / 60);
    if (hrs < 24)
        return `${hrs}h ago`;
    const days = Math.round(hrs / 24);
    if (days < 30)
        return `${days}d ago`;
    const months = Math.round(days / 30);
    if (months < 12)
        return `${months}mo ago`;
    return `${Math.round(months / 12)}y ago`;
}

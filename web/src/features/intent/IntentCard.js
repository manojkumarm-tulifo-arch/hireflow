import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Briefcase, GraduationCap, MapPin, Hash, CheckCircle2, Flame, Target, CircleDollarSign, Globe, Pencil, ShieldCheck, } from 'lucide-react';
import { Spinner } from '@/components/ui/primitives';
import { cn } from '@/lib/cn';
const workModeLabel = {
    ONSITE: 'On-site',
    REMOTE: 'Remote',
    HYBRID: 'Hybrid',
};
const priorityCopy = {
    LOW: 'Low Priority',
    MEDIUM: 'Medium Priority',
    HIGH: 'High Priority',
    CRITICAL: 'Critical',
};
const priorityColor = {
    LOW: 'bg-line-soft text-ink-sub',
    MEDIUM: 'bg-blue-50 text-blue-700',
    HIGH: 'bg-red-50 text-red-700',
    CRITICAL: 'bg-red-50 text-red-700',
};
const signalDot = {
    HIGH: 'bg-green-500',
    MEDIUM: 'bg-amber-500',
    LOW: 'bg-ink-mute',
};
export function IntentCard({ intent, onConfirm, onEdit, confirming }) {
    const isDrafted = intent.status === 'DRAFTED';
    const role = intent.role;
    return (_jsxs("div", { className: "rounded-[10px] overflow-hidden border-[1.5px] shadow-sm bg-white", style: { borderColor: isDrafted ? 'rgba(91,76,255,0.2)' : 'rgba(16,185,129,0.25)' }, children: [_jsxs("div", { className: cn('px-3.5 py-2.5 flex items-center justify-between', isDrafted ? 'bg-accent-soft' : 'bg-green-50'), children: [_jsxs("div", { className: "flex items-center gap-1.5", children: [_jsx(Target, { className: cn('w-3.5 h-3.5', isDrafted ? 'text-accent' : 'text-green-700') }), _jsx("span", { className: cn('text-[11px] font-bold uppercase tracking-wider', isDrafted ? 'text-accent' : 'text-green-700'), children: isDrafted ? 'Hiring Intent' : 'Intent Confirmed' })] }), _jsxs("span", { className: cn('text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full inline-flex items-center gap-1', priorityColor[intent.priority]), children: [_jsx(Flame, { className: "w-2.5 h-2.5" }), priorityCopy[intent.priority]] })] }), _jsxs("div", { className: "p-4 space-y-3.5", children: [_jsx("div", { children: _jsxs("div", { className: "flex items-center gap-2", children: [_jsx(Briefcase, { className: "w-4 h-4 text-accent" }), _jsx("h3", { className: "text-[15px] font-bold text-ink leading-tight", children: role.title })] }) }), role.skills.length > 0 && (_jsx("div", { className: "flex flex-wrap gap-1", children: role.skills.map((s) => (_jsx("span", { className: cn('text-[10px] font-bold px-1.5 py-0.5 rounded', s.required ? 'bg-accent-soft text-accent' : 'bg-line-soft text-ink-sub'), children: s.name }, s.name))) })), _jsxs("div", { className: "grid grid-cols-2 gap-1.5", children: [_jsx(Fact, { icon: _jsx(GraduationCap, { className: "w-3.5 h-3.5" }), label: `${role.experience.min_years}-${role.experience.max_years} years` }), _jsx(Fact, { icon: _jsx(Hash, { className: "w-3.5 h-3.5" }), label: `${role.headcount} ${role.headcount === 1 ? 'position' : 'positions'}` }), _jsx(Fact, { icon: _jsx(MapPin, { className: "w-3.5 h-3.5" }), label: role.locations.length ? role.locations.join(', ') : workModeLabel[role.work_mode] }), _jsx(Fact, { icon: _jsx(Globe, { className: "w-3.5 h-3.5" }), label: workModeLabel[role.work_mode] }), intent.budget && (_jsx(Fact, { icon: _jsx(CircleDollarSign, { className: "w-3.5 h-3.5" }), label: formatBudget(intent.budget.min_minor, intent.budget.max_minor, intent.budget.currency), span: true }))] }), intent.intent_signals.length > 0 && (_jsx(SignalSection, { title: "Intent Signals", tone: "accent", children: intent.intent_signals.map((sig) => (_jsx(SignalRow, { label: sig.label, value: sig.value, children: _jsx("span", { className: cn('inline-block w-1.5 h-1.5 rounded-full', signalDot[sig.level]) }) }, sig.label))) })), intent.trust_signals.length > 0 && (_jsx(SignalSection, { title: "Trust Signals", tone: "green", children: intent.trust_signals.map((sig) => (_jsx(SignalRow, { label: sig.label, value: sig.value, children: sig.required ? (_jsx(ShieldCheck, { className: "w-3 h-3 text-green-600" })) : (_jsx("span", { className: "text-[9px] font-bold uppercase tracking-wider text-ink-mute", children: "opt" })) }, sig.label))) })), isDrafted && (onConfirm || onEdit) && (_jsxs("div", { className: "flex items-center gap-2 pt-1", children: [onConfirm && (_jsxs("button", { onClick: onConfirm, disabled: confirming, className: "flex-1 h-10 inline-flex items-center justify-center gap-1.5 rounded-[10px] bg-accent hover:bg-accent-hover text-white text-sm font-bold transition-colors disabled:opacity-40 disabled:cursor-not-allowed shadow-sm", children: [confirming ? _jsx(Spinner, {}) : _jsx(CheckCircle2, { className: "w-4 h-4" }), "Confirm Intent"] })), onEdit && (_jsxs("button", { onClick: onEdit, className: "h-10 px-4 inline-flex items-center justify-center gap-1.5 rounded-[10px] bg-white hover:bg-line-soft border border-line text-ink text-sm font-semibold transition-colors", children: [_jsx(Pencil, { className: "w-3.5 h-3.5" }), " Edit"] }))] }))] })] }));
}
function Fact({ icon, label, span }) {
    return (_jsxs("div", { className: cn('flex items-center gap-1.5 text-[12px] text-ink-sub', span && 'col-span-2'), children: [_jsx("span", { className: "text-ink-mute", children: icon }), _jsx("span", { className: "font-semibold truncate", title: label, children: label })] }));
}
function SignalSection({ title, tone, children }) {
    const toneClass = tone === 'accent' ? 'text-accent' : 'text-green-700';
    return (_jsxs("div", { children: [_jsx("div", { className: cn('text-[10px] font-bold uppercase tracking-widest mb-1.5', toneClass), children: title }), _jsx("div", { className: "space-y-1", children: children })] }));
}
function SignalRow({ label, value, children }) {
    return (_jsxs("div", { className: "flex items-center justify-between px-3 py-1.5 rounded-[8px] bg-line-soft text-[11px]", children: [_jsx("span", { className: "font-semibold text-ink-sub", children: label }), _jsxs("span", { className: "flex items-center gap-1.5 font-bold text-ink", children: [value, children] })] }));
}
function formatBudget(minMinor, maxMinor, currency) {
    const fmt = (cents) => {
        const v = cents / 100;
        if (v >= 100000)
            return `${(v / 100000).toFixed(v % 100000 === 0 ? 0 : 1)}L`;
        if (v >= 1000)
            return `${(v / 1000).toFixed(v % 1000 === 0 ? 0 : 1)}K`;
        return `${v}`;
    };
    return `${currency} ${fmt(minMinor)}-${fmt(maxMinor)}`;
}

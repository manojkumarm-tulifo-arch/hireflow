import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { cn } from '@/lib/cn';
export function Button({ className, variant = 'primary', size = 'md', ...props }) {
    const base = 'inline-flex items-center justify-center gap-1.5 font-semibold rounded-md transition-colors disabled:opacity-40 disabled:cursor-not-allowed';
    const variants = {
        primary: 'bg-accent hover:bg-accent-hover text-white',
        secondary: 'bg-line-soft hover:bg-line text-ink border border-line',
        ghost: 'text-ink-sub hover:text-ink hover:bg-line-soft',
        danger: 'bg-red-500 hover:bg-red-600 text-white',
    };
    const sizes = { sm: 'h-8 px-3 text-xs', md: 'h-10 px-4 text-sm' };
    return _jsx("button", { className: cn(base, variants[variant], sizes[size], className), ...props });
}
export function Card({ className, children, ...props }) {
    return (_jsx("div", { className: cn('bg-white rounded-xl border border-line shadow-sm', className), ...props, children: children }));
}
export function Badge({ children, tone = 'neutral', }) {
    const tones = {
        neutral: 'bg-line-soft text-ink-sub',
        accent: 'bg-accent-soft text-accent',
        success: 'bg-green-50 text-green-700',
        warning: 'bg-amber-50 text-amber-700',
        danger: 'bg-red-50 text-red-700',
        info: 'bg-blue-50 text-blue-700',
    };
    return (_jsx("span", { className: cn('inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-bold uppercase tracking-wider', tones[tone]), children: children }));
}
export function Input({ className, ...props }) {
    return (_jsx("input", { className: cn('w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent', className), ...props }));
}
export function Spinner({ className }) {
    return (_jsx("div", { className: cn('inline-block animate-spin rounded-full border-2 border-line border-t-accent h-4 w-4', className) }));
}
export function EmptyState({ title, hint }) {
    return (_jsxs("div", { className: "text-center py-12", children: [_jsx("p", { className: "text-ink font-semibold", children: title }), hint && _jsx("p", { className: "text-sm text-ink-sub mt-1", children: hint })] }));
}

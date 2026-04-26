import { cn } from '@/lib/cn';
import type { ButtonHTMLAttributes, HTMLAttributes, InputHTMLAttributes, ReactNode } from 'react';

export function Button({
  className,
  variant = 'primary',
  size = 'md',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md';
}) {
  const base = 'inline-flex items-center justify-center gap-1.5 font-semibold rounded-md transition-colors disabled:opacity-40 disabled:cursor-not-allowed';
  const variants = {
    primary: 'bg-accent hover:bg-accent-hover text-white',
    secondary: 'bg-line-soft hover:bg-line text-ink border border-line',
    ghost: 'text-ink-sub hover:text-ink hover:bg-line-soft',
    danger: 'bg-red-500 hover:bg-red-600 text-white',
  };
  const sizes = { sm: 'h-8 px-3 text-xs', md: 'h-10 px-4 text-sm' };
  return <button className={cn(base, variants[variant], sizes[size], className)} {...props} />;
}

export function Card({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('bg-white rounded-xl border border-line shadow-sm', className)} {...props}>
      {children}
    </div>
  );
}

export function Badge({
  children,
  tone = 'neutral',
}: { children: ReactNode; tone?: 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info' }) {
  const tones = {
    neutral: 'bg-line-soft text-ink-sub',
    accent: 'bg-accent-soft text-accent',
    success: 'bg-green-50 text-green-700',
    warning: 'bg-amber-50 text-amber-700',
    danger: 'bg-red-50 text-red-700',
    info: 'bg-blue-50 text-blue-700',
  };
  return (
    <span className={cn('inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-bold uppercase tracking-wider', tones[tone])}>
      {children}
    </span>
  );
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent',
        className,
      )}
      {...props}
    />
  );
}

export function Spinner({ className }: { className?: string }) {
  return (
    <div className={cn('inline-block animate-spin rounded-full border-2 border-line border-t-accent h-4 w-4', className)} />
  );
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="text-center py-12">
      <p className="text-ink font-semibold">{title}</p>
      {hint && <p className="text-sm text-ink-sub mt-1">{hint}</p>}
    </div>
  );
}

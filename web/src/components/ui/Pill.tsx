import { cn } from '@/lib/cn';
import type { ReactNode } from 'react';

type PillVariant = 'strong' | 'moderate' | 'weak' | 'info' | 'warn' | 'error' | 'neutral';

const variantClasses: Record<PillVariant, string> = {
  strong: 'bg-green-50 text-green-700',
  moderate: 'bg-amber-50 text-amber-700',
  weak: 'bg-red-50 text-red-700',
  info: 'bg-blue-50 text-blue-700',
  warn: 'bg-amber-50 text-amber-700',
  error: 'bg-red-50 text-red-700',
  neutral: 'bg-line-soft text-ink-sub',
};

export function Pill({
  variant = 'neutral',
  children,
}: {
  variant?: PillVariant;
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider',
        variantClasses[variant],
      )}
    >
      {children}
    </span>
  );
}

export function BandPill({
  band,
}: {
  band: 'strong' | 'moderate' | 'weak' | null;
}) {
  if (!band) return null;
  return <Pill variant={band}>{band}</Pill>;
}

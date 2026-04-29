import {
  Briefcase, GraduationCap, MapPin, Hash, CheckCircle2, Flame, Target,
  CircleDollarSign, Globe, Pencil, ShieldCheck,
  Users, UserCircle, Lightbulb,
} from 'lucide-react';
import { Spinner } from '@/components/ui/primitives';
import { cn } from '@/lib/cn';
import type { Intent, Priority, SignalLevel, WorkMode } from '@/api/types';
import type { ReactNode } from 'react';

const workModeLabel: Record<WorkMode, string> = {
  ONSITE: 'On-site',
  REMOTE: 'Remote',
  HYBRID: 'Hybrid',
};

const priorityCopy: Record<Priority, string> = {
  LOW: 'Low Priority',
  MEDIUM: 'Medium Priority',
  HIGH: 'High Priority',
  CRITICAL: 'Critical',
};

const priorityColor: Record<Priority, string> = {
  LOW: 'bg-line-soft text-ink-sub',
  MEDIUM: 'bg-blue-50 text-blue-700',
  HIGH: 'bg-red-50 text-red-700',
  CRITICAL: 'bg-red-50 text-red-700',
};

const signalDot: Record<SignalLevel, string> = {
  HIGH: 'bg-green-500',
  MEDIUM: 'bg-amber-500',
  LOW: 'bg-ink-mute',
};

interface Props {
  intent: Intent;
  onConfirm?: () => void;
  onEdit?: () => void;
  confirming?: boolean;
}

export function IntentCard({ intent, onConfirm, onEdit, confirming }: Props) {
  const isDrafted = intent.status === 'DRAFTED';
  const role = intent.role;

  return (
    <div className="rounded-[10px] overflow-hidden border-[1.5px] shadow-sm bg-white"
      style={{ borderColor: isDrafted ? 'rgba(91,76,255,0.2)' : 'rgba(16,185,129,0.25)' }}
    >
      <div className={cn(
        'px-3.5 py-2.5 flex items-center justify-between',
        isDrafted ? 'bg-accent-soft' : 'bg-green-50',
      )}>
        <div className="flex items-center gap-1.5">
          <Target className={cn('w-3.5 h-3.5', isDrafted ? 'text-accent' : 'text-green-700')} />
          <span className={cn(
            'text-[11px] font-bold uppercase tracking-wider',
            isDrafted ? 'text-accent' : 'text-green-700',
          )}>
            {isDrafted ? 'Hiring Intent' : 'Intent Confirmed'}
          </span>
        </div>
        <span className={cn(
          'text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full inline-flex items-center gap-1',
          priorityColor[intent.priority],
        )}>
          <Flame className="w-2.5 h-2.5" />{priorityCopy[intent.priority]}
        </span>
      </div>

      <div className="p-4 space-y-3.5">
        <div>
          <div className="flex items-center gap-2">
            <Briefcase className="w-4 h-4 text-accent" />
            <h3 className="text-[15px] font-bold text-ink leading-tight">{role.title}</h3>
          </div>
        </div>

        {role.skills.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {role.skills.map((s) => (
              <span
                key={s.name}
                className={cn(
                  'text-[10px] font-bold px-1.5 py-0.5 rounded',
                  s.required ? 'bg-accent-soft text-accent' : 'bg-line-soft text-ink-sub',
                )}
              >
                {s.name}
              </span>
            ))}
          </div>
        )}

        <div className="grid grid-cols-2 gap-1.5">
          <Fact icon={<GraduationCap className="w-3.5 h-3.5" />}
            label={`${role.experience.min_years}-${role.experience.max_years} years`} />
          <Fact icon={<Hash className="w-3.5 h-3.5" />}
            label={`${role.headcount} ${role.headcount === 1 ? 'position' : 'positions'}`} />
          <Fact icon={<MapPin className="w-3.5 h-3.5" />}
            label={role.locations.length ? role.locations.join(', ') : workModeLabel[role.work_mode]} />
          <Fact icon={<Globe className="w-3.5 h-3.5" />} label={workModeLabel[role.work_mode]} />
          {intent.budget && (
            <Fact
              icon={<CircleDollarSign className="w-3.5 h-3.5" />}
              label={formatBudget(intent.budget.min_minor, intent.budget.max_minor, intent.budget.currency)}
              span
            />
          )}
          {intent.team && (
            <Fact icon={<Users className="w-3.5 h-3.5" />} label={intent.team} />
          )}
          {intent.reports_to && (
            <Fact icon={<UserCircle className="w-3.5 h-3.5" />} label={`Reports to ${intent.reports_to}`} />
          )}
        </div>

        {intent.reason && (
          <div className="flex items-start gap-2 pt-0.5 text-[12px] text-ink-sub leading-relaxed">
            <Lightbulb className="w-3.5 h-3.5 text-accent flex-shrink-0 mt-0.5" />
            <span><span className="font-bold text-ink-mute uppercase tracking-wider text-[10px] mr-1.5">Reason</span>{intent.reason}</span>
          </div>
        )}

        {intent.intent_signals.length > 0 && (
          <SignalSection title="Intent Signals" tone="accent">
            {intent.intent_signals.map((sig) => (
              <SignalRow key={sig.label} label={sig.label} value={sig.value}>
                <span className={cn('inline-block w-1.5 h-1.5 rounded-full', signalDot[sig.level])} />
              </SignalRow>
            ))}
          </SignalSection>
        )}

        {intent.trust_signals.length > 0 && (
          <SignalSection title="Trust Signals" tone="green">
            {intent.trust_signals.map((sig) => (
              <SignalRow key={sig.label} label={sig.label} value={sig.value}>
                {sig.required ? (
                  <ShieldCheck className="w-3 h-3 text-green-600" />
                ) : (
                  <span className="text-[9px] font-bold uppercase tracking-wider text-ink-mute">opt</span>
                )}
              </SignalRow>
            ))}
          </SignalSection>
        )}

        {isDrafted && (onConfirm || onEdit) && (
          <div className="flex items-center gap-2 pt-1">
            {onConfirm && (
              <button
                onClick={onConfirm}
                disabled={confirming}
                className="flex-1 h-10 inline-flex items-center justify-center gap-1.5 rounded-[10px] bg-accent hover:bg-accent-hover text-white text-sm font-bold transition-colors disabled:opacity-40 disabled:cursor-not-allowed shadow-sm"
              >
                {confirming ? <Spinner /> : <CheckCircle2 className="w-4 h-4" />}
                Confirm Intent
              </button>
            )}
            {onEdit && (
              <button
                onClick={onEdit}
                className="h-10 px-4 inline-flex items-center justify-center gap-1.5 rounded-[10px] bg-white hover:bg-line-soft border border-line text-ink text-sm font-semibold transition-colors"
              >
                <Pencil className="w-3.5 h-3.5" /> Edit
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function Fact({ icon, label, span }: { icon: ReactNode; label: string; span?: boolean }) {
  return (
    <div className={cn('flex items-center gap-1.5 text-[12px] text-ink-sub', span && 'col-span-2')}>
      <span className="text-ink-mute">{icon}</span>
      <span className="font-semibold truncate" title={label}>{label}</span>
    </div>
  );
}

function SignalSection({ title, tone, children }: { title: string; tone: 'accent' | 'green'; children: ReactNode }) {
  const toneClass = tone === 'accent' ? 'text-accent' : 'text-green-700';
  return (
    <div>
      <div className={cn('text-[10px] font-bold uppercase tracking-widest mb-1.5', toneClass)}>
        {title}
      </div>
      <div className="space-y-1">{children}</div>
    </div>
  );
}

function SignalRow({ label, value, children }: { label: string; value: string; children?: ReactNode }) {
  return (
    <div className="flex items-center justify-between px-3 py-1.5 rounded-[8px] bg-line-soft text-[11px]">
      <span className="font-semibold text-ink-sub">{label}</span>
      <span className="flex items-center gap-1.5 font-bold text-ink">
        {value}
        {children}
      </span>
    </div>
  );
}

function formatBudget(minMinor: number, maxMinor: number, currency: string): string {
  const fmt = (cents: number) => {
    const v = cents / 100;
    if (v >= 100000) return `${(v / 100000).toFixed(v % 100000 === 0 ? 0 : 1)}L`;
    if (v >= 1000) return `${(v / 1000).toFixed(v % 1000 === 0 ? 0 : 1)}K`;
    return `${v}`;
  };
  return `${currency} ${fmt(minMinor)}-${fmt(maxMinor)}`;
}

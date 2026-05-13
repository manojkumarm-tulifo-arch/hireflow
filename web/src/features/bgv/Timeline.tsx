import {
  CheckCircle2,
  CircleDashed,
  FileCheck,
  FilePlus,
  FileX,
  Flag,
  PlayCircle,
  RotateCcw,
  Send,
  UserPlus,
} from 'lucide-react';
import type { BGVTimelineEntry } from '@/api/bgvTypes';

interface PayloadStep { step?: string }
interface PayloadDoc { document_label?: string; label?: string }
interface PayloadReopen { reason?: string; from_status?: string }

/**
 * Timeline renders the per-submission event log. Events come straight
 * from the outbox with a typed name + opaque JSON payload — we map
 * known names to a friendly icon + label, and fall through to the raw
 * name for anything we don't recognise (so a new BE event ships as
 * unstyled but visible, not invisible).
 */
export function Timeline({ items }: { items: BGVTimelineEntry[] }) {
  if (items.length === 0) {
    return (
      <p className="text-xs text-ink-mute italic">
        No events recorded yet.
      </p>
    );
  }
  return (
    <ol className="space-y-3">
      {items.map((e, i) => (
        <TimelineRow key={e.id} entry={e} isLast={i === items.length - 1} />
      ))}
    </ol>
  );
}

function TimelineRow({ entry, isLast }: { entry: BGVTimelineEntry; isLast: boolean }) {
  const view = describe(entry);
  const Icon = view.icon;
  return (
    <li className="flex gap-3">
      <div className="flex flex-col items-center">
        <span
          className={`w-7 h-7 rounded-full flex items-center justify-center shrink-0 ${view.iconCls}`}
        >
          <Icon className="w-3.5 h-3.5" />
        </span>
        {!isLast && <span className="w-px flex-1 bg-line mt-1" />}
      </div>
      <div className="flex-1 pb-3">
        <p className="text-sm font-semibold text-ink">{view.label}</p>
        {view.detail && (
          <p className="text-xs text-ink-sub mt-0.5">{view.detail}</p>
        )}
        <p className="text-[11px] text-ink-mute mt-0.5">
          {formatTimestamp(entry.occurred_at)}
        </p>
      </div>
    </li>
  );
}

interface View {
  icon: typeof UserPlus;
  iconCls: string;
  label: string;
  detail?: string;
}

function describe(entry: BGVTimelineEntry): View {
  const p = (entry.payload ?? {}) as PayloadStep & PayloadDoc & PayloadReopen;
  switch (entry.event_name) {
    case 'bgv.BGVInvited':
      return {
        icon: UserPlus,
        iconCls: 'bg-line-soft text-ink-sub',
        label: 'Invitation sent',
      };
    case 'bgv.BGVStarted':
      return {
        icon: PlayCircle,
        iconCls: 'bg-blue-50 text-blue-700',
        label: 'Candidate started the flow',
      };
    case 'bgv.StepCompleted':
      return {
        icon: CheckCircle2,
        iconCls: 'bg-green-50 text-green-700',
        label: 'Step saved',
        detail: humaniseStep(p.step),
      };
    case 'bgv.DocumentCaptured':
      return {
        icon: FileCheck,
        iconCls: 'bg-green-50 text-green-700',
        label: 'Document uploaded',
        detail: p.document_label ?? p.label,
      };
    case 'bgv.DocumentCleared':
      return {
        icon: FileX,
        iconCls: 'bg-amber-50 text-amber-700',
        label: 'Document cleared',
        detail: p.document_label ?? p.label,
      };
    case 'bgv.DocumentAdded':
      return {
        icon: FilePlus,
        iconCls: 'bg-blue-50 text-blue-700',
        label: 'Custom document added',
        detail: p.document_label ?? p.label,
      };
    case 'bgv.BGVSubmitted':
      return {
        icon: Send,
        iconCls: 'bg-accent-soft text-accent',
        label: 'Submitted for verification',
      };
    case 'bgv.BGVReopened':
      return {
        icon: RotateCcw,
        iconCls: 'bg-amber-50 text-amber-700',
        label: 'Reopened by recruiter',
        detail: [p.from_status && `From ${p.from_status.replace('_', ' ')}`, p.reason && `Reason: ${p.reason}`]
          .filter(Boolean)
          .join(' · '),
      };
    case 'bgv.BGVFlagged':
      return {
        icon: Flag,
        iconCls: 'bg-red-50 text-red-700',
        label: 'Flagged for review',
      };
    default:
      return {
        icon: CircleDashed,
        iconCls: 'bg-line-soft text-ink-sub',
        label: entry.event_name.replace(/^bgv\./, ''),
      };
  }
}

const STEP_LABELS: Record<string, string> = {
  personal_info: 'Personal Information',
  address: 'Address',
  emergency_contacts: 'Emergency Contacts',
  professional_references: 'Professional References',
  digital_profile: 'Digital Profile',
  declarations: 'Declarations',
};

function humaniseStep(step?: string): string | undefined {
  if (!step) return undefined;
  return STEP_LABELS[step] ?? step;
}

function formatTimestamp(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  return new Date(t).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

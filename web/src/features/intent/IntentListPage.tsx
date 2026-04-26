import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowDownAZ, BriefcaseBusiness, ChevronRight, Flame, GraduationCap, Hash,
  Loader2, MapPin, Plus, Search, Sparkles, Target, Zap,
} from 'lucide-react';
import { intentApi } from '@/api/intent';
import type { Intent, IntentSortOrder, IntentStatus, IntentStatusCounts, Priority } from '@/api/types';
import { Button, EmptyState } from '@/components/ui/primitives';
import { IntentStatusBadge } from '@/components/ui/StatusBadge';
import { cn } from '@/lib/cn';

type StatusKey = IntentStatus | 'ALL';

const STATUS_KEYS: StatusKey[] = ['ALL', 'DRAFTED', 'CONFIRMED', 'CANCELLED', 'CLOSED'];

const STATUS_COPY: Record<StatusKey, string> = {
  ALL: 'All',
  DRAFTED: 'Drafted',
  CONFIRMED: 'Confirmed',
  CANCELLED: 'Cancelled',
  CLOSED: 'Closed',
};

const SORT_OPTIONS: Array<{ value: IntentSortOrder; label: string; icon: typeof ArrowDownAZ }> = [
  { value: 'NEWEST', label: 'Newest', icon: ArrowDownAZ },
  { value: 'URGENT', label: 'Urgent first', icon: Zap },
];

export function IntentListPage() {
  const [statusFilter, setStatusFilter] = useState<StatusKey>('ALL');
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState<IntentSortOrder>('NEWEST');

  const summaryQuery = useQuery({
    queryKey: ['intents', 'summary'],
    queryFn: () => intentApi.summary(),
    staleTime: 15_000,
  });

  const listQuery = useQuery({
    queryKey: ['intents', { statusFilter, search, sort }],
    queryFn: () =>
      intentApi.list({
        status: statusFilter === 'ALL' ? undefined : statusFilter,
        q: search.trim() || undefined,
        sort,
      }),
  });

  const counts = summaryQuery.data?.counts;

  return (
    <div className="px-8 py-6 space-y-5 max-w-6xl">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="font-display text-3xl font-bold text-ink">Hiring Intents</h1>
          <p className="text-sm text-ink-sub mt-1">
            Every role your team is hiring for — drafts, confirmed, and historical.
          </p>
        </div>
        <Link to="/intents/new">
          <Button><Plus className="w-4 h-4" /> New Intent</Button>
        </Link>
      </header>

      <SummaryStrip counts={counts} loading={summaryQuery.isLoading} />

      <div className="flex flex-wrap items-center gap-2">
        <FilterChips counts={counts} active={statusFilter} onChange={setStatusFilter} />
        <div className="flex-1" />
        <SearchInput value={search} onChange={setSearch} />
        <SortPicker value={sort} onChange={setSort} />
      </div>

      <ResultGrid query={listQuery} />
    </div>
  );
}

function SummaryStrip({ counts, loading }: { counts?: IntentStatusCounts; loading: boolean }) {
  const tiles: Array<{ label: string; value?: number; icon: typeof Sparkles; tone: string }> = useMemo(() => [
    { label: 'Total', value: counts?.total, icon: Sparkles, tone: 'text-accent bg-accent-soft' },
    { label: 'Drafted', value: counts?.DRAFTED, icon: Target, tone: 'text-amber-700 bg-amber-50' },
    { label: 'Confirmed', value: counts?.CONFIRMED, icon: BriefcaseBusiness, tone: 'text-green-700 bg-green-50' },
    { label: 'Closed', value: counts?.CLOSED, icon: Hash, tone: 'text-ink-sub bg-line-soft' },
  ], [counts]);

  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
      {tiles.map((t) => {
        const Icon = t.icon;
        return (
          <div key={t.label} className="rounded-[12px] bg-white border border-line p-3.5 flex items-center gap-3">
            <div className={cn('w-9 h-9 rounded-[10px] flex items-center justify-center', t.tone)}>
              <Icon className="w-4 h-4" />
            </div>
            <div>
              <div className="text-[10px] font-bold uppercase tracking-wider text-ink-sub">{t.label}</div>
              <div className="text-xl font-bold text-ink leading-none mt-0.5">
                {loading ? <span className="text-ink-mute">—</span> : (t.value ?? 0)}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function FilterChips({
  counts, active, onChange,
}: { counts?: IntentStatusCounts; active: StatusKey; onChange: (k: StatusKey) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {STATUS_KEYS.map((s) => {
        const n = !counts ? null : (s === 'ALL' ? counts.total : counts[s as IntentStatus]);
        const isActive = active === s;
        return (
          <button
            key={s}
            onClick={() => onChange(s)}
            className={cn(
              'h-8 px-3 inline-flex items-center gap-1.5 rounded-full text-[11px] font-bold uppercase tracking-wider transition-colors border',
              isActive
                ? 'bg-accent text-white border-accent'
                : 'bg-white border-line text-ink-sub hover:text-ink hover:border-ink-mute',
            )}
          >
            {STATUS_COPY[s]}
            {n != null && (
              <span className={cn(
                'min-w-[18px] h-[18px] px-1 rounded-full inline-flex items-center justify-center text-[10px]',
                isActive ? 'bg-white/20 text-white' : 'bg-line-soft text-ink-sub',
              )}>{n}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}

function SearchInput({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="relative">
      <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 text-ink-mute pointer-events-none" />
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Search role title…"
        className="h-9 w-64 pl-9 pr-3 rounded-[10px] bg-white border border-line text-sm text-ink placeholder:text-ink-mute focus:outline-none focus:border-accent transition-colors"
      />
    </div>
  );
}

function SortPicker({ value, onChange }: { value: IntentSortOrder; onChange: (v: IntentSortOrder) => void }) {
  return (
    <div className="inline-flex p-1 rounded-[10px] bg-white border border-line">
      {SORT_OPTIONS.map((opt) => {
        const Icon = opt.icon;
        const isActive = value === opt.value;
        return (
          <button
            key={opt.value}
            onClick={() => onChange(opt.value)}
            className={cn(
              'h-7 px-2.5 inline-flex items-center gap-1.5 rounded-[7px] text-[11px] font-bold transition-colors',
              isActive ? 'bg-accent text-white' : 'text-ink-sub hover:text-ink',
            )}
          >
            <Icon className="w-3 h-3" />
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}

function ResultGrid({ query }: { query: ReturnType<typeof useQuery<Intent[], Error>> }) {
  if (query.isLoading) {
    return (
      <div className="py-16 flex items-center justify-center text-ink-sub">
        <Loader2 className="w-5 h-5 animate-spin" />
      </div>
    );
  }
  if (query.error) {
    return (
      <div className="rounded-[10px] border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">
        {(query.error as Error).message}
      </div>
    );
  }
  const intents = query.data ?? [];
  if (intents.length === 0) {
    return (
      <EmptyState
        title="No intents match these filters"
        hint="Try clearing the search or status filter, or capture a new intent."
      />
    );
  }
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
      {intents.map((i) => <IntentCardRow key={i.id} intent={i} />)}
    </div>
  );
}

const priorityPill: Record<Priority, string> = {
  LOW: 'bg-line-soft text-ink-sub',
  MEDIUM: 'bg-blue-50 text-blue-700',
  HIGH: 'bg-amber-50 text-amber-700',
  CRITICAL: 'bg-red-50 text-red-700',
};

function IntentCardRow({ intent }: { intent: Intent }) {
  const role = intent.role;
  const skills = role.skills.slice(0, 4);
  const overflow = role.skills.length - skills.length;
  return (
    <Link to={`/intents/${intent.id}`} className="group">
      <div className="rounded-[12px] bg-white border border-line shadow-sm p-4 h-full flex flex-col gap-3 transition-colors group-hover:border-accent">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <h3 className="text-[15px] font-bold text-ink leading-tight truncate" title={role.title}>
              {role.title}
            </h3>
            <p className="text-[11px] text-ink-mute mt-0.5">{relativeTime(intent.created_at)}</p>
          </div>
          <IntentStatusBadge status={intent.status} />
        </div>

        {skills.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {skills.map((s) => (
              <span key={s.name} className={cn(
                'text-[10px] font-bold px-1.5 py-0.5 rounded',
                s.required ? 'bg-accent-soft text-accent' : 'bg-line-soft text-ink-sub',
              )}>{s.name}</span>
            ))}
            {overflow > 0 && (
              <span className="text-[10px] font-bold px-1.5 py-0.5 rounded bg-line-soft text-ink-sub">
                +{overflow}
              </span>
            )}
          </div>
        )}

        <dl className="grid grid-cols-2 gap-y-1.5 text-[12px] text-ink-sub">
          <Fact icon={<GraduationCap className="w-3.5 h-3.5" />} value={`${role.experience.min_years}-${role.experience.max_years} yrs`} />
          <Fact icon={<Hash className="w-3.5 h-3.5" />} value={`${role.headcount} ${role.headcount === 1 ? 'position' : 'positions'}`} />
          <Fact icon={<MapPin className="w-3.5 h-3.5" />} value={role.locations.length ? role.locations.join(', ') : role.work_mode} />
          <Fact icon={<BriefcaseBusiness className="w-3.5 h-3.5" />} value={role.work_mode} />
        </dl>

        <div className="mt-auto pt-2 flex items-center justify-between border-t border-line">
          <span className={cn(
            'text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full inline-flex items-center gap-1',
            priorityPill[intent.priority],
          )}>
            <Flame className="w-2.5 h-2.5" /> {intent.priority}
          </span>
          <span className="text-[11px] text-ink-mute inline-flex items-center gap-1 group-hover:text-accent">
            View <ChevronRight className="w-3.5 h-3.5" />
          </span>
        </div>
      </div>
    </Link>
  );
}

function Fact({ icon, value }: { icon: React.ReactNode; value: string }) {
  return (
    <div className="flex items-center gap-1.5 truncate">
      <span className="text-ink-mute flex-shrink-0">{icon}</span>
      <span className="font-semibold truncate" title={value}>{value}</span>
    </div>
  );
}

function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  const diffMs = Date.now() - then;
  const mins = Math.round(diffMs / 60_000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.round(hrs / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.round(days / 30);
  if (months < 12) return `${months}mo ago`;
  return `${Math.round(months / 12)}y ago`;
}

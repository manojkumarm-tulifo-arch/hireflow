import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight, Mail, Phone } from 'lucide-react';
import { bgvApi } from '@/api/bgv';
import type { BGVStatus } from '@/api/bgvTypes';
import { Card, EmptyState, Spinner } from '@/components/ui/primitives';
import { BGVStatusBadge } from '@/components/ui/StatusBadge';

const STATUSES: Array<BGVStatus | 'ALL'> = [
  'ALL',
  'INVITED',
  'IN_PROGRESS',
  'SUBMITTED',
  'UNDER_REVIEW',
  'VERIFIED',
  'FLAGGED',
];

const PAGE_LIMIT = 20;

export function BGVListPage() {
  const [filter, setFilter] = useState<BGVStatus | 'ALL'>('ALL');
  const { data, isLoading, error } = useQuery({
    queryKey: ['bgv.list', filter],
    queryFn: () =>
      bgvApi.list({
        limit: PAGE_LIMIT,
        ...(filter === 'ALL' ? {} : { status: filter }),
      }),
  });

  return (
    <div className="px-8 py-6 max-w-5xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-ink">BGV Submissions</h1>
        <p className="text-sm text-ink-sub mt-0.5">
          Background-verification queue. Open a row to review steps, documents, and the timeline,
          or to reopen a submission for the candidate to revise.
        </p>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {STATUSES.map((s) => (
          <button
            key={s}
            onClick={() => setFilter(s)}
            className={`px-3 py-1.5 rounded-full text-xs font-bold uppercase tracking-wider ${
              filter === s
                ? 'bg-accent text-white'
                : 'bg-white border border-line text-ink-sub hover:text-ink'
            }`}
          >
            {s.replace('_', ' ')}
          </button>
        ))}
      </div>

      {isLoading && (
        <div className="flex justify-center py-12">
          <Spinner />
        </div>
      )}
      {error && (
        <Card className="p-4 text-sm text-red-600">{(error as Error).message}</Card>
      )}
      {data && data.items.length === 0 && (
        <EmptyState
          title="No submissions match"
          hint={
            filter === 'ALL'
              ? 'Invite a candidate from the SecureVerify side to populate this queue.'
              : `No submissions in the ${filter.replace('_', ' ')} state.`
          }
        />
      )}
      {data && data.items.length > 0 && (
        <>
          <p className="text-[11px] text-ink-mute font-bold uppercase tracking-wider">
            Showing {data.items.length} of {data.total}
          </p>
          <div className="space-y-2">
            {data.items.map((s) => (
              <Link key={s.id} to={`/bgv/${s.id}`}>
                <Card className="px-5 py-4 hover:border-accent transition-colors">
                  <div className="flex items-start justify-between gap-4">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-3 flex-wrap">
                        <h3 className="text-sm font-bold text-ink truncate">
                          {s.candidate.name}
                        </h3>
                        <BGVStatusBadge status={s.status} />
                      </div>
                      {(s.candidate.position || s.candidate.company) && (
                        <p className="text-xs text-ink-sub mt-0.5">
                          {[s.candidate.position, s.candidate.company]
                            .filter(Boolean)
                            .join(' · ')}
                        </p>
                      )}
                      <div className="flex items-center gap-4 mt-1.5 text-[11px] text-ink-mute">
                        <span className="inline-flex items-center gap-1">
                          <Mail className="w-3 h-3" />
                          {s.candidate.email}
                        </span>
                        <span className="inline-flex items-center gap-1">
                          <Phone className="w-3 h-3" />
                          {s.candidate.phone}
                        </span>
                      </div>
                      <p className="text-[11px] text-ink-mute mt-2">
                        {timestampLabel(s.status, {
                          submitted_at: s.submitted_at,
                          reviewed_at: s.reviewed_at,
                          updated_at: s.updated_at,
                        })}
                      </p>
                    </div>
                    <ChevronRight className="w-4 h-4 text-ink-mute mt-1 shrink-0" />
                  </div>
                </Card>
              </Link>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function timestampLabel(
  status: BGVStatus,
  ts: { submitted_at?: string | null; reviewed_at?: string | null; updated_at: string },
): string {
  if ((status === 'VERIFIED' || status === 'FLAGGED') && ts.reviewed_at) {
    return `Reviewed ${formatRelative(ts.reviewed_at)}`;
  }
  if ((status === 'SUBMITTED' || status === 'UNDER_REVIEW') && ts.submitted_at) {
    return `Submitted ${formatRelative(ts.submitted_at)}`;
  }
  return `Last updated ${formatRelative(ts.updated_at)}`;
}

function formatRelative(iso: string): string {
  const then = Date.parse(iso);
  if (!Number.isFinite(then)) return iso;
  const diffMs = Date.now() - then;
  const mins = Math.round(diffMs / 60_000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  if (days < 30) return `${days}d ago`;
  // Past a month, calendar date is more useful than a relative noise.
  return new Date(then).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}

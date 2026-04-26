import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ChevronRight } from 'lucide-react';
import { postingApi } from '@/api/posting';
import type { PostingStatus } from '@/api/types';
import { Card, EmptyState, Spinner } from '@/components/ui/primitives';
import { PostingStatusBadge } from '@/components/ui/StatusBadge';

const STATUSES: Array<PostingStatus | 'ALL'> = ['ALL', 'DRAFT', 'PUBLISHED', 'CLOSED', 'ARCHIVED'];

export function PostingListPage() {
  const [filter, setFilter] = useState<PostingStatus | 'ALL'>('ALL');
  const { data, isLoading, error } = useQuery({
    queryKey: ['postings', filter],
    queryFn: () => postingApi.list(filter === 'ALL' ? undefined : { status: filter }),
  });

  return (
    <div className="px-8 py-6 max-w-5xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-ink">Job Postings</h1>
        <p className="text-sm text-ink-sub mt-0.5">
          Drafted automatically when intents are confirmed. Publish to one or more channels to start sourcing.
        </p>
      </div>

      <div className="flex gap-1.5">
        {STATUSES.map((s) => (
          <button
            key={s}
            onClick={() => setFilter(s)}
            className={`px-3 py-1.5 rounded-full text-xs font-bold uppercase tracking-wider ${
              filter === s ? 'bg-accent text-white' : 'bg-white border border-line text-ink-sub hover:text-ink'
            }`}
          >
            {s}
          </button>
        ))}
      </div>

      {isLoading && <div className="flex justify-center py-12"><Spinner /></div>}
      {error && <Card className="p-4 text-sm text-red-600">{(error as Error).message}</Card>}
      {data && data.length === 0 && (
        <EmptyState title="No postings yet" hint="Confirm a hiring intent to draft a posting automatically." />
      )}
      {data && data.length > 0 && (
        <div className="space-y-2">
          {data.map((p) => (
            <Link key={p.id} to={`/postings/${p.id}`}>
              <Card className="px-5 py-4 flex items-center justify-between hover:border-accent transition-colors">
                <div>
                  <div className="flex items-center gap-3">
                    <h3 className="text-sm font-bold text-ink">{p.jd.title}</h3>
                    <PostingStatusBadge status={p.status} />
                    <span className="text-[11px] text-ink-mute font-mono">v{p.jd.version}</span>
                  </div>
                  <p className="text-xs text-ink-sub mt-1">
                    {p.sources.length === 0
                      ? 'Not yet distributed'
                      : `${p.sources.length} source${p.sources.length === 1 ? '' : 's'}: ${p.sources.map((s) => s.channel).join(', ')}`}
                  </p>
                </div>
                <ChevronRight className="w-4 h-4 text-ink-mute" />
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

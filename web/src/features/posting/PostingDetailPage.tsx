import { useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Linkedin, Globe, Mail, Database, XCircle } from 'lucide-react';
import { postingApi } from '@/api/posting';
import type { SourceChannel } from '@/api/types';
import { Button, Card, Spinner } from '@/components/ui/primitives';
import { PostingStatusBadge } from '@/components/ui/StatusBadge';

const CHANNELS: Array<{ id: SourceChannel; label: string; icon: typeof Linkedin }> = [
  { id: 'LINKEDIN', label: 'LinkedIn', icon: Linkedin },
  { id: 'CAREER_PAGE', label: 'Career Page', icon: Globe },
  { id: 'EMAIL', label: 'Email Inbox', icon: Mail },
  { id: 'INTERNAL_DB', label: 'Internal DB', icon: Database },
];

export function PostingDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Set<SourceChannel>>(new Set());
  const [closeReason, setCloseReason] = useState('');

  const { data: posting, isLoading } = useQuery({
    queryKey: ['posting', id],
    queryFn: () => postingApi.get(id),
    enabled: !!id,
  });

  const publishMutation = useMutation({
    mutationFn: (channels: SourceChannel[]) => postingApi.publish(id, channels),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['posting', id] });
      qc.invalidateQueries({ queryKey: ['postings'] });
      setSelected(new Set());
    },
  });

  const closeMutation = useMutation({
    mutationFn: () => postingApi.close(id, closeReason),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['posting', id] });
      qc.invalidateQueries({ queryKey: ['postings'] });
    },
  });

  if (isLoading) return <div className="p-12 flex justify-center"><Spinner /></div>;
  if (!posting) return <div className="p-8">Not found.</div>;

  const isTerminal = posting.status === 'CLOSED' || posting.status === 'ARCHIVED';
  const existingChannels = new Set(posting.sources.map((s) => s.channel));

  const toggle = (c: SourceChannel) => {
    const copy = new Set(selected);
    if (copy.has(c)) copy.delete(c); else copy.add(c);
    setSelected(copy);
  };

  return (
    <div className="px-8 py-6 max-w-3xl space-y-6">
      <Link to="/postings" className="inline-flex items-center gap-1 text-xs text-ink-sub hover:text-ink">
        <ArrowLeft className="w-3.5 h-3.5" /> Back to postings
      </Link>

      <Card className="p-6 space-y-4">
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-xl font-bold text-ink">{posting.jd.title}</h1>
            <p className="text-xs text-ink-mute mt-1">JD v{posting.jd.version} · intent {posting.intent_id}</p>
          </div>
          <PostingStatusBadge status={posting.status} />
        </div>
        <p className="text-sm text-ink-sub">{posting.jd.summary}</p>

        {posting.jd.responsibilities.length > 0 && (
          <Section title="Responsibilities" items={posting.jd.responsibilities} />
        )}
        {posting.jd.requirements.length > 0 && (
          <Section title="Requirements" items={posting.jd.requirements} />
        )}
      </Card>

      <Card className="p-6 space-y-4">
        <h2 className="text-sm font-bold text-ink">Source Distribution</h2>

        {posting.sources.length > 0 && (
          <div className="space-y-1.5">
            {posting.sources.map((s) => {
              const Icon = CHANNELS.find((c) => c.id === s.channel)?.icon ?? Globe;
              return (
                <div key={s.channel} className="flex items-center justify-between px-3 py-2 rounded bg-line-soft">
                  <div className="flex items-center gap-2">
                    <Icon className="w-4 h-4 text-accent" />
                    <span className="text-sm font-semibold">{s.channel}</span>
                  </div>
                  <span className="text-[11px] font-bold uppercase tracking-wider text-ink-sub">{s.status}</span>
                </div>
              );
            })}
          </div>
        )}

        {!isTerminal && (
          <div className="space-y-3 pt-2 border-t border-line">
            <p className="text-xs text-ink-sub">Add new channels to distribute to:</p>
            <div className="grid grid-cols-2 gap-2">
              {CHANNELS.map(({ id: c, label, icon: Icon }) => {
                const already = existingChannels.has(c);
                const isOn = selected.has(c);
                return (
                  <button
                    key={c}
                    disabled={already}
                    onClick={() => toggle(c)}
                    className={`flex items-center gap-2 px-3 py-2 rounded-md border text-sm font-semibold transition-colors ${
                      already
                        ? 'border-line bg-line-soft text-ink-mute cursor-not-allowed'
                        : isOn
                          ? 'border-accent bg-accent-soft text-accent'
                          : 'border-line bg-white text-ink hover:border-accent'
                    }`}
                  >
                    <Icon className="w-4 h-4" />
                    {label}
                    {already && <span className="ml-auto text-[10px]">added</span>}
                  </button>
                );
              })}
            </div>
            <Button
              onClick={() => publishMutation.mutate(Array.from(selected))}
              disabled={selected.size === 0 || publishMutation.isPending}
              className="w-full"
            >
              {publishMutation.isPending ? <Spinner /> : null}
              Publish to {selected.size} channel{selected.size === 1 ? '' : 's'}
            </Button>
            {publishMutation.isError && (
              <p className="text-xs text-red-600">{(publishMutation.error as Error).message}</p>
            )}
          </div>
        )}
      </Card>

      {!isTerminal && (
        <Card className="p-6 space-y-3">
          <h2 className="text-sm font-bold text-ink">Close Posting</h2>
          <input
            value={closeReason}
            onChange={(e) => setCloseReason(e.target.value)}
            placeholder="Reason (e.g., position filled)"
            className="w-full h-10 px-3 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent"
          />
          <Button
            variant="danger"
            onClick={() => closeMutation.mutate()}
            disabled={!closeReason.trim() || closeMutation.isPending}
            className="w-full"
          >
            {closeMutation.isPending ? <Spinner /> : <XCircle className="w-4 h-4" />}
            Close Posting
          </Button>
        </Card>
      )}

      {posting.close_reason && (
        <Card className="p-4 text-sm text-ink-sub">
          <span className="font-semibold text-ink">Closed:</span> {posting.close_reason}
        </Card>
      )}
    </div>
  );
}

function Section({ title, items }: { title: string; items: string[] }) {
  return (
    <div>
      <h3 className="text-[10px] font-bold uppercase tracking-widest text-ink-sub mb-1.5">{title}</h3>
      <ul className="list-disc list-inside text-sm text-ink space-y-0.5">
        {items.map((item, i) => <li key={i}>{item}</li>)}
      </ul>
    </div>
  );
}

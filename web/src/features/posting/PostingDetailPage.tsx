import { useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, XCircle } from 'lucide-react';
import { postingApi } from '@/api/posting';
import { Button, Card, Spinner } from '@/components/ui/primitives';
import { PostingStatusBadge } from '@/components/ui/StatusBadge';
import { UploadCard } from '@/features/sourcing/UploadCard';
import { CandidateListSection } from '@/features/sourcing/CandidateListSection';

export function PostingDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const qc = useQueryClient();
  const [closeReason, setCloseReason] = useState('');

  const { data: posting, isLoading } = useQuery({
    queryKey: ['posting', id],
    queryFn: () => postingApi.get(id),
    enabled: !!id,
  });

  const publishMutation = useMutation({
    mutationFn: () => postingApi.publish(id, []),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['posting', id] });
      qc.invalidateQueries({ queryKey: ['postings'] });
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
  const isDraft = posting.status === 'DRAFT';
  const isActive = !isDraft && !isTerminal;

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

      {isDraft && (
        <Card className="p-6 space-y-4">
          <h2 className="text-sm font-bold text-ink">Ready to start sourcing?</h2>
          <p className="text-sm text-ink-sub">
            Publish this posting to begin uploading resumes and tracking candidates.
          </p>
          <Button
            onClick={() => publishMutation.mutate()}
            disabled={publishMutation.isPending}
            className="w-full"
          >
            {publishMutation.isPending ? <Spinner /> : null}
            Publish Posting
          </Button>
          {publishMutation.isError && (
            <p className="text-xs text-red-600">{(publishMutation.error as Error).message}</p>
          )}
        </Card>
      )}

      {isActive && (
        <>
          <UploadCard intentId={posting.intent_id} />
          <CandidateListSection intentId={posting.intent_id} />
        </>
      )}

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

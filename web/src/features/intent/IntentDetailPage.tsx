import { useParams, Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { intentApi } from '@/api/intent';
import { postingApi } from '@/api/posting';
import { Spinner, Card } from '@/components/ui/primitives';
import { IntentCard } from './IntentCard';

export function IntentDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const qc = useQueryClient();

  const { data: intent, isLoading } = useQuery({
    queryKey: ['intent', id],
    queryFn: () => intentApi.get(id),
    enabled: !!id,
  });

  // If the intent is confirmed, surface the resulting posting (if any).
  const { data: postings } = useQuery({
    queryKey: ['postings', { intent_id: id }],
    queryFn: () => postingApi.list({ intent_id: id }),
    enabled: !!intent && intent.status === 'CONFIRMED',
  });

  const confirmMutation = useMutation({
    mutationFn: () => intentApi.confirm(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['intent', id] });
      qc.invalidateQueries({ queryKey: ['intents'] });
      qc.invalidateQueries({ queryKey: ['postings'] });
    },
  });

  if (isLoading) return <div className="p-12 flex justify-center"><Spinner /></div>;
  if (!intent) return <div className="p-8">Not found.</div>;

  const linkedPosting = postings?.[0];

  return (
    <div className="px-8 py-6 max-w-3xl space-y-6">
      <Link to="/intents" className="inline-flex items-center gap-1 text-xs text-ink-sub hover:text-ink">
        <ArrowLeft className="w-3.5 h-3.5" /> Back to intents
      </Link>

      <IntentCard
        intent={intent}
        onConfirm={() => confirmMutation.mutate()}
        confirming={confirmMutation.isPending}
      />

      {confirmMutation.isError && (
        <Card className="p-4 text-sm text-red-600">{(confirmMutation.error as Error).message}</Card>
      )}

      {intent.status === 'CONFIRMED' && (
        <Card className="p-5">
          <h3 className="text-sm font-bold text-ink mb-2">Linked Job Posting</h3>
          {linkedPosting ? (
            <Link to={`/postings/${linkedPosting.id}`} className="text-sm text-accent hover:underline">
              {linkedPosting.jd.title} · {linkedPosting.status}
            </Link>
          ) : (
            <p className="text-xs text-ink-sub">
              The posting is being drafted by the IntentConfirmed consumer. Refresh in a moment.
            </p>
          )}
        </Card>
      )}
    </div>
  );
}

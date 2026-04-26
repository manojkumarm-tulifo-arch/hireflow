import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useParams, Link } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { intentApi } from '@/api/intent';
import { postingApi } from '@/api/posting';
import { Spinner, Card } from '@/components/ui/primitives';
import { IntentCard } from './IntentCard';
export function IntentDetailPage() {
    const { id = '' } = useParams();
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
    if (isLoading)
        return _jsx("div", { className: "p-12 flex justify-center", children: _jsx(Spinner, {}) });
    if (!intent)
        return _jsx("div", { className: "p-8", children: "Not found." });
    const linkedPosting = postings?.[0];
    return (_jsxs("div", { className: "px-8 py-6 max-w-3xl space-y-6", children: [_jsxs(Link, { to: "/intents", className: "inline-flex items-center gap-1 text-xs text-ink-sub hover:text-ink", children: [_jsx(ArrowLeft, { className: "w-3.5 h-3.5" }), " Back to intents"] }), _jsx(IntentCard, { intent: intent, onConfirm: () => confirmMutation.mutate(), confirming: confirmMutation.isPending }), confirmMutation.isError && (_jsx(Card, { className: "p-4 text-sm text-red-600", children: confirmMutation.error.message })), intent.status === 'CONFIRMED' && (_jsxs(Card, { className: "p-5", children: [_jsx("h3", { className: "text-sm font-bold text-ink mb-2", children: "Linked Job Posting" }), linkedPosting ? (_jsxs(Link, { to: `/postings/${linkedPosting.id}`, className: "text-sm text-accent hover:underline", children: [linkedPosting.jd.title, " \u00B7 ", linkedPosting.status] })) : (_jsx("p", { className: "text-xs text-ink-sub", children: "The posting is being drafted by the IntentConfirmed consumer. Refresh in a moment." }))] }))] }));
}

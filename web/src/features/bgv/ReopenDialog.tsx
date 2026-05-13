import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { bgvApi } from '@/api/bgv';
import { ApiError } from '@/api/client';
import { Button } from '@/components/ui/primitives';

/**
 * ReopenDialog — modal form for the recruiter Reopen action. The
 * aggregate requires a non-empty reason (audit-trail invariant) and
 * rejects from INVITED / IN_PROGRESS / VERIFIED states. We mirror the
 * non-empty check client-side and surface the BE error code if the
 * state guard trips (e.g. someone re-opens then this dialog races).
 */
export function ReopenDialog({
  submissionId,
  onClose,
  onReopened,
}: {
  submissionId: string;
  onClose: () => void;
  onReopened: () => void;
}) {
  const [reason, setReason] = useState('');
  const queryClient = useQueryClient();

  const mutation = useMutation({
    mutationFn: () => bgvApi.reopen(submissionId, reason.trim()),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bgv.submission', submissionId] });
      queryClient.invalidateQueries({ queryKey: ['bgv.timeline', submissionId] });
      queryClient.invalidateQueries({ queryKey: ['bgv.list'] });
      onReopened();
    },
  });

  const trimmed = reason.trim();
  const blocked = trimmed.length === 0 || mutation.isPending;
  const err = mutation.error instanceof ApiError ? mutation.error : null;

  return (
    <div className="fixed inset-0 bg-ink/40 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-md">
        <div className="flex items-center justify-between px-5 py-4 border-b border-line">
          <h2 className="text-base font-bold text-ink">Reopen submission</h2>
          <button
            type="button"
            onClick={onClose}
            className="text-ink-mute hover:text-ink"
            aria-label="Close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!blocked) mutation.mutate();
          }}
          className="px-5 py-4 space-y-4"
        >
          <p className="text-sm text-ink-sub">
            Re-opens the submission so the candidate can fix something. The reason is recorded on
            the audit timeline; both the candidate and the recruiter side will see it.
          </p>
          <label className="block">
            <span className="text-[11px] font-bold uppercase tracking-wider text-ink-sub">
              Reason
            </span>
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="e.g. PAN scan was unreadable — please re-upload"
              rows={4}
              className="mt-1 w-full px-3 py-2 rounded-md text-sm bg-line-soft border border-line focus:outline-none focus:border-accent resize-none"
              autoFocus
              disabled={mutation.isPending}
            />
          </label>
          {err && (
            <div className="rounded-md border border-red-200 bg-red-50 p-3 text-xs text-red-700">
              <p className="font-semibold">{friendlyReopenMessage(err)}</p>
              {err.code && (
                <p className="text-red-600/80 mt-0.5">Code: {err.code}</p>
              )}
            </div>
          )}
          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="ghost" onClick={onClose} disabled={mutation.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={blocked}>
              {mutation.isPending ? 'Reopening…' : 'Confirm reopen'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

function friendlyReopenMessage(err: ApiError): string {
  switch (err.code) {
    case 'invalid_input':
      return 'Reason is required.';
    case 'invalid_state_transition':
      return "This submission can't be reopened in its current state.";
    case 'forbidden_subject':
      return 'Only recruiters can reopen submissions.';
    default:
      return err.message || 'Reopen failed.';
  }
}

import { Link } from 'react-router-dom';
import { MoreHorizontal } from 'lucide-react';
import { Button } from '@/components/ui/primitives';
import { Menu, MenuItem } from '@/components/ui/Menu';
import type { Application, ApplicationStatus } from '@/api/types';

const TERMINAL: ApplicationStatus[] = ['Hired', 'Rejected'];

interface ApplicationActionsProps {
  application: Application;
  onShortlist?: (id: string) => void;
  onHire?: (id: string) => void;
  onReject?: (id: string) => void;
}

export function ApplicationActions({
  application,
  onShortlist,
  onHire,
  onReject,
}: ApplicationActionsProps) {
  const { id, status } = application;

  if (TERMINAL.includes(status)) {
    return <span className="text-ink-sub text-sm">—</span>;
  }

  const showShortlistPrimary = status === 'Scored';
  const showHirePrimary =
    status === 'Shortlisted' || status === 'Interviewing';

  // ⋮ menu items:
  // "Re-shortlist" when not Scored
  // "Hire" when not Shortlisted/Interviewing/Hired (already in primary for those)
  // "Reject" (danger)
  // "View detail" (Link — 404 until deep-dive slice)
  const showReShortlistItem = status !== 'Scored';
  const showHireMenuItem =
    status !== 'Shortlisted' && status !== 'Interviewing' && status !== 'Hired';

  return (
    <div className="flex items-center gap-2">
      {showShortlistPrimary && (
        <Button size="sm" variant="primary" onClick={() => onShortlist?.(id)}>
          Shortlist
        </Button>
      )}
      {showHirePrimary && (
        <Button size="sm" variant="primary" onClick={() => onHire?.(id)}>
          Hire
        </Button>
      )}

      <Menu trigger={<MoreHorizontal className="w-4 h-4" />}>
        {showReShortlistItem && (
          <MenuItem onClick={() => onShortlist?.(id)}>Re-shortlist</MenuItem>
        )}
        {showHireMenuItem && (
          <MenuItem onClick={() => onHire?.(id)}>Hire</MenuItem>
        )}
        <MenuItem danger onClick={() => onReject?.(id)}>
          Reject
        </MenuItem>
        <Link
          to={`/candidates/${application.candidate_id}`}
          className="w-full text-left px-3 py-2 text-sm text-ink hover:bg-line-soft block"
        >
          View detail
        </Link>
      </Menu>
    </div>
  );
}

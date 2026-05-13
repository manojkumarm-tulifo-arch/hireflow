import { Badge } from './primitives';
import type { IntentStatus, PostingStatus } from '@/api/types';
import type { BGVStatus } from '@/api/bgvTypes';

const intentTone: Record<IntentStatus, 'neutral' | 'accent' | 'success' | 'warning' | 'danger'> = {
  DRAFTED: 'warning',
  CONFIRMED: 'success',
  CANCELLED: 'danger',
  CLOSED: 'neutral',
};
const postingTone: Record<PostingStatus, 'neutral' | 'accent' | 'success' | 'warning' | 'danger'> = {
  DRAFT: 'warning',
  PUBLISHED: 'success',
  CLOSED: 'neutral',
  ARCHIVED: 'neutral',
};
const bgvTone: Record<BGVStatus, 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info'> = {
  INVITED: 'neutral',
  IN_PROGRESS: 'info',
  SUBMITTED: 'accent',
  UNDER_REVIEW: 'warning',
  VERIFIED: 'success',
  FLAGGED: 'danger',
};

export function IntentStatusBadge({ status }: { status: IntentStatus }) {
  return <Badge tone={intentTone[status]}>{status}</Badge>;
}
export function PostingStatusBadge({ status }: { status: PostingStatus }) {
  return <Badge tone={postingTone[status]}>{status}</Badge>;
}
export function BGVStatusBadge({ status }: { status: BGVStatus }) {
  return <Badge tone={bgvTone[status]}>{status.replace('_', ' ')}</Badge>;
}

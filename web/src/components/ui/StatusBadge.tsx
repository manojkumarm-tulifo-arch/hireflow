import { Badge } from './primitives';
import type { IntentStatus, PostingStatus } from '@/api/types';

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

export function IntentStatusBadge({ status }: { status: IntentStatus }) {
  return <Badge tone={intentTone[status]}>{status}</Badge>;
}
export function PostingStatusBadge({ status }: { status: PostingStatus }) {
  return <Badge tone={postingTone[status]}>{status}</Badge>;
}

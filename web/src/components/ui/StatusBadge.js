import { jsx as _jsx } from "react/jsx-runtime";
import { Badge } from './primitives';
const intentTone = {
    DRAFTED: 'warning',
    CONFIRMED: 'success',
    CANCELLED: 'danger',
    CLOSED: 'neutral',
};
const postingTone = {
    DRAFT: 'warning',
    PUBLISHED: 'success',
    CLOSED: 'neutral',
    ARCHIVED: 'neutral',
};
export function IntentStatusBadge({ status }) {
    return _jsx(Badge, { tone: intentTone[status], children: status });
}
export function PostingStatusBadge({ status }) {
    return _jsx(Badge, { tone: postingTone[status], children: status });
}

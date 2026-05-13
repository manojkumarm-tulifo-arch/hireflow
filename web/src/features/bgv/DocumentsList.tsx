import { useState } from 'react';
import { Download, FileText } from 'lucide-react';
import { downloadDocument } from '@/api/bgv';
import type { BGVDocument } from '@/api/bgvTypes';
import { Badge, Spinner } from '@/components/ui/primitives';
import { ApiError } from '@/api/client';

interface DocumentsListProps {
  submissionId: string;
  documents: BGVDocument[];
}

/**
 * DocumentsList renders the captured catalog grouped by category. Each
 * captured row gets a Download button that auth-fetches the file from
 * candidate-bgv (works for both the local-fs adapter, which streams
 * bytes inline, and the S3 adapter, which 302s to a pre-signed URL —
 * fetch follows the redirect transparently).
 *
 * Pending rows render greyed out with no action; the recruiter can
 * still see what hasn't been captured.
 */
export function DocumentsList({ submissionId, documents }: DocumentsListProps) {
  if (documents.length === 0) {
    return <p className="text-xs text-ink-mute italic">No documents on this submission.</p>;
  }

  const groups = groupByCategory(documents);
  return (
    <div className="space-y-5">
      {groups.map(([category, docs]) => (
        <div key={category}>
          <p className="text-[11px] font-bold uppercase tracking-wider text-ink-mute mb-2">
            {category}
          </p>
          <div className="grid sm:grid-cols-2 gap-2">
            {docs.map((d) => (
              <DocumentRow key={d.id} submissionId={submissionId} doc={d} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function DocumentRow({ submissionId, doc }: { submissionId: string; doc: BGVDocument }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const captured = doc.status === 'CAPTURED' && doc.file !== undefined;
  async function onDownload() {
    setBusy(true);
    setErr(null);
    try {
      await downloadDocument(submissionId, doc.id, doc.label);
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'Download failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className={`flex items-center gap-3 p-3 rounded-lg border ${
        captured ? 'border-line bg-white' : 'border-dashed border-line bg-line-soft'
      }`}
    >
      <FileText
        className={`w-4 h-4 shrink-0 ${captured ? 'text-accent' : 'text-ink-mute'}`}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <p className="text-sm font-semibold text-ink truncate">{doc.label}</p>
          {!doc.required && <Badge tone="neutral">Optional</Badge>}
          {doc.custom && <Badge tone="info">Custom</Badge>}
        </div>
        {captured && doc.file ? (
          <p className="text-[11px] text-ink-mute mt-0.5">
            {formatBytes(doc.file.size_bytes)} · {doc.file.mime_type}
          </p>
        ) : (
          <p className="text-[11px] text-ink-mute mt-0.5">Not captured</p>
        )}
        {err && <p className="text-[11px] text-red-600 mt-1">{err}</p>}
      </div>
      {captured && (
        <button
          type="button"
          onClick={onDownload}
          disabled={busy}
          className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-bold text-accent hover:bg-accent-soft disabled:opacity-50"
        >
          {busy ? <Spinner /> : <Download className="w-3.5 h-3.5" />}
          {busy ? '' : 'Download'}
        </button>
      )}
    </div>
  );
}

function groupByCategory(docs: BGVDocument[]): Array<[string, BGVDocument[]]> {
  const map = new Map<string, BGVDocument[]>();
  for (const d of docs) {
    const arr = map.get(d.category) ?? [];
    arr.push(d);
    map.set(d.category, arr);
  }
  return Array.from(map.entries());
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

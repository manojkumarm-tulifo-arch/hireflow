import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ChevronLeft, RotateCcw } from 'lucide-react';
import { bgvApi } from '@/api/bgv';
import type {
  BGVAddress,
  BGVDeclarations,
  BGVDigitalProfile,
  BGVEmergencyContacts,
  BGVPersonalInfo,
  BGVReferences,
  BGVStatus,
} from '@/api/bgvTypes';
import { Button, Card, Spinner } from '@/components/ui/primitives';
import { BGVStatusBadge } from '@/components/ui/StatusBadge';
import { Timeline } from './Timeline';
import { DocumentsList } from './DocumentsList';
import { ReopenDialog } from './ReopenDialog';

const REOPENABLE_STATUSES: BGVStatus[] = ['SUBMITTED', 'UNDER_REVIEW', 'FLAGGED'];

export function BGVDetailPage() {
  const { id = '' } = useParams<{ id: string }>();
  const [reopenOpen, setReopenOpen] = useState(false);

  const subQuery = useQuery({
    queryKey: ['bgv.submission', id],
    queryFn: () => bgvApi.get(id),
    enabled: id.length > 0,
  });

  const timelineQuery = useQuery({
    queryKey: ['bgv.timeline', id],
    queryFn: () => bgvApi.timeline(id),
    enabled: id.length > 0,
  });

  if (subQuery.isLoading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner />
      </div>
    );
  }
  if (subQuery.error) {
    return (
      <div className="px-8 py-6 max-w-5xl">
        <Card className="p-4 text-sm text-red-600">
          {(subQuery.error as Error).message}
        </Card>
      </div>
    );
  }
  const sub = subQuery.data;
  if (!sub) return null;

  const canReopen = REOPENABLE_STATUSES.includes(sub.status);

  return (
    <div className="px-8 py-6 max-w-5xl space-y-6">
      <Link
        to="/bgv"
        className="inline-flex items-center gap-1 text-xs text-ink-sub hover:text-ink"
      >
        <ChevronLeft className="w-3.5 h-3.5" />
        Back to queue
      </Link>

      <header className="flex items-start justify-between gap-4 flex-wrap">
        <div className="min-w-0">
          <div className="flex items-center gap-3 flex-wrap">
            <h1 className="text-2xl font-bold text-ink">{sub.candidate.name}</h1>
            <BGVStatusBadge status={sub.status} />
          </div>
          <p className="text-sm text-ink-sub mt-1">
            {[sub.candidate.position, sub.candidate.company].filter(Boolean).join(' · ') ||
              'Position not specified'}
          </p>
          <p className="text-xs text-ink-mute mt-0.5">
            {sub.candidate.email} · {sub.candidate.phone}
          </p>
        </div>
        {canReopen && (
          <Button variant="secondary" onClick={() => setReopenOpen(true)}>
            <RotateCcw className="w-3.5 h-3.5" />
            Reopen
          </Button>
        )}
      </header>

      <Card className="p-5">
        <SectionLabel>Lifecycle</SectionLabel>
        <div className="grid sm:grid-cols-3 gap-3">
          <Stamp label="Created" iso={sub.created_at} />
          <Stamp label="Started" iso={sub.started_at} />
          <Stamp label="Submitted" iso={sub.submitted_at} />
          <Stamp label="Last update" iso={sub.updated_at} />
          <Stamp label="Reviewed" iso={sub.reviewed_at} />
          <Stamp label="Token" iso={null} value={sub.token} mono />
        </div>
      </Card>

      <SectionedCard title="Personal Information">
        <PersonalView personal={sub.personal} />
      </SectionedCard>

      <SectionedCard title="Address">
        <AddressView address={sub.address} />
      </SectionedCard>

      <SectionedCard title="Emergency Contacts">
        <EmergencyView emergency={sub.emergency} />
      </SectionedCard>

      <SectionedCard title="Documents">
        <DocumentsList submissionId={sub.id} documents={sub.documents} />
      </SectionedCard>

      <SectionedCard title="Professional References">
        <ReferencesView refs={sub.references} />
      </SectionedCard>

      <SectionedCard title="Digital Profile">
        <DigitalView digital={sub.digital} />
      </SectionedCard>

      <SectionedCard title="Declarations & Consent">
        <DeclarationsView declarations={sub.declarations} />
      </SectionedCard>

      <SectionedCard title="Timeline">
        {timelineQuery.isLoading && <Spinner />}
        {timelineQuery.error && (
          <p className="text-sm text-red-600">
            Timeline unavailable: {(timelineQuery.error as Error).message}
          </p>
        )}
        {timelineQuery.data && <Timeline items={timelineQuery.data.items} />}
      </SectionedCard>

      {reopenOpen && (
        <ReopenDialog
          submissionId={sub.id}
          onClose={() => setReopenOpen(false)}
          onReopened={() => setReopenOpen(false)}
        />
      )}
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="text-[11px] font-bold uppercase tracking-wider text-ink-mute mb-3">
      {children}
    </p>
  );
}

function SectionedCard({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Card className="p-5">
      <SectionLabel>{title}</SectionLabel>
      {children}
    </Card>
  );
}

function Stamp({
  label,
  iso,
  value,
  mono,
}: {
  label: string;
  iso?: string | null;
  value?: string;
  mono?: boolean;
}) {
  const text = value ?? (iso ? formatStamp(iso) : '—');
  return (
    <div>
      <p className="text-[10px] font-bold uppercase tracking-wider text-ink-mute">{label}</p>
      <p className={`text-sm text-ink mt-0.5 ${mono ? 'font-mono' : ''}`}>{text}</p>
    </div>
  );
}

function formatStamp(iso: string | null | undefined): string {
  if (!iso) return '—';
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  return new Date(t).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function MissingNote({ label }: { label: string }) {
  return <p className="text-xs text-ink-mute italic">{label} not yet captured.</p>;
}

function KvGrid({ items }: { items: Array<[string, string | undefined]> }) {
  return (
    <dl className="grid sm:grid-cols-2 gap-x-6 gap-y-3">
      {items.map(([k, v]) => (
        <div key={k}>
          <dt className="text-[10px] font-bold uppercase tracking-wider text-ink-mute">
            {k}
          </dt>
          <dd className="text-sm text-ink mt-0.5 break-words">{v && v.length > 0 ? v : '—'}</dd>
        </div>
      ))}
    </dl>
  );
}

function PersonalView({ personal }: { personal?: BGVPersonalInfo }) {
  if (!personal) return <MissingNote label="Personal information" />;
  return (
    <KvGrid
      items={[
        ['Full legal name', personal.full_name],
        ['Date of birth', formatDate(personal.dob)],
        ['Gender', personal.gender],
        ['Blood group', personal.blood_group],
        ['Marital status', personal.marital],
        ['Father’s name', personal.father_name],
        ['Mother’s name', personal.mother_name],
        ['Nationality', personal.nationality],
        ['Languages', personal.languages],
      ]}
    />
  );
}

function AddressView({ address }: { address?: BGVAddress }) {
  if (!address) return <MissingNote label="Address" />;
  return (
    <div className="space-y-4">
      <div>
        <p className="text-[10px] font-bold uppercase tracking-wider text-ink-mute mb-1">
          Current
        </p>
        <KvGrid
          items={[
            ['Line 1', address.current.line1],
            ['Line 2', address.current.line2],
            ['City', address.current.city],
            ['State', address.current.state],
            ['PIN', address.current.pin],
            ['Country', address.current.country],
            ['Duration', address.duration],
          ]}
        />
      </div>
      {address.perm_same ? (
        <p className="text-xs text-ink-sub italic">
          Permanent address marked same as current.
        </p>
      ) : address.permanent ? (
        <div>
          <p className="text-[10px] font-bold uppercase tracking-wider text-ink-mute mb-1">
            Permanent
          </p>
          <KvGrid
            items={[
              ['Line 1', address.permanent.line1],
              ['Line 2', address.permanent.line2],
              ['City', address.permanent.city],
              ['State', address.permanent.state],
              ['PIN', address.permanent.pin],
              ['Country', address.permanent.country],
            ]}
          />
        </div>
      ) : null}
    </div>
  );
}

function EmergencyView({ emergency }: { emergency?: BGVEmergencyContacts }) {
  if (!emergency) return <MissingNote label="Emergency contacts" />;
  return (
    <div className="space-y-4">
      <div>
        <p className="text-[10px] font-bold uppercase tracking-wider text-ink-mute mb-1">
          Primary
        </p>
        <KvGrid
          items={[
            ['Name', emergency.primary.name],
            ['Relationship', emergency.primary.relationship],
            ['Phone', emergency.primary.phone],
            ['Email', emergency.primary.email],
          ]}
        />
      </div>
      {emergency.secondary && (
        <div>
          <p className="text-[10px] font-bold uppercase tracking-wider text-ink-mute mb-1">
            Secondary
          </p>
          <KvGrid
            items={[
              ['Name', emergency.secondary.name],
              ['Relationship', emergency.secondary.relationship],
              ['Phone', emergency.secondary.phone],
              ['Email', emergency.secondary.email],
            ]}
          />
        </div>
      )}
    </div>
  );
}

function ReferencesView({ refs }: { refs?: BGVReferences }) {
  if (!refs) return <MissingNote label="References" />;
  return (
    <div className="space-y-4">
      {([refs.first, refs.second] as const).map((r, i) => (
        <div key={i}>
          <p className="text-[10px] font-bold uppercase tracking-wider text-ink-mute mb-1">
            Reference {i + 1}
          </p>
          <KvGrid
            items={[
              ['Name', r.name],
              ['Designation', r.designation],
              ['Company', r.company],
              ['Phone', r.phone],
              ['Email', r.email],
              ['Relationship', r.relationship],
            ]}
          />
        </div>
      ))}
    </div>
  );
}

function DigitalView({ digital }: { digital?: BGVDigitalProfile }) {
  if (!digital) return <MissingNote label="Digital profile" />;
  return (
    <KvGrid
      items={[
        ['LinkedIn', digital.linkedin],
        ['GitHub', digital.github],
        ['Portfolio', digital.portfolio],
        ['Memberships', digital.memberships],
      ]}
    />
  );
}

function DeclarationsView({ declarations }: { declarations?: BGVDeclarations }) {
  if (!declarations) return <MissingNote label="Declarations" />;
  const consents: Array<[string, boolean]> = [
    ['BGV consent', declarations.bgv_consent],
    ['NDA / confidentiality', declarations.nda],
    ['Code of conduct', declarations.code_of_conduct],
    ['DPDP / GDPR consent', declarations.dpdp_consent],
    ['Criminal-record self-declaration', declarations.criminal_declaration],
  ];
  return (
    <div className="space-y-4">
      <ul className="space-y-1.5">
        {consents.map(([label, ok]) => (
          <li key={label} className="flex items-center gap-2 text-sm">
            <span
              className={`w-4 h-4 rounded-full inline-flex items-center justify-center text-[10px] font-bold ${
                ok ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'
              }`}
            >
              {ok ? '✓' : '✕'}
            </span>
            <span className="text-ink">{label}</span>
          </li>
        ))}
      </ul>
      <div>
        <p className="text-[10px] font-bold uppercase tracking-wider text-ink-mute mb-1">
          Signature
        </p>
        <p className="text-base text-ink italic">{declarations.signature || '—'}</p>
      </div>
    </div>
  );
}

function formatDate(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  return new Date(t).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}

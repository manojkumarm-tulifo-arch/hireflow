import { useState, useMemo, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Spinner, Button, EmptyState } from '@/components/ui/primitives'
import { CandidateCard } from './CandidateCard'
import { CandidateDenseRow } from './CandidateDenseRow'
import { BulkActionBar } from './BulkActionBar'
import { CandidateListToolbar } from './CandidateListToolbar'
import type { SortKey, Density } from './CandidateListToolbar'
import { useApplicationsList, flattenApplications } from './useApplicationsList'
import { useShortlist } from './useShortlist'
import { useHire } from './useHire'
import { useReject } from './useReject'
import type { Application, ApplicationStatus } from '@/api/types'

const DENSITY_KEY = 'hireflow.candidateListDensity'

function loadDensity(): Density {
  try {
    const stored = localStorage.getItem(DENSITY_KEY)
    if (stored === 'dense' || stored === 'card') return stored
  } catch {
    // ignore
  }
  return 'card'
}

function saveDensity(d: Density) {
  try {
    localStorage.setItem(DENSITY_KEY, d)
  } catch {
    // ignore
  }
}

function sortApplications(apps: Application[], sort: SortKey): Application[] {
  const copy = [...apps]
  switch (sort) {
    case 'score_desc':
      return copy.sort((a, b) => (b.overall_score ?? -1) - (a.overall_score ?? -1))
    case 'recent':
      return copy.sort(
        (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
      )
    case 'name_asc':
      return copy.sort((a, b) =>
        a.candidate.full_name.localeCompare(b.candidate.full_name),
      )
    default:
      return copy
  }
}

function filterBySearch(apps: Application[], q: string): Application[] {
  if (!q.trim()) return apps
  const lower = q.toLowerCase()
  return apps.filter(
    (app) =>
      app.candidate.full_name.toLowerCase().includes(lower) ||
      app.candidate.headline.toLowerCase().includes(lower) ||
      app.candidate.location.toLowerCase().includes(lower) ||
      app.candidate.judge_summary.toLowerCase().includes(lower),
  )
}

interface CandidateListSectionProps {
  intentId: string
}

export function CandidateListSection({ intentId }: CandidateListSectionProps) {
  const [searchParams, setSearchParams] = useSearchParams()

  // URL-backed filter + sort
  const activeStatus = searchParams.get('status') ?? ''
  const sort = (searchParams.get('sort') as SortKey) ?? 'score_desc'

  function setStatus(value: string) {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      if (value) next.set('status', value)
      else next.delete('status')
      return next
    })
  }

  function setSort(value: SortKey) {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      next.set('sort', value)
      return next
    })
  }

  // localStorage-backed density
  const [density, setDensityState] = useState<Density>(loadDensity)
  function setDensity(d: Density) {
    saveDensity(d)
    setDensityState(d)
  }

  // Local search state
  const [search, setSearch] = useState('')

  // Selection state
  const [selected, setSelected] = useState<Set<string>>(new Set())

  function handleSelect(id: string, checked: boolean) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })
  }

  function clearSelection() {
    setSelected(new Set())
  }

  // Data
  const query = useApplicationsList(intentId, activeStatus || undefined)
  const allPages = query.data?.pages
  const allApps = flattenApplications(allPages)

  // Counts by status from loaded pages (approximate)
  const countsByStatus = useMemo(() => {
    const counts: Partial<Record<ApplicationStatus | '', number>> = {}
    counts[''] = allApps.length
    for (const app of allApps) {
      counts[app.status] = (counts[app.status] ?? 0) + 1
    }
    return counts
  }, [allApps])

  // Client-side search + sort
  const displayed = useMemo(
    () => sortApplications(filterBySearch(allApps, search), sort),
    [allApps, search, sort],
  )

  // Mutations
  const shortlist = useShortlist(intentId)
  const hire = useHire(intentId)
  const reject = useReject(intentId)

  const handleShortlist = useCallback(
    (id: string) => shortlist.mutate(id),
    [shortlist],
  )
  const handleHire = useCallback((id: string) => hire.mutate(id), [hire])
  const handleReject = useCallback(
    (id: string) => {
      const reason = window.prompt('Enter a reason for rejection:')
      if (reason === null || reason.trim() === '') return
      reject.mutate({ applicationId: id, reason: reason.trim() })
    },
    [reject],
  )

  // Empty state message
  function emptyMessage() {
    if (search) return { title: 'No matches', hint: 'Try a different search term.' }
    if (activeStatus)
      return {
        title: `No ${activeStatus.toLowerCase()} candidates`,
        hint: 'Try selecting a different status filter.',
      }
    return {
      title: 'No candidates yet',
      hint: 'Upload resumes above to start sourcing.',
    }
  }

  return (
    <div className="flex flex-col min-h-0">
      <CandidateListToolbar
        activeStatus={activeStatus}
        onStatusChange={setStatus}
        countsByStatus={countsByStatus}
        search={search}
        onSearchChange={setSearch}
        sort={sort}
        onSortChange={setSort}
        density={density}
        onDensityChange={setDensity}
      />

      {/* List body */}
      <div className="flex-1 px-4 py-3 space-y-2">
        {query.isLoading && (
          <div className="flex justify-center py-12">
            <Spinner className="w-6 h-6" />
          </div>
        )}

        {query.isError && (
          <EmptyState
            title="Failed to load candidates"
            hint="Check your connection and try again."
          />
        )}

        {!query.isLoading && !query.isError && displayed.length === 0 && (
          <EmptyState {...emptyMessage()} />
        )}

        {!query.isLoading && !query.isError && displayed.length > 0 && (
          <>
            {density === 'card' ? (
              <div className="space-y-2">
                {displayed.map((app) => (
                  <CandidateCard
                    key={app.id}
                    application={app}
                    selected={selected.has(app.id)}
                    onSelect={handleSelect}
                    onShortlist={handleShortlist}
                    onHire={handleHire}
                    onReject={handleReject}
                  />
                ))}
              </div>
            ) : (
              <div className="overflow-x-auto rounded-xl border border-line bg-white shadow-sm">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-line bg-line-soft/50 text-left">
                      <th className="px-3 py-2 w-8" />
                      <th className="px-3 py-2 font-semibold text-ink text-xs uppercase tracking-wide">
                        Name
                      </th>
                      <th className="px-3 py-2 font-semibold text-ink text-xs uppercase tracking-wide">
                        Headline
                      </th>
                      <th className="px-3 py-2 font-semibold text-ink text-xs uppercase tracking-wide">
                        Location
                      </th>
                      <th className="px-3 py-2 text-right font-semibold text-ink text-xs uppercase tracking-wide">
                        Score
                      </th>
                      <th className="px-3 py-2 font-semibold text-ink text-xs uppercase tracking-wide">
                        Band
                      </th>
                      <th className="px-3 py-2 font-semibold text-ink text-xs uppercase tracking-wide">
                        Status
                      </th>
                      <th className="px-3 py-2" />
                    </tr>
                  </thead>
                  <tbody>
                    {displayed.map((app) => (
                      <CandidateDenseRow
                        key={app.id}
                        application={app}
                        selected={selected.has(app.id)}
                        onSelect={handleSelect}
                        onShortlist={handleShortlist}
                        onHire={handleHire}
                        onReject={handleReject}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {/* Load more */}
            {query.hasNextPage && (
              <div className="flex justify-center py-4">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => query.fetchNextPage()}
                  disabled={query.isFetchingNextPage}
                >
                  {query.isFetchingNextPage ? (
                    <span className="flex items-center gap-2">
                      <Spinner />
                      Loading…
                    </span>
                  ) : (
                    'Load more'
                  )}
                </Button>
              </div>
            )}
          </>
        )}
      </div>

      {/* Bulk action bar */}
      <BulkActionBar
        selectedIds={selected}
        intentId={intentId}
        onClear={clearSelection}
      />
    </div>
  )
}

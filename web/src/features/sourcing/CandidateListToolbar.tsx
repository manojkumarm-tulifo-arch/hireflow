import { cn } from '@/lib/cn'
import { Input } from '@/components/ui/primitives'
import { AlignJustify, LayoutGrid } from 'lucide-react'
import type { ApplicationStatus } from '@/api/types'

export type SortKey = 'score_desc' | 'recent' | 'name_asc'
export type Density = 'dense' | 'card'

interface StatusChipDef {
  label: string
  value: string
}

const STATUS_CHIPS: StatusChipDef[] = [
  { label: 'All', value: '' },
  { label: 'Scored', value: 'Scored' },
  { label: 'Shortlisted', value: 'Shortlisted' },
  { label: 'Interviewing', value: 'Interviewing' },
  { label: 'Hired', value: 'Hired' },
  { label: 'Rejected', value: 'Rejected' },
]

const SORT_OPTIONS: { label: string; value: SortKey }[] = [
  { label: 'Score (high → low)', value: 'score_desc' },
  { label: 'Most recent', value: 'recent' },
  { label: 'Name (A → Z)', value: 'name_asc' },
]

interface CandidateListToolbarProps {
  activeStatus: string
  onStatusChange: (status: string) => void
  countsByStatus: Partial<Record<ApplicationStatus | '', number>>
  search: string
  onSearchChange: (q: string) => void
  sort: SortKey
  onSortChange: (sort: SortKey) => void
  density: Density
  onDensityChange: (density: Density) => void
}

export function CandidateListToolbar({
  activeStatus,
  onStatusChange,
  countsByStatus,
  search,
  onSearchChange,
  sort,
  onSortChange,
  density,
  onDensityChange,
}: CandidateListToolbarProps) {
  return (
    <div className="sticky top-0 z-20 bg-white border-b border-line px-4 py-2 space-y-2">
      {/* Status chips */}
      <div className="flex flex-wrap gap-1.5">
        {STATUS_CHIPS.map(({ label, value }) => {
          const count = countsByStatus[value as ApplicationStatus | '']
          const isActive = activeStatus === value
          return (
            <button
              key={value}
              onClick={() => onStatusChange(value)}
              className={cn(
                'inline-flex items-center gap-1 px-3 py-1 rounded-full text-xs font-semibold transition-colors',
                isActive
                  ? 'bg-accent text-white'
                  : 'bg-line-soft text-ink-sub hover:bg-line hover:text-ink',
              )}
            >
              {label}
              {count != null && count > 0 && (
                <span
                  className={cn(
                    'text-[10px] font-bold px-1.5 py-0.5 rounded-full',
                    isActive ? 'bg-white/20 text-white' : 'bg-line text-ink-sub',
                  )}
                >
                  {count}
                </span>
              )}
            </button>
          )
        })}
      </div>

      {/* Search + Sort + Density */}
      <div className="flex items-center gap-2">
        <div className="flex-1 max-w-xs">
          <Input
            placeholder="Search candidates…"
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            className="h-8 text-xs"
          />
        </div>

        <select
          value={sort}
          onChange={(e) => onSortChange(e.target.value as SortKey)}
          className="h-8 px-2 text-xs rounded-md border border-line bg-white text-ink focus:outline-none focus:border-accent"
        >
          {SORT_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>

        <div className="flex items-center border border-line rounded-md overflow-hidden">
          <button
            onClick={() => onDensityChange('card')}
            className={cn(
              'h-8 w-8 flex items-center justify-center transition-colors',
              density === 'card' ? 'bg-accent text-white' : 'text-ink-sub hover:bg-line-soft',
            )}
            title="Card view"
            aria-label="Card view"
          >
            <LayoutGrid className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={() => onDensityChange('dense')}
            className={cn(
              'h-8 w-8 flex items-center justify-center transition-colors',
              density === 'dense' ? 'bg-accent text-white' : 'text-ink-sub hover:bg-line-soft',
            )}
            title="Dense view"
            aria-label="Dense view"
          >
            <AlignJustify className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>
    </div>
  )
}

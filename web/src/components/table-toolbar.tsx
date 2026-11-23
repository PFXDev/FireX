import type { ReactNode } from 'react'
import { ArrowDownIcon, ArrowUpDownIcon, ArrowUpIcon, SearchIcon, XIcon } from 'lucide-react'

import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { TableHead } from '@/components/ui/table'
import { cn } from '@/lib/utils'

export type SortDirection = 'asc' | 'desc'

export function TableToolbar({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return <div className={cn('flex flex-wrap items-center gap-2 px-4', className)}>{children}</div>
}

export function SearchInput({
  value,
  onChange,
  placeholder = '搜索…',
  ariaLabel = '搜索',
  clearLabel = '清除搜索',
  className,
}: {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  ariaLabel?: string
  clearLabel?: string
  className?: string
}) {
  return (
    <InputGroup className={cn('w-full sm:w-64', className)}>
      <InputGroupInput
        aria-label={ariaLabel}
        placeholder={placeholder}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      <InputGroupAddon>
        <SearchIcon />
      </InputGroupAddon>
      {value && (
        <InputGroupAddon align="inline-end">
          <InputGroupButton size="icon-xs" aria-label={clearLabel} onClick={() => onChange('')}>
            <XIcon />
          </InputGroupButton>
        </InputGroupAddon>
      )}
    </InputGroup>
  )
}

export function SortableHead({
  label,
  active,
  direction,
  onToggle,
  className,
  align = 'left',
}: {
  label: string
  active: boolean
  direction: SortDirection
  onToggle: () => void
  className?: string
  align?: 'left' | 'right'
}) {
  const Icon = !active ? ArrowUpDownIcon : direction === 'asc' ? ArrowUpIcon : ArrowDownIcon
  return (
    <TableHead
      className={className}
      aria-sort={active ? (direction === 'asc' ? 'ascending' : 'descending') : 'none'}
    >
      <button
        type="button"
        onClick={onToggle}
        className={cn(
          'inline-flex items-center gap-1 rounded-sm py-0.5 text-left font-medium transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring',
          active ? 'text-foreground' : 'text-muted-foreground',
          align === 'right' && 'ml-auto'
        )}
      >
        <span>{label}</span>
        <Icon className={cn('size-3.5 shrink-0', active ? 'text-foreground' : 'opacity-60')} />
      </button>
    </TableHead>
  )
}

export function BulkActionBar({
  count,
  children,
  onClear,
  disabled = false,
  className,
}: {
  count: number
  children: ReactNode
  onClear?: () => void
  disabled?: boolean
  className?: string
}) {
  if (count <= 0) return null
  return (
    <div
      role="region"
      aria-label="批量操作"
      className={cn(
        'mx-4 flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/50 px-3 py-2 text-sm',
        className
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium tabular-nums">已选 {count} 项</span>
        <div className="flex flex-wrap items-center gap-1.5">{children}</div>
      </div>
      {onClear && (
        <button
          type="button"
          disabled={disabled}
          onClick={onClear}
          className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline disabled:pointer-events-none disabled:opacity-50"
        >
          取消选择
        </button>
      )}
    </div>
  )
}

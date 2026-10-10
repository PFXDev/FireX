import { RefreshCwIcon, TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'

/** The refresh control every list card carries in its CardAction. */
export function RefreshButton({
  loading,
  disabled,
  onClick,
  label = '刷新',
}: {
  loading: boolean
  disabled?: boolean
  onClick: () => void
  label?: string
}) {
  return (
    <Button type="button" variant="outline" size="sm" disabled={loading || disabled} onClick={onClick}>
      {loading ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
      {loading ? '加载中…' : label}
    </Button>
  )
}

/** A failed load, with the retry next to the message rather than elsewhere on the page. */
export function LoadErrorAlert({
  title,
  message,
  onRetry,
  retrying = false,
  className,
}: {
  title: string
  message: string
  onRetry?: () => void
  retrying?: boolean
  className?: string
}) {
  return (
    <Alert variant="destructive" className={className}>
      <TriangleAlertIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
      {onRetry && (
        <AlertAction>
          <Button type="button" variant="outline" size="sm" disabled={retrying} onClick={onRetry}>
            {retrying ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
            {retrying ? '重试中…' : '重试'}
          </Button>
        </AlertAction>
      )}
    </Alert>
  )
}

/**
 * Placeholder rows shaped like the table that will replace them. `columns`
 * takes the same responsive classes as the real TableHead cells so hidden
 * columns stay hidden while loading.
 */
export function TableSkeleton({
  columns,
  rows = 4,
}: {
  columns: Array<{ label?: string; className?: string; width?: string }>
  rows?: number
}) {
  return (
    <Table aria-busy="true" aria-label="正在加载">
      <TableHeader>
        <TableRow>
          {columns.map((column, index) => (
            <TableHead key={index} className={column.className}>
              {column.label ?? <span className="sr-only">操作</span>}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {Array.from({ length: rows }).map((_, row) => (
          <TableRow key={row}>
            {columns.map((column, index) => (
              <TableCell key={index} className={column.className}>
                {column.label === undefined ? (
                  <Skeleton className={cn('ml-auto h-7', column.width ?? 'w-20')} />
                ) : (
                  <Skeleton className={cn('h-4', column.width ?? 'w-24')} />
                )}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

/** Placeholder for a form or detail pane: label + control pairs. */
export function FieldsSkeleton({ fields = 4, className }: { fields?: number; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-5', className)} aria-busy="true" aria-label="正在加载">
      {Array.from({ length: fields }).map((_, index) => (
        <div key={index} className="flex flex-col gap-2">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-8 w-full" />
        </div>
      ))}
    </div>
  )
}

/** Placeholder for a vertical list of selectable rows, e.g. a master list. */
export function ListSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-1.5" aria-busy="true" aria-label="正在加载">
      {Array.from({ length: rows }).map((_, index) => (
        <Skeleton key={index} className="h-9 w-full" />
      ))}
    </div>
  )
}

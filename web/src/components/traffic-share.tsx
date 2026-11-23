import { useRef } from 'react'

import { NumberInput } from '@/components/number-input'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'

/** Categorical series colors in fixed order; see --series-* in index.css. */
export const SERIES_COLORS = Array.from({ length: 8 }, (_, i) => `var(--series-${i + 1})`)

export type ShareSlice = {
  key: string | number
  label: string
  value: number
  upload?: number
  download?: number
  limit?: number
  overQuota?: boolean
  href?: string
  meta?: string
}

/**
 * foldSlices keeps the largest seven slices and folds the rest into one, so a
 * chart never needs a ninth color.
 */
export function foldSlices(slices: ShareSlice[], otherLabel = '其他'): ShareSlice[] {
  const sorted = [...slices].filter((slice) => slice.value > 0).sort((a, b) => b.value - a.value)
  if (sorted.length <= SERIES_COLORS.length) return sorted
  const kept = sorted.slice(0, SERIES_COLORS.length - 1)
  const tail = sorted.slice(SERIES_COLORS.length - 1)
  const rest = tail.reduce((sum, slice) => sum + slice.value, 0)
  const hasDir = tail.some((s) => s.upload !== undefined || s.download !== undefined)
  return [
    ...kept,
    {
      key: '__other',
      label: otherLabel,
      value: rest,
      ...(hasDir
        ? {
            upload: tail.reduce((sum, slice) => sum + (slice.upload ?? 0), 0),
            download: tail.reduce((sum, slice) => sum + (slice.download ?? 0), 0),
          }
        : {}),
    },
  ]
}

function percent(value: number, total: number) {
  if (total <= 0) return '0%'
  const p = (value / total) * 100
  return p > 0 && p < 1 ? '<1%' : `${Math.round(p)}%`
}

/**
 * TrafficShareBar is a part-to-whole bar of byte counts (or custom counts via
 * formatValue) with a legend that carries every value, optional up/down
 * breakdown, quota hints, and deep-link navigation.
 */
export function TrafficShareBar({
  slices,
  ariaLabel,
  emptyText = '当前周期还没有流量。',
  formatValue = formatBytes,
}: {
  slices: ShareSlice[]
  ariaLabel: string
  emptyText?: string
  formatValue?: (value: number) => string
}) {
  const total = slices.reduce((sum, slice) => sum + slice.value, 0)
  if (total <= 0) {
    return <p className="text-sm text-muted-foreground">{emptyText}</p>
  }
  return (
    <div className="flex flex-col gap-3">
      <div role="img" aria-label={ariaLabel} className="flex h-3 w-full gap-0.5 overflow-hidden rounded-full">
        {slices.map((slice, i) => (
          <Tooltip key={slice.key}>
            <TooltipTrigger
              render={
                <span
                  className="h-full min-w-1 first:rounded-l-full last:rounded-r-full"
                  style={{ flexGrow: slice.value, flexBasis: 0, background: SERIES_COLORS[i % SERIES_COLORS.length] }}
                />
              }
            />
            <TooltipContent>
              <div className="flex flex-col gap-0.5 tabular-nums">
                <span>
                  {slice.label}：{formatValue(slice.value)}（{percent(slice.value, total)}）
                </span>
                {(slice.upload !== undefined || slice.download !== undefined) && (
                  <span className="text-[11px] opacity-85">
                    ↓ {formatBytes(slice.download ?? 0)} · ↑ {formatBytes(slice.upload ?? 0)}
                  </span>
                )}
                {slice.limit != null && slice.limit > 0 && (
                  <span className="text-[11px] opacity-85">
                    配额 {formatBytes(slice.value)} / {formatBytes(slice.limit)}（
                    {Math.min(999, Math.round((slice.value / slice.limit) * 100))}%）
                  </span>
                )}
              </div>
            </TooltipContent>
          </Tooltip>
        ))}
      </div>
      <ul className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
        {slices.map((slice, i) => {
          const quotaPct =
            slice.limit && slice.limit > 0 ? Math.round((slice.value / slice.limit) * 100) : null
          return (
            <li key={slice.key} className="flex min-w-0 flex-col gap-0.5">
              <div className="flex min-w-0 items-center gap-2">
                <span
                  aria-hidden
                  className="size-2.5 shrink-0 rounded-sm"
                  style={{ background: SERIES_COLORS[i % SERIES_COLORS.length] }}
                />
                {slice.href ? (
                  <a
                    href={slice.href}
                    className="min-w-0 flex-1 truncate underline-offset-4 hover:underline"
                    title={slice.label}
                  >
                    {slice.label}
                  </a>
                ) : (
                  <span className="min-w-0 flex-1 truncate" title={slice.label}>
                    {slice.label}
                  </span>
                )}
                <span className="shrink-0 font-medium tabular-nums">{formatValue(slice.value)}</span>
                <span className="w-10 shrink-0 text-right tabular-nums text-muted-foreground">
                  {percent(slice.value, total)}
                </span>
              </div>
              {(slice.upload !== undefined || slice.meta || quotaPct !== null) && (
                <div className="flex items-center justify-between gap-2 pl-4.5 text-[11px] text-muted-foreground tabular-nums">
                  <span className="truncate">
                    {slice.upload !== undefined && slice.download !== undefined
                      ? `↓ ${formatBytes(slice.download)} / ↑ ${formatBytes(slice.upload)}`
                      : slice.meta ?? ''}
                    {slice.upload !== undefined && slice.meta ? ` · ${slice.meta}` : ''}
                  </span>
                  {quotaPct !== null && (
                    <span
                      className={cn(
                        'shrink-0',
                        slice.overQuota || quotaPct >= 100
                          ? 'font-medium text-destructive'
                          : quotaPct >= 80
                            ? 'font-medium text-warning'
                            : '',
                      )}
                    >
                      配额 {quotaPct}%
                    </span>
                  )}
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

export type WeightSlice = {
  key: number
  label: string
  weight: number
}

/**
 * WeightSplitter edits percentages that add up to 100: drag a divider to move
 * share between its neighbours, use arrow keys for 1% steps, or enter an exact
 * integer percentage in the legend input (siblings rebalance automatically).
 */
export function WeightSplitter({
  slices,
  disabled,
  onChange,
}: {
  slices: WeightSlice[]
  disabled?: boolean
  onChange: (next: WeightSlice[]) => void
}) {
  const barRef = useRef<HTMLDivElement>(null)

  const moveDivider = (i: number, upTo: number) => {
    const before = slices.slice(0, i).reduce((sum, slice) => sum + slice.weight, 0)
    const pair = slices[i].weight + slices[i + 1].weight
    const left = Math.min(pair - 1, Math.max(1, Math.round(upTo) - before))
    if (left === slices[i].weight) return
    onChange(slices.map((slice, j) => (j === i ? { ...slice, weight: left } : j === i + 1 ? { ...slice, weight: pair - left } : slice)))
  }

  const updateSliceWeight = (index: number, raw: number) => {
    if (slices.length < 2) return
    const max = 100 - (slices.length - 1)
    const target = Math.min(max, Math.max(1, Math.round(raw)))
    if (target === slices[index].weight) return
    const rest = 100 - target
    const others = slices.map((slice, idx) => ({ idx, weight: slice.weight })).filter((item) => item.idx !== index)
    const otherSum = others.reduce((sum, item) => sum + item.weight, 0)
    const nextWeights = slices.map((slice) => slice.weight)
    nextWeights[index] = target
    let allocated = 0
    others.forEach((item, pos) => {
      if (pos === others.length - 1) {
        nextWeights[item.idx] = Math.max(1, rest - allocated)
      } else {
        const proportional = otherSum > 0 ? Math.round((item.weight / otherSum) * rest) : 1
        const capped = Math.min(proportional, rest - allocated - (others.length - 1 - pos))
        nextWeights[item.idx] = Math.max(1, capped)
        allocated += nextWeights[item.idx]
      }
    })
    onChange(slices.map((slice, idx) => ({ ...slice, weight: nextWeights[idx] })))
  }

  const startDrag = (i: number) => (event: React.PointerEvent<HTMLButtonElement>) => {
    if (disabled || !barRef.current) return
    event.preventDefault()
    const rect = barRef.current.getBoundingClientRect()
    const target = event.currentTarget
    target.setPointerCapture(event.pointerId)
    const move = (e: PointerEvent) => moveDivider(i, ((e.clientX - rect.left) / rect.width) * 100)
    const stop = () => {
      target.removeEventListener('pointermove', move)
      target.removeEventListener('pointerup', stop)
      target.removeEventListener('pointercancel', stop)
    }
    target.addEventListener('pointermove', move)
    target.addEventListener('pointerup', stop)
    target.addEventListener('pointercancel', stop)
  }

  let offset = 0
  return (
    <div className="flex flex-col gap-3">
      <div ref={barRef} className="relative flex h-8 w-full gap-0.5 select-none">
        {slices.map((slice, i) => (
          <div
            key={slice.key}
            className="flex h-full min-w-0 items-center justify-center overflow-hidden text-xs font-medium text-white first:rounded-l-md last:rounded-r-md"
            style={{ width: `${slice.weight}%`, background: SERIES_COLORS[i % SERIES_COLORS.length] }}
          >
            {slice.weight >= 8 && <span className="tabular-nums drop-shadow-sm">{slice.weight}%</span>}
          </div>
        ))}
        {slices.slice(0, -1).map((slice, i) => {
          offset += slice.weight
          return (
            <button
              key={slice.key}
              type="button"
              role="slider"
              aria-label={`${slice.label} 与 ${slices[i + 1].label} 的分界`}
              aria-valuemin={1}
              aria-valuemax={99}
              aria-valuenow={slice.weight}
              aria-valuetext={`${slice.label} ${slice.weight}%，${slices[i + 1].label} ${slices[i + 1].weight}%`}
              disabled={disabled}
              className={cn(
                'absolute top-1/2 z-10 h-10 w-4 -translate-x-1/2 -translate-y-1/2 touch-none rounded-sm outline-none',
                'after:absolute after:inset-y-1 after:left-1/2 after:w-1 after:-translate-x-1/2 after:rounded-full after:bg-foreground after:ring-2 after:ring-background',
                'focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50',
                !disabled && 'cursor-ew-resize',
              )}
              style={{ left: `${offset}%` }}
              onPointerDown={startDrag(i)}
              onKeyDown={(event) => {
                const before = slices.slice(0, i + 1).reduce((sum, s) => sum + s.weight, 0)
                if (event.key === 'ArrowLeft' || event.key === 'ArrowDown') moveDivider(i, before - 1)
                else if (event.key === 'ArrowRight' || event.key === 'ArrowUp') moveDivider(i, before + 1)
                else return
                event.preventDefault()
              }}
            />
          )
        })}
      </div>
      <ul className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
        {slices.map((slice, i) => (
          <li key={slice.key} className="flex min-w-0 items-center gap-2">
            <span
              aria-hidden
              className="size-2.5 shrink-0 rounded-sm"
              style={{ background: SERIES_COLORS[i % SERIES_COLORS.length] }}
            />
            <span className="min-w-0 flex-1 truncate" title={slice.label}>
              {slice.label}
            </span>
            {!disabled && slices.length > 1 ? (
              <div className="flex items-center gap-1">
                <NumberInput
                  aria-label={`${slice.label} 流量占比`}
                  className="h-7 w-16 text-right tabular-nums"
                  min={1}
                  max={100 - (slices.length - 1)}
                  step={1}
                  integer
                  value={slice.weight}
                  onValueChange={(next) => updateSliceWeight(i, next)}
                />
                <span className="text-xs text-muted-foreground">%</span>
              </div>
            ) : (
              <span className="tabular-nums">{slice.weight}%</span>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}

/** evenWeights splits 100 over n parts, giving the remainder to the first ones. */
export function evenWeights(n: number): number[] {
  if (n <= 0) return []
  return Array.from({ length: n }, (_, i) => Math.floor(100 / n) + (i < 100 % n ? 1 : 0))
}

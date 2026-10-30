import { useRef } from 'react'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'

/** Categorical series colors in fixed order; see --series-* in index.css. */
export const SERIES_COLORS = Array.from({ length: 8 }, (_, i) => `var(--series-${i + 1})`)

export type ShareSlice = {
  key: string | number
  label: string
  value: number
}

/**
 * foldSlices keeps the largest seven slices and folds the rest into one, so a
 * chart never needs a ninth color.
 */
export function foldSlices(slices: ShareSlice[], otherLabel = '其他'): ShareSlice[] {
  const sorted = [...slices].filter((slice) => slice.value > 0).sort((a, b) => b.value - a.value)
  if (sorted.length <= SERIES_COLORS.length) return sorted
  const kept = sorted.slice(0, SERIES_COLORS.length - 1)
  const rest = sorted.slice(SERIES_COLORS.length - 1).reduce((sum, slice) => sum + slice.value, 0)
  return [...kept, { key: '__other', label: otherLabel, value: rest }]
}

function percent(value: number, total: number) {
  if (total <= 0) return '0%'
  const p = (value / total) * 100
  return p > 0 && p < 1 ? '<1%' : `${Math.round(p)}%`
}

/**
 * TrafficShareBar is a part-to-whole bar of byte counts with a legend that
 * carries every value, so the colors never have to be read on their own.
 * Colors follow the slice's position in the list the caller passes, so keep
 * that order stable across renders.
 */
export function TrafficShareBar({ slices, ariaLabel }: { slices: ShareSlice[]; ariaLabel: string }) {
  const total = slices.reduce((sum, slice) => sum + slice.value, 0)
  if (total <= 0) {
    return <p className="text-sm text-muted-foreground">当前周期还没有流量。</p>
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
                  style={{ flexGrow: slice.value, flexBasis: 0, background: SERIES_COLORS[i] }}
                />
              }
            />
            <TooltipContent>
              {slice.label}：{formatBytes(slice.value)}（{percent(slice.value, total)}）
            </TooltipContent>
          </Tooltip>
        ))}
      </div>
      <ul className="grid gap-x-6 gap-y-1.5 text-sm sm:grid-cols-2">
        {slices.map((slice, i) => (
          <li key={slice.key} className="flex min-w-0 items-center gap-2">
            <span aria-hidden className="size-2.5 shrink-0 rounded-sm" style={{ background: SERIES_COLORS[i] }} />
            <span className="min-w-0 flex-1 truncate" title={slice.label}>
              {slice.label}
            </span>
            <span className="tabular-nums">{formatBytes(slice.value)}</span>
            <span className="w-10 text-right tabular-nums text-muted-foreground">{percent(slice.value, total)}</span>
          </li>
        ))}
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
 * share between its neighbours, or use the arrow keys on it for 1% steps.
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

  // Moves the divider after slice i so that slices 0..i add up to `upTo`,
  // keeping every slice at 1% or more.
  const moveDivider = (i: number, upTo: number) => {
    const before = slices.slice(0, i).reduce((sum, slice) => sum + slice.weight, 0)
    const pair = slices[i].weight + slices[i + 1].weight
    const left = Math.min(pair - 1, Math.max(1, Math.round(upTo) - before))
    if (left === slices[i].weight) return
    onChange(slices.map((slice, j) => (j === i ? { ...slice, weight: left } : j === i + 1 ? { ...slice, weight: pair - left } : slice)))
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
      <ul className="grid gap-x-6 gap-y-1.5 text-sm sm:grid-cols-2">
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
            <span className="tabular-nums">{slice.weight}%</span>
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

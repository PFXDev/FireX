import { useId, useState } from 'react'

import { buildSparklinePath } from '@/lib/live-stats'
import { cn } from '@/lib/utils'

export interface SparklineSeries {
  label?: string
  values: (number | null)[]
  color: string
  dashed?: boolean
  fillOpacity?: number
  strokeOpacity?: number
  formatValue?: (v: number) => string
}

const VIEWBOX_WIDTH = 240
const VIEWBOX_HEIGHT = 80

export function SparklineBackdrop({
  series,
  min,
  max,
  className,
}: {
  series: SparklineSeries[]
  min?: number
  max?: number
  className?: string
}) {
  const rawId = useId()
  const baseId = rawId.replace(/:/g, '')

  const allValues = series.flatMap((s) => s.values).filter((v): v is number => v !== null && Number.isFinite(v))
  if (allValues.length === 0) return null

  const resolvedMin = min ?? Math.min(...allValues)
  const resolvedMax = max ?? Math.max(...allValues)

  return (
    <div
      aria-hidden="true"
      className={cn(
        'pointer-events-none absolute inset-x-0 bottom-0 z-0 h-20 select-none overflow-hidden',
        className,
      )}
    >
      <svg
        viewBox={`0 0 ${VIEWBOX_WIDTH} ${VIEWBOX_HEIGHT}`}
        preserveAspectRatio="none"
        className="h-full w-full"
      >
        <defs>
          {series.map((item, idx) => (
            <linearGradient key={idx} id={`${baseId}-grad-${idx}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={item.color} stopOpacity={item.fillOpacity ?? 0.16} />
              <stop offset="100%" stopColor={item.color} stopOpacity={0.01} />
            </linearGradient>
          ))}
        </defs>
        {series.map((item, idx) => {
          if (item.values.length === 0) return null
          const { line, area } = buildSparklinePath(item.values, VIEWBOX_WIDTH, VIEWBOX_HEIGHT, {
            min: resolvedMin,
            max: resolvedMax,
            padTop: 8,
            padBottom: 2,
          })
          if (!line) return null
          return (
            <g key={idx}>
              <path d={area} fill={`url(#${baseId}-grad-${idx})`} />
              <path
                d={line}
                fill="none"
                stroke={item.color}
                strokeOpacity={item.strokeOpacity ?? 0.36}
                strokeWidth={1.5}
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeDasharray={item.dashed ? '3 3' : undefined}
                vectorEffect="non-scaling-stroke"
              />
            </g>
          )
        })}
      </svg>
    </div>
  )
}

const CHART_WIDTH = 280
const CHART_HEIGHT = 56
const PAD_TOP = 6
const PAD_BOTTOM = 3

function formatClock(ts: number): string {
  return new Date(ts).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

/**
 * InteractiveSparkline renders a compact live trend chart with pointer hover
 * scrubbing, showing exact timestamps and series values in a non-clipping
 * header bar along with the window span and peak summary when idle.
 */
export function InteractiveSparkline({
  series,
  timestamps,
  min,
  max,
  windowLabel,
  peakLabel,
  className,
}: {
  series: SparklineSeries[]
  timestamps: number[]
  min?: number
  max?: number
  windowLabel?: string
  peakLabel?: string
  className?: string
}) {
  const rawId = useId()
  const baseId = rawId.replace(/:/g, '')
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)

  const pointCount = Math.max(timestamps.length, ...series.map((s) => s.values.length))
  const allValues = series
    .flatMap((s) => s.values)
    .filter((v): v is number => v !== null && Number.isFinite(v))

  if (pointCount === 0 || allValues.length === 0) {
    return (
      <div className={cn('flex flex-col gap-1 border-t border-border/50 pt-2', className)}>
        <div className="flex items-center justify-between text-[11px] text-muted-foreground">
          <span>{windowLabel || '等待采样…'}</span>
          <span>—</span>
        </div>
        <div className="h-11 w-full rounded bg-muted/30" />
      </div>
    )
  }

  let resolvedMin = min ?? Math.min(...allValues)
  let resolvedMax = max ?? Math.max(...allValues)
  if (!Number.isFinite(resolvedMin)) resolvedMin = 0
  if (!Number.isFinite(resolvedMax)) resolvedMax = 1
  if (resolvedMax <= resolvedMin) {
    if (resolvedMax === 0) {
      resolvedMin = 0
      resolvedMax = 1
    } else {
      resolvedMin -= 1
      resolvedMax += 1
    }
  }
  const span = resolvedMax - resolvedMin
  const usableHeight = Math.max(1, CHART_HEIGHT - PAD_TOP - PAD_BOTTOM)

  const clampedHover = hoverIndex !== null ? Math.min(pointCount - 1, Math.max(0, hoverIndex)) : null
  const hoverX =
    clampedHover !== null
      ? pointCount <= 1
        ? CHART_WIDTH / 2
        : (clampedHover * CHART_WIDTH) / (pointCount - 1)
      : null

  const valueToY = (value: number) => {
    const ratio = (Math.min(resolvedMax, Math.max(resolvedMin, value)) - resolvedMin) / span
    return PAD_TOP + (1 - ratio) * usableHeight
  }

  const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const rect = event.currentTarget.getBoundingClientRect()
    if (rect.width <= 0 || pointCount <= 1) {
      setHoverIndex(0)
      return
    }
    const rel = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width))
    setHoverIndex(Math.round(rel * (pointCount - 1)))
  }

  return (
    <div className={cn('flex flex-col gap-1 border-t border-border/50 pt-2', className)}>
      <div className="flex min-h-4 items-center justify-between gap-2 text-[11px] text-muted-foreground tabular-nums">
        {clampedHover !== null ? (
          <>
            <span className="shrink-0 font-medium text-foreground">
              {timestamps[clampedHover] ? formatClock(timestamps[clampedHover]) : `#${clampedHover + 1}`}
            </span>
            <span className="flex min-w-0 items-center gap-2 truncate">
              {series.map((item, idx) => {
                const rawVal = item.values[clampedHover]
                const text =
                  rawVal != null && Number.isFinite(rawVal)
                    ? item.formatValue
                      ? item.formatValue(rawVal)
                      : rawVal.toFixed(1)
                    : '—'
                return (
                  <span key={idx} className="inline-flex items-center gap-1 truncate">
                    <span
                      className="size-1.5 shrink-0 rounded-full"
                      style={{ background: item.color }}
                      aria-hidden="true"
                    />
                    {item.label && <span>{item.label}</span>}
                    <span className="font-medium text-foreground">{text}</span>
                  </span>
                )
              })}
            </span>
          </>
        ) : (
          <>
            <span className="truncate">{windowLabel || '实时趋势'}</span>
            {peakLabel && <span className="shrink-0 truncate">{peakLabel}</span>}
          </>
        )}
      </div>

      <div
        className="relative h-12 w-full cursor-crosshair touch-none select-none"
        onPointerMove={handlePointerMove}
        onPointerLeave={() => setHoverIndex(null)}
      >
        <svg
          viewBox={`0 0 ${CHART_WIDTH} ${CHART_HEIGHT}`}
          preserveAspectRatio="none"
          className="h-full w-full overflow-visible"
          aria-hidden="true"
        >
          <defs>
            {series.map((item, idx) => (
              <linearGradient key={idx} id={`${baseId}-igrad-${idx}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={item.color} stopOpacity={item.fillOpacity ?? 0.22} />
                <stop offset="100%" stopColor={item.color} stopOpacity={0.02} />
              </linearGradient>
            ))}
          </defs>

          {series.map((item, idx) => {
            if (item.values.length === 0) return null
            const { line, area } = buildSparklinePath(item.values, CHART_WIDTH, CHART_HEIGHT, {
              min: resolvedMin,
              max: resolvedMax,
              padTop: PAD_TOP,
              padBottom: PAD_BOTTOM,
            })
            if (!line) return null
            return (
              <g key={idx}>
                <path d={area} fill={`url(#${baseId}-igrad-${idx})`} />
                <path
                  d={line}
                  fill="none"
                  stroke={item.color}
                  strokeOpacity={item.strokeOpacity ?? 0.85}
                  strokeWidth={1.75}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeDasharray={item.dashed ? '3 3' : undefined}
                  vectorEffect="non-scaling-stroke"
                />
              </g>
            )
          })}

          {hoverX !== null && (
            <line
              x1={hoverX}
              y1={0}
              x2={hoverX}
              y2={CHART_HEIGHT}
              stroke="var(--foreground)"
              strokeOpacity={0.28}
              strokeWidth={1}
              strokeDasharray="2 2"
              vectorEffect="non-scaling-stroke"
            />
          )}
        </svg>

        {hoverX !== null && clampedHover !== null && (
          <div className="pointer-events-none absolute inset-0">
            {series.map((item, idx) => {
              const rawVal = item.values[clampedHover]
              if (rawVal == null || !Number.isFinite(rawVal)) return null
              const leftPct = (hoverX / CHART_WIDTH) * 100
              const topPct = (valueToY(rawVal) / CHART_HEIGHT) * 100
              return (
                <span
                  key={idx}
                  className="absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full ring-2 ring-card"
                  style={{
                    left: `${leftPct}%`,
                    top: `${topPct}%`,
                    background: item.color,
                  }}
                />
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

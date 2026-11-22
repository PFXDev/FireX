import { useId } from 'react'

import { buildSparklinePath } from '@/lib/live-stats'
import { cn } from '@/lib/utils'

export interface SparklineSeries {
  values: (number | null)[]
  color: string
  dashed?: boolean
  fillOpacity?: number
  strokeOpacity?: number
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

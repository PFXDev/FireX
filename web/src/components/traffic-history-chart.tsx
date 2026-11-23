import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowDownIcon, ArrowUpIcon, BarChart3Icon } from 'lucide-react'

import { api } from '@/api'
import type { Overview, TrafficHistoryRange, TrafficHistoryResponse } from '@/api'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { errorMessage, formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'

const RANGE_LABELS: Record<TrafficHistoryRange, string> = {
  '24h': '24 小时',
  '7d': '近 7 天',
  '30d': '近 30 天',
}

function formatBucketLabel(ts: number, granularity: 'hour' | 'day'): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  if (granularity === 'hour') {
    return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:00`
  }
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

function formatAxisTick(ts: number, granularity: 'hour' | 'day'): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  if (granularity === 'hour') {
    return `${pad(d.getHours())}:00`
  }
  return `${d.getMonth() + 1}/${d.getDate()}`
}

export function TrafficHistoryCard({
  overview,
  refreshSignal,
}: {
  overview: Overview
  refreshSignal: number | null
}) {
  const [range, setRange] = useState<TrafficHistoryRange>('24h')
  const [scope, setScope] = useState<string>('all')
  const [history, setHistory] = useState<TrafficHistoryResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [hoverIdx, setHoverIdx] = useState<number | null>(null)

  const scopeItems = useMemo(() => {
    const items: { value: string; label: string }[] = [{ value: 'all', label: '全部节点' }]
    for (const g of overview.trafficByGroup) {
      if (g.id > 0) {
        items.push({ value: `group:${g.id}`, label: `节点组 · ${g.name}` })
      }
    }
    for (const p of overview.panels) {
      items.push({ value: `panel:${p.id}`, label: `面板 · ${p.name}` })
    }
    return items
  }, [overview.panels, overview.trafficByGroup])

  const effectiveScope = useMemo(
    () => (scopeItems.some((item) => item.value === scope) ? scope : 'all'),
    [scope, scopeItems],
  )

  const fetchHistory = useCallback(async () => {
    const params = new URLSearchParams({ range })
    if (effectiveScope.startsWith('panel:')) {
      params.set('panelId', effectiveScope.slice('panel:'.length))
    } else if (effectiveScope.startsWith('group:')) {
      params.set('groupId', effectiveScope.slice('group:'.length))
    }
    try {
      const res = await api.get<TrafficHistoryResponse>(`/overview/traffic-history?${params.toString()}`)
      setHistory(res)
      setError(null)
    } catch (err) {
      setError(errorMessage(err, '无法加载历史流量趋势'))
    } finally {
      setLoading(false)
    }
  }, [range, effectiveScope])

  useEffect(() => {
    setLoading((prev) => (history ? prev : true))
    void fetchHistory()
  }, [fetchHistory, refreshSignal])

  const buckets = history?.buckets ?? []
  const maxTotal = useMemo(
    () => Math.max(0, ...buckets.map((b) => b.upload + b.download)),
    [buckets],
  )
  const totalBytes = (history?.totalUpload ?? 0) + (history?.totalDownload ?? 0)
  const activeBucket = hoverIdx !== null && buckets[hoverIdx] ? buckets[hoverIdx] : null

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-1.5">
          <BarChart3Icon className="size-4 text-muted-foreground" />
          <span>流量趋势</span>
        </CardTitle>
        <CardDescription>
          {history
            ? `${RANGE_LABELS[range]}合计 ${formatBytes(totalBytes)}（↓ ${formatBytes(history.totalDownload)} · ↑ ${formatBytes(history.totalUpload)}）${
                history.peakTotal > 0
                  ? ` · 峰值 ${formatBytes(history.peakTotal)}（${formatBucketLabel(history.peakBucketTs, history.granularity)}）`
                  : ''
              }`
            : '按小时 / 天统计的历史流量变化'}
        </CardDescription>
        <CardAction>
          <div className="flex flex-wrap items-center gap-2">
            {scopeItems.length > 1 && (
              <Select
                items={scopeItems}
                value={effectiveScope}
                onValueChange={(value) => setScope(value || 'all')}
              >
                <SelectTrigger aria-label="筛选节点范围" size="sm" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {scopeItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            )}
            <ToggleGroup
              aria-label="趋势时间范围"
              variant="outline"
              size="sm"
              value={[range]}
              onValueChange={(value) => {
                if (value[0]) {
                  setHoverIdx(null)
                  setRange(value[0] as TrafficHistoryRange)
                }
              }}
            >
              <ToggleGroupItem value="24h">24h</ToggleGroupItem>
              <ToggleGroupItem value="7d">7 天</ToggleGroupItem>
              <ToggleGroupItem value="30d">30 天</ToggleGroupItem>
            </ToggleGroup>
          </div>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {loading && !history ? (
          <Skeleton className="h-44 w-full" />
        ) : error && !history ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : maxTotal <= 0 ? (
          <div className="flex h-36 flex-col items-center justify-center gap-1 rounded-lg border border-dashed text-center">
            <p className="text-sm font-medium text-muted-foreground">该时段暂无历史流量记录</p>
            <p className="text-xs text-muted-foreground">
              随着定时流量采集持续运行，这里会自动累积按小时与按天的流量变化。
            </p>
          </div>
        ) : (
          <>
            <div className="flex min-h-5 flex-wrap items-center justify-between gap-2 text-xs tabular-nums">
              <div className="flex items-center gap-3 text-muted-foreground">
                <span className="inline-flex items-center gap-1.5">
                  <span className="size-2 rounded-xs bg-brand" aria-hidden="true" />
                  下行
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <span className="size-2 rounded-xs bg-success" aria-hidden="true" />
                  上行
                </span>
              </div>
              {activeBucket ? (
                <div className="flex flex-wrap items-center gap-3">
                  <span className="font-medium text-foreground">
                    {formatBucketLabel(activeBucket.ts, history?.granularity ?? 'hour')}
                  </span>
                  <span className="inline-flex items-center gap-0.5 text-muted-foreground">
                    <ArrowDownIcon className="size-3 text-brand" />
                    {formatBytes(activeBucket.download)}
                  </span>
                  <span className="inline-flex items-center gap-0.5 text-muted-foreground">
                    <ArrowUpIcon className="size-3 text-success" />
                    {formatBytes(activeBucket.upload)}
                  </span>
                  <span className="font-semibold text-foreground">
                    共 {formatBytes(activeBucket.upload + activeBucket.download)}
                  </span>
                </div>
              ) : (
                <span className="text-muted-foreground">悬停柱状图查看具体时段明细</span>
              )}
            </div>

            <div className="flex flex-col gap-1.5">
              <div className="grid grid-cols-[auto_1fr] items-stretch gap-2">
                <div className="flex flex-col justify-between py-0.5 text-right text-[11px] text-muted-foreground tabular-nums">
                  <span>{formatBytes(maxTotal)}</span>
                  <span>{formatBytes(Math.round(maxTotal / 2))}</span>
                  <span>0</span>
                </div>

                <div
                  className="relative flex h-36 items-end gap-[3px] border-b border-border/70 px-0.5 pt-1"
                  onMouseLeave={() => setHoverIdx(null)}
                >
                  {/* Horizontal reference gridlines */}
                  <div
                    aria-hidden="true"
                    className="pointer-events-none absolute inset-x-0 top-1 border-t border-dashed border-border/50"
                  />
                  <div
                    aria-hidden="true"
                    className="pointer-events-none absolute inset-x-0 top-1/2 border-t border-dashed border-border/50"
                  />

                  {buckets.map((bucket, idx) => {
                    const sum = bucket.upload + bucket.download
                    const totalPct = maxTotal > 0 ? (sum / maxTotal) * 100 : 0
                    const downShare = sum > 0 ? (bucket.download / sum) * 100 : 0
                    const upShare = sum > 0 ? 100 - downShare : 0
                    const isHovered = hoverIdx === idx
                    const isPeak = sum > 0 && sum === maxTotal
                    return (
                      <button
                        key={bucket.ts}
                        type="button"
                        aria-label={`${formatBucketLabel(bucket.ts, history?.granularity ?? 'hour')}：共 ${formatBytes(sum)}`}
                        onMouseEnter={() => setHoverIdx(idx)}
                        onFocus={() => setHoverIdx(idx)}
                        onBlur={() => setHoverIdx((cur) => (cur === idx ? null : cur))}
                        className={cn(
                          'group relative z-10 flex h-full min-w-0 flex-1 flex-col justify-end rounded-t-xs outline-none transition-colors',
                          isHovered ? 'bg-accent/60' : 'hover:bg-accent/40',
                        )}
                      >
                        <div
                          className={cn(
                            'flex w-full flex-col justify-end overflow-hidden rounded-t-xs transition-all duration-200',
                            isPeak && 'ring-1 ring-brand/50',
                          )}
                          style={{
                            height: sum > 0 ? `${Math.max(3, totalPct)}%` : '0%',
                          }}
                        >
                          {bucket.upload > 0 && (
                            <div
                              className="w-full bg-success/85 transition-opacity group-hover:bg-success"
                              style={{ height: `${upShare}%` }}
                            />
                          )}
                          {bucket.download > 0 && (
                            <div
                              className="w-full bg-brand/85 transition-opacity group-hover:bg-brand"
                              style={{ height: `${downShare}%` }}
                            />
                          )}
                        </div>
                      </button>
                    )
                  })}
                </div>
              </div>

              {/* X-axis labels */}
              <div className="grid grid-cols-[auto_1fr] gap-2">
                <span className="invisible text-[11px] tabular-nums">{formatBytes(maxTotal)}</span>
                <div className="flex items-center justify-between text-[11px] text-muted-foreground tabular-nums">
                  {buckets.length > 0 && (
                    <>
                      <span>{formatAxisTick(buckets[0].ts, history?.granularity ?? 'hour')}</span>
                      {buckets.length >= 6 && (
                        <span>
                          {formatAxisTick(
                            buckets[Math.floor(buckets.length / 3)].ts,
                            history?.granularity ?? 'hour',
                          )}
                        </span>
                      )}
                      {buckets.length >= 6 && (
                        <span>
                          {formatAxisTick(
                            buckets[Math.floor((buckets.length * 2) / 3)].ts,
                            history?.granularity ?? 'hour',
                          )}
                        </span>
                      )}
                      <span>
                        {formatAxisTick(
                          buckets[buckets.length - 1].ts,
                          history?.granularity ?? 'hour',
                        )}
                      </span>
                    </>
                  )}
                </div>
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}

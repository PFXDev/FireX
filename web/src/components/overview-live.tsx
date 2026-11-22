import { useState } from 'react'
import {
  ActivityIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  CpuIcon,
  GaugeIcon,
  InfoIcon,
  MemoryStickIcon,
  NetworkIcon,
  PauseCircleIcon,
} from 'lucide-react'

import type { LiveMetricStats, LivePanelStat, LiveTrendPoint } from '@/api'
import { SparklineBackdrop } from '@/components/sparkline-backdrop'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { LiveStatsState } from '@/hooks/use-live-stats'
import { formatBytes, formatSpeed } from '@/lib/format'
import { loadLevelMeta, sortPanelsBySpeed } from '@/lib/live-stats'

export function OverviewLiveSection({ live }: { live: LiveStatsState }) {
  const [showNodes, setShowNodes] = useState(false)

  if (!live.enabled) return null

  const { snapshot, history, status } = live

  if (!snapshot) {
    if (status === 'paused') {
      return (
        <Alert>
          <PauseCircleIcon />
          <AlertTitle>实时监控已暂停</AlertTitle>
          <AlertDescription>当前页面不在前台，返回此标签页后会自动恢复节点实时采样。</AlertDescription>
        </Alert>
      )
    }
    return <LiveOverviewSkeleton />
  }

  const { summary, panels } = snapshot
  const sortedPanels = sortPanelsBySpeed(panels)
  const topPanel = sortedPanels.find((p) => p.online && p.netUp + p.netDown > 0)
  const levelMeta = loadLevelMeta(summary.systemLoadLevel)
  const hasOnline = summary.onlinePanels > 0
  const userCoverage = `已采样 ${summary.userSamplePanels}/${summary.onlinePanels} 个在线节点`
  const partialUsers = summary.userSamplePanels < summary.onlinePanels

  return (
    <section aria-label="实时节点监控" className="flex flex-col gap-4">
      {status === 'paused' && (
        <Alert>
          <PauseCircleIcon />
          <AlertTitle>实时采集已暂停，当前显示最后一次快照</AlertTitle>
          <AlertDescription>切回此标签页后将自动恢复实时串流。</AlertDescription>
        </Alert>
      )}

      {/* Row 1: Speed + Connections/Online + Fleet System Load */}
      <div className="grid gap-4 lg:grid-cols-3">
        {/* Card 1: Total Real-Time Speed + Expandable Per-Node Breakdown */}
        <Card className="relative">
          <SparklineBackdrop
            min={0}
            series={[
              {
                values: history.map((p) => p.netDown),
                color: 'var(--brand)',
                fillOpacity: 0.16,
                strokeOpacity: 0.4,
              },
              {
                values: history.map((p) => p.netUp),
                color: 'var(--success)',
                dashed: true,
                fillOpacity: 0.08,
                strokeOpacity: 0.34,
              },
            ]}
          />
          <CardHeader className="relative z-10">
            <CardTitle>实时网速</CardTitle>
            <CardDescription>
              {summary.totalPanels === 0
                ? '暂无启用节点'
                : `${summary.activePanels} / ${summary.totalPanels} 个节点有实时流量`}
            </CardDescription>
            <CardAction>
              <Button
                type="button"
                variant={showNodes ? 'secondary' : 'outline'}
                size="xs"
                aria-expanded={showNodes}
                onClick={() => setShowNodes((prev) => !prev)}
                disabled={panels.length === 0}
              >
                <span>分节点 ({panels.length})</span>
                {showNodes ? <ChevronUpIcon data-icon="inline-end" /> : <ChevronDownIcon data-icon="inline-end" />}
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="relative z-10 flex flex-col gap-3">
            <div className="flex items-baseline gap-2">
              <strong className="font-heading text-3xl font-semibold tabular-nums">
                {formatSpeed(summary.totalNetUp + summary.totalNetDown)}
              </strong>
              <span className="text-xs text-muted-foreground">总吞吐</span>
            </div>
            <div className="grid grid-cols-2 gap-2.5 text-xs">
              <div className="flex items-center justify-between gap-2 rounded-lg bg-muted/60 px-2.5 py-2 backdrop-blur-[1px]">
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                  <ArrowDownIcon className="size-3.5 text-brand" />
                  下行
                </span>
                <span className="font-medium tabular-nums">{formatSpeed(summary.totalNetDown)}</span>
              </div>
              <div className="flex items-center justify-between gap-2 rounded-lg bg-muted/60 px-2.5 py-2 backdrop-blur-[1px]">
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                  <ArrowUpIcon className="size-3.5 text-success" />
                  上行
                </span>
                <span className="font-medium tabular-nums">{formatSpeed(summary.totalNetUp)}</span>
              </div>
            </div>
            <p className="truncate text-xs text-muted-foreground">
              {topPanel
                ? `峰值节点：${topPanel.name} (${formatSpeed(topPanel.netUp + topPanel.netDown)})`
                : hasOnline
                  ? '当前各节点暂无吞吐'
                  : '等待节点上线…'}
            </p>
          </CardContent>
        </Card>

        {/* Card 2: Connections & Online Counts (Nodes vs Users) */}
        <Card className="relative">
          <SparklineBackdrop
            min={0}
            series={[
              {
                values: history.map((p) => p.connections),
                color: 'var(--brand)',
                fillOpacity: 0.15,
                strokeOpacity: 0.38,
              },
            ]}
          />
          <CardHeader className="relative z-10">
            <CardTitle>连接与在线</CardTitle>
            <CardDescription>实时连接数 · 区分节点与用户</CardDescription>
            <CardAction>
              <Badge variant="outline">
                <NetworkIcon data-icon="inline-start" />
                TCP {summary.totalTcp} · UDP {summary.totalUdp}
              </Badge>
            </CardAction>
          </CardHeader>
          <CardContent className="relative z-10 flex flex-col gap-3">
            <div className="flex items-baseline gap-2">
              <strong className="font-heading text-3xl font-semibold tabular-nums">
                {summary.totalConnections.toLocaleString('zh-CN')}
              </strong>
              <span className="text-xs text-muted-foreground">活跃连接</span>
            </div>
            <div className="grid grid-cols-2 gap-2.5">
              <div className="flex flex-col gap-0.5 rounded-lg bg-muted/60 px-3 py-2 backdrop-blur-[1px]">
                <span className="text-xs text-muted-foreground">在线节点</span>
                <div className="flex items-baseline gap-1">
                  <span className="font-heading text-lg font-semibold tabular-nums">
                    {summary.onlinePanels}
                  </span>
                  <span className="text-xs text-muted-foreground tabular-nums">
                    / {summary.totalPanels} · 活跃 {summary.activePanels}
                  </span>
                </div>
              </div>
              <div className="flex flex-col gap-0.5 rounded-lg bg-muted/60 px-3 py-2 backdrop-blur-[1px]">
                <div className="flex items-center gap-1 text-xs text-muted-foreground">
                  <span>在线用户</span>
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <button
                          type="button"
                          aria-label="在线用户统计说明"
                          className="inline-flex text-muted-foreground hover:text-foreground"
                        >
                          <InfoIcon className="size-3" />
                        </button>
                      }
                    />
                    <TooltipContent>
                      按 @FireX 跨节点去重。{userCoverage}。
                    </TooltipContent>
                  </Tooltip>
                </div>
                <div className="flex items-baseline gap-1">
                  <span className="font-heading text-lg font-semibold tabular-nums">
                    {summary.userSamplePanels > 0 ? summary.onlineUsers : '—'}
                  </span>
                  <span className="truncate text-xs text-muted-foreground tabular-nums">
                    {partialUsers || !hasOnline
                      ? userCoverage
                      : summary.onlineClients > summary.onlineUsers
                      ? `会话 ${summary.onlineUserSessions} · 全部 ${summary.onlineClients}`
                      : `跨节点会话 ${summary.onlineUserSessions}`}
                  </span>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Card 3: Fleet System Overall Load Indicator */}
        <Card className="relative">
          <SparklineBackdrop
            min={0}
            max={100}
            series={[
              {
                values: history.map((p) => p.systemLoad),
                color: levelMeta.colorVar,
                fillOpacity: 0.16,
                strokeOpacity: 0.4,
              },
            ]}
          />
          <CardHeader className="relative z-10">
            <CardTitle>系统总体负载</CardTitle>
            <CardDescription>全网综合负载指数（0–100）</CardDescription>
            <CardAction>
              <StatusBadge tone={levelMeta.tone}>
                <ActivityIcon data-icon="inline-start" />
                {levelMeta.label}
              </StatusBadge>
            </CardAction>
          </CardHeader>
          <CardContent className="relative z-10 flex flex-col gap-3">
            <div className="flex items-baseline gap-2">
              <strong className="font-heading text-3xl font-semibold tabular-nums">
                {summary.loadIndex.samples > 0 ? summary.systemLoadIndex.toFixed(1) : '—'}
              </strong>
              <span className="text-xs text-muted-foreground">/ 100</span>
            </div>
            <div className="grid grid-cols-2 gap-2.5 text-xs">
              <div className="flex flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2 backdrop-blur-[1px]">
                <span className="text-muted-foreground">集群总核心</span>
                <span className="font-medium tabular-nums">
                  {hasOnline ? `${summary.totalCpuCores} 核` : '—'}
                </span>
              </div>
              <div className="flex flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2 backdrop-blur-[1px]">
                <span className="text-muted-foreground">集群 Load 1/5/15</span>
                <span className="truncate font-medium tabular-nums">
                  {hasOnline
                    ? `${summary.fleetLoad1.toFixed(2)} / ${summary.fleetLoad5.toFixed(2)} / ${summary.fleetLoad15.toFixed(2)}`
                    : '—'}
                </span>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              有效样本 {summary.loadIndex.samples}/{summary.onlinePanels} · 平均负载占 75%，峰值占 25%
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Expandable Per-Node Live Speed Breakdown */}
      {showNodes && <PerNodeSpeedCard panels={sortedPanels} />}

      {/* Row 2: Min / Max / Avg for CPU, Memory, Node Load Index */}
      <div className="grid gap-4 lg:grid-cols-3">
        <ExtremeMetricCard
          title="CPU 使用率"
          description={hasOnline ? `已采样 ${summary.cpu.samples}/${summary.onlinePanels} 个在线节点` : '暂无在线节点'}
          icon={<CpuIcon className="size-3.5 text-muted-foreground" />}
          stats={summary.cpu}
          unit="%"
          history={history}
          pickValue={(p) => p.cpuAvg}
          color="var(--chart-1)"
        />
        <ExtremeMetricCard
          title="内存使用率"
          description={
            hasOnline && summary.totalMemTotal > 0
              ? `集群 ${formatBytes(summary.totalMemUsed)} / ${formatBytes(summary.totalMemTotal)}`
              : '暂无在线节点'
          }
          icon={<MemoryStickIcon className="size-3.5 text-muted-foreground" />}
          stats={summary.mem}
          unit="%"
          history={history}
          pickValue={(p) => p.memAvg}
          color="var(--chart-3)"
        />
        <ExtremeMetricCard
          title="节点负载指数"
          description="CPU 45% + 内存 30% + Load1/核 25%"
          icon={<GaugeIcon className="size-3.5 text-muted-foreground" />}
          stats={summary.loadIndex}
          unit=""
          history={history}
          pickValue={(p) => p.loadAvg}
          color="var(--chart-4)"
        />
      </div>
    </section>
  )
}

function PerNodeSpeedCard({ panels }: { panels: LivePanelStat[] }) {
  const maxSpeed = Math.max(
    1,
    ...panels.map((p) => (p.online ? p.netUp + p.netDown : 0)),
  )

  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>分节点实时网速</CardTitle>
        <CardDescription>按总吞吐降序排列 · 共 {panels.length} 个已启用节点</CardDescription>
      </CardHeader>
      <CardContent>
        {panels.length === 0 ? (
          <p className="text-sm text-muted-foreground">还没有启用的面板节点。</p>
        ) : (
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {panels.map((panel) => {
              const total = panel.online ? panel.netUp + panel.netDown : 0
              const pct = panel.online && total > 0 ? Math.max(2, Math.round((total / maxSpeed) * 100)) : 0
              return (
                <div
                  key={panel.id}
                  className="flex flex-col gap-1.5 rounded-lg border bg-muted/30 p-3"
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <span
                        className={
                          panel.online
                            ? 'size-2 shrink-0 rounded-full bg-success'
                            : 'size-2 shrink-0 rounded-full bg-destructive'
                        }
                        aria-hidden="true"
                      />
                      <span className="truncate font-medium" title={panel.name}>
                        {panel.name}
                      </span>
                    </div>
                    <span className="shrink-0 font-heading font-semibold tabular-nums">
                      {panel.online ? formatSpeed(total) : '离线'}
                    </span>
                  </div>

                  <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full bg-brand transition-all duration-300"
                      style={{ width: `${pct}%` }}
                    />
                  </div>

                  {panel.online ? (
                    <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground tabular-nums">
                      <span className="inline-flex items-center gap-2">
                        <span className="inline-flex items-center gap-0.5">
                          <ArrowDownIcon className="size-3 text-brand" />
                          {formatSpeed(panel.netDown)}
                        </span>
                        <span className="inline-flex items-center gap-0.5">
                          <ArrowUpIcon className="size-3 text-success" />
                          {formatSpeed(panel.netUp)}
                        </span>
                      </span>
                      <span>
                        CPU {panel.cpu != null ? `${panel.cpu.toFixed(0)}%` : '—'} · 负载 {panel.loadIndex?.toFixed(0) ?? '—'}
                      </span>
                    </div>
                  ) : (
                    <p className="truncate text-xs text-destructive" title={panel.error || '节点不可达'}>
                      {panel.error || '节点不可达'}
                    </p>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ExtremeMetricCard({
  title,
  description,
  icon,
  stats,
  unit,
  history,
  pickValue,
  color,
}: {
  title: string
  description: string
  icon: React.ReactNode
  stats: LiveMetricStats
  unit: string
  history: LiveTrendPoint[]
  pickValue: (p: LiveTrendPoint) => number | null
  color: string
}) {
  const hasSamples = stats.samples > 0
  const formatVal = (v: number) => `${v.toFixed(1)}${unit}`

  return (
    <Card className="relative">
      <SparklineBackdrop
        min={0}
        max={100}
        series={[
          {
            values: history.map(pickValue),
            color,
            fillOpacity: 0.14,
            strokeOpacity: 0.36,
          },
        ]}
      />
      <CardHeader className="relative z-10">
        <CardTitle className="flex items-center gap-1.5">
          {icon}
          <span>{title}</span>
        </CardTitle>
        <CardDescription>{description}</CardDescription>
        <CardAction>
          <Badge variant="outline">均值</Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="relative z-10 flex flex-col gap-3">
        <div className="flex items-baseline gap-2">
          <strong className="font-heading text-3xl font-semibold tabular-nums">
            {hasSamples ? formatVal(stats.avg) : '—'}
          </strong>
          <span className="text-xs text-muted-foreground">平均</span>
        </div>

        <div className="grid grid-cols-2 gap-2.5 text-xs">
          <div className="flex min-w-0 flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2 backdrop-blur-[1px]">
            <div className="flex items-center justify-between gap-1 text-muted-foreground">
              <span>最高</span>
              <span className="font-semibold text-foreground tabular-nums">
                {hasSamples ? formatVal(stats.max.value) : '—'}
              </span>
            </div>
            <span className="truncate text-muted-foreground" title={stats.max.panelName}>
              {hasSamples ? stats.max.panelName || `#${stats.max.panelId}` : '—'}
            </span>
          </div>

          <div className="flex min-w-0 flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2 backdrop-blur-[1px]">
            <div className="flex items-center justify-between gap-1 text-muted-foreground">
              <span>最低</span>
              <span className="font-semibold text-foreground tabular-nums">
                {hasSamples ? formatVal(stats.min.value) : '—'}
              </span>
            </div>
            <span className="truncate text-muted-foreground" title={stats.min.panelName}>
              {hasSamples ? stats.min.panelName || `#${stats.min.panelId}` : '—'}
            </span>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function LiveOverviewSkeleton() {
  return (
    <section aria-busy="true" aria-label="正在连接实时监控" className="flex flex-col gap-4">
      <div className="grid gap-4 lg:grid-cols-3">
        {Array.from({ length: 6 }, (_, idx) => (
          <Card key={idx}>
            <CardHeader>
              <CardTitle>
                <Skeleton className="h-5 w-24" />
              </CardTitle>
              <CardDescription>
                <Skeleton className="h-4 w-36" />
              </CardDescription>
              <CardAction>
                <Skeleton className="h-5 w-16" />
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <Skeleton className="h-9 w-28" />
              <div className="grid grid-cols-2 gap-2.5">
                <Skeleton className="h-11 w-full" />
                <Skeleton className="h-11 w-full" />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
}

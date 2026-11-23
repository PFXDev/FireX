import { useMemo, useState } from 'react'
import {
  ActivityIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  CpuIcon,
  GaugeIcon,
  HardDriveIcon,
  InfoIcon,
  MemoryStickIcon,
  NetworkIcon,
  PauseCircleIcon,
  TriangleAlertIcon,
  UsersIcon,
} from 'lucide-react'

import type { LiveMetricStats, LiveOnlineUser, LivePanelStat, LiveTrendPoint } from '@/api'
import { InteractiveSparkline } from '@/components/sparkline-backdrop'
import { StatusBadge } from '@/components/status-badge'
import { SearchInput } from '@/components/table-toolbar'
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
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { LiveStatsState } from '@/hooks/use-live-stats'
import { formatBytes, formatSpeed } from '@/lib/format'
import {
  formatWindowSpan,
  isXrayAbnormal,
  loadLevelMeta,
  metricSeverity,
  sortPanelsBySpeed,
} from '@/lib/live-stats'
import { cn } from '@/lib/utils'

export function OverviewLiveSection({ live }: { live: LiveStatsState }) {
  const [showNodes, setShowNodes] = useState(false)
  const [connTrendMode, setConnTrendMode] = useState<'connections' | 'users'>('connections')
  const [onlineDialogOpen, setOnlineDialogOpen] = useState(false)

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
  const onlineUserList = snapshot.onlineUsers ?? []
  const sortedPanels = sortPanelsBySpeed(panels)
  const topPanel = sortedPanels.find((p) => p.online && p.netUp + p.netDown > 0)
  const levelMeta = loadLevelMeta(summary.systemLoadLevel)
  const hasOnline = summary.onlinePanels > 0
  const userCoverage = `已采样 ${summary.userSamplePanels}/${summary.onlinePanels} 个在线节点`
  const partialUsers = summary.userSamplePanels < summary.onlinePanels
  const timestamps = history.map((p) => p.t)
  const windowLabel = formatWindowSpan(history, snapshot.intervalMs)

  const peakSpeed = history.reduce((best, p) => Math.max(best, p.netUp + p.netDown), 0)
  const peakConnections = history.reduce((best, p) => Math.max(best, p.connections), 0)
  const peakOnlineUsers = history.reduce(
    (best, p) => (p.onlineUsers != null ? Math.max(best, p.onlineUsers) : best),
    0,
  )
  const peakSystemLoad = history.reduce(
    (best, p) => (p.systemLoad != null ? Math.max(best, p.systemLoad) : best),
    0,
  )
  const hasUserHistory = history.some((p) => p.onlineUsers != null)

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
        <Card>
          <CardHeader>
            <CardTitle>实时网速</CardTitle>
            <CardDescription>
              {summary.totalPanels === 0
                ? '暂无启用节点'
                : `${summary.activePanels} / ${summary.totalPanels} 个活跃节点（有吞吐或在线客户端）`}
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
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-baseline gap-2">
              <strong className="font-heading text-3xl font-semibold tabular-nums">
                {formatSpeed(summary.totalNetUp + summary.totalNetDown)}
              </strong>
              <span className="text-xs text-muted-foreground">主机网卡总吞吐</span>
            </div>
            <div className="grid grid-cols-2 gap-2.5 text-xs">
              <div className="flex items-center justify-between gap-2 rounded-lg bg-muted/60 px-2.5 py-2">
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                  <ArrowDownIcon className="size-3.5 text-brand" />
                  下行
                </span>
                <span className="font-medium tabular-nums">{formatSpeed(summary.totalNetDown)}</span>
              </div>
              <div className="flex items-center justify-between gap-2 rounded-lg bg-muted/60 px-2.5 py-2">
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
            <InteractiveSparkline
              timestamps={timestamps}
              min={0}
              windowLabel={windowLabel}
              peakLabel={peakSpeed > 0 ? `峰值 ${formatSpeed(peakSpeed)}` : undefined}
              series={[
                {
                  label: '↓',
                  values: history.map((p) => p.netDown),
                  color: 'var(--brand)',
                  formatValue: formatSpeed,
                },
                {
                  label: '↑',
                  values: history.map((p) => p.netUp),
                  color: 'var(--success)',
                  dashed: true,
                  fillOpacity: 0.1,
                  formatValue: formatSpeed,
                },
              ]}
            />
          </CardContent>
        </Card>

        {/* Card 2: Connections & Online Counts (Nodes vs Users) */}
        <Card>
          <CardHeader>
            <CardTitle>连接与在线</CardTitle>
            <CardDescription>
              TCP {summary.totalTcp.toLocaleString('zh-CN')} · UDP {summary.totalUdp.toLocaleString('zh-CN')}
            </CardDescription>
            <CardAction>
              <ToggleGroup
                aria-label="趋势线维度"
                variant="outline"
                size="sm"
                value={[connTrendMode]}
                onValueChange={(value) => {
                  if (value[0]) setConnTrendMode(value[0] as 'connections' | 'users')
                }}
              >
                <ToggleGroupItem value="connections" className="h-6 px-2 text-[11px]">
                  连接
                </ToggleGroupItem>
                <ToggleGroupItem
                  value="users"
                  className="h-6 px-2 text-[11px]"
                  disabled={!hasUserHistory && summary.userSamplePanels === 0}
                >
                  用户
                </ToggleGroupItem>
              </ToggleGroup>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-baseline justify-between gap-2">
              <div className="flex items-baseline gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">
                  {summary.totalConnections.toLocaleString('zh-CN')}
                </strong>
                <span className="text-xs text-muted-foreground">活跃连接</span>
              </div>
              {summary.xrayAbnormalPanels > 0 && (
                <StatusBadge tone="bad">
                  <TriangleAlertIcon data-icon="inline-start" />
                  {summary.xrayAbnormalPanels} 台 Xray 异常
                </StatusBadge>
              )}
            </div>
            <div className="grid grid-cols-2 gap-2.5">
              <div className="flex flex-col gap-0.5 rounded-lg bg-muted/60 px-3 py-2">
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
              <button
                type="button"
                onClick={() => setOnlineDialogOpen(true)}
                disabled={summary.userSamplePanels === 0}
                className={cn(
                  'group flex flex-col gap-0.5 rounded-lg bg-muted/60 px-3 py-2 text-left transition-colors outline-none',
                  summary.userSamplePanels > 0 &&
                    'cursor-pointer hover:bg-accent/70 focus-visible:ring-2 focus-visible:ring-ring',
                )}
              >
                <div className="flex items-center justify-between gap-1 text-xs text-muted-foreground">
                  <span className="inline-flex items-center gap-1">
                    <span>在线用户</span>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <span
                            role="img"
                            aria-label="在线用户统计说明"
                            className="inline-flex text-muted-foreground hover:text-foreground"
                          >
                            <InfoIcon className="size-3" />
                          </span>
                        }
                      />
                      <TooltipContent>
                        按 @FireX 跨节点去重，点击查看在线用户与所在节点。{userCoverage}。
                      </TooltipContent>
                    </Tooltip>
                  </span>
                  {summary.onlineUsers > 0 && (
                    <span className="text-[11px] text-primary opacity-80 group-hover:opacity-100">明细</span>
                  )}
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
              </button>
            </div>
            <InteractiveSparkline
              timestamps={timestamps}
              min={0}
              windowLabel={windowLabel}
              peakLabel={
                connTrendMode === 'connections'
                  ? `峰值 ${peakConnections.toLocaleString('zh-CN')}`
                  : hasUserHistory
                    ? `峰值 ${peakOnlineUsers} 人`
                    : '等待完整采样'
              }
              series={
                connTrendMode === 'connections'
                  ? [
                      {
                        label: '连接',
                        values: history.map((p) => p.connections),
                        color: 'var(--brand)',
                        formatValue: (v) => Math.round(v).toLocaleString('zh-CN'),
                      },
                    ]
                  : [
                      {
                        label: '在线用户',
                        values: history.map((p) => p.onlineUsers),
                        color: 'var(--success)',
                        formatValue: (v) => `${Math.round(v)} 人`,
                      },
                    ]
              }
            />
          </CardContent>
        </Card>

        {/* Card 3: Fleet System Overall Load Indicator */}
        <Card>
          <CardHeader>
            <CardTitle>系统总体负载</CardTitle>
            <CardDescription>全网综合负载指数（均值 75% + 峰值 25%）</CardDescription>
            <CardAction>
              <StatusBadge tone={levelMeta.tone}>
                <ActivityIcon data-icon="inline-start" />
                {levelMeta.label}
              </StatusBadge>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-baseline justify-between gap-2">
              <div className="flex items-baseline gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">
                  {summary.loadIndex.samples > 0 ? summary.systemLoadIndex.toFixed(1) : '—'}
                </strong>
                <span className="text-xs text-muted-foreground">/ 100</span>
              </div>
              {summary.rtt.samples > 0 && (
                <Badge variant="outline" className="tabular-nums">
                  <NetworkIcon data-icon="inline-start" />
                  延迟 {Math.round(summary.rtt.avg)}ms（峰值 {Math.round(summary.rtt.max.value)}ms）
                </Badge>
              )}
            </div>
            <div className="grid grid-cols-2 gap-2.5 text-xs">
              <div className="flex flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2">
                <span className="text-muted-foreground">核均 Load1 · 总核心</span>
                <span className="truncate font-medium tabular-nums">
                  {hasOnline
                    ? `${summary.fleetLoadPerCore.toFixed(2)} / 核 · ${summary.totalCpuCores} 核`
                    : '—'}
                </span>
              </div>
              <div className="flex flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2">
                <span className="text-muted-foreground">集群 Load 1/5/15</span>
                <span className="truncate font-medium tabular-nums">
                  {hasOnline
                    ? `${summary.fleetLoad1.toFixed(2)} / ${summary.fleetLoad5.toFixed(2)} / ${summary.fleetLoad15.toFixed(2)}`
                    : '—'}
                </span>
              </div>
            </div>
            <InteractiveSparkline
              timestamps={timestamps}
              min={0}
              max={100}
              windowLabel={windowLabel}
              peakLabel={peakSystemLoad > 0 ? `峰值 ${peakSystemLoad.toFixed(1)}` : undefined}
              series={[
                {
                  label: '负载',
                  values: history.map((p) => p.systemLoad),
                  color: levelMeta.colorVar,
                  formatValue: (v) => v.toFixed(1),
                },
              ]}
            />
          </CardContent>
        </Card>
      </div>

      {/* Expandable Per-Node Live Speed Breakdown */}
      {showNodes && <PerNodeSpeedCard panels={sortedPanels} />}

      {/* Row 2: Min / Max / Avg for CPU, Memory (fleet-weighted), Node Load + Disk */}
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
          windowLabel={windowLabel}
        />
        <ExtremeMetricCard
          title="内存使用率"
          description={
            hasOnline && summary.totalMemTotal > 0
              ? `集群 ${formatBytes(summary.totalMemUsed)} / ${formatBytes(summary.totalMemTotal)} · 节点均值 ${summary.mem.avg.toFixed(1)}%`
              : '暂无在线节点'
          }
          icon={<MemoryStickIcon className="size-3.5 text-muted-foreground" />}
          primaryValue={hasOnline && summary.totalMemTotal > 0 ? summary.fleetMemPercent : undefined}
          primaryLabel={hasOnline && summary.totalMemTotal > 0 ? '集群加权' : '平均'}
          badgeLabel="集群占比"
          stats={summary.mem}
          unit="%"
          history={history}
          pickValue={(p) => p.memAvg}
          color="var(--chart-3)"
          windowLabel={windowLabel}
        />
        <ExtremeMetricCard
          title="节点负载指数"
          description={
            summary.disk.samples > 0
              ? `CPU 45% + 内存 30% + Load 25% · 最高磁盘 ${summary.disk.max.value.toFixed(0)}% (${summary.disk.max.panelName})`
              : 'CPU 45% + 内存 30% + Load1/核 25%'
          }
          icon={<GaugeIcon className="size-3.5 text-muted-foreground" />}
          badgeNode={
            summary.highDiskPanels > 0 ? (
              <StatusBadge tone="warn">
                <HardDriveIcon data-icon="inline-start" />
                {summary.highDiskPanels} 台磁盘 ≥85%
              </StatusBadge>
            ) : undefined
          }
          stats={summary.loadIndex}
          unit=""
          history={history}
          pickValue={(p) => p.loadAvg}
          color="var(--chart-4)"
          windowLabel={windowLabel}
        />
      </div>

      <OnlineUsersDialog
        open={onlineDialogOpen}
        onOpenChange={setOnlineDialogOpen}
        users={onlineUserList}
        coverage={userCoverage}
      />
    </section>
  )
}

function OnlineUsersDialog({
  open,
  onOpenChange,
  users,
  coverage,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  users: LiveOnlineUser[]
  coverage: string
}) {
  const [query, setQuery] = useState('')
  const filtered = useMemo(() => {
    const kw = query.trim().toLowerCase()
    if (!kw) return users
    return users.filter(
      (u) =>
        u.username.toLowerCase().includes(kw) ||
        u.panels.some((panelName) => panelName.toLowerCase().includes(kw)),
    )
  }, [query, users])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex flex-col overflow-hidden sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UsersIcon className="size-4 text-muted-foreground" />
            <span>当前在线用户 ({users.length})</span>
          </DialogTitle>
          <DialogDescription>
            按 @FireX 跨节点去重，实时展示每位在线用户当前连接的面板。{coverage}。
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3">
          {users.length > 0 && (
            <SearchInput
              value={query}
              onChange={setQuery}
              placeholder="按用户名或面板名筛选…"
              ariaLabel="筛选在线用户"
              className="w-full sm:w-full"
            />
          )}
          {filtered.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">
              {users.length === 0 ? '当前暂无在线 FireX 用户。' : '没有匹配的在线用户。'}
            </p>
          ) : (
            <ul className="flex max-h-80 flex-col divide-y divide-border/60 overflow-y-auto rounded-lg border">
              {filtered.map((user) => (
                <li key={user.username} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
                  <span className="font-medium">{user.username}</span>
                  <div className="flex flex-wrap items-center gap-1">
                    {user.panels.map((panelName) => (
                      <Badge key={panelName} variant="secondary" className="text-[11px]">
                        {panelName}
                      </Badge>
                    ))}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </DialogBody>
      </DialogContent>
    </Dialog>
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
              const xrayBad = isXrayAbnormal(panel)
              return (
                <div
                  key={panel.id}
                  className={cn(
                    'flex flex-col gap-1.5 rounded-lg border bg-muted/30 p-3',
                    (!panel.online || xrayBad) && 'border-destructive/40',
                  )}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <span
                        className={
                          !panel.online || xrayBad
                            ? 'size-2 shrink-0 rounded-full bg-destructive'
                            : 'size-2 shrink-0 rounded-full bg-success'
                        }
                        aria-hidden="true"
                      />
                      <span className="truncate font-medium" title={panel.name}>
                        {panel.name}
                      </span>
                      {xrayBad && (
                        <Badge variant="destructive" className="h-4 px-1 text-[10px]">
                          Xray {panel.xrayState}
                        </Badge>
                      )}
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
                        CPU {panel.cpu != null ? `${panel.cpu.toFixed(0)}%` : '—'} · 磁盘{' '}
                        {panel.diskTotal > 0 ? `${panel.diskPercent.toFixed(0)}%` : '—'}
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
  primaryValue,
  primaryLabel = '平均',
  badgeLabel = '均值',
  badgeNode,
  unit,
  history,
  pickValue,
  color,
  windowLabel,
}: {
  title: string
  description: string
  icon: React.ReactNode
  stats: LiveMetricStats
  primaryValue?: number
  primaryLabel?: string
  badgeLabel?: string
  badgeNode?: React.ReactNode
  unit: string
  history: LiveTrendPoint[]
  pickValue: (p: LiveTrendPoint) => number | null
  color: string
  windowLabel: string
}) {
  const hasSamples = stats.samples > 0
  const formatVal = (v: number) => `${v.toFixed(1)}${unit}`
  const displayPrimary = primaryValue !== undefined ? primaryValue : stats.avg
  const maxSeverity = metricSeverity(hasSamples ? stats.max.value : null, 75, 90)
  const values = history.map(pickValue)
  const peak = values.reduce<number | null>(
    (best, v) => (v != null && (best === null || v > best) ? v : best),
    null,
  )

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-1.5">
          {icon}
          <span>{title}</span>
        </CardTitle>
        <CardDescription>{description}</CardDescription>
        <CardAction>{badgeNode ?? <Badge variant="outline">{badgeLabel}</Badge>}</CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-baseline gap-2">
          <strong className="font-heading text-3xl font-semibold tabular-nums">
            {hasSamples ? formatVal(displayPrimary) : '—'}
          </strong>
          <span className="text-xs text-muted-foreground">{primaryLabel}</span>
        </div>

        <div className="grid grid-cols-2 gap-2.5 text-xs">
          <div className="flex min-w-0 flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2">
            <div className="flex items-center justify-between gap-1 text-muted-foreground">
              <span>最高</span>
              <span
                className={cn(
                  'font-semibold tabular-nums',
                  maxSeverity === 'bad'
                    ? 'text-destructive'
                    : maxSeverity === 'warn'
                      ? 'text-warning'
                      : 'text-foreground',
                )}
              >
                {hasSamples ? formatVal(stats.max.value) : '—'}
              </span>
            </div>
            <span className="truncate text-muted-foreground" title={stats.max.panelName}>
              {hasSamples ? stats.max.panelName || `#${stats.max.panelId}` : '—'}
            </span>
          </div>

          <div className="flex min-w-0 flex-col gap-0.5 rounded-lg bg-muted/60 px-2.5 py-2">
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

        <InteractiveSparkline
          timestamps={history.map((p) => p.t)}
          min={0}
          max={100}
          windowLabel={windowLabel}
          peakLabel={peak !== null ? `窗口峰值 ${formatVal(peak)}` : undefined}
          series={[
            {
              label: title,
              values,
              color,
              formatValue: formatVal,
            },
          ]}
        />
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
              <Skeleton className="h-14 w-full" />
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
}

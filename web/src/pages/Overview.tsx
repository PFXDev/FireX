import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  ActivityIcon,
  ArrowRightIcon,
  Clock3Icon,
  LayersIcon,
  PlusIcon,
  RefreshCwIcon,
  ServerIcon,
  ServerOffIcon,
  TriangleAlertIcon,
  UsersIcon,
} from 'lucide-react'

import { api } from '@/api'
import type { LivePanelStat, Overview } from '@/api'
import { CodeText } from '@/components/code-display'
import { InfoPopover } from '@/components/info-popover'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { OverviewLiveSection } from '@/components/overview-live'
import { PageHeader } from '@/components/page-header'
import { PanelStatusBadge, StatusBadge } from '@/components/status-badge'
import { foldSlices, TrafficShareBar } from '@/components/traffic-share'
import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { Progress, ProgressLabel, ProgressValue } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useLiveStats, type LiveStatus } from '@/hooks/use-live-stats'
import { usePageVisible } from '@/hooks/use-page-visible'
import { errorMessage, formatBytes, formatSpeed, formatTime } from '@/lib/format'
import { canAccess } from '@/lib/navigation'
import { useSession } from '@/lib/session'

const POLL_INTERVAL_MS = 15_000

type RefreshMode = 'initial' | 'background'

type SyncProblem = {
  stage: string
  message: string
}

export function OverviewPage() {
  const [data, setData] = useState<Overview | null>(null)
  const [initialLoading, setInitialLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const [syncing, setSyncing] = useState(false)
  // A full sync discovers panels and rewrites routing, which operators cannot do.
  const { isSuperAdmin, canViewConfig } = useSession()
  const live = useLiveStats()
  const visible = usePageVisible()
  const [syncProblems, setSyncProblems] = useState<SyncProblem[]>([])
  const activeRefresh = useRef<Promise<boolean> | null>(null)
  const hasLoadedRef = useRef(false)

  const access = { isSuperAdmin, canViewConfig }
  const canViewPanels = canAccess('panels', access)
  const canViewInbounds = canAccess('inbounds', access)

  const refresh = useCallback((mode: RefreshMode = 'background'): Promise<boolean> => {
    if (activeRefresh.current) return activeRefresh.current

    if (mode === 'initial') {
      setInitialLoading(true)
      setLoadError(null)
    } else {
      setRefreshing(true)
    }

    const request = (async () => {
      try {
        const next = await api.get<Overview>('/overview')
        setData(next)
        setLoadError(null)
        setLastUpdated(Date.now())
        hasLoadedRef.current = true
        return true
      } catch (err) {
        setLoadError(errorMessage(err, '无法加载总览数据'))
        return false
      } finally {
        if (mode === 'initial') setInitialLoading(false)
        else setRefreshing(false)
        activeRefresh.current = null
      }
    })()

    activeRefresh.current = request
    return request
  }, [])

  useEffect(() => {
    if (!visible) return
    let cancelled = false
    let timer: number | undefined

    const poll = async (mode: RefreshMode) => {
      await refresh(mode)
      if (!cancelled) {
        timer = window.setTimeout(() => void poll('background'), POLL_INTERVAL_MS)
      }
    }

    void poll(hasLoadedRef.current ? 'background' : 'initial')
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [refresh, visible])

  const syncNow = async () => {
    setSyncing(true)
    setSyncProblems([])
    try {
      const result = await api.post<{
        discoverError: string
        reconcileError: string
        trafficError: string
      }>('/sync')

      const problems: SyncProblem[] = []
      if (result.discoverError) problems.push({ stage: '节点发现', message: result.discoverError })
      if (result.reconcileError) problems.push({ stage: '配置下发', message: result.reconcileError })
      if (result.trafficError) problems.push({ stage: '流量采集', message: result.trafficError })
      setSyncProblems(problems)

      if (problems.length > 0) {
        toast.warning(`同步已完成，但有 ${problems.length} 个阶段需要处理`)
      } else {
        toast.success('同步完成')
      }

      const pendingRefresh = activeRefresh.current
      if (pendingRefresh) await pendingRefresh
      const refreshed = await refresh('background')
      if (!refreshed) toast.error('同步操作已完成，但刷新总览数据失败')
    } catch (err) {
      toast.error(errorMessage(err, '同步失败'))
    } finally {
      setSyncing(false)
    }
  }

  if (!data && initialLoading) {
    return <OverviewLoading />
  }

  if (!data) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="总览" description="核心健康状态、资源可用性与累计用量" />
        <LoadErrorAlert
          title="暂时无法加载总览"
          message={loadError ?? '请检查网络连接后重试'}
          retrying={initialLoading}
          onRetry={() => void refresh('initial')}
        />
      </div>
    )
  }

  const counts = data.counts
  const onlinePanels = data.panels.filter((panel) => panel.status === 'online').length
  const panelRate = percentage(onlinePanels, counts.panels)
  const nodeRate = percentage(counts.enabledInbounds, counts.inbounds)
  const userRate = percentage(counts.activeUsers, counts.users)
  const hasAttention = data.failedSyncs > 0 || syncProblems.length > 0
  const liveByPanel = new Map<number, LivePanelStat>(
    (live.snapshot?.panels ?? []).map((panel) => [panel.id, panel]),
  )
  const showLiveColumns = live.enabled && live.snapshot !== null

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="总览" description="核心健康状态、资源可用性与累计用量">
        <div className="flex items-center gap-2 rounded-lg border bg-card px-2.5 py-1">
          <Switch
            id="overview-live-toggle"
            size="sm"
            checked={live.enabled}
            onCheckedChange={live.setEnabled}
            aria-label="实时监控"
          />
          <Label htmlFor="overview-live-toggle" className="cursor-pointer gap-1.5 text-xs font-medium">
            <span>实时监控</span>
            {live.enabled && <LiveStatusPill status={live.status} intervalMs={live.snapshot?.intervalMs} />}
          </Label>
        </div>
        <Badge variant="outline" aria-live="polite">
          {refreshing ? <Spinner data-icon="inline-start" /> : <Clock3Icon data-icon="inline-start" />}
          {refreshing
            ? '刷新中…'
            : lastUpdated
              ? `更新于 ${formatRefreshTime(lastUpdated)}`
              : '等待刷新'}
        </Badge>
        {isSuperAdmin && (
          <Button onClick={() => void syncNow()} disabled={syncing}>
            {syncing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
            {syncing ? '同步中…' : '立即同步'}
          </Button>
        )}
      </PageHeader>

      {loadError && (
        <Alert variant="warning">
          <TriangleAlertIcon />
          <AlertTitle>后台刷新失败，当前显示上次数据</AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              disabled={refreshing}
              onClick={() => void refresh('background')}
            >
              {refreshing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
              {refreshing ? '重试中…' : '重试'}
            </Button>
          </AlertAction>
        </Alert>
      )}

      {hasAttention && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertTitle>同步状态需要处理</AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-1">
              {data.failedSyncs > 0 && <p>有 {data.failedSyncs} 条用户下发记录失败。</p>}
              {syncProblems.length > 0 && (
                <ul className="flex list-disc flex-col gap-1 pl-4">
                  {syncProblems.map((problem) => (
                    <li key={problem.stage}>
                      {problem.stage}：{problem.message}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="sm"
              render={<a href={data.failedSyncs > 0 ? '#/users?status=failed' : '#/panels'} />}
              nativeButton={false}
            >
              {data.failedSyncs > 0 ? '查看异常用户' : '查看面板'}
              <ArrowRightIcon data-icon="inline-end" />
            </Button>
          </AlertAction>
        </Alert>
      )}

      <OverviewLiveSection live={live} />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <KpiLink href={canViewPanels ? '#/panels' : undefined} label="前往面板管理">
          <Card className={canViewPanels ? 'h-full transition-colors group-hover:border-primary/40' : 'h-full'}>
            <CardHeader>
              <CardTitle className="flex items-center gap-1.5">
                面板
                {canViewPanels && <ArrowRightIcon className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />}
              </CardTitle>
              <CardDescription>控制平面连通状态</CardDescription>
              <CardAction>
                <StatusBadge tone={counts.panels === 0 ? 'idle' : onlinePanels === counts.panels ? 'good' : 'bad'}>
                  <ServerIcon data-icon="inline-start" />
                  {counts.panels === 0
                    ? '尚未配置'
                    : onlinePanels === counts.panels
                      ? '全部在线'
                      : `${counts.panels - onlinePanels} 个异常`}
                </StatusBadge>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <div className="flex items-end gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">{onlinePanels}</strong>
                <span className="pb-1 text-sm text-muted-foreground">/ {counts.panels} 在线</span>
              </div>
              <Progress value={panelRate}>
                <ProgressLabel>在线率</ProgressLabel>
                <ProgressValue>{() => `${panelRate}%`}</ProgressValue>
              </Progress>
            </CardContent>
          </Card>
        </KpiLink>

        <KpiLink
          href={canViewInbounds ? (counts.missingInbounds > 0 ? '#/inbounds?status=missing' : '#/inbounds') : undefined}
          label="前往入站管理"
        >
          <Card className={canViewInbounds ? 'h-full transition-colors group-hover:border-primary/40' : 'h-full'}>
            <CardHeader>
              <CardTitle className="flex items-center gap-1.5">
                入站
                {canViewInbounds && <ArrowRightIcon className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />}
              </CardTitle>
              <CardDescription>可用节点与失联情况</CardDescription>
              <CardAction>
                <StatusBadge tone={counts.inbounds === 0 ? 'idle' : counts.missingInbounds > 0 ? 'bad' : 'good'}>
                  <LayersIcon data-icon="inline-start" />
                  {counts.inbounds === 0
                    ? '尚未发现'
                    : counts.missingInbounds > 0
                      ? `${counts.missingInbounds} 个失联`
                      : '状态正常'}
                </StatusBadge>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <div className="flex items-end gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">{counts.enabledInbounds}</strong>
                <span className="pb-1 text-sm text-muted-foreground">/ {counts.inbounds} 启用</span>
              </div>
              <Progress value={nodeRate}>
                <ProgressLabel>启用率</ProgressLabel>
                <ProgressValue>{() => `${nodeRate}%`}</ProgressValue>
              </Progress>
            </CardContent>
          </Card>
        </KpiLink>

        <KpiLink href={data.failedSyncs > 0 ? '#/users?status=failed' : '#/users'} label="前往用户管理">
          <Card className="h-full transition-colors group-hover:border-primary/40">
            <CardHeader>
              <CardTitle className="flex items-center gap-1.5">
                用户
                <ArrowRightIcon className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
              </CardTitle>
              <CardDescription>账户可用状态 · {counts.plans} 个套餐</CardDescription>
              <CardAction>
                <Badge variant={counts.users === 0 ? 'outline' : 'secondary'}>
                  <UsersIcon data-icon="inline-start" />
                  {counts.users === 0 ? '尚无用户' : `${counts.activeUsers} 个可用`}
                </Badge>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <div className="flex items-end gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">{counts.activeUsers}</strong>
                <span className="pb-1 text-sm text-muted-foreground">/ {counts.users} 可用</span>
              </div>
              <Progress value={userRate}>
                <ProgressLabel>可用率</ProgressLabel>
                <ProgressValue>{() => `${userRate}%`}</ProgressValue>
              </Progress>
            </CardContent>
          </Card>
        </KpiLink>

        <Card>
          <CardHeader>
            <CardTitle>累计流量</CardTitle>
            <CardDescription>
              总计 {formatBytes(data.totalUpload + data.totalDownload)}
              {data.upload + data.download > 0 || data.totalUpload + data.totalDownload > 0
                ? ` · 本周期 ${formatBytes(data.upload + data.download)}`
                : ''}
            </CardDescription>
            <CardAction>
              <Badge variant="outline">
                <ActivityIcon data-icon="inline-start" />
                全局累计
              </Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-2 gap-3">
              <div className="flex min-w-0 flex-col gap-1 rounded-lg bg-muted/50 p-3">
                <dt className="text-xs text-muted-foreground">累计上行</dt>
                <dd className="truncate font-heading text-lg font-semibold tabular-nums">
                  {formatBytes(data.totalUpload)}
                </dd>
              </div>
              <div className="flex min-w-0 flex-col gap-1 rounded-lg bg-muted/50 p-3">
                <dt className="text-xs text-muted-foreground">累计下行</dt>
                <dd className="truncate font-heading text-lg font-semibold tabular-nums">
                  {formatBytes(data.totalDownload)}
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>

      <TrafficDistribution data={data} />

      <Card>
        <CardHeader>
          <CardTitle>面板健康</CardTitle>
          <CardDescription>共 {data.panels.length} 个面板，每 15 秒自动刷新，快速定位不可达面板。</CardDescription>
          <CardAction>
            <RefreshButton loading={refreshing} onClick={() => void refresh('background')} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {data.panels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ServerOffIcon />
                </EmptyMedia>
                <EmptyTitle>还没有面板</EmptyTitle>
                <EmptyDescription>添加第一个 3X-UI 面板后，这里会持续展示连通状态。</EmptyDescription>
              </EmptyHeader>
              {canViewPanels && (
                <EmptyContent>
                  <Button render={<a href="#/panels" />} nativeButton={false}>
                    <PlusIcon data-icon="inline-start" />
                    添加面板
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>状态</TableHead>
                  {showLiveColumns && (
                    <>
                      <TableHead>实时网速 (↑ / ↓)</TableHead>
                      <TableHead>CPU</TableHead>
                      <TableHead>内存</TableHead>
                      <TableHead className="hidden lg:table-cell">负载指数</TableHead>
                      <TableHead className="hidden sm:table-cell">连接 (TCP/UDP)</TableHead>
                      <TableHead className="hidden md:table-cell">在线用户</TableHead>
                      <TableHead className="hidden xl:table-cell">延迟</TableHead>
                    </>
                  )}
                  <TableHead className="hidden md:table-cell">最近连通</TableHead>
                  <TableHead className="hidden lg:table-cell">最近错误</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.panels.map((panel) => {
                  const stat = liveByPanel.get(panel.id)
                  const effectiveStatus = stat ? (stat.online ? 'online' : 'offline') : panel.status
                  const effectiveError = stat && !stat.online && stat.error ? stat.error : panel.lastError
                  return (
                    <TableRow key={panel.id}>
                      <TableCell>
                        {canViewPanels ? (
                          <a href="#/panels" className="font-medium underline-offset-4 hover:underline">
                            {panel.name}
                          </a>
                        ) : (
                          <span className="font-medium">{panel.name}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1.5">
                          <PanelStatusBadge status={effectiveStatus} />
                          {effectiveError && (
                            <span className="lg:hidden">
                              <InfoPopover
                                label={`查看 ${panel.name} 的错误信息`}
                                title={`${panel.name} 最近错误`}
                                content={effectiveError}
                                copyValue={effectiveError}
                                className="text-destructive"
                              >
                                <TriangleAlertIcon className="size-3.5" />
                              </InfoPopover>
                            </span>
                          )}
                        </div>
                      </TableCell>
                      {showLiveColumns && (
                        <>
                          <TableCell className="whitespace-nowrap tabular-nums">
                            {stat?.online ? (
                              <span className="text-xs">
                                ↑ {formatSpeed(stat.netUp)} / ↓ {formatSpeed(stat.netDown)}
                              </span>
                            ) : (
                              <span className="text-muted-foreground">—</span>
                            )}
                          </TableCell>
                          <TableCell className="tabular-nums">
                            {stat?.cpu != null ? `${stat.cpu.toFixed(1)}%` : '—'}
                          </TableCell>
                          <TableCell className="whitespace-nowrap tabular-nums">
                            {stat?.online ? `${stat.memPercent.toFixed(1)}%` : '—'}
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums lg:table-cell">
                            {stat?.loadIndex != null ? `${stat.loadIndex.toFixed(1)} (${stat.load1.toFixed(2)})` : '—'}
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums sm:table-cell">
                            {stat?.online ? `${stat.tcpCount + stat.udpCount} (${stat.tcpCount}/${stat.udpCount})` : '—'}
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums md:table-cell">
                            {stat?.onlineUsers != null && stat.onlineClients != null
                              ? stat.onlineClients > stat.onlineUsers
                                ? `${stat.onlineUsers} (全量 ${stat.onlineClients})`
                                : String(stat.onlineUsers)
                              : '—'}
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums xl:table-cell">
                            {stat?.online ? `${stat.rttMs} ms` : '—'}
                          </TableCell>
                        </>
                      )}
                      <TableCell className="hidden md:table-cell">
                        <span className="whitespace-nowrap text-muted-foreground">
                          {formatTime(stat?.online ? stat.sampledAt : panel.lastSeenAt)}
                        </span>
                      </TableCell>
                      <TableCell className="hidden lg:table-cell">
                        {effectiveError ? (
                          <InfoPopover
                            label={`查看 ${panel.name} 的错误信息`}
                            title={`${panel.name} 最近错误`}
                            content={effectiveError}
                            copyValue={effectiveError}
                          >
                            <CodeText className="block max-w-80 truncate underline decoration-dotted underline-offset-4">
                              {effectiveError}
                            </CodeText>
                          </InfoPopover>
                        ) : (
                          <CodeText>—</CodeText>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function KpiLink({
  href,
  label,
  children,
}: {
  href?: string
  label: string
  children: React.ReactNode
}) {
  if (!href) return <>{children}</>
  return (
    <a href={href} aria-label={label} className="group block rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
      {children}
    </a>
  )
}

function OverviewLoading() {
  return (
    <div className="flex flex-col gap-6" aria-busy="true">
      <PageHeader title="总览" description="核心健康状态、资源可用性与累计用量" />
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Card key={index}>
            <CardHeader>
              <CardTitle>
                <Skeleton className="h-5 w-16" />
              </CardTitle>
              <CardDescription>
                <Skeleton className="h-4 w-28" />
              </CardDescription>
              <CardAction>
                <Skeleton className="h-5 w-20" />
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <Skeleton className="h-9 w-24" />
              <Skeleton className="h-8 w-full" />
            </CardContent>
          </Card>
        ))}
      </div>
      <Card>
        <CardHeader>
          <CardTitle>
            <Skeleton className="h-5 w-24" />
          </CardTitle>
          <CardDescription>
            <Skeleton className="h-4 w-48" />
          </CardDescription>
          <CardAction>
            <Skeleton className="h-7 w-16" />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          <TableSkeleton
            columns={[
              { label: '名称', width: 'w-28' },
              { label: '状态', width: 'w-14' },
              { label: '最近连通', className: 'hidden md:table-cell', width: 'w-32' },
              { label: '最近错误', className: 'hidden lg:table-cell', width: 'w-48' },
            ]}
          />
        </CardContent>
      </Card>
    </div>
  )
}

function percentage(value: number, total: number): number {
  if (total <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((value / total) * 100)))
}

function formatRefreshTime(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

function LiveStatusPill({ status, intervalMs }: { status: LiveStatus; intervalMs?: number }) {
  if (status === 'live') {
    const secs = intervalMs ? Math.max(1, Math.round(intervalMs / 1000)) : 2
    return (
      <span className="inline-flex items-center gap-1 text-[11px] text-success tabular-nums">
        <span className="size-1.5 animate-pulse rounded-full bg-success" aria-hidden="true" />
        {secs}s
      </span>
    )
  }
  if (status === 'paused') {
    return (
      <span className="inline-flex items-center gap-1 text-[11px] text-muted-foreground">
        <span className="size-1.5 rounded-full bg-muted-foreground" aria-hidden="true" />
        已暂停
      </span>
    )
  }
  if (status === 'connecting' || status === 'reconnecting') {
    return (
      <span className="inline-flex items-center gap-1 text-[11px] text-warning">
        <Spinner className="size-2.5" />
        {status === 'reconnecting' ? '重连中' : '连接中'}
      </span>
    )
  }
  return null
}

/**
 * TrafficDistribution shows how FireX traffic splits by panel or by node group,
 * either for each panel's current period or all-time.
 */
function TrafficDistribution({ data }: { data: Overview }) {
  const [scope, setScope] = useState<'period' | 'total'>('period')
  const [by, setBy] = useState<'group' | 'panel'>('group')
  const source = by === 'group' ? data.trafficByGroup : data.trafficByPanel
  const sliceValue = (slice: Overview['trafficByPanel'][number]) =>
    scope === 'period' ? slice.upload + slice.download : slice.totalUpload + slice.totalDownload
  const slices = foldSlices(
    source.map((slice) => ({ key: slice.id, label: slice.name || `#${slice.id}`, value: sliceValue(slice) })),
  )
  const total = source.reduce((sum, slice) => sum + sliceValue(slice), 0)
  return (
    <Card>
      <CardHeader>
        <CardTitle>流量分布</CardTitle>
        <CardDescription>
          {scope === 'period'
            ? `各面板当前周期的用量，共 ${formatBytes(total)}；节点组按其覆盖的面板汇总。`
            : `各面板历史累计用量，共 ${formatBytes(total)}；节点组按其覆盖的面板汇总。`}
        </CardDescription>
        <CardAction>
          <div className="flex flex-wrap items-center gap-2">
            <ToggleGroup
              aria-label="统计范围"
              variant="outline"
              size="sm"
              value={[scope]}
              onValueChange={(value) => value[0] && setScope(value[0] as 'period' | 'total')}
            >
              <ToggleGroupItem value="period">本周期</ToggleGroupItem>
              <ToggleGroupItem value="total">历史累计</ToggleGroupItem>
            </ToggleGroup>
            <ToggleGroup
              aria-label="分布维度"
              variant="outline"
              size="sm"
              value={[by]}
              onValueChange={(value) => value[0] && setBy(value[0] as 'group' | 'panel')}
            >
              <ToggleGroupItem value="group">按节点组</ToggleGroupItem>
              <ToggleGroupItem value="panel">按面板</ToggleGroupItem>
            </ToggleGroup>
          </div>
        </CardAction>
      </CardHeader>
      <CardContent>
        <TrafficShareBar
          slices={slices}
          ariaLabel={
            scope === 'period'
              ? by === 'group'
                ? '按节点组的本周期流量分布'
                : '按面板的本周期流量分布'
              : by === 'group'
                ? '按节点组的历史累计流量分布'
                : '按面板的历史累计流量分布'
          }
          emptyText={scope === 'period' ? '当前周期还没有流量。' : '还没有累计流量。'}
        />
      </CardContent>
    </Card>
  )
}

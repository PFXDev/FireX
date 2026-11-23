import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  ActivityIcon,
  ArrowDownIcon,
  ArrowRightIcon,
  ArrowUpIcon,
  Clock3Icon,
  LayersIcon,
  PlusIcon,
  RefreshCwIcon,
  ServerIcon,
  ServerOffIcon,
  SmartphoneIcon,
  TrendingDownIcon,
  TrendingUpIcon,
  TriangleAlertIcon,
  UsersIcon,
} from 'lucide-react'

import { api } from '@/api'
import type { LivePanelStat, Overview, OverviewPanelHealth } from '@/api'
import { CodeText } from '@/components/code-display'
import { InfoPopover } from '@/components/info-popover'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { OverviewLiveSection } from '@/components/overview-live'
import { PageHeader } from '@/components/page-header'
import { PanelStatusBadge, StatusBadge } from '@/components/status-badge'
import { SearchInput, SortableHead, TableToolbar, type SortDirection } from '@/components/table-toolbar'
import { TrafficHistoryCard } from '@/components/traffic-history-chart'
import { foldSlices, TrafficShareBar, type ShareSlice } from '@/components/traffic-share'
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
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { Progress, ProgressLabel, ProgressValue } from '@/components/ui/progress'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useLiveStats, type LiveInterval, type LiveStatus } from '@/hooks/use-live-stats'
import { usePageVisible } from '@/hooks/use-page-visible'
import {
  errorMessage,
  formatBytes,
  formatDeltaPercent,
  formatSpeed,
  formatTime,
  formatUptime,
} from '@/lib/format'
import { isXrayAbnormal, metricSeverity, panelSeverityRank } from '@/lib/live-stats'
import { canAccess } from '@/lib/navigation'
import { useSession } from '@/lib/session'
import { cn } from '@/lib/utils'

const POLL_INTERVAL_MS = 15_000
const LIVE_INTERVALS: LiveInterval[] = [2, 5, 10]

type RefreshMode = 'initial' | 'background'

type SyncProblem = {
  stage: string
  message: string
}

type AttentionItem = {
  key: string
  severity: 'bad' | 'warn'
  label: string
  href?: string
}

type PanelHealthFilter = 'all' | 'issues' | 'online' | 'offline' | 'overQuota' | 'disabled'
type PanelHealthSortKey =
  | 'severity'
  | 'name'
  | 'inbounds'
  | 'traffic'
  | 'speed'
  | 'cpu'
  | 'mem'
  | 'load'
  | 'disk'
  | 'connections'
  | 'onlineUsers'
  | 'rtt'
  | 'lastSeenAt'

const PANEL_FILTER_ITEMS: { value: PanelHealthFilter; label: string }[] = [
  { value: 'all', label: '全部面板' },
  { value: 'issues', label: '仅看异常' },
  { value: 'online', label: '在线' },
  { value: 'offline', label: '离线' },
  { value: 'overQuota', label: '已超配额' },
  { value: 'disabled', label: '已停用' },
]

export function OverviewPage() {
  const [data, setData] = useState<Overview | null>(null)
  const [initialLoading, setInitialLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const [syncing, setSyncing] = useState(false)
  const [trafficCardMode, setTrafficCardMode] = useState<'total' | 'period'>('total')
  const [panelQuery, setPanelQuery] = useState('')
  const [panelFilter, setPanelFilter] = useState<PanelHealthFilter>('all')
  const [panelSortKey, setPanelSortKey] = useState<PanelHealthSortKey>('severity')
  const [panelSortDir, setPanelSortDir] = useState<SortDirection>('asc')

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
  const canViewGroups = canAccess('node-groups', access)
  const canViewExternalSubs = canAccess('external-subscriptions', access)
  const canViewRouting = canAccess('routing', access)

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

  const liveByPanel = useMemo(
    () => new Map<number, LivePanelStat>((live.snapshot?.panels ?? []).map((panel) => [panel.id, panel])),
    [live.snapshot],
  )
  const showLiveColumns = live.enabled && live.snapshot !== null

  const togglePanelSort = (key: PanelHealthSortKey) => {
    if (panelSortKey === key) {
      setPanelSortDir((cur) => (cur === 'asc' ? 'desc' : 'asc'))
    } else {
      setPanelSortKey(key)
      setPanelSortDir(key === 'name' || key === 'severity' ? 'asc' : 'desc')
    }
  }

  const visiblePanels = useMemo(() => {
    if (!data) return []
    const kw = panelQuery.trim().toLowerCase()
    const filtered = data.panels.filter((panel) => {
      const stat = showLiveColumns ? liveByPanel.get(panel.id) : undefined
      const effectiveOnline = panel.enabled && (stat ? stat.online : panel.status === 'online')
      const xrayBad = panel.enabled && isXrayAbnormal(stat)
      const highDisk = Boolean(panel.enabled && stat && stat.diskTotal > 0 && stat.diskPercent >= 85)
      const hasIssue = panel.enabled && (!effectiveOnline || xrayBad || panel.overQuota || highDisk || panel.missingInbounds > 0)

      if (panelFilter === 'issues' && !hasIssue) return false
      if (panelFilter === 'online' && !effectiveOnline) return false
      if (panelFilter === 'offline' && (!panel.enabled || effectiveOnline)) return false
      if (panelFilter === 'overQuota' && !panel.overQuota) return false
      if (panelFilter === 'disabled' && panel.enabled) return false
      if (kw && !panel.name.toLowerCase().includes(kw)) return false
      return true
    })

    const factor = panelSortDir === 'asc' ? 1 : -1
    return [...filtered].sort((a, b) => {
      const sa = showLiveColumns ? liveByPanel.get(a.id) : undefined
      const sb = showLiveColumns ? liveByPanel.get(b.id) : undefined
      let diff = 0
      switch (panelSortKey) {
        case 'severity': {
          diff = panelSeverityRank(a, sa) - panelSeverityRank(b, sb)
          if (diff === 0) diff = a.name.localeCompare(b.name, 'zh-CN')
          return diff * factor
        }
        case 'name':
          return a.name.localeCompare(b.name, 'zh-CN') * factor
        case 'inbounds':
          diff = a.enabledInbounds - b.enabledInbounds
          break
        case 'traffic': {
          const ua = a.upload + a.download
          const ub = b.upload + b.download
          diff = ua - ub
          break
        }
        case 'speed': {
          const va = sa?.online ? sa.netUp + sa.netDown : -1
          const vb = sb?.online ? sb.netUp + sb.netDown : -1
          diff = va - vb
          break
        }
        case 'cpu':
          diff = (sa?.cpu ?? -1) - (sb?.cpu ?? -1)
          break
        case 'mem':
          diff = (sa?.online ? sa.memPercent : -1) - (sb?.online ? sb.memPercent : -1)
          break
        case 'load':
          diff = (sa?.loadIndex ?? -1) - (sb?.loadIndex ?? -1)
          break
        case 'disk':
          diff = (sa?.online && sa.diskTotal > 0 ? sa.diskPercent : -1) - (sb?.online && sb.diskTotal > 0 ? sb.diskPercent : -1)
          break
        case 'connections':
          diff = (sa?.online ? sa.tcpCount + sa.udpCount : -1) - (sb?.online ? sb.tcpCount + sb.udpCount : -1)
          break
        case 'onlineUsers':
          diff = (sa?.onlineUsers ?? -1) - (sb?.onlineUsers ?? -1)
          break
        case 'rtt':
          diff = (sa?.online ? sa.rttMs : -1) - (sb?.online ? sb.rttMs : -1)
          break
        case 'lastSeenAt': {
          const ta = sa?.online ? sa.sampledAt : a.lastSeenAt
          const tb = sb?.online ? sb.sampledAt : b.lastSeenAt
          diff = ta - tb
          break
        }
      }
      if (diff !== 0) return diff * factor
      return a.name.localeCompare(b.name, 'zh-CN')
    })
  }, [data, liveByPanel, panelFilter, panelQuery, panelSortDir, panelSortKey, showLiveColumns])

  if (!data && initialLoading) {
    return <OverviewLoading />
  }

  if (!data) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="总览" description="核心健康状态、风险预警、流量趋势与节点实时状态" />
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
  const breakdown = data.userBreakdown
  const todayTraffic = data.todayTraffic
  const attention = data.attention

  // Align enabled-panel online stats with SSE when live mode is active so KPI cards and table agree.
  const enabledPanels = counts.enabledPanels
  const onlinePanels = showLiveColumns
    ? live.snapshot!.summary.onlinePanels
    : counts.onlinePanels
  const offlinePanels = Math.max(0, enabledPanels - onlinePanels)
  const xrayAbnormalPanels = showLiveColumns ? live.snapshot!.summary.xrayAbnormalPanels : 0
  const highDiskPanels = showLiveColumns ? live.snapshot!.summary.highDiskPanels : 0

  const panelRate = percentage(onlinePanels, enabledPanels)
  const nodeRate = percentage(counts.enabledInbounds, counts.inbounds)
  const userRate = percentage(counts.activeUsers, counts.users)

  const configAttentionInbounds = counts.driftedInbounds + counts.pendingInbounds
  const inboundLink = !canViewInbounds
    ? undefined
    : counts.missingInbounds > 0
      ? '#/inbounds?status=missing'
      : configAttentionInbounds > 0
        ? '#/inbounds?status=attention'
        : counts.ungroupedInbounds > 0
          ? '#/inbounds?status=ungrouped'
          : '#/inbounds'

  const panelLink = !canViewPanels
    ? undefined
    : offlinePanels > 0
      ? '#/panels?status=offline'
      : counts.overQuotaPanels > 0
        ? '#/panels?status=overQuota'
        : '#/panels'

  const attentionItems: AttentionItem[] = []
  if (offlinePanels > 0) {
    attentionItems.push({
      key: 'offline-panels',
      severity: 'bad',
      label: `${offlinePanels} 台启用面板离线`,
      href: canViewPanels ? '#/panels?status=offline' : undefined,
    })
  }
  if (xrayAbnormalPanels > 0) {
    attentionItems.push({
      key: 'xray-abnormal',
      severity: 'bad',
      label: `${xrayAbnormalPanels} 台节点 Xray 未运行`,
      href: canViewPanels ? '#/panels' : undefined,
    })
  }
  if (counts.overQuotaPanels > 0) {
    attentionItems.push({
      key: 'over-quota',
      severity: 'bad',
      label: `${counts.overQuotaPanels} 台面板已超配额（已移出订阅）`,
      href: canViewPanels ? '#/panels?status=overQuota' : undefined,
    })
  }
  if (data.failedSyncs > 0) {
    attentionItems.push({
      key: 'failed-syncs',
      severity: 'bad',
      label: `${data.failedSyncs} 条用户下发失败`,
      href: '#/users?status=failed',
    })
  }
  if (counts.missingInbounds > 0) {
    attentionItems.push({
      key: 'missing-inbounds',
      severity: 'bad',
      label: `${counts.missingInbounds} 个入站失联`,
      href: canViewInbounds ? '#/inbounds?status=missing' : undefined,
    })
  }
  if (counts.failedExternalSubscriptions > 0) {
    attentionItems.push({
      key: 'failed-ext-subs',
      severity: 'bad',
      label: `${counts.failedExternalSubscriptions} 个外部订阅刷新失败`,
      href: canViewExternalSubs ? '#/external-subscriptions' : undefined,
    })
  }
  if (counts.nearQuotaPanels > 0) {
    attentionItems.push({
      key: 'near-quota',
      severity: 'warn',
      label: `${counts.nearQuotaPanels} 台面板配额 ≥80%`,
      href: canViewPanels ? '#/panels' : undefined,
    })
  }
  if (highDiskPanels > 0) {
    attentionItems.push({
      key: 'high-disk',
      severity: 'warn',
      label: `${highDiskPanels} 台节点磁盘 ≥85%`,
    })
  }
  if (configAttentionInbounds > 0) {
    attentionItems.push({
      key: 'config-attention',
      severity: 'warn',
      label: `${configAttentionInbounds} 个入站配置偏离或待下发`,
      href: canViewInbounds ? '#/inbounds?status=attention' : undefined,
    })
  }
  if (counts.ungroupedInbounds > 0) {
    attentionItems.push({
      key: 'ungrouped-inbounds',
      severity: 'warn',
      label: `${counts.ungroupedInbounds} 个启用入站未归组`,
      href: canViewInbounds ? '#/inbounds?status=ungrouped' : undefined,
    })
  }
  if (breakdown.expiringSoon > 0) {
    attentionItems.push({
      key: 'expiring-users',
      severity: 'warn',
      label: `${breakdown.expiringSoon} 名用户 7 天内到期`,
      href: '#/users?status=expiring',
    })
  }
  if (breakdown.nearDepletion > 0) {
    attentionItems.push({
      key: 'near-depletion',
      severity: 'warn',
      label: `${breakdown.nearDepletion} 名用户流量 ≥85%`,
      href: '#/users?status=nearDepletion',
    })
  }
  if (attention.pendingAgentSessions > 0) {
    attentionItems.push({
      key: 'pending-agent',
      severity: 'warn',
      label: `${attention.pendingAgentSessions} 个 Agent 修改待审核`,
      href: isSuperAdmin ? '#/agent' : undefined,
    })
  }
  if (attention.routingNeedsReview) {
    attentionItems.push({
      key: 'routing-review',
      severity: 'warn',
      label: '分流矩阵待确认',
      href: canViewRouting ? '#/routing' : undefined,
    })
  }
  if (attention.updateAvailable) {
    attentionItems.push({
      key: 'update-available',
      severity: 'warn',
      label: attention.updateReady
        ? `新版本 ${attention.updateAvailable} 已就绪待重启`
        : `发现新版本 ${attention.updateAvailable}`,
      href: isSuperAdmin ? '#/system' : undefined,
    })
  }

  const hasCriticalAttention =
    syncProblems.length > 0 || attentionItems.some((item) => item.severity === 'bad')
  const hasAttention = syncProblems.length > 0 || attentionItems.length > 0

  const todayTotal = todayTraffic.todayUpload + todayTraffic.todayDownload
  const yesterdaySameTimeTotal =
    todayTraffic.yesterdaySameTimeUpload + todayTraffic.yesterdaySameTimeDownload
  const yesterdayTotal = todayTraffic.yesterdayUpload + todayTraffic.yesterdayDownload
  const todayDelta = formatDeltaPercent(todayTotal, yesterdaySameTimeTotal)

  const displayedUp = trafficCardMode === 'total' ? data.totalUpload : data.upload
  const displayedDown = trafficCardMode === 'total' ? data.totalDownload : data.download

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="总览" description="核心健康状态、风险预警、流量趋势与节点实时状态">
        <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-card px-2.5 py-1">
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
          {live.enabled && (
            <div className="flex items-center gap-0.5 border-l pl-2" role="group" aria-label="采样频率">
              {LIVE_INTERVALS.map((secs) => (
                <button
                  key={secs}
                  type="button"
                  onClick={() => live.setInterval(secs)}
                  className={cn(
                    'rounded px-1.5 py-0.5 text-[11px] tabular-nums transition-colors',
                    live.interval === secs
                      ? 'bg-primary text-primary-foreground font-medium'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                  )}
                >
                  {secs}s
                </button>
              ))}
            </div>
          )}
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
          <div className="mt-2">
            <Button
              variant="outline"
              size="sm"
              disabled={refreshing}
              onClick={() => void refresh('background')}
            >
              {refreshing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
              {refreshing ? '重试中…' : '重试'}
            </Button>
          </div>
        </Alert>
      )}

      {hasAttention && (
        <Alert variant={hasCriticalAttention ? 'destructive' : 'warning'}>
          <TriangleAlertIcon />
          <AlertTitle>
            {hasCriticalAttention ? '检测到需要立即处理的异常' : '有以下待处理与风险预警'}
          </AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-2 pt-1">
              {attentionItems.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {attentionItems.map((item) => {
                    const pillClass = cn(
                      'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium transition-colors',
                      item.severity === 'bad'
                        ? 'border-destructive/30 bg-destructive-surface text-destructive-surface-foreground hover:bg-destructive/15'
                        : 'border-warning/30 bg-warning-surface text-warning-surface-foreground hover:bg-warning/15',
                    )
                    if (item.href) {
                      return (
                        <a key={item.key} href={item.href} className={pillClass}>
                          <span>{item.label}</span>
                          <ArrowRightIcon className="size-3" />
                        </a>
                      )
                    }
                    return (
                      <span key={item.key} className={pillClass}>
                        {item.label}
                      </span>
                    )
                  })}
                </div>
              )}
              {syncProblems.length > 0 && (
                <ul className="flex list-disc flex-col gap-1 pl-4 text-xs">
                  {syncProblems.map((problem) => (
                    <li key={problem.stage}>
                      {problem.stage}：{problem.message}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </AlertDescription>
        </Alert>
      )}

      <OverviewLiveSection live={live} />

      {/* 4 Core KPI Cards */}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {/* KPI 1: Panels */}
        <KpiLink href={panelLink} label="前往面板管理">
          <Card className={canViewPanels ? 'h-full transition-colors group-hover:border-primary/40' : 'h-full'}>
            <CardHeader>
              <CardTitle className="flex items-center gap-1.5">
                面板
                {canViewPanels && (
                  <ArrowRightIcon className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                )}
              </CardTitle>
              <CardDescription>
                {counts.disabledPanels > 0
                  ? `已启用 ${enabledPanels} 台 · ${counts.disabledPanels} 台已停用`
                  : '控制平面连通状态'}
              </CardDescription>
              <CardAction>
                <StatusBadge
                  tone={
                    enabledPanels === 0
                      ? 'idle'
                      : offlinePanels > 0 || xrayAbnormalPanels > 0 || counts.overQuotaPanels > 0
                        ? 'bad'
                        : counts.nearQuotaPanels > 0
                          ? 'warn'
                          : 'good'
                  }
                >
                  <ServerIcon data-icon="inline-start" />
                  {counts.panels === 0
                    ? '尚未配置'
                    : enabledPanels === 0
                      ? '全部停用'
                      : offlinePanels > 0
                        ? `${offlinePanels} 台离线`
                        : xrayAbnormalPanels > 0
                          ? `${xrayAbnormalPanels} 台 Xray 异常`
                          : counts.overQuotaPanels > 0
                            ? `${counts.overQuotaPanels} 台超配额`
                            : '全部在线'}
                </StatusBadge>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex items-end gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">{onlinePanels}</strong>
                <span className="pb-1 text-sm text-muted-foreground">/ {enabledPanels} 启用在线</span>
              </div>
              <Progress value={panelRate}>
                <ProgressLabel>启用在线率</ProgressLabel>
                <ProgressValue>{() => `${panelRate}%`}</ProgressValue>
              </Progress>
              {(counts.overQuotaPanels > 0 || counts.nearQuotaPanels > 0) && (
                <p className="text-xs text-muted-foreground tabular-nums">
                  {counts.overQuotaPanels > 0 ? `超配额 ${counts.overQuotaPanels} 台` : ''}
                  {counts.overQuotaPanels > 0 && counts.nearQuotaPanels > 0 ? ' · ' : ''}
                  {counts.nearQuotaPanels > 0 ? `接近配额 ${counts.nearQuotaPanels} 台` : ''}
                </p>
              )}
            </CardContent>
          </Card>
        </KpiLink>

        {/* KPI 2: Inbounds */}
        <KpiLink href={inboundLink} label="前往入站管理">
          <Card className={canViewInbounds ? 'h-full transition-colors group-hover:border-primary/40' : 'h-full'}>
            <CardHeader>
              <CardTitle className="flex items-center gap-1.5">
                入站
                {canViewInbounds && (
                  <ArrowRightIcon className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
                )}
              </CardTitle>
              <CardDescription>
                {counts.nodeGroups} 个节点组
                {counts.externalSubscriptions > 0 ? ` · ${counts.externalSubscriptions} 个外部订阅` : ''}
              </CardDescription>
              <CardAction>
                <StatusBadge
                  tone={
                    counts.inbounds === 0
                      ? 'idle'
                      : counts.missingInbounds > 0 || counts.driftedInbounds > 0
                        ? 'bad'
                        : counts.pendingInbounds > 0 || counts.ungroupedInbounds > 0
                          ? 'warn'
                          : 'good'
                  }
                >
                  <LayersIcon data-icon="inline-start" />
                  {counts.inbounds === 0
                    ? '尚未发现'
                    : counts.missingInbounds > 0
                      ? `${counts.missingInbounds} 个失联`
                      : configAttentionInbounds > 0
                        ? `${configAttentionInbounds} 个配置待处理`
                        : counts.ungroupedInbounds > 0
                          ? `${counts.ungroupedInbounds} 个未归组`
                          : '状态正常'}
                </StatusBadge>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex items-end gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">{counts.enabledInbounds}</strong>
                <span className="pb-1 text-sm text-muted-foreground">/ {counts.inbounds} 启用</span>
              </div>
              <Progress value={nodeRate}>
                <ProgressLabel>启用率</ProgressLabel>
                <ProgressValue>{() => `${nodeRate}%`}</ProgressValue>
              </Progress>
              {(counts.driftedInbounds > 0 || counts.pendingInbounds > 0 || counts.ungroupedInbounds > 0) && (
                <p className="truncate text-xs text-muted-foreground tabular-nums">
                  {[
                    counts.driftedInbounds > 0 ? `偏离 ${counts.driftedInbounds}` : '',
                    counts.pendingInbounds > 0 ? `待下发 ${counts.pendingInbounds}` : '',
                    counts.ungroupedInbounds > 0 ? `未归组 ${counts.ungroupedInbounds}` : '',
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </p>
              )}
            </CardContent>
          </Card>
        </KpiLink>

        {/* KPI 3: Users with status breakdown */}
        <KpiLink href={data.failedSyncs > 0 ? '#/users?status=failed' : '#/users'} label="前往用户管理">
          <Card className="h-full transition-colors group-hover:border-primary/40">
            <CardHeader>
              <CardTitle className="flex items-center gap-1.5">
                用户
                <ArrowRightIcon className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
              </CardTitle>
              <CardDescription>
                {counts.plans} 个套餐 · 近 24h 活跃订阅 {breakdown.subActive24h} 人
              </CardDescription>
              <CardAction>
                <Badge variant={counts.users === 0 ? 'outline' : 'secondary'}>
                  <UsersIcon data-icon="inline-start" />
                  {counts.users === 0 ? '尚无用户' : `${userRate}% 可用`}
                </Badge>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex items-end gap-2">
                <strong className="font-heading text-3xl font-semibold tabular-nums">{counts.activeUsers}</strong>
                <span className="pb-1 text-sm text-muted-foreground">/ {counts.users} 可用</span>
              </div>
              <UserStatusBreakdownBar breakdown={breakdown} total={counts.users} />
              <p className="truncate text-xs text-muted-foreground tabular-nums">
                {counts.users === 0
                  ? '创建用户后将展示状态构成'
                  : [
                      breakdown.expired > 0 ? `过期 ${breakdown.expired}` : '',
                      breakdown.depleted > 0 ? `耗尽 ${breakdown.depleted}` : '',
                      breakdown.disabled > 0 ? `停用 ${breakdown.disabled}` : '',
                      breakdown.expiringSoon > 0 ? `将到期 ${breakdown.expiringSoon}` : '',
                    ]
                      .filter(Boolean)
                      .join(' · ') || '全部用户处于正常状态'}
              </p>
            </CardContent>
          </Card>
        </KpiLink>

        {/* KPI 4: Traffic Overview (Total / Period + Today vs Yesterday) */}
        <Card className="h-full">
          <CardHeader>
            <CardTitle>{trafficCardMode === 'total' ? '累计流量' : '用户本周期'}</CardTitle>
            <CardDescription>
              总计 {formatBytes(displayedUp + displayedDown)}
              {trafficCardMode === 'total'
                ? ` · 用户本周期 ${formatBytes(data.upload + data.download)}`
                : ` · 历史累计 ${formatBytes(data.totalUpload + data.totalDownload)}`}
            </CardDescription>
            <CardAction>
              <ToggleGroup
                aria-label="流量统计口径"
                variant="outline"
                size="sm"
                value={[trafficCardMode]}
                onValueChange={(value) => {
                  if (value[0]) setTrafficCardMode(value[0] as 'total' | 'period')
                }}
              >
                <ToggleGroupItem value="total" className="h-6 px-2 text-[11px]">
                  累计
                </ToggleGroupItem>
                <ToggleGroupItem value="period" className="h-6 px-2 text-[11px]">
                  周期
                </ToggleGroupItem>
              </ToggleGroup>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <dl className="grid grid-cols-2 gap-2.5">
              <div className="flex min-w-0 flex-col gap-0.5 rounded-lg bg-muted/50 px-2.5 py-2">
                <dt className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                  <ArrowDownIcon className="size-3 text-brand" />
                  下行
                </dt>
                <dd className="truncate font-heading text-lg font-semibold tabular-nums">
                  {formatBytes(displayedDown)}
                </dd>
              </div>
              <div className="flex min-w-0 flex-col gap-0.5 rounded-lg bg-muted/50 px-2.5 py-2">
                <dt className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                  <ArrowUpIcon className="size-3 text-success" />
                  上行
                </dt>
                <dd className="truncate font-heading text-lg font-semibold tabular-nums">
                  {formatBytes(displayedUp)}
                </dd>
              </div>
            </dl>
            <Tooltip>
              <TooltipTrigger
                render={
                  <div className="flex items-center justify-between gap-2 rounded-lg border border-border/60 px-2.5 py-1.5 text-xs tabular-nums">
                    <span className="inline-flex items-center gap-1.5 text-muted-foreground">
                      <ActivityIcon className="size-3.5 text-brand" />
                      今日 {formatBytes(todayTotal)}
                    </span>
                    {todayDelta ? (
                      <span
                        className={cn(
                          'inline-flex items-center gap-0.5 font-medium',
                          todayDelta.direction === 'up'
                            ? 'text-brand'
                            : todayDelta.direction === 'down'
                              ? 'text-muted-foreground'
                              : 'text-muted-foreground',
                        )}
                      >
                        {todayDelta.direction === 'up' && <TrendingUpIcon className="size-3" />}
                        {todayDelta.direction === 'down' && <TrendingDownIcon className="size-3" />}
                        较昨日同期 {todayDelta.label}
                      </span>
                    ) : (
                      <span className="text-muted-foreground">昨日 {formatBytes(yesterdayTotal)}</span>
                    )}
                  </div>
                }
              />
              <TooltipContent>
                <div className="flex flex-col gap-0.5 tabular-nums">
                  <span>
                    今日：↓ {formatBytes(todayTraffic.todayDownload)} · ↑ {formatBytes(todayTraffic.todayUpload)}
                  </span>
                  <span>
                    昨日同期：{formatBytes(yesterdaySameTimeTotal)} · 昨日全天：{formatBytes(yesterdayTotal)}
                  </span>
                </div>
              </TooltipContent>
            </Tooltip>
          </CardContent>
        </Card>
      </div>

      {/* Historical Traffic Trend (24h / 7d / 30d) */}
      <TrafficHistoryCard overview={data} refreshSignal={lastUpdated} />

      {/* Traffic Distribution + Subscription Client Activity */}
      <div className="grid gap-4 xl:grid-cols-3">
        <div className="xl:col-span-2">
          <TrafficDistribution
            data={data}
            canViewPanels={canViewPanels}
            canViewGroups={canViewGroups}
          />
        </div>
        <ClientActivityCard data={data} />
      </div>

      {/* Panel Health Table */}
      <Card id="panel-health">
        <CardHeader>
          <CardTitle>面板健康</CardTitle>
          <CardDescription>
            共 {data.panels.length} 台面板（{enabledPanels} 台启用
            {counts.disabledPanels > 0 ? ` · ${counts.disabledPanels} 台停用` : ''}），默认异常置顶，每 15 秒自动刷新。
          </CardDescription>
          <CardAction>
            <RefreshButton loading={refreshing} onClick={() => void refresh('background')} />
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 px-0">
          {data.panels.length > 0 && (
            <TableToolbar>
              <SearchInput
                value={panelQuery}
                onChange={setPanelQuery}
                placeholder="搜索面板名称…"
                ariaLabel="搜索面板"
              />
              <Select
                items={PANEL_FILTER_ITEMS}
                value={panelFilter}
                onValueChange={(value) => setPanelFilter((value as PanelHealthFilter) || 'all')}
              >
                <SelectTrigger aria-label="按健康状态筛选" size="sm" className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {PANEL_FILTER_ITEMS.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {(panelQuery || panelFilter !== 'all') && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setPanelQuery('')
                    setPanelFilter('all')
                  }}
                >
                  清除筛选
                </Button>
              )}
            </TableToolbar>
          )}

          {data.panels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ServerOffIcon />
                </EmptyMedia>
                <EmptyTitle>还没有面板</EmptyTitle>
                <EmptyDescription>添加第一个 3X-UI 面板后，这里会持续展示连通状态与健康指标。</EmptyDescription>
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
          ) : visiblePanels.length === 0 ? (
            <p className="px-4 py-8 text-center text-sm text-muted-foreground">没有符合当前筛选条件的面板。</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableHead
                    label="名称"
                    active={panelSortKey === 'name'}
                    direction={panelSortDir}
                    onToggle={() => togglePanelSort('name')}
                  />
                  <SortableHead
                    label="状态"
                    active={panelSortKey === 'severity'}
                    direction={panelSortDir}
                    onToggle={() => togglePanelSort('severity')}
                  />
                  <SortableHead
                    label="入站"
                    active={panelSortKey === 'inbounds'}
                    direction={panelSortDir}
                    onToggle={() => togglePanelSort('inbounds')}
                    className="hidden sm:table-cell"
                  />
                  <SortableHead
                    label="本周期用量 / 配额"
                    active={panelSortKey === 'traffic'}
                    direction={panelSortDir}
                    onToggle={() => togglePanelSort('traffic')}
                    className="hidden md:table-cell"
                  />
                  {showLiveColumns && (
                    <>
                      <SortableHead
                        label="实时网速 (↑ / ↓)"
                        active={panelSortKey === 'speed'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('speed')}
                      />
                      <SortableHead
                        label="CPU"
                        active={panelSortKey === 'cpu'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('cpu')}
                      />
                      <SortableHead
                        label="内存"
                        active={panelSortKey === 'mem'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('mem')}
                      />
                      <SortableHead
                        label="负载"
                        active={panelSortKey === 'load'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('load')}
                        className="hidden xl:table-cell"
                      />
                      <SortableHead
                        label="磁盘"
                        active={panelSortKey === 'disk'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('disk')}
                        className="hidden xl:table-cell"
                      />
                      <SortableHead
                        label="连接"
                        active={panelSortKey === 'connections'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('connections')}
                        className="hidden lg:table-cell"
                      />
                      <SortableHead
                        label="在线用户"
                        active={panelSortKey === 'onlineUsers'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('onlineUsers')}
                        className="hidden lg:table-cell"
                      />
                      <SortableHead
                        label="延迟"
                        active={panelSortKey === 'rtt'}
                        direction={panelSortDir}
                        onToggle={() => togglePanelSort('rtt')}
                        className="hidden xl:table-cell"
                      />
                    </>
                  )}
                  <SortableHead
                    label="最近连通"
                    active={panelSortKey === 'lastSeenAt'}
                    direction={panelSortDir}
                    onToggle={() => togglePanelSort('lastSeenAt')}
                    className="hidden md:table-cell"
                  />
                  <TableHead className="hidden lg:table-cell">最近错误</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visiblePanels.map((panel) => {
                  const stat = showLiveColumns ? liveByPanel.get(panel.id) : undefined
                  const effectiveStatus = !panel.enabled
                    ? 'disabled'
                    : stat
                      ? stat.online
                        ? 'online'
                        : 'offline'
                      : panel.status
                  const xrayBad = panel.enabled && isXrayAbnormal(stat)
                  const highDisk = Boolean(panel.enabled && stat?.online && stat.diskTotal > 0 && stat.diskPercent >= 85)
                  const effectiveError =
                    stat && !stat.online && stat.error
                      ? stat.error
                      : xrayBad && stat?.xrayError
                        ? `Xray ${stat.xrayState}: ${stat.xrayError}`
                        : panel.lastError
                  const used = panel.upload + panel.download
                  const quotaPct = panel.trafficLimit > 0 ? Math.min(100, Math.round((used / panel.trafficLimit) * 100)) : null
                  const xrayVersion = stat?.xrayVersion || panel.xrayVersion
                  const uptimeText = stat?.online && stat.uptime > 0 ? formatUptime(stat.uptime) : ''

                  return (
                    <TableRow key={panel.id} className={cn(!panel.enabled && 'opacity-65')}>
                      <TableCell>
                        <div className="flex flex-col gap-0.5">
                          {canViewPanels ? (
                            <a href="#/panels" className="font-medium underline-offset-4 hover:underline">
                              {panel.name}
                            </a>
                          ) : (
                            <span className="font-medium">{panel.name}</span>
                          )}
                          {(xrayVersion || uptimeText || panel.userCount > 0) && (
                            <span className="text-[11px] text-muted-foreground tabular-nums">
                              {[
                                xrayVersion ? `Xray ${xrayVersion}` : '',
                                uptimeText ? `运行 ${uptimeText}` : '',
                                panel.userCount > 0 ? `${panel.userCount} 用户` : '',
                              ]
                                .filter(Boolean)
                                .join(' · ')}
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-1">
                          {effectiveStatus === 'disabled' ? (
                            <StatusBadge tone="idle">已停用</StatusBadge>
                          ) : (
                            <PanelStatusBadge status={effectiveStatus} />
                          )}
                          {xrayBad && (
                            <StatusBadge tone="bad" title={stat?.xrayError || `Xray 状态: ${stat?.xrayState}`}>
                              Xray {stat?.xrayState}
                            </StatusBadge>
                          )}
                          {panel.overQuota && <StatusBadge tone="bad">超配额</StatusBadge>}
                          {highDisk && <StatusBadge tone="warn">磁盘 {stat?.diskPercent.toFixed(0)}%</StatusBadge>}
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
                      <TableCell className="hidden whitespace-nowrap tabular-nums sm:table-cell">
                        <span>
                          {panel.enabledInbounds} / {panel.inboundCount}
                        </span>
                        {panel.missingInbounds > 0 && (
                          <span className="ml-1 text-xs text-destructive">({panel.missingInbounds} 失联)</span>
                        )}
                      </TableCell>
                      <TableCell className="hidden whitespace-nowrap tabular-nums md:table-cell">
                        <PanelQuotaCell panel={panel} used={used} quotaPct={quotaPct} />
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
                          <TableCell className="whitespace-nowrap tabular-nums">
                            <MetricCell value={stat?.cpu} unit="%" warnAt={75} badAt={90} />
                          </TableCell>
                          <TableCell className="whitespace-nowrap tabular-nums">
                            <MetricCell
                              value={stat?.online ? stat.memPercent : null}
                              unit="%"
                              warnAt={75}
                              badAt={90}
                              title={
                                stat?.online && stat.memTotal > 0
                                  ? `${formatBytes(stat.memUsed)} / ${formatBytes(stat.memTotal)}`
                                  : undefined
                              }
                            />
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums xl:table-cell">
                            <MetricCell
                              value={stat?.loadIndex}
                              unit=""
                              warnAt={65}
                              badAt={85}
                              suffix={stat?.loadIndex != null ? ` (${stat.load1.toFixed(2)})` : undefined}
                            />
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums xl:table-cell">
                            <MetricCell
                              value={stat?.online && stat.diskTotal > 0 ? stat.diskPercent : null}
                              unit="%"
                              warnAt={75}
                              badAt={85}
                              title={
                                stat?.online && stat.diskTotal > 0
                                  ? `${formatBytes(stat.diskUsed)} / ${formatBytes(stat.diskTotal)}`
                                  : undefined
                              }
                            />
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums lg:table-cell">
                            {stat?.online ? `${stat.tcpCount + stat.udpCount} (${stat.tcpCount}/${stat.udpCount})` : '—'}
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums lg:table-cell">
                            {stat?.onlineUsers != null && stat.onlineClients != null
                              ? stat.onlineClients > stat.onlineUsers
                                ? `${stat.onlineUsers} (全量 ${stat.onlineClients})`
                                : String(stat.onlineUsers)
                              : '—'}
                          </TableCell>
                          <TableCell className="hidden whitespace-nowrap tabular-nums xl:table-cell">
                            <MetricCell
                              value={stat?.online ? stat.rttMs : null}
                              unit=" ms"
                              decimals={0}
                              warnAt={200}
                              badAt={500}
                            />
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
                            <CodeText className="block max-w-64 truncate underline decoration-dotted underline-offset-4">
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

function PanelQuotaCell({
  panel,
  used,
  quotaPct,
}: {
  panel: OverviewPanelHealth
  used: number
  quotaPct: number | null
}) {
  if (panel.trafficLimit <= 0) {
    return (
      <span className="text-xs text-muted-foreground" title={`历史累计 ${formatBytes(panel.totalUpload + panel.totalDownload)}`}>
        {formatBytes(used)}
      </span>
    )
  }
  const severity = panel.overQuota || (quotaPct !== null && quotaPct >= 100) ? 'bad' : quotaPct !== null && quotaPct >= 80 ? 'warn' : 'normal'
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div className="flex min-w-28 flex-col gap-1">
            <div className="flex items-center justify-between gap-2 text-xs">
              <span
                className={cn(
                  severity === 'bad'
                    ? 'font-semibold text-destructive'
                    : severity === 'warn'
                      ? 'font-medium text-warning'
                      : '',
                )}
              >
                {formatBytes(used)}
              </span>
              <span className="text-muted-foreground">/ {formatBytes(panel.trafficLimit)}</span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div
                className={cn(
                  'h-full rounded-full transition-all',
                  severity === 'bad' ? 'bg-destructive' : severity === 'warn' ? 'bg-warning' : 'bg-brand',
                )}
                style={{ width: `${Math.max(used > 0 ? 2 : 0, quotaPct ?? 0)}%` }}
              />
            </div>
          </div>
        }
      />
      <TooltipContent>
        <div className="flex flex-col gap-0.5 tabular-nums">
          <span>
            本周期：↓ {formatBytes(panel.download)} · ↑ {formatBytes(panel.upload)} ({quotaPct}%)
          </span>
          <span>历史累计：{formatBytes(panel.totalUpload + panel.totalDownload)}</span>
          {panel.nextTrafficReset > 0 && <span>下次重置：{formatTime(panel.nextTrafficReset)}</span>}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}

function MetricCell({
  value,
  unit,
  warnAt,
  badAt,
  decimals = 1,
  suffix,
  title,
}: {
  value: number | null | undefined
  unit: string
  warnAt: number
  badAt: number
  decimals?: number
  suffix?: string
  title?: string
}) {
  if (value == null || !Number.isFinite(value)) {
    return <span className="text-muted-foreground">—</span>
  }
  const severity = metricSeverity(value, warnAt, badAt)
  return (
    <span
      title={title}
      className={cn(
        severity === 'bad'
          ? 'font-semibold text-destructive'
          : severity === 'warn'
            ? 'font-medium text-warning'
            : '',
      )}
    >
      {value.toFixed(decimals)}
      {unit}
      {suffix ?? ''}
    </span>
  )
}

function UserStatusBreakdownBar({
  breakdown,
  total,
}: {
  breakdown: Overview['userBreakdown']
  total: number
}) {
  if (total <= 0) {
    return <div className="h-2 w-full rounded-full bg-muted" />
  }
  const segments = [
    { key: 'active', label: '可用', count: breakdown.active, color: 'bg-success' },
    { key: 'expired', label: '已过期', count: breakdown.expired, color: 'bg-warning' },
    { key: 'depleted', label: '流量耗尽', count: breakdown.depleted, color: 'bg-destructive' },
    { key: 'disabled', label: '已停用', count: breakdown.disabled, color: 'bg-muted-foreground/40' },
  ].filter((s) => s.count > 0)

  return (
    <div
      role="img"
      aria-label={`用户状态构成：可用 ${breakdown.active}，已过期 ${breakdown.expired}，流量耗尽 ${breakdown.depleted}，已停用 ${breakdown.disabled}`}
      className="flex h-2 w-full gap-0.5 overflow-hidden rounded-full bg-muted"
    >
      {segments.map((seg) => (
        <span
          key={seg.key}
          title={`${seg.label}：${seg.count} 人`}
          className={cn('h-full min-w-1 first:rounded-l-full last:rounded-r-full', seg.color)}
          style={{ flexGrow: seg.count, flexBasis: 0 }}
        />
      ))}
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
    <a
      href={href}
      aria-label={label}
      className="group block rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      {children}
    </a>
  )
}

function OverviewLoading() {
  return (
    <div className="flex flex-col gap-6" aria-busy="true">
      <PageHeader title="总览" description="核心健康状态、风险预警、流量趋势与节点实时状态" />
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
            <Skeleton className="h-4 w-56" />
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Skeleton className="h-40 w-full" />
        </CardContent>
      </Card>
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

type DistributionDimension = 'group' | 'panel' | 'plan' | 'user'

/**
 * TrafficDistribution splits FireX traffic by node group, panel, plan, or top
 * users, for either the current period or all-time, with clear period semantics.
 */
function TrafficDistribution({
  data,
  canViewPanels,
  canViewGroups,
}: {
  data: Overview
  canViewPanels: boolean
  canViewGroups: boolean
}) {
  const [scope, setScope] = useState<'period' | 'total'>('period')
  const [by, setBy] = useState<DistributionDimension>('group')

  const { slices, total, description, ariaLabel } = useMemo(() => {
    if (by === 'user') {
      const list = scope === 'period' ? data.topUsers.period : data.topUsers.total
      const mapped: ShareSlice[] = list.map((u) => {
        const up = scope === 'period' ? u.upload : u.totalUpload
        const down = scope === 'period' ? u.download : u.totalDownload
        return {
          key: u.id,
          label: u.username,
          value: up + down,
          upload: up,
          download: down,
          limit: scope === 'period' ? u.trafficLimit : undefined,
          href: '#/users',
          meta: u.planName || '未分配套餐',
        }
      })
      const sum = mapped.reduce((acc, s) => acc + s.value, 0)
      return {
        slices: foldSlices(mapped),
        total: sum,
        description:
          scope === 'period'
            ? `当前用户周期用量 Top ${list.length}，合计 ${formatBytes(sum)}（全量用户本周期 ${formatBytes(data.upload + data.download)}）。`
            : `历史累计用量 Top ${list.length} 用户，合计 ${formatBytes(sum)}。`,
        ariaLabel: scope === 'period' ? '用户本周期 Top 流量分布' : '用户历史累计 Top 流量分布',
      }
    }

    const source =
      by === 'group'
        ? data.trafficByGroup
        : by === 'panel'
          ? data.trafficByPanel
          : data.trafficByPlan

    const mapped: ShareSlice[] = source.map((slice) => {
      const up = scope === 'period' ? slice.upload : slice.totalUpload
      const down = scope === 'period' ? slice.download : slice.totalDownload
      return {
        key: slice.id,
        label: slice.name || `#${slice.id}`,
        value: up + down,
        upload: up,
        download: down,
        limit: by === 'panel' && scope === 'period' ? slice.trafficLimit : undefined,
        overQuota: by === 'panel' && scope === 'period' ? slice.overQuota : undefined,
        href:
          by === 'panel' && canViewPanels
            ? '#/panels'
            : by === 'group' && canViewGroups && slice.id > 0
              ? '#/node-groups'
              : by === 'plan'
                ? '#/plans'
                : undefined,
      }
    })
    const sum = mapped.reduce((acc, s) => acc + s.value, 0)
    const desc =
      by === 'plan'
        ? scope === 'period'
          ? `按用户当前周期汇总的各套餐用量，共 ${formatBytes(sum)}。`
          : `各套餐下用户的历史累计用量，共 ${formatBytes(sum)}。`
        : scope === 'period'
          ? `按面板重置周期统计的用量，共 ${formatBytes(sum)}；节点组按其覆盖的面板汇总。`
          : `各面板历史累计用量，共 ${formatBytes(sum)}；节点组按其覆盖的面板汇总。`
    const labelMap: Record<'group' | 'panel' | 'plan', string> = {
      group: scope === 'period' ? '按节点组的本周期流量分布' : '按节点组的历史累计流量分布',
      panel: scope === 'period' ? '按面板的本周期流量分布' : '按面板的历史累计流量分布',
      plan: scope === 'period' ? '按套餐的本周期流量分布' : '按套餐的历史累计流量分布',
    }
    return {
      slices: foldSlices(mapped),
      total: sum,
      description: desc,
      ariaLabel: labelMap[by],
    }
  }, [by, canViewGroups, canViewPanels, data, scope])

  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle>流量分布</CardTitle>
        <CardDescription>{description}</CardDescription>
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
              onValueChange={(value) => value[0] && setBy(value[0] as DistributionDimension)}
            >
              <ToggleGroupItem value="group">节点组</ToggleGroupItem>
              <ToggleGroupItem value="panel">面板</ToggleGroupItem>
              <ToggleGroupItem value="plan">套餐</ToggleGroupItem>
              <ToggleGroupItem value="user">Top 用户</ToggleGroupItem>
            </ToggleGroup>
          </div>
        </CardAction>
      </CardHeader>
      <CardContent>
        <TrafficShareBar
          slices={slices}
          ariaLabel={ariaLabel}
          emptyText={
            total <= 0
              ? scope === 'period'
                ? '当前周期还没有流量。'
                : '还没有累计流量。'
              : undefined
          }
        />
      </CardContent>
    </Card>
  )
}

function ClientActivityCard({ data }: { data: Overview }) {
  const { userBreakdown, clients, counts } = data
  const clientSlices = useMemo(
    () =>
      foldSlices(
        clients.map((item) => ({
          key: item.name,
          label: item.name,
          value: item.count,
        })),
      ),
    [clients],
  )

  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="flex items-center gap-1.5">
          <SmartphoneIcon className="size-4 text-muted-foreground" />
          <span>订阅活跃与客户端</span>
        </CardTitle>
        <CardDescription>
          共 {userBreakdown.subFetched} / {counts.users} 名用户拉取过订阅
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="grid grid-cols-2 gap-2.5 text-xs">
          <div className="flex flex-col gap-0.5 rounded-lg bg-muted/50 px-3 py-2">
            <span className="text-muted-foreground">近 24 小时活跃</span>
            <strong className="font-heading text-xl font-semibold tabular-nums">
              {userBreakdown.subActive24h}
              <span className="ml-1 text-xs font-normal text-muted-foreground">人</span>
            </strong>
          </div>
          <div className="flex flex-col gap-0.5 rounded-lg bg-muted/50 px-3 py-2">
            <span className="text-muted-foreground">近 7 天活跃</span>
            <strong className="font-heading text-xl font-semibold tabular-nums">
              {userBreakdown.subActive7d}
              <span className="ml-1 text-xs font-normal text-muted-foreground">人</span>
            </strong>
          </div>
        </div>
        <TrafficShareBar
          slices={clientSlices}
          ariaLabel="订阅客户端分布"
          emptyText="还没有记录到客户端订阅拉取。"
          formatValue={(n) => `${n} 人`}
        />
      </CardContent>
    </Card>
  )
}

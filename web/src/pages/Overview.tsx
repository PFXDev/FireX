import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  ActivityIcon,
  ArrowRightIcon,
  Clock3Icon,
  LayersIcon,
  RefreshCwIcon,
  ServerIcon,
  ServerOffIcon,
  TriangleAlertIcon,
  UsersIcon,
} from 'lucide-react'

import { api } from '@/api'
import type { Overview } from '@/api'
import { CodeText } from '@/components/code-display'
import { PageHeader } from '@/components/page-header'
import { PanelStatusBadge } from '@/components/status-badge'
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
import { Progress, ProgressLabel, ProgressValue } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatBytes, formatTime } from '@/lib/format'

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
  const [syncProblems, setSyncProblems] = useState<SyncProblem[]>([])
  const activeRefresh = useRef<Promise<boolean> | null>(null)

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
    let cancelled = false
    let timer: number | undefined

    const poll = async (mode: RefreshMode) => {
      await refresh(mode)
      if (!cancelled) {
        timer = window.setTimeout(() => void poll('background'), POLL_INTERVAL_MS)
      }
    }

    void poll('initial')
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [refresh])

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
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertTitle>暂时无法加载总览</AlertTitle>
          <AlertDescription>{loadError ?? '请检查网络连接后重试。'}</AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="xs"
              disabled={initialLoading}
              onClick={() => void refresh('initial')}
            >
              <RefreshCwIcon data-icon="inline-start" />
              重试
            </Button>
          </AlertAction>
        </Alert>
      </div>
    )
  }

  const counts = data.counts
  const onlinePanels = data.panels.filter((panel) => panel.status === 'online').length
  const panelRate = percentage(onlinePanels, counts.panels)
  const nodeRate = percentage(counts.enabledInbounds, counts.inbounds)
  const userRate = percentage(counts.activeUsers, counts.users)
  const hasAttention = data.failedSyncs > 0 || syncProblems.length > 0

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="总览" description="核心健康状态、资源可用性与累计用量">
        <Badge variant="outline" aria-live="polite">
          {refreshing ? <Spinner data-icon="inline-start" /> : <Clock3Icon data-icon="inline-start" />}
          {refreshing
            ? '正在刷新'
            : lastUpdated
              ? `更新于 ${formatRefreshTime(lastUpdated)}`
              : '等待刷新'}
        </Badge>
        <Button onClick={() => void syncNow()} disabled={syncing}>
          {syncing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
          {syncing ? '同步中…' : '立即同步'}
        </Button>
      </PageHeader>

      {loadError && (
        <Alert variant="warning">
          <TriangleAlertIcon />
          <AlertTitle>后台刷新失败，当前显示上次数据</AlertTitle>
          <AlertDescription>{loadError}</AlertDescription>
          <AlertAction>
            <Button
              variant="outline"
              size="xs"
              disabled={refreshing}
              onClick={() => void refresh('background')}
            >
              <RefreshCwIcon data-icon="inline-start" />
              再试一次
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
              size="xs"
              render={<a href={data.failedSyncs > 0 ? '#/users' : '#/panels'} />}
              nativeButton={false}
            >
              {data.failedSyncs > 0 ? '查看用户' : '查看面板'}
              <ArrowRightIcon data-icon="inline-end" />
            </Button>
          </AlertAction>
        </Alert>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Card>
          <CardHeader>
            <CardTitle>面板</CardTitle>
            <CardDescription>控制平面连通状态</CardDescription>
            <CardAction>
              <Badge
                variant={
                  counts.panels === 0 ? 'outline' : onlinePanels === counts.panels ? 'success' : 'destructive'
                }
              >
                <ServerIcon data-icon="inline-start" />
                {counts.panels === 0
                  ? '尚未配置'
                  : onlinePanels === counts.panels
                    ? '全部在线'
                    : `${counts.panels - onlinePanels} 个异常`}
              </Badge>
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

        <Card>
          <CardHeader>
            <CardTitle>入站</CardTitle>
            <CardDescription>可用节点与失联情况</CardDescription>
            <CardAction>
              <Badge
                variant={
                  counts.inbounds === 0 ? 'outline' : counts.missingInbounds > 0 ? 'destructive' : 'success'
                }
              >
                <LayersIcon data-icon="inline-start" />
                {counts.inbounds === 0
                  ? '尚未发现'
                  : counts.missingInbounds > 0
                    ? `${counts.missingInbounds} 个失联`
                    : '状态正常'}
              </Badge>
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

        <Card>
          <CardHeader>
            <CardTitle>用户</CardTitle>
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

        <Card>
          <CardHeader>
            <CardTitle>累计流量</CardTitle>
            <CardDescription>总计 {formatBytes(data.upload + data.download)}</CardDescription>
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
                  {formatBytes(data.upload)}
                </dd>
              </div>
              <div className="flex min-w-0 flex-col gap-1 rounded-lg bg-muted/50 p-3">
                <dt className="text-xs text-muted-foreground">累计下行</dt>
                <dd className="truncate font-heading text-lg font-semibold tabular-nums">
                  {formatBytes(data.download)}
                </dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>面板健康</CardTitle>
          <CardDescription>每 15 秒自动刷新，快速定位不可达面板。</CardDescription>
          <CardAction>
            <Badge variant={data.panels.length === 0 ? 'outline' : 'secondary'}>
              {data.panels.length} 个面板
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent>
          {data.panels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ServerOffIcon />
                </EmptyMedia>
                <EmptyTitle>还没有面板</EmptyTitle>
                <EmptyDescription>添加第一个 3X-UI 面板后，这里会持续展示连通状态。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button render={<a href="#/panels" />} nativeButton={false}>
                  添加面板
                  <ArrowRightIcon data-icon="inline-end" />
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="hidden md:table-cell">最近连通</TableHead>
                  <TableHead className="hidden lg:table-cell">最近错误</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.panels.map((panel) => (
                  <TableRow key={panel.id}>
                    <TableCell>
                      <span className="font-medium">{panel.name}</span>
                    </TableCell>
                    <TableCell>
                      <PanelStatusBadge status={panel.status} />
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <span className="whitespace-nowrap text-muted-foreground">
                        {formatTime(panel.lastSeenAt)}
                      </span>
                    </TableCell>
                    <TableCell className="hidden lg:table-cell">
                      <CodeText
                        className="block max-w-80 truncate"
                        title={panel.lastError || undefined}
                      >
                        {panel.lastError || '—'}
                      </CodeText>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
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
            <Skeleton className="h-5 w-16" />
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-10 w-full" />
          ))}
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

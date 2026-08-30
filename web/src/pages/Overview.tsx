import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { RefreshCwIcon, ServerOffIcon, TriangleAlertIcon } from 'lucide-react'

import { api } from '@/api'
import type { Overview } from '@/api'
import { PageHeader } from '@/components/page-header'
import { PanelStatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatBytes, formatTime } from '@/lib/format'

export function OverviewPage() {
  const [data, setData] = useState<Overview | null>(null)
  const [syncing, setSyncing] = useState(false)

  const load = useCallback(async () => {
    setData(await api.get<Overview>('/overview'))
  }, [])

  useEffect(() => {
    void load()
    const timer = setInterval(() => void load(), 15000)
    return () => clearInterval(timer)
  }, [load])

  const syncNow = async () => {
    setSyncing(true)
    try {
      const res = await api.post<{ discoverError: string; reconcileError: string; trafficError: string }>('/sync')
      const problems = [res.discoverError, res.reconcileError, res.trafficError].filter(Boolean)
      if (problems.length) problems.forEach((p) => toast.error(p))
      else toast.success('同步完成')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '同步失败'))
    } finally {
      setSyncing(false)
    }
  }

  if (!data) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="总览" description="面板健康状态与全局用量" />
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
          {Array.from({ length: 6 }, (_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-4 w-16" />
                <Skeleton className="h-7 w-20" />
              </CardHeader>
            </Card>
          ))}
        </div>
        <Card>
          <CardHeader>
            <Skeleton className="h-5 w-24" />
            <Skeleton className="h-4 w-32" />
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </CardContent>
        </Card>
      </div>
    )
  }

  const c = data.counts
  const online = data.panels.filter((p) => p.status === 'online').length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="总览" description="面板健康状态与全局用量">
        <Button onClick={syncNow} disabled={syncing}>
          {syncing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
          {syncing ? '同步中…' : '立即同步'}
        </Button>
      </PageHeader>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
        <Stat label="面板" value={c.panels} sub={`${online} 在线`} />
        <Stat
          label="节点"
          value={c.enabledNodes}
          sub={`共 ${c.nodes} 个${c.missingNodes ? ` · ${c.missingNodes} 已失联` : ''}`}
        />
        <Stat label="套餐" value={c.plans} />
        <Stat label="用户" value={c.users} sub={`${c.activeUsers} 个可用`} />
        <Stat label="上行" value={formatBytes(data.upload)} />
        <Stat label="下行" value={formatBytes(data.download)} />
      </div>

      {data.failedSyncs > 0 && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertTitle>有 {data.failedSyncs} 条下发记录失败</AlertTitle>
          <AlertDescription>到「用户」页查看具体面板和错误原因，修复后可单独重发。</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>面板状态</CardTitle>
          <CardDescription>每 15 秒刷新一次</CardDescription>
        </CardHeader>
        <CardContent>
          {data.panels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ServerOffIcon />
                </EmptyMedia>
                <EmptyTitle>还没有面板</EmptyTitle>
                <EmptyDescription>到「面板」页添加第一个 3x-ui 面板。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最近连通</TableHead>
                  <TableHead>错误</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.panels.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="font-medium">{p.name}</TableCell>
                    <TableCell>
                      <PanelStatusBadge status={p.status} />
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatTime(p.lastSeenAt)}
                    </TableCell>
                    <TableCell className="max-w-80 truncate font-mono text-xs text-muted-foreground">
                      {p.lastError || '—'}
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

function Stat({ label, value, sub }: { label: string; value: number | string; sub?: string }) {
  return (
    <Card>
      <CardHeader>
        <CardDescription className="text-xs tracking-wide uppercase">{label}</CardDescription>
        <CardTitle className="text-2xl font-semibold tabular-nums">{value}</CardTitle>
      </CardHeader>
      {sub && <CardContent className="text-xs text-muted-foreground">{sub}</CardContent>}
    </Card>
  )
}

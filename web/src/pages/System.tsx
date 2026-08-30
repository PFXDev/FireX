import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  CheckCircle2Icon,
  DownloadIcon,
  RefreshCwIcon,
  RotateCwIcon,
  TriangleAlertIcon,
  XIcon,
} from 'lucide-react'

import { api } from '@/api'
import type { UpdateCheck, UpdateStatus, VersionInfo } from '@/api'
import { PageHeader } from '@/components/page-header'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress, ProgressLabel, ProgressValue } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { errorMessage } from '@/lib/format'

const BUSY_STATES: UpdateStatus['state'][] = ['checking', 'downloading', 'applying']

const STATE_LABELS: Record<UpdateStatus['state'], string> = {
  idle: '空闲',
  checking: '检查中',
  downloading: '下载中',
  ready: '待重启',
  applying: '安装中',
  failed: '失败',
}

export function SystemPage() {
  const [info, setInfo] = useState<VersionInfo | null>(null)
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [acting, setActing] = useState(false)
  const [check, setCheck] = useState<UpdateCheck | null>(null)

  const busy = status ? BUSY_STATES.includes(status.state) : false

  const loadStatus = useCallback(async () => {
    setStatus(await api.get<UpdateStatus>('/update/status'))
  }, [])

  useEffect(() => {
    void api.get<VersionInfo>('/version').then(setInfo)
    void loadStatus()
  }, [loadStatus])

  // Poll faster while the updater is working so the progress bar tracks the
  // download instead of jumping.
  useEffect(() => {
    const timer = setInterval(() => {
      // A restart briefly kills the server; keep polling until it answers.
      void loadStatus().catch(() => {})
    }, busy ? 1500 : 5000)
    return () => clearInterval(timer)
  }, [busy, loadStatus])

  // The page itself was served by the binary that just got replaced, so once a
  // new version answers, reload to pick up the matching UI bundle.
  const seenVersion = useRef<string>(undefined)
  useEffect(() => {
    const current = status?.currentVersion
    if (!current) return
    if (seenVersion.current && seenVersion.current !== current) {
      window.location.reload()
      return
    }
    seenVersion.current = current
  }, [status?.currentVersion])

  const runCheck = async () => {
    setActing(true)
    try {
      const res = await api.post<UpdateCheck>('/update/check')
      setCheck(res)
      if (res.hasUpdate) toast.success(`发现新版本 ${res.latestVersion}`)
      else toast.success('已是最新版本')
      await loadStatus()
    } catch (err) {
      toast.error(errorMessage(err, '检查更新失败'))
    } finally {
      setActing(false)
    }
  }

  const apply = async () => {
    setActing(true)
    try {
      await api.post('/update/apply')
      toast.success(status?.state === 'ready' ? '正在安装并重启' : '已开始更新')
      await loadStatus()
    } catch (err) {
      toast.error(errorMessage(err, '更新失败'))
    } finally {
      setActing(false)
    }
  }

  const dismiss = async () => {
    setActing(true)
    try {
      setStatus(await api.post<UpdateStatus>('/update/dismiss'))
      setCheck(null)
      toast.success('已忽略该更新')
    } catch (err) {
      toast.error(errorMessage(err, '操作失败'))
    } finally {
      setActing(false)
    }
  }

  if (!info || !status) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="系统" description="版本信息与自动更新" />
        <Skeleton className="h-64 w-full max-w-2xl" />
      </div>
    )
  }

  const showProgress = status.state === 'downloading' || status.state === 'applying'

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="系统" description="版本信息与自动更新">
        <Button variant="outline" onClick={runCheck} disabled={acting || busy}>
          {acting || busy ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
          检查更新
        </Button>
      </PageHeader>

      <Card className="max-w-2xl">
        <CardHeader>
          <CardTitle>当前版本</CardTitle>
          <CardDescription>由发布流水线在构建时写入二进制。</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2">
          <Detail label="版本" value={info.version} mono />
          <Detail label="提交" value={info.commit} mono />
          <Detail label="构建时间" value={info.buildTime} mono />
          <Detail label="发布仓库" value={info.updateRepo} mono />
          <Detail
            label="更新通道"
            value={info.updateChannel === 'dev' ? 'dev（预发布，需手动确认重启）' : 'stable（正式版，自动安装）'}
          />
          <Detail
            label="自动检查"
            value={info.updateEnabled ? `已开启（来源 ${info.updateSource}）` : '已关闭（仍可手动检查）'}
          />
        </CardContent>
      </Card>

      <Card className="max-w-2xl">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            更新
            <Badge variant={badgeVariant(status.state)}>{STATE_LABELS[status.state]}</Badge>
          </CardTitle>
          <CardDescription>
            {status.lastCheck ? `最近检查：${new Date(status.lastCheck).toLocaleString('zh-CN', { hour12: false })}` : '尚未检查过'}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {status.state === 'failed' && status.error && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>更新失败</AlertTitle>
              <AlertDescription className="font-mono text-xs">{status.error}</AlertDescription>
            </Alert>
          )}

          {status.state === 'ready' && (
            <Alert>
              <CheckCircle2Icon />
              <AlertTitle>{status.latestVersion} 已下载并校验</AlertTitle>
              <AlertDescription>
                安装会重启 FireX。在途的面板同步会先跑完，订阅服务中断约一秒。
              </AlertDescription>
            </Alert>
          )}

          {showProgress && (
            <Progress value={status.progress}>
              <ProgressLabel>{status.state === 'downloading' ? '下载中' : '安装中'}</ProgressLabel>
              {status.state === 'downloading' && (
                <ProgressValue>{() => `${Math.round(status.downloadProgress)}%`}</ProgressValue>
              )}
            </Progress>
          )}

          {status.latestVersion && status.state !== 'ready' && (
            <Detail label="最新版本" value={status.latestVersion} mono />
          )}

          {check && !check.hasUpdate && !busy && status.state !== 'ready' && (
            <p className="text-sm text-muted-foreground">已是最新版本，无需更新。</p>
          )}

          {status.releaseNotes ? (
            <div className="flex flex-col gap-1">
              <span className="text-xs tracking-wide text-muted-foreground uppercase">发布说明</span>
              <pre className="max-h-60 overflow-auto rounded-md bg-muted/50 p-3 text-xs whitespace-pre-wrap">
                {status.releaseNotes}
              </pre>
            </div>
          ) : (
            // Direct GitHub checks read version.json rather than the REST API,
            // which is where release notes live.
            <p className="text-xs text-muted-foreground">
              {info.updateSource === 'github' ? '直连模式不获取发布说明。' : '暂无发布说明。'}
            </p>
          )}
        </CardContent>
        <CardFooter className="gap-2">
          <Button onClick={apply} disabled={acting || busy}>
            {status.state === 'ready' ? (
              <RotateCwIcon data-icon="inline-start" />
            ) : (
              <DownloadIcon data-icon="inline-start" />
            )}
            {status.state === 'ready' ? '安装并重启' : '立即更新'}
          </Button>
          {status.state === 'ready' && (
            <Button variant="outline" onClick={dismiss} disabled={acting}>
              <XIcon data-icon="inline-start" />
              忽略
            </Button>
          )}
        </CardFooter>
      </Card>
    </div>
  )
}

function badgeVariant(state: UpdateStatus['state']) {
  if (state === 'failed') return 'destructive' as const
  if (state === 'ready') return 'warning' as const
  if (state === 'idle') return 'outline' as const
  return 'secondary' as const
}

function Detail({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs tracking-wide text-muted-foreground uppercase">{label}</span>
      <span className={mono ? 'font-mono text-sm break-all' : 'text-sm'}>{value || '—'}</span>
    </div>
  )
}

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
import { CodeBlock, CodeText } from '@/components/code-display'
import { FieldsSkeleton, LoadErrorAlert } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { ServerConfigEditor } from '@/components/server-config-editor'
import { StatusBadge, type Tone } from '@/components/status-badge'
import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Progress, ProgressLabel, ProgressValue } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { errorMessage, formatTime } from '@/lib/format'

// A downloaded update waiting for a restart is good news, not a warning.
const STATE_TONES: Record<UpdateStatus['state'], Tone> = {
  idle: 'idle',
  checking: 'idle',
  downloading: 'idle',
  ready: 'good',
  applying: 'idle',
  failed: 'bad',
}

const BUSY_STATES: UpdateStatus['state'][] = ['checking', 'downloading', 'applying']

const STATE_LABELS: Record<UpdateStatus['state'], string> = {
  idle: '空闲',
  checking: '检查中',
  downloading: '下载中',
  ready: '待重启',
  applying: '安装中',
  failed: '失败',
}

type UpdateAction = 'check' | 'apply' | 'dismiss' | null

export function SystemPage() {
  const [info, setInfo] = useState<VersionInfo | null>(null)
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [action, setAction] = useState<UpdateAction>(null)
  const [check, setCheck] = useState<UpdateCheck | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const statusRequest = useRef<Promise<UpdateStatus> | null>(null)
  const statusRequestVersion = useRef(0)

  const busy = status ? BUSY_STATES.includes(status.state) : false

  const commitStatus = useCallback((nextStatus: UpdateStatus) => {
    statusRequestVersion.current += 1
    setStatus(nextStatus)
  }, [])

  const loadStatus = useCallback(async (force = false) => {
    if (!force && statusRequest.current) return statusRequest.current

    const requestVersion = ++statusRequestVersion.current
    const request = api.get<UpdateStatus>('/update/status')
    if (!force) statusRequest.current = request

    try {
      const nextStatus = await request
      if (requestVersion === statusRequestVersion.current) setStatus(nextStatus)
      return nextStatus
    } finally {
      if (!force && statusRequest.current === request) statusRequest.current = null
    }
  }, [])

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const [nextInfo, nextStatus] = await Promise.all([
        api.get<VersionInfo>('/version'),
        api.get<UpdateStatus>('/update/status'),
      ])
      setInfo(nextInfo)
      commitStatus(nextStatus)
    } catch (err) {
      setLoadError(errorMessage(err, '加载系统信息失败'))
    } finally {
      setLoading(false)
    }
  }, [commitStatus])

  useEffect(() => {
    void load()
  }, [load])

  // Poll faster while the updater is working so the progress bar tracks the
  // download instead of jumping.
  useEffect(() => {
    if (!status) return

    const timer = setInterval(() => {
      // A restart briefly kills the server; keep polling until it answers.
      void loadStatus().catch(() => {})
    }, busy ? 1500 : 5000)
    return () => clearInterval(timer)
  }, [busy, loadStatus, status])

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
    setAction('check')
    setCheck(null)
    try {
      const res = await api.post<UpdateCheck>('/update/check')
      setCheck(res)
      if (res.hasUpdate) toast.success(`发现新版本 ${res.latestVersion}`)
      else toast.success('已是最新版本')
      try {
        await loadStatus(true)
      } catch {
        toast.warning('检查已完成，但状态刷新失败；页面会自动重试')
      }
    } catch (err) {
      toast.error(errorMessage(err, '检查更新失败'))
    } finally {
      setAction(null)
    }
  }

  const apply = async () => {
    setAction('apply')
    try {
      await api.post('/update/apply')
      toast.success(status?.state === 'ready' ? '正在安装并重启' : '已开始更新')
      setCheck(null)
      try {
        await loadStatus(true)
      } catch {
        toast.warning('更新已启动，状态暂不可用；页面会自动重试')
      }
    } catch (err) {
      toast.error(errorMessage(err, '更新失败'))
    } finally {
      setAction(null)
    }
  }

  const dismiss = async () => {
    setAction('dismiss')
    try {
      commitStatus(await api.post<UpdateStatus>('/update/dismiss'))
      setCheck(null)
      toast.success('已忽略该更新')
    } catch (err) {
      toast.error(errorMessage(err, '操作失败'))
    } finally {
      setAction(null)
    }
  }

  if (!info || !status) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="系统" description="管理服务端配置、版本与应用更新" />
        {loadError ? (
          <LoadErrorAlert title="无法加载系统信息" message={loadError} retrying={loading} onRetry={() => void load()} />
        ) : (
          <div className="grid gap-4 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
            <Card>
              <CardHeader>
                <CardTitle>当前版本</CardTitle>
                <CardDescription>当前运行的构建信息与已生效的更新配置。</CardDescription>
              </CardHeader>
              <CardContent>
                <FieldsSkeleton fields={3} />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>应用更新</CardTitle>
                <CardDescription>
                  <Skeleton className="h-4 w-40" />
                </CardDescription>
              </CardHeader>
              <CardContent>
                <FieldsSkeleton fields={2} />
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    )
  }

  const showProgress = status.state === 'downloading' || status.state === 'applying'
  const updateAvailable = check?.hasUpdate === true
  const showApply = status.state === 'ready' || (updateAvailable && !busy)
  const checking = action === 'check' || status.state === 'checking'

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="系统" description="管理服务端配置、版本与应用更新">
        <Button
          variant="outline"
          onClick={runCheck}
          disabled={action !== null || busy || status.state === 'ready'}
        >
          {checking ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
          {checking ? '检查中…' : '检查更新'}
        </Button>
      </PageHeader>

      <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        更新状态：{STATE_LABELS[status.state]}
      </span>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
        <Card>
          <CardHeader>
            <CardTitle>当前版本</CardTitle>
            <CardDescription>当前运行的构建信息与已生效的更新配置。</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
              <Detail label="版本" value={info.version} mono />
              <Detail label="提交" value={info.commit} mono />
              <Detail label="构建时间" value={info.buildTime} mono dateTime={info.buildTime} />
              <Detail label="发布仓库" value={info.updateRepo} mono />
              <Detail
                label="更新通道"
                value={info.updateChannel === 'dev' ? 'dev（预发布，需手动确认重启）' : 'stable（正式版，自动安装）'}
              />
              <Detail
                label="自动检查"
                value={info.updateEnabled ? `已开启（来源 ${info.updateSource}）` : '已关闭（仍可手动检查）'}
              />
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>应用更新</CardTitle>
            <CardDescription>
              {status.lastCheck ? (
                <>
                  最近检查：
                  <time dateTime={status.lastCheck}>{formatTime(Date.parse(status.lastCheck))}</time>
                </>
              ) : (
                '尚未检查过更新'
              )}
            </CardDescription>
            <CardAction>
              <StatusBadge tone={STATE_TONES[status.state]}>{STATE_LABELS[status.state]}</StatusBadge>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {status.state === 'idle' && !check && (
              <Alert>
                <CheckCircle2Icon />
                <AlertTitle>更新服务已就绪</AlertTitle>
                <AlertDescription>
                  {info.updateEnabled
                    ? '后台会按配置自动检查；也可以随时手动检查最新版本。'
                    : '自动检查当前未开启；可以使用上方按钮手动检查最新版本。'}
                </AlertDescription>
              </Alert>
            )}
            {status.state === 'failed' && status.error && !check && (
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>更新失败</AlertTitle>
                <AlertDescription>
                  <CodeText className="break-all">{status.error}</CodeText>
                </AlertDescription>
                <AlertAction>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={action !== null || busy}
                    onClick={runCheck}
                  >
                    {checking ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
                    {checking ? '重试中…' : '重试'}
                  </Button>
                </AlertAction>
              </Alert>
            )}

            {status.state === 'ready' && (
              <Alert>
                <CheckCircle2Icon />
                <AlertTitle>{status.latestVersion} 已下载并校验</AlertTitle>
                <AlertDescription>
                  安装会重启 FireX。在途的面板同步会先完成，订阅服务预计中断约一秒。
                </AlertDescription>
              </Alert>
            )}

            {updateAvailable && !busy && status.state !== 'ready' && (
              <Alert>
                <DownloadIcon />
                <AlertTitle>发现新版本 {check.latestVersion}</AlertTitle>
                <AlertDescription>更新已准备好，可在确认后开始下载和安装。</AlertDescription>
              </Alert>
            )}

            {check && !check.hasUpdate && !busy && status.state !== 'ready' && (
              <Alert>
                <CheckCircle2Icon />
                <AlertTitle>已是最新版本</AlertTitle>
                <AlertDescription>当前通道暂时没有需要安装的新版本。</AlertDescription>
              </Alert>
            )}

            {showProgress && (
              <Progress value={status.progress}>
                <ProgressLabel>{status.state === 'downloading' ? '正在下载更新' : '正在安装更新'}</ProgressLabel>
                <ProgressValue>{() => `${Math.round(status.progress)}%`}</ProgressValue>
              </Progress>
            )}

            {status.latestVersion && (updateAvailable || status.state === 'failed') && (
              <dl>
                <Detail label="最新版本" value={status.latestVersion} mono />
              </dl>
            )}

            {status.releaseNotes ? (
              <section className="flex flex-col gap-1" aria-labelledby="release-notes-title">
                <h3 id="release-notes-title" className="text-xs text-muted-foreground">
                  发布说明
                </h3>
                <CodeBlock
                  tabIndex={0}
                  role="region"
                  aria-labelledby="release-notes-title"
                  className="max-h-60"
                >
                  {status.releaseNotes}
                </CodeBlock>
              </section>
            ) : (
              <p className="text-xs text-muted-foreground">
                {info.updateSource === 'github' ? '直连模式不获取发布说明。' : '暂无发布说明。'}
              </p>
            )}
          </CardContent>
          {showApply && (
            <CardFooter className="flex-col items-stretch gap-2 sm:flex-row sm:items-center">
              <Button
                className="w-full sm:w-auto"
                onClick={apply}
                disabled={action !== null || busy}
              >
                {action === 'apply' ? (
                  <Spinner data-icon="inline-start" />
                ) : status.state === 'ready' ? (
                  <RotateCwIcon data-icon="inline-start" />
                ) : (
                  <DownloadIcon data-icon="inline-start" />
                )}
                {action === 'apply'
                  ? status.state === 'ready' ? '安装中…' : '启动中…'
                  : status.state === 'ready' ? '安装并重启' : '立即更新'}
              </Button>
              {status.state === 'ready' && (
                <Button
                  variant="outline"
                  className="w-full sm:w-auto"
                  onClick={dismiss}
                  disabled={action !== null}
                >
                  {action === 'dismiss' ? <Spinner data-icon="inline-start" /> : <XIcon data-icon="inline-start" />}
                  {action === 'dismiss' ? '忽略中…' : '忽略'}
                </Button>
              )}
            </CardFooter>
          )}
        </Card>
      </div>
      <ServerConfigEditor />
    </div>
  )
}


function Detail({
  label,
  value,
  mono,
  dateTime,
}: {
  label: string
  value: string
  mono?: boolean
  dateTime?: string
}) {
  const content = value || '—'

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm">
        {mono && value ? (
          <CodeText className="text-sm break-all text-foreground">
            {dateTime ? <time dateTime={dateTime}>{content}</time> : content}
          </CodeText>
        ) : (
          content
        )}
      </dd>
    </div>
  )
}

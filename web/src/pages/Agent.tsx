import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  BotIcon,
  CheckCircle2Icon,
  CircleDashedIcon,
  CopyIcon,
  GitMergeIcon,
  LoaderCircleIcon,
  RotateCcwIcon,
  SparklesIcon,
  TriangleAlertIcon,
  Undo2Icon,
  XIcon,
} from 'lucide-react'

import { ApiError, type AgentSession, type SnapshotChange, api } from '@/api'
import { CodeBlock, CodeText } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { reportSyncError } from '@/lib/sync'
import { copyText, errorMessage, formatTime } from '@/lib/format'
import { cn } from '@/lib/utils'

const POLL_MS = 3000

const TTL_ITEMS = [
  { value: '30', label: '30 分钟' },
  { value: '120', label: '2 小时' },
  { value: '1440', label: '24 小时' },
]

const SECTION_LABEL: Record<string, string> = {
  panels: '面板',
  inbounds: '入站',
  inboundTemplates: '入站模板',
  nodeGroups: '节点组',
  profiles: '分流方案',
  policies: '分流策略',
  egresses: '出口',
  ruleSets: '规则集',
  clashTemplate: 'Clash 模板',
}

const ACTION_LABEL = { create: '新增', update: '修改', delete: '删除' } as const
const ACTION_TONE = { create: 'good', update: 'warn', delete: 'bad' } as const

// The prompt carries the token, which the server keeps only as a hash. It is
// kept in this browser until the session ends so the waiting page can show it again.
const promptKey = (id: number) => `firex.agent.prompt.${id}`

function ago(ms: number) {
  if (!ms) return '—'
  const seconds = Math.max(0, Math.round((Date.now() - ms) / 1000))
  if (seconds < 60) return `${seconds} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  return formatTime(ms)
}

function remaining(ms: number) {
  const minutes = Math.max(0, Math.ceil((ms - Date.now()) / 60000))
  if (minutes < 60) return `${minutes} 分钟`
  const hours = `${Math.floor(minutes / 60)} 小时`
  return minutes % 60 ? `${hours} ${minutes % 60} 分钟` : hours
}

function renderValue(value: unknown) {
  if (value === undefined || value === null || value === '') return '—'
  if (typeof value === 'string') return value
  return JSON.stringify(value, null, 2)
}

export function AgentPage() {
  const [session, setSession] = useState<AgentSession | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [prompt, setPrompt] = useState<string | null>(null)

  const adopt = useCallback((next: AgentSession | null) => {
    setSession(next)
    setPrompt(next ? localStorage.getItem(promptKey(next.id)) : null)
    if (next && !next.live) localStorage.removeItem(promptKey(next.id))
  }, [])

  const loadCurrent = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const res = await api.get<{ session: AgentSession | null }>('/agent-sessions/current')
      adopt(res.session)
    } catch (err) {
      setLoadError(errorMessage(err, '加载会话失败'))
    } finally {
      setLoading(false)
    }
  }, [adopt])

  useEffect(() => {
    void loadCurrent()
  }, [loadCurrent])

  // Poll while the agent can still act: it may read, submit or resubmit at any
  // moment, and the page has to follow without a reload.
  const submissionsRef = useRef(0)
  useEffect(() => {
    submissionsRef.current = session?.submissions ?? 0
  }, [session?.submissions])
  const liveId = session?.live ? session.id : null
  useEffect(() => {
    if (liveId === null) return
    const timer = window.setInterval(async () => {
      try {
        const res = await api.get<{ session: AgentSession }>(`/agent-sessions/${liveId}`)
        if (res.session.state === 'submitted' && res.session.submissions > submissionsRef.current && submissionsRef.current > 0) {
          toast.info('agent 提交了新版本，差异已更新')
        }
        adopt(res.session)
      } catch {
        // A transient failure is retried on the next tick.
      }
    }, POLL_MS)
    return () => window.clearInterval(timer)
  }, [liveId, adopt])

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Agent 修改"
        description="把配置交给本地 agent 修改：生成一次性提示词，agent 提交后在这里审核差异，合并后令牌立即失效。"
      />
      {loadError && (
        <LoadErrorAlert title="无法加载会话" message={loadError} retrying={loading} onRetry={() => void loadCurrent()} />
      )}
      {loading && !session ? (
        <Card>
          <CardHeader>
            <Skeleton className="h-5 w-40" />
          </CardHeader>
          <CardContent>
            <Skeleton className="h-32 w-full" />
          </CardContent>
        </Card>
      ) : !session ? (
        !loadError && (
          <StartCard
            onStarted={(next, text) => {
              localStorage.setItem(promptKey(next.id), text)
              adopt(next)
            }}
          />
        )
      ) : session.live && session.state === 'submitted' ? (
        <ReviewCard session={session} onChange={adopt} />
      ) : session.live ? (
        <WaitingCard session={session} prompt={prompt} onChange={adopt} />
      ) : (
        <FinishedCard session={session} onRestart={() => adopt(null)} />
      )}
    </div>
  )
}

function StartCard({ onStarted }: { onStarted: (session: AgentSession, prompt: string) => void }) {
  const [task, setTask] = useState('')
  const [ttl, setTtl] = useState('120')
  const [busy, setBusy] = useState(false)

  const start = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    try {
      const res = await api.post<{ session: AgentSession; prompt: string }>('/agent-sessions', {
        task,
        ttlMinutes: Number(ttl),
      })
      onStarted(res.session, res.prompt)
    } catch (err) {
      toast.error(errorMessage(err, '生成失败'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={start} aria-busy={busy}>
      <Card>
        <CardHeader>
          <CardTitle>交给 agent 修改</CardTitle>
          <CardDescription>
            生成一段提示词，里面带有生产端地址和只能用于这一次会话的令牌。agent 只能读取配置和提交修改，什么都不会直接生效。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field data-disabled={busy || undefined}>
              <FieldLabel htmlFor="agent-task">要做什么</FieldLabel>
              <Textarea
                id="agent-task"
                rows={4}
                disabled={busy}
                placeholder="例如：给 VIP 方案的 OpenAI 出口只保留美国节点组"
                value={task}
                onChange={(event) => setTask(event.target.value)}
              />
              <FieldDescription>写进提示词里交给 agent；留空则由你在对话里另行说明。</FieldDescription>
            </Field>
            <Field data-disabled={busy || undefined} className="max-w-xs">
              <FieldLabel htmlFor="agent-ttl">有效期</FieldLabel>
              <Select items={TTL_ITEMS} value={ttl} disabled={busy} onValueChange={(value) => value && setTtl(value)}>
                <SelectTrigger id="agent-ttl" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {TTL_ITEMS.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>到期前没有合并，会话和令牌自动作废。</FieldDescription>
            </Field>
          </FieldGroup>
        </CardContent>
        <CardFooter>
          <Button type="submit" className="w-full sm:w-auto" disabled={busy}>
            {busy ? <Spinner data-icon="inline-start" /> : <SparklesIcon data-icon="inline-start" />}
            {busy ? '生成中…' : '生成提示词'}
          </Button>
        </CardFooter>
      </Card>
    </form>
  )
}

function Step({ done, active, children }: { done: boolean; active: boolean; children: React.ReactNode }) {
  return (
    <li className="flex items-center gap-2 text-sm">
      {done ? (
        <CheckCircle2Icon className="size-4 text-success" />
      ) : active ? (
        <LoaderCircleIcon className="size-4 animate-spin text-primary" />
      ) : (
        <CircleDashedIcon className="size-4 text-muted-foreground" />
      )}
      <span className={done || active ? '' : 'text-muted-foreground'}>{children}</span>
    </li>
  )
}

function useCancel(session: AgentSession, onChange: (next: AgentSession) => void) {
  const [open, setOpen] = useState(false)
  const cancel = async () => {
    try {
      const res = await api.post<{ session: AgentSession }>(`/agent-sessions/${session.id}/cancel`)
      toast.success('会话已取消，令牌已失效')
      onChange(res.session)
    } catch (err) {
      toast.error(errorMessage(err, '取消失败'))
      throw err
    }
  }
  const dialog = (
    <ConfirmDialog
      open={open}
      onOpenChange={setOpen}
      title="取消这次 agent 会话？"
      description="令牌立即失效，agent 的提交会被丢弃，配置不会有任何改动。"
      confirmLabel="取消会话"
      onConfirm={cancel}
    />
  )
  return { openCancel: () => setOpen(true), cancelDialog: dialog }
}

function WaitingCard({
  session,
  prompt,
  onChange,
}: {
  session: AgentSession
  prompt: string | null
  onChange: (next: AgentSession) => void
}) {
  const { openCancel, cancelDialog } = useCancel(session, onChange)
  const returned = session.state === 'returned'

  const copy = async () => {
    if (prompt && (await copyText(prompt))) toast.success('提示词已复制，粘贴给 agent 即可')
    else toast.error('复制失败，请手动选择复制')
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <BotIcon className="size-5" />
            {returned ? '已退回，等待 agent 重新提交' : session.readAt ? 'agent 正在修改' : '等待 agent 连接'}
          </CardTitle>
          <CardDescription>
            还剩 {remaining(session.expiresAt)} 过期 · agent 最近活动 {ago(session.lastSeenAt)}
          </CardDescription>
          <CardAction>
            <Button variant="outline" size="sm" onClick={openCancel}>
              <XIcon data-icon="inline-start" />
              取消会话
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <ol className="flex flex-col gap-2">
            <Step done active={false}>已生成提示词</Step>
            <Step done={session.readAt > 0} active={session.readAt === 0}>
              agent 读取配置
            </Step>
            <Step done={false} active={session.readAt > 0}>
              {returned ? `agent 按意见修改（已提交 ${session.submissions} 次）` : 'agent 修改并提交'}
            </Step>
            <Step done={false} active={false}>人工审核并合并</Step>
          </ol>

          {returned && session.feedback && (
            <Alert>
              <Undo2Icon />
              <AlertTitle>退回意见</AlertTitle>
              <AlertDescription className="whitespace-pre-wrap">{session.feedback}</AlertDescription>
            </Alert>
          )}

          {session.task && (
            <div className="flex flex-col gap-1">
              <span className="text-sm font-medium">任务</span>
              <p className="text-sm whitespace-pre-wrap text-muted-foreground">{session.task}</p>
            </div>
          )}

          {prompt ? (
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium">提示词</span>
                <Button size="sm" onClick={() => void copy()}>
                  <CopyIcon data-icon="inline-start" />
                  复制
                </Button>
              </div>
              <CodeBlock className="max-h-80 select-all">{prompt}</CodeBlock>
            </div>
          ) : (
            <Alert>
              <TriangleAlertIcon />
              <AlertTitle>提示词只在生成它的浏览器里显示</AlertTitle>
              <AlertDescription>
                FireX 不保存令牌原文。如果 agent 还没拿到提示词，取消这次会话再重新生成。
              </AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>
      {cancelDialog}
    </>
  )
}

// Short scalar values read best inline; anything long or structured gets a
// block so JSON keeps its shape.
function isInline(value: string) {
  return value.length <= 60 && !value.includes('\n')
}

function FieldValue({ value, side }: { value: unknown; side: 'before' | 'after' }) {
  const text = renderValue(value)
  const tone = side === 'before' ? 'bg-destructive/10 line-through decoration-destructive/40' : 'bg-success/10'
  if (isInline(text)) {
    return <code className={cn('rounded px-1.5 py-0.5 font-mono text-xs break-all', tone)}>{text}</code>
  }
  return <CodeBlock className={cn('max-h-48', tone)}>{text}</CodeBlock>
}

function ChangeRow({ change }: { change: SnapshotChange }) {
  return (
    <li className="flex flex-col gap-2 rounded-lg border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge tone={ACTION_TONE[change.action]}>{ACTION_LABEL[change.action]}</StatusBadge>
        <span className="text-xs text-muted-foreground">{SECTION_LABEL[change.section] ?? change.section}</span>
        <span className="font-medium break-all">{change.key}</span>
      </div>
      {change.fields && change.fields.length > 0 && (
        <dl className="flex flex-col gap-1.5">
          {change.fields.map((field) => (
            <div key={field.path} className="grid items-start gap-1 sm:grid-cols-[10rem_1fr]">
              <dt className="pt-0.5">
                <CodeText>{field.path}</CodeText>
              </dt>
              <dd className="flex min-w-0 flex-wrap items-center gap-1.5">
                {change.action !== 'create' && <FieldValue value={field.before} side="before" />}
                {change.action === 'update' && <span className="text-xs text-muted-foreground">→</span>}
                {change.action !== 'delete' && <FieldValue value={field.after} side="after" />}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </li>
  )
}

function ReviewCard({ session, onChange }: { session: AgentSession; onChange: (next: AgentSession) => void }) {
  const preview = session.preview
  const removed = preview?.removedPanels ?? []
  const [confirmRemoval, setConfirmRemoval] = useState(false)
  const [mergeOpen, setMergeOpen] = useState(false)
  const [returnOpen, setReturnOpen] = useState(false)
  const [feedback, setFeedback] = useState('')
  const [returning, setReturning] = useState(false)
  const { openCancel, cancelDialog } = useCancel(session, onChange)

  const merge = async () => {
    try {
      const res = await api.post<{ session: AgentSession }>(`/agent-sessions/${session.id}/merge`, {
        submissions: session.submissions,
        allowPanelRemoval: removed.length > 0 && confirmRemoval,
      })
      toast.success('已合并，令牌已失效')
      reportSyncError(res.session.result?.syncError)
      onChange(res.session)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.warning(err.message)
        const res = await api.get<{ session: AgentSession }>(`/agent-sessions/${session.id}`)
        onChange(res.session)
        return
      }
      toast.error(errorMessage(err, '合并失败'))
      throw err
    }
  }

  const sendBack = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!feedback.trim()) return
    setReturning(true)
    try {
      const res = await api.post<{ session: AgentSession }>(`/agent-sessions/${session.id}/return`, { feedback })
      toast.success('已退回给 agent')
      setReturnOpen(false)
      setFeedback('')
      onChange(res.session)
    } catch (err) {
      toast.error(errorMessage(err, '退回失败'))
    } finally {
      setReturning(false)
    }
  }

  const blocked = session.stale || (removed.length > 0 && !confirmRemoval)

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>审核 agent 的修改</CardTitle>
          <CardDescription>
            第 {session.submissions} 次提交 · {ago(session.submittedAt)} · 还剩 {remaining(session.expiresAt)} 过期
          </CardDescription>
          <CardAction>
            <Button variant="outline" size="sm" onClick={openCancel}>
              <XIcon data-icon="inline-start" />
              取消会话
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Alert>
            <BotIcon />
            <AlertTitle>agent 的说明</AlertTitle>
            <AlertDescription className="whitespace-pre-wrap">{session.summary}</AlertDescription>
          </Alert>

          {session.submissions > 1 && session.feedback && (
            <Alert>
              <Undo2Icon />
              <AlertTitle>上次的退回意见</AlertTitle>
              <AlertDescription className="whitespace-pre-wrap">{session.feedback}</AlertDescription>
            </Alert>
          )}

          {session.stale && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>配置在 agent 提交后被修改过</AlertTitle>
              <AlertDescription>这份提交基于旧配置，不能合并。退回给 agent，让它基于最新配置重做。</AlertDescription>
            </Alert>
          )}

          {removed.length > 0 && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>会删除面板：{removed.join('、')}</AlertTitle>
              <AlertDescription>
                <p>这些面板上的全部 FireX 客户端会被删除，用户立即失去这些节点。</p>
                <Field orientation="horizontal" className="mt-2">
                  <Checkbox id="confirm-removal" checked={confirmRemoval} onCheckedChange={(checked) => setConfirmRemoval(checked)} />
                  <FieldLabel htmlFor="confirm-removal">我确认删除这些面板</FieldLabel>
                </Field>
              </AlertDescription>
            </Alert>
          )}

          {preview && (preview.reconcilePlans > 0 || preview.discover.length > 0) && (
            <ul className="flex flex-col gap-1 text-sm text-muted-foreground">
              {preview.reconcilePlans > 0 && (
                <li>合并后会立即把 {preview.reconcilePlans} 个套餐的用户推送到面板（可用入站有变化）。</li>
              )}
              {preview.discover.length > 0 && <li>合并后会发现面板 {preview.discover.join('、')} 的入站。</li>}
            </ul>
          )}

          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium">差异（{preview?.changes.length ?? 0} 项）</span>
            <ul className="flex flex-col gap-2">
              {preview?.changes.map((change) => (
                <ChangeRow key={`${change.section}/${change.key}`} change={change} />
              ))}
            </ul>
          </div>
        </CardContent>
        <CardFooter className="flex-col items-stretch gap-2 sm:flex-row sm:items-center">
          {session.stale ? (
            <Button
              className="w-full sm:w-auto"
              onClick={() => {
                setFeedback('生产配置在你提交之后被修改过。请重新读取快照，基于最新配置重做修改后再提交。')
                setReturnOpen(true)
              }}
            >
              <RotateCcwIcon data-icon="inline-start" />
              退回 agent 重做
            </Button>
          ) : (
            <Button className="w-full sm:w-auto" disabled={blocked} onClick={() => setMergeOpen(true)}>
              <GitMergeIcon data-icon="inline-start" />
              合并
            </Button>
          )}
          {!session.stale && (
            <Button variant="outline" className="w-full sm:w-auto" onClick={() => setReturnOpen(true)}>
              <Undo2Icon data-icon="inline-start" />
              退回修改
            </Button>
          )}
        </CardFooter>
      </Card>

      <ConfirmDialog
        open={mergeOpen}
        onOpenChange={setMergeOpen}
        title="合并这些修改？"
        description="修改会立即在生产端生效，这次会话的令牌随即失效。"
        confirmLabel="合并"
        onConfirm={merge}
      />

      <Dialog open={returnOpen} onOpenChange={(open) => !returning && setReturnOpen(open)}>
        <DialogContent>
          <form className="contents" onSubmit={sendBack}>
            <DialogHeader>
              <DialogTitle>退回给 agent</DialogTitle>
              <DialogDescription>agent 会读到这段意见，按它修改后重新提交，令牌继续有效。</DialogDescription>
            </DialogHeader>
            <Field data-disabled={returning || undefined}>
              <FieldLabel htmlFor="agent-feedback">意见</FieldLabel>
              <Textarea
                id="agent-feedback"
                rows={4}
                disabled={returning}
                value={feedback}
                onChange={(event) => setFeedback(event.target.value)}
              />
            </Field>
            <DialogFooter>
              <Button type="submit" disabled={returning || !feedback.trim()}>
                {returning ? <Spinner data-icon="inline-start" /> : <Undo2Icon data-icon="inline-start" />}
                {returning ? '退回中…' : '退回'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      {cancelDialog}
    </>
  )
}

function FinishedCard({ session, onRestart }: { session: AgentSession; onRestart: () => void }) {
  const result = session.result
  const title =
    session.state === 'merged' ? '已合并' : session.state === 'cancelled' ? '会话已取消' : '会话已过期'

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {session.state === 'merged' ? <CheckCircle2Icon className="size-5 text-success" /> : <XIcon className="size-5" />}
          {title}
        </CardTitle>
        <CardDescription>令牌已失效，agent 不能再访问。</CardDescription>
      </CardHeader>
      {result && (
        <CardContent className="flex flex-col gap-3 text-sm">
          <p>
            应用了 {result.changes.length} 项修改
            {result.reconciledPlans > 0 && `，推送了 ${result.reconciledPlans} 个套餐的用户`}。
          </p>
          {result.syncError && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>下发到面板时出错</AlertTitle>
              <AlertDescription>{result.syncError}</AlertDescription>
            </Alert>
          )}
          {result.remoteCleanupFailures > 0 && (
            <p className="text-destructive">有 {result.remoteCleanupFailures} 个已删除面板上的客户端没能清理，请到面板上手动删除。</p>
          )}
          {result.discover.map((d) => (
            <p key={d.panel} className={d.error ? 'text-destructive' : 'text-muted-foreground'}>
              面板 {d.panel}：{d.error ? `发现入站失败：${d.error}` : '已发现入站'}
            </p>
          ))}
          {result.pendingDeployments.length > 0 && (
            <div className="flex flex-col gap-1">
              <span className="font-medium">以下入站配置待下发，需要到「入站配置」预览后下发：</span>
              <ul className="flex flex-col gap-1">
                {result.pendingDeployments.map((p) => (
                  <li key={p.inboundId}>
                    <a className="underline underline-offset-4" href={`#/inbound-configs?id=${p.inboundId}`}>
                      {p.panel}#{p.remoteId}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </CardContent>
      )}
      <CardFooter>
        <Button className="w-full sm:w-auto" onClick={onRestart}>
          <SparklesIcon data-icon="inline-start" />
          开始新的会话
        </Button>
      </CardFooter>
    </Card>
  )
}

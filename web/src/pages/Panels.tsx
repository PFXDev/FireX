import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  PencilIcon,
  PlugZapIcon,
  PlusIcon,
  RefreshCwIcon,
  ServerOffIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from 'lucide-react'

import { api } from '@/api'
import type { Panel } from '@/api'
import { CodeText } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { PanelStatusBadge, StatusBadge } from '@/components/status-badge'
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatTime } from '@/lib/format'

type Draft = {
  id?: number
  name: string
  baseUrl: string
  apiToken: string
  skipTlsVerify: boolean
  enabled: boolean
  remark: string
}

type LoadState = 'loading' | 'ready' | 'error'
type PendingAction = 'test' | 'save' | 'delete'
type DraftErrors = Partial<Record<'name' | 'baseUrl' | 'apiToken', string>>

const emptyDraft: Draft = {
  name: '',
  baseUrl: '',
  apiToken: '',
  skipTlsVerify: false,
  enabled: true,
  remark: '',
}

function validateDraft(draft: Draft): DraftErrors {
  const errors: DraftErrors = {}
  if (!draft.name.trim()) errors.name = '请输入便于识别的面板名称。'

  const rawUrl = draft.baseUrl.trim()
  if (!rawUrl) {
    errors.baseUrl = '请输入面板地址。'
  } else {
    try {
      const url = new URL(rawUrl)
      if (!['http:', 'https:'].includes(url.protocol)) errors.baseUrl = '面板地址必须使用 http 或 https。'
    } catch {
      errors.baseUrl = '请输入包含协议和主机名的有效地址。'
    }
  }

  if (draft.id === undefined && !draft.apiToken.trim()) {
    errors.apiToken = '新增面板时必须填写 API Token。'
  }
  return errors
}

export function PanelsPage() {
  const [panels, setPanels] = useState<Panel[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [draft, setDraft] = useState<Draft | null>(null)
  const [validationAttempted, setValidationAttempted] = useState(false)
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [discovering, setDiscovering] = useState<Set<number>>(new Set())
  const [pendingDelete, setPendingDelete] = useState<Panel | null>(null)

  const load = useCallback(async () => {
    setLoadState('loading')
    setLoadError('')
    try {
      setPanels(await api.get<Panel[]>('/panels'))
      setLoadState('ready')
    } catch (err) {
      setLoadError(errorMessage(err, '无法加载面板，请稍后重试。'))
      setLoadState('error')
    }
  }, [])

  const refreshAfterMutation = useCallback(async () => {
    try {
      setPanels(await api.get<Panel[]>('/panels'))
      setLoadError('')
      setLoadState('ready')
    } catch (err) {
      toast.error(`操作已完成，但面板列表刷新失败：${errorMessage(err, '未知错误')}`)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const draftErrors = useMemo(() => (draft ? validateDraft(draft) : {}), [draft])
  const hasDraftErrors = Object.keys(draftErrors).length > 0
  const dialogPending = pendingAction === 'test' || pendingAction === 'save'
  const onlineCount = useMemo(() => panels.filter((panel) => panel.status === 'online').length, [panels])
  const enabledNodeCount = useMemo(() => panels.reduce((sum, panel) => sum + panel.enabledInbounds, 0), [panels])

  const openCreate = () => {
    setValidationAttempted(false)
    setDraft({ ...emptyDraft })
  }

  const openEditor = (panel: Panel) => {
    setValidationAttempted(false)
    setDraft({
      id: panel.id,
      name: panel.name,
      baseUrl: panel.baseUrl,
      apiToken: '',
      skipTlsVerify: panel.skipTlsVerify,
      enabled: panel.enabled,
      remark: panel.remark,
    })
  }

  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft) return
    setValidationAttempted(true)
    if (Object.keys(validateDraft(draft)).length > 0) return

    setPendingAction('save')
    try {
      if (draft.id !== undefined) {
        await api.put(`/panels/${draft.id}`, draft)
        toast.success('已保存')
      } else {
        const result = await api.post<{ discoverError: string }>('/panels', draft)
        if (result.discoverError) toast.error(`已添加，但拉取节点失败：${result.discoverError}`)
        else toast.success('已添加并同步入站')
      }
      setDraft(null)
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setPendingAction(null)
    }
  }

  const test = async () => {
    if (!draft) return
    setValidationAttempted(true)
    if (Object.keys(validateDraft(draft)).length > 0) return

    setPendingAction('test')
    try {
      const result = await api.post<{ xrayVersion: string; panelVersion: string; inbounds: number }>('/panels/test', draft)
      toast.success(
        `连接成功：面板 ${result.panelVersion || '?'} / Xray ${result.xrayVersion || '?'}，${result.inbounds} 个入站`,
      )
    } catch (err) {
      toast.error(errorMessage(err, '连接失败'))
    } finally {
      setPendingAction(null)
    }
  }

  const discover = async (panel: Panel) => {
    setDiscovering((current) => new Set(current).add(panel.id))
    try {
      const result = await api.post<{ inbounds: number }>(`/panels/${panel.id}/discover`)
      toast.success(`已同步 ${result.inbounds} 个入站`)
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '同步失败'))
    } finally {
      setDiscovering((current) => {
        const next = new Set(current)
        next.delete(panel.id)
        return next
      })
    }
  }

  const remove = async (panel: Panel) => {
    setPendingAction('delete')
    try {
      const result = await api.del<{ remoteCleanupFailures: number }>(`/panels/${panel.id}`)
      if (result.remoteCleanupFailures > 0) {
        toast.error(`已删除，但有 ${result.remoteCleanupFailures} 个客户端未能从面板移除，需要手动清理`)
      } else {
        toast.success('已删除')
      }
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    } finally {
      setPendingAction(null)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="面板"
        description="每个面板是一台独立的 3x-ui。FireX 用面板的 API Token（admin 作用域）下发配置。"
      >
        <Button type="button" onClick={openCreate}>
          <PlusIcon data-icon="inline-start" />
          添加面板
        </Button>
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>面板列表</CardTitle>
          <CardDescription>
            {loadState === 'ready' ? `共 ${panels.length} 个面板，集中查看连通性与节点同步状态。` : '管理 FireX 连接的 3x-ui 面板。'}
          </CardDescription>
          <CardAction>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={loadState === 'loading' || pendingAction !== null || discovering.size > 0}
              onClick={() => void load()}
            >
              {loadState === 'loading' ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <RefreshCwIcon data-icon="inline-start" />
              )}
              {loadState === 'loading' ? '加载中…' : '刷新'}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loadState === 'loading' ? (
            <PanelsTableSkeleton />
          ) : loadState === 'error' ? (
            <div className="px-4">
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>面板加载失败</AlertTitle>
                <AlertDescription>{loadError}</AlertDescription>
                <AlertAction>
                  <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
                    <RefreshCwIcon data-icon="inline-start" />
                    重试
                  </Button>
                </AlertAction>
              </Alert>
            </div>
          ) : panels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ServerOffIcon />
                </EmptyMedia>
                <EmptyTitle>还没有面板</EmptyTitle>
                <EmptyDescription>添加一台 3x-ui，FireX 会自动拉取它的入站。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button type="button" onClick={openCreate}>
                  <PlusIcon data-icon="inline-start" />
                  添加面板
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead className="hidden lg:table-cell">地址</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className="hidden md:table-cell">入站</TableHead>
                  <TableHead className="hidden xl:table-cell">Xray</TableHead>
                  <TableHead className="hidden lg:table-cell">最近连通</TableHead>
                  <TableHead>
                    <span className="sr-only">操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {panels.map((panel) => {
                  const discoveringPanel = discovering.has(panel.id)
                  return (
                    <TableRow key={panel.id}>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <div className="flex items-center gap-2">
                            <span className="font-medium">{panel.name}</span>
                            {!panel.enabled && <StatusBadge tone="idle">已停用</StatusBadge>}
                          </div>
                          {panel.remark && <span className="text-xs text-muted-foreground">{panel.remark}</span>}
                        </div>
                      </TableCell>
                      <TableCell className="hidden lg:table-cell">
                        <CodeText className="block max-w-72 truncate" title={panel.baseUrl}>
                          {panel.baseUrl}
                        </CodeText>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <PanelStatusBadge status={panel.status} />
                          {panel.lastError && (
                            <CodeText
                              className="max-w-40 truncate xl:max-w-72"
                              title={panel.lastError}
                            >
                              {panel.lastError}
                            </CodeText>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="hidden tabular-nums md:table-cell">
                        {panel.enabledInbounds} / {panel.inboundCount}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground xl:table-cell">{panel.xrayVersion || '—'}</TableCell>
                      <TableCell className="hidden text-muted-foreground lg:table-cell">{formatTime(panel.lastSeenAt)}</TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            aria-label={`拉取 ${panel.name} 的入站`}
                            disabled={discoveringPanel || pendingAction !== null}
                            onClick={() => void discover(panel)}
                          >
                            {discoveringPanel ? (
                              <Spinner data-icon="inline-start" />
                            ) : (
                              <RefreshCwIcon data-icon="inline-start" />
                            )}
                            <span className="hidden 2xl:inline">{discoveringPanel ? '同步中…' : '拉取入站'}</span>
                          </Button>
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            aria-label={`编辑 ${panel.name}`}
                            disabled={discoveringPanel || pendingAction !== null}
                            onClick={() => openEditor(panel)}
                          >
                            <PencilIcon data-icon="inline-start" />
                            <span className="hidden sm:inline">编辑</span>
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            aria-label={`删除 ${panel.name}`}
                            disabled={discoveringPanel || pendingAction !== null}
                            onClick={() => setPendingDelete(panel)}
                          >
                            <Trash2Icon data-icon="inline-start" />
                            <span className="hidden sm:inline">删除</span>
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
        <CardFooter className="flex-wrap justify-between gap-2">
          <span className="text-muted-foreground">
            {loadState === 'ready' ? `${onlineCount} 个面板在线` : '面板状态会在刷新后更新'}
          </span>
          <span className="text-muted-foreground">
            {loadState === 'ready' ? `${enabledNodeCount} 个节点已启用` : '—'}
          </span>
        </CardFooter>
      </Card>

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => {
          if (!open && !dialogPending) setDraft(null)
        }}
      >
        <DialogContent className="sm:max-w-lg">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{draft.id !== undefined ? '编辑面板' : '添加面板'}</DialogTitle>
                <DialogDescription>
                  地址需要包含协议、端口和 basePath，例如 https://1.2.3.4:2053/mypath，不要带 /panel。
                </DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <Field
                  data-invalid={(validationAttempted && !!draftErrors.name) || undefined}
                  data-disabled={dialogPending || undefined}
                >
                  <FieldLabel htmlFor="panel-name">名称</FieldLabel>
                  <Input
                    id="panel-name"
                    required
                    disabled={dialogPending}
                    value={draft.name}
                    aria-invalid={(validationAttempted && !!draftErrors.name) || undefined}
                    onChange={(event) => setDraft({ ...draft, name: event.target.value })}
                  />
                  {validationAttempted && draftErrors.name && <FieldError>{draftErrors.name}</FieldError>}
                </Field>
                <Field
                  data-invalid={(validationAttempted && !!draftErrors.baseUrl) || undefined}
                  data-disabled={dialogPending || undefined}
                >
                  <FieldLabel htmlFor="panel-url">面板地址</FieldLabel>
                  <Input
                    id="panel-url"
                    type="url"
                    required
                    disabled={dialogPending}
                    placeholder="https://panel.example.com:2053"
                    value={draft.baseUrl}
                    aria-invalid={(validationAttempted && !!draftErrors.baseUrl) || undefined}
                    onChange={(event) => setDraft({ ...draft, baseUrl: event.target.value })}
                  />
                  {validationAttempted && draftErrors.baseUrl && <FieldError>{draftErrors.baseUrl}</FieldError>}
                </Field>
                <Field
                  data-invalid={(validationAttempted && !!draftErrors.apiToken) || undefined}
                  data-disabled={dialogPending || undefined}
                >
                  <FieldLabel htmlFor="panel-token">API Token</FieldLabel>
                  <Input
                    id="panel-token"
                    type="password"
                    required={draft.id === undefined}
                    disabled={dialogPending}
                    autoComplete="off"
                    value={draft.apiToken}
                    aria-invalid={(validationAttempted && !!draftErrors.apiToken) || undefined}
                    onChange={(event) => setDraft({ ...draft, apiToken: event.target.value })}
                  />
                  {validationAttempted && draftErrors.apiToken && <FieldError>{draftErrors.apiToken}</FieldError>}
                  <FieldDescription>
                    {draft.id !== undefined
                      ? '留空表示不修改。在 3x-ui 的「设置 → API 令牌」创建 admin 作用域的令牌。'
                      : '在 3x-ui 的「设置 → API 令牌」创建 admin 作用域的令牌。'}
                  </FieldDescription>
                </Field>
                <Field data-disabled={dialogPending || undefined}>
                  <FieldLabel htmlFor="panel-remark">备注</FieldLabel>
                  <Input
                    id="panel-remark"
                    disabled={dialogPending}
                    value={draft.remark}
                    onChange={(event) => setDraft({ ...draft, remark: event.target.value })}
                  />
                </Field>
                <Field orientation="horizontal" data-disabled={dialogPending || undefined}>
                  <FieldLabel htmlFor="panel-skip-tls">跳过 TLS 证书校验</FieldLabel>
                  <Switch
                    id="panel-skip-tls"
                    disabled={dialogPending}
                    checked={draft.skipTlsVerify}
                    onCheckedChange={(value) => setDraft({ ...draft, skipTlsVerify: value })}
                  />
                </Field>
                <Field orientation="horizontal" data-disabled={dialogPending || undefined}>
                  <FieldLabel htmlFor="panel-enabled">启用</FieldLabel>
                  <Switch
                    id="panel-enabled"
                    disabled={dialogPending}
                    checked={draft.enabled}
                    onCheckedChange={(value) => setDraft({ ...draft, enabled: value })}
                  />
                </Field>
              </FieldGroup>
              <DialogFooter>
                <Button type="button" variant="outline" disabled={dialogPending} onClick={() => void test()}>
                  {pendingAction === 'test' ? (
                    <Spinner data-icon="inline-start" />
                  ) : (
                    <PlugZapIcon data-icon="inline-start" />
                  )}
                  {pendingAction === 'test' ? '测试中…' : '测试连接'}
                </Button>
                <Button type="button" variant="outline" disabled={dialogPending} onClick={() => setDraft(null)}>
                  取消
                </Button>
                <Button type="submit" disabled={dialogPending || (validationAttempted && hasDraftErrors)}>
                  {pendingAction === 'save' && <Spinner data-icon="inline-start" />}
                  {pendingAction === 'save' ? '保存中…' : '保存'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除面板「${pendingDelete?.name ?? ''}」？`}
        description="FireX 会尽量先从该面板删除它创建的客户端，然后移除本地的入站、节点组成员关系和下发记录。"
        confirmLabel="删除"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

function PanelsTableSkeleton() {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>名称</TableHead>
          <TableHead className="hidden lg:table-cell">地址</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="hidden md:table-cell">入站</TableHead>
          <TableHead className="hidden xl:table-cell">Xray</TableHead>
          <TableHead className="hidden lg:table-cell">最近连通</TableHead>
          <TableHead><span className="sr-only">操作</span></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {Array.from({ length: 4 }).map((_, index) => (
          <TableRow key={index}>
            <TableCell><Skeleton className="h-4 w-28" /></TableCell>
            <TableCell className="hidden lg:table-cell"><Skeleton className="h-4 w-48" /></TableCell>
            <TableCell><Skeleton className="h-5 w-14" /></TableCell>
            <TableCell className="hidden md:table-cell"><Skeleton className="h-4 w-12" /></TableCell>
            <TableCell className="hidden xl:table-cell"><Skeleton className="h-4 w-20" /></TableCell>
            <TableCell className="hidden lg:table-cell"><Skeleton className="h-4 w-32" /></TableCell>
            <TableCell><Skeleton className="ml-auto h-7 w-24" /></TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

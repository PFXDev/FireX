import { type FormEvent, useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { CloudUploadIcon, FileCogIcon, HistoryIcon, LayersIcon, LayoutTemplateIcon, MousePointerClickIcon, RefreshCwIcon, SaveIcon, ServerIcon, ShieldCheckIcon, TriangleAlertIcon, UnlinkIcon } from 'lucide-react'

import { api, type Inbound, type InboundDeployment } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { ChangesTable, ConfigEditor, type ConfigChange, type Parameters } from '@/components/inbound-config-editor'
import { FieldsSkeleton, ListSkeleton, LoadErrorAlert, RefreshButton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { ConfigStateBadge, StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { confirmUnsavedNavigation, useUnsavedGuard } from '@/hooks/use-unsaved-guard'
import { errorMessage, formatTime } from '@/lib/format'
import { hashID, inboundLabel as label, inboundTitle } from '@/lib/inbound'
import { reportSyncError } from '@/lib/sync'

type Detail = {
  config: { revision: number; appliedRevision: number } | null
  parameters: Parameters | null
  state: Inbound['configState']
  deployments: InboundDeployment[]
}

type Preview = {
  mode: InboundDeployment['mode']
  panelName: string
  baseUrl: string
  remoteId: number
  changes: ConfigChange[]
  warnings: string[]
  targetRevision: string
}

const modeLabels: Record<InboundDeployment['mode'], string> = { update: '更新', create: '新建', rebind: '重新绑定' }

export function InboundConfigsPage() {
  const [inbounds, setInbounds] = useState<Inbound[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [selectedID, setSelectedID] = useState<number | null>(hashID)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [draft, setDraft] = useState<Parameters>({})
  const [invalid, setInvalid] = useState(false)
  const [reveal, setReveal] = useState(false)
  const [busy, setBusy] = useState(false)
  const [releasing, setReleasing] = useState(false)
  const [templateName, setTemplateName] = useState<string | null>(null)
  const [deploying, setDeploying] = useState(false)
  const [rebindID, setRebindID] = useState(0)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [deployError, setDeployError] = useState('')
  const [detailError, setDetailError] = useState('')
  const [pending, setPending] = useState<'save' | 'template' | 'preview' | 'deploy' | 'takeover' | null>(null)

  const selected = inbounds.find((inbound) => inbound.id === selectedID) ?? null
  const dirty = !!detail?.parameters && (invalid || JSON.stringify(draft) !== JSON.stringify(detail.parameters))
  useUnsavedGuard(dirty)

  const loadList = useCallback(async () => {
    setLoading(true)
    try {
      setInbounds(await api.get<Inbound[]>('/inbounds'))
      setLoadError('')
    } catch (err) {
      setLoadError(errorMessage(err, '无法加载入站'))
    } finally {
      setLoading(false)
    }
  }, [])

  // Re-fetches the list after a mutation without swapping it for a skeleton.
  const refreshList = useCallback(async () => {
    try {
      setInbounds(await api.get<Inbound[]>('/inbounds'))
      setLoadError('')
    } catch (err) {
      toast.error(`操作已完成，但入站列表刷新失败：${errorMessage(err, '未知错误')}`)
    }
  }, [])

  const adopt = (next: Detail) => {
    setDetail(next)
    setDraft(next.parameters ?? {})
    setInvalid(false)
  }

  const loadDetail = useCallback(async (id: number) => {
    setBusy(true)
    setDetailError('')
    try {
      adopt(await api.get<Detail>(`/inbounds/${id}/config`))
    } catch (err) {
      setDetailError(errorMessage(err, '无法读取入站配置'))
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void loadList()
  }, [loadList])

  useEffect(() => {
    setDetail(null)
    setReveal(false)
    if (selectedID) void loadDetail(selectedID)
  }, [selectedID, loadDetail])

  useEffect(() => {
    const onHash = () => setSelectedID(hashID())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  const select = (id: number) => {
    if (id === selectedID || !confirmUnsavedNavigation()) return
    window.history.replaceState(window.history.state, '', `#/inbound-configs?id=${id}`)
    setSelectedID(id)
  }

  // Every mutation returns the fresh detail; the list carries the state badges.
  const mutate = async (request: () => Promise<Detail>, success: string, action: typeof pending = null) => {
    setBusy(true)
    setPending(action)
    try {
      adopt(await request())
      toast.success(success)
      await refreshList()
    } catch (err) {
      toast.error(errorMessage(err, '操作失败'))
      throw err
    } finally {
      setBusy(false)
      setPending(null)
    }
  }

  const id = selectedID ?? 0
  const takeOver = (deployment?: InboundDeployment) =>
    mutate(() => api.post<Detail>(`/inbounds/${id}/config`, deployment ? { deploymentId: deployment.id } : {}), '已接管入站配置', 'takeover').catch(() => undefined)
  const save = () =>
    mutate(() => api.put<Detail>(`/inbounds/${id}/config`, { revision: detail?.config?.revision, parameters: draft }), '配置已保存，下发后生效', 'save').catch(() => undefined)
  const restore = (deployment: InboundDeployment) => {
    if (!confirmUnsavedNavigation()) return
    void mutate(() => api.put<Detail>(`/inbounds/${id}/config`, { revision: detail?.config?.revision, deploymentId: deployment.id }), '已载入下发前的备份，预览后下发即可恢复').catch(() => undefined)
  }
  // Rethrows so ConfirmDialog stays open on failure.
  const release = () => mutate(() => api.del<Detail>(`/inbounds/${id}/config`), '已释放接管，面板配置未改动')

  const saveTemplate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!templateName?.trim()) return
    setBusy(true)
    setPending('template')
    try {
      const result = await api.post<{ template: { id: number } }>('/inbound-templates', { name: templateName, inboundId: id })
      toast.success('已另存为模板，节点相关字段已去除')
      setTemplateName(null)
      window.location.hash = `#/inbound-templates?id=${result.template.id}`
    } catch (err) {
      toast.error(errorMessage(err, '保存模板失败'))
    } finally {
      setBusy(false)
      setPending(null)
    }
  }

  const runPreview = async (rebind: number) => {
    setBusy(true)
    setPending('preview')
    setPreview(null)
    setDeployError('')
    try {
      setPreview(await api.post<Preview>(`/inbounds/${id}/config/preview`, { revision: detail?.config?.revision, rebindInboundId: rebind }))
    } catch (err) {
      setDeployError(errorMessage(err, '预览失败'))
    } finally {
      setBusy(false)
      setPending(null)
    }
  }

  const openDeploy = () => {
    setRebindID(0)
    setDeploying(true)
    void runPreview(0)
  }

  const deploy = async () => {
    if (!preview) return
    setBusy(true)
    setPending('deploy')
    setDeployError('')
    try {
      const result = await api.post<{ deployment: InboundDeployment; syncError: string }>(`/inbounds/${id}/config/deploy`, {
        revision: detail?.config?.revision, rebindInboundId: rebindID, targetRevision: preview.targetRevision,
      })
      setPreview(null)
      if (result.deployment.status === 'verified') {
        toast.success('已下发并读回验证')
        reportSyncError(result.syncError)
        setDeploying(false)
      } else {
        setDeployError(`下发结果待核对：${result.deployment.error}`)
      }
      adopt(await api.get<Detail>(`/inbounds/${id}/config`))
      await refreshList()
    } catch (err) {
      setPreview(null)
      setDeployError(errorMessage(err, '下发失败，请重新预览'))
    } finally {
      setBusy(false)
      setPending(null)
    }
  }

  // A renumbered copy shows up as a separate, unreviewed inbound on the same panel.
  const rebindCandidates = selected
    ? inbounds
        .filter((row) => row.panelId === selected.panelId && row.id !== selected.id && !row.missing && !row.configState && row.groupCount === 0 && row.protocol === selected.protocol)
        .sort((a, b) => Number(b.port === selected.port) - Number(a.port === selected.port))
    : []
  const candidateLabel = (row: Inbound) => `绑定到面板入站 #${row.remoteId} · ${label(row)} · 端口 ${row.port}${row.port === selected?.port ? '（与原入站相同）' : ''}`

  // The panel copy may vanish before discovery marks it missing; a port
  // conflict on create is then the hint that it was renumbered.
  const chooseTarget = !!selected?.missing || rebindID !== 0 || preview?.mode === 'create' || deployError.includes('重新绑定')

  const panels = [...new Set(inbounds.map((row) => row.panelName))]
  const managed = !!detail?.config

  const firstLoad = loading && inbounds.length === 0
  const refresh = () => void Promise.all([loadList(), selectedID && loadDetail(selectedID)])

  const listCard = (
    <Card className="min-w-0 xl:col-span-1">
      <CardHeader>
        <CardTitle>入站列表</CardTitle>
        <CardDescription>{firstLoad ? '读取所有面板的入站。' : `共 ${inbounds.length} 个入站。`}</CardDescription>
        <CardAction>
          <RefreshButton loading={loading} disabled={busy} onClick={refresh} />
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {loadError && <LoadErrorAlert title="入站加载失败" message={loadError} retrying={loading} onRetry={() => void loadList()} />}
        {firstLoad ? (
          <ListSkeleton />
        ) : inbounds.length === 0 ? (
          !loadError && (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <LayersIcon />
                </EmptyMedia>
                <EmptyTitle>还没有入站</EmptyTitle>
                <EmptyDescription>先添加面板，FireX 会自动发现它的入站。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button render={<a href="#/panels" />} nativeButton={false}>
                  <ServerIcon data-icon="inline-start" />
                  前往面板
                </Button>
              </EmptyContent>
            </Empty>
          )
        ) : (
          <nav className="flex min-w-0 flex-col gap-4" aria-label="入站">
            {panels.map((panelName) => (
              <div key={panelName} className="flex min-w-0 flex-col gap-1">
                <span className="px-2 text-xs text-muted-foreground">{panelName}</span>
                {inbounds.filter((row) => row.panelName === panelName).map((row) => (
                  <Button
                    key={row.id}
                    type="button"
                    variant={row.id === selectedID ? 'secondary' : 'ghost'}
                    className="h-auto w-full justify-between gap-2 py-2"
                    aria-current={row.id === selectedID ? 'page' : undefined}
                    disabled={busy}
                    onClick={() => select(row.id)}
                  >
                    <span className="min-w-0 truncate">{inboundTitle(row)} · #{row.remoteId}</span>
                    {(row.configState || row.missing) && <ConfigStateBadge state={row.configState} />}
                  </Button>
                ))}
              </div>
            ))}
          </nav>
        )}
      </CardContent>
    </Card>
  )

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="入站配置"
        description="FireX 为每个接管的入站保存一份完整配置。面板被改动、入站被删除或重新编号时，可预览差异后完整下发恢复。"
      />

      {inbounds.length === 0 ? (
        listCard
      ) : (
        <div className="grid min-w-0 items-start gap-6 xl:grid-cols-4">
          {listCard}
          <div className="flex min-w-0 flex-col gap-6 xl:col-span-3">
            {!selected ? (
              <Card>
                <CardContent>
                  <Empty>
                    <EmptyHeader>
                      <EmptyMedia variant="icon">
                        <MousePointerClickIcon />
                      </EmptyMedia>
                      <EmptyTitle>选择一个入站</EmptyTitle>
                      <EmptyDescription>接管后，自动发现只会对比差异，不会改动 FireX 保存的配置。</EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                </CardContent>
              </Card>
            ) : detailError && !detail ? (
              <Card>
                <CardContent>
                  <LoadErrorAlert title="入站配置读取失败" message={detailError} retrying={busy} onRetry={() => void loadDetail(selected.id)} />
                </CardContent>
              </Card>
            ) : !detail ? (
              <Card>
                <CardContent>
                  <FieldsSkeleton />
                </CardContent>
              </Card>
            ) : (
              <>
                <Card>
                  <CardHeader>
                    <CardTitle className="truncate">{inboundTitle(selected)}</CardTitle>
                    <CardDescription className="flex flex-wrap items-center gap-2">
                      <span>{selected.panelName} · 面板入站 #{selected.remoteId}</span>
                      {managed && <ConfigStateBadge state={detail.state} />}
                      {managed && <Badge variant="outline">版本 {detail.config?.revision}</Badge>}
                      {dirty && <Badge variant="secondary">未保存</Badge>}
                    </CardDescription>
                    {managed && (
                      <CardAction className="flex flex-wrap justify-end gap-2">
                        <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => setReleasing(true)}>
                          <UnlinkIcon data-icon="inline-start" />
                          释放接管
                        </Button>
                        <Button type="button" variant="outline" size="sm" disabled={busy || dirty} onClick={() => setTemplateName(`${label(selected)} 模板`)}>
                          <LayoutTemplateIcon data-icon="inline-start" />
                          另存为模板
                        </Button>
                        <Button type="button" variant="outline" size="sm" disabled={busy || !dirty || invalid} onClick={() => void save()}>
                          {pending === 'save' ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
                          {pending === 'save' ? '保存中…' : '保存'}
                        </Button>
                        <Button type="button" size="sm" disabled={busy || dirty} title={dirty ? '请先保存配置' : undefined} onClick={openDeploy}>
                          <CloudUploadIcon data-icon="inline-start" />
                          预览并下发
                        </Button>
                      </CardAction>
                    )}
                  </CardHeader>
                  <CardContent className="flex flex-col gap-6">
                    {detailError && <LoadErrorAlert title="入站配置刷新失败" message={detailError} retrying={busy} onRetry={() => void loadDetail(selected.id)} />}
                    {detail.state === 'drifted' && (
                      <Alert variant="destructive">
                        <TriangleAlertIcon />
                        <AlertTitle>面板配置在 FireX 之外被修改</AlertTitle>
                        <AlertDescription>预览并下发可恢复为接管配置。若面板上的改动是有意的，可先释放接管再重新接管当前配置。</AlertDescription>
                      </Alert>
                    )}
                    {detail.state === 'missing' && (
                      <Alert variant="destructive">
                        <TriangleAlertIcon />
                        <AlertTitle>面板上找不到此入站</AlertTitle>
                        <AlertDescription>下发会按接管配置重新创建，或绑定到面板上重新编号的同一入站。FireX 编号、分组与用户保持不变。</AlertDescription>
                      </Alert>
                    )}
                    {managed ? (
                      <FieldGroup>
                        <Field orientation="horizontal">
                          <FieldLabel htmlFor="reveal-config">显示密钥</FieldLabel>
                          <Switch id="reveal-config" checked={reveal} onCheckedChange={setReveal} />
                        </Field>
                        <ConfigEditor key={selected.id} parameters={draft} onChange={setDraft} onInvalidChange={setInvalid} disabled={busy} reveal={reveal} />
                      </FieldGroup>
                    ) : (
                      <Empty className="border">
                        <EmptyHeader>
                          <EmptyMedia variant="icon">
                            <FileCogIcon />
                          </EmptyMedia>
                          <EmptyTitle>FireX 尚未接管此入站</EmptyTitle>
                          <EmptyDescription>
                            {selected.missing
                              ? '面板上已找不到此入站。可以从下面的下发记录备份接管。'
                              : `接管会把最近同步的面板配置（${formatTime(selected.lastSeenAt)}）保存为 FireX 的配置。请先确认面板当前配置正确。`}
                          </EmptyDescription>
                        </EmptyHeader>
                        {!selected.missing && (
                          <EmptyContent>
                            <Button type="button" disabled={busy} onClick={() => void takeOver()}>
                              {pending === 'takeover' ? <Spinner data-icon="inline-start" /> : <ShieldCheckIcon data-icon="inline-start" />}
                              {pending === 'takeover' ? '接管中…' : '接管当前配置'}
                            </Button>
                          </EmptyContent>
                        )}
                      </Empty>
                    )}
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader>
                    <CardTitle>下发记录</CardTitle>
                    <CardDescription>每次写入面板前都会保存当时的面板配置，可载入后重新下发恢复。</CardDescription>
                  </CardHeader>
                  <CardContent className="px-0">
                    {detail.deployments.length === 0 ? (
                      <Empty>
                        <EmptyHeader>
                          <EmptyMedia variant="icon">
                            <HistoryIcon />
                          </EmptyMedia>
                          <EmptyTitle>尚无下发记录</EmptyTitle>
                          <EmptyDescription>首次下发后，这里会保留写入前的面板配置备份。</EmptyDescription>
                        </EmptyHeader>
                      </Empty>
                    ) : (
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>时间</TableHead>
                            <TableHead>操作</TableHead>
                            <TableHead>结果</TableHead>
                            <TableHead>
                              <span className="sr-only">备份</span>
                            </TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {detail.deployments.map((row) => (
                            <TableRow key={row.id}>
                              <TableCell className="text-muted-foreground tabular-nums">{formatTime(row.createdAt)}</TableCell>
                              <TableCell>{modeLabels[row.mode] ?? row.mode} · 面板入站 #{row.remoteId || '—'}</TableCell>
                              <TableCell className="whitespace-normal">
                                <div className="flex flex-col items-start gap-1">
                                  <StatusBadge tone={row.status === 'verified' ? 'good' : 'warn'}>{row.status === 'verified' ? '已验证' : '待核对'}</StatusBadge>
                                  {row.error && <span className="max-w-sm text-xs text-destructive [overflow-wrap:anywhere]">{row.error}</span>}
                                </div>
                              </TableCell>
                              <TableCell>
                                <div className="flex justify-end">
                                  {row.hasBackup && (
                                    <Button type="button" variant="outline" size="sm" disabled={busy || dirty} onClick={() => (managed ? restore(row) : void takeOver(row))}>
                                      <HistoryIcon data-icon="inline-start" />
                                      {managed ? '载入下发前配置' : '从此备份接管'}
                                    </Button>
                                  )}
                                </div>
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    )}
                  </CardContent>
                </Card>
              </>
            )}
          </div>
        </div>
      )}

      <Dialog open={deploying} onOpenChange={(open) => !busy && setDeploying(open)}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>下发入站配置</DialogTitle>
            <DialogDescription>
              {selected && `${label(selected)} · ${selected.panelName}`}。只写入一次，随后读回验证；目标的客户端与流量计数保持不变。
            </DialogDescription>
          </DialogHeader>
          {chooseTarget && (
            <Field data-disabled={busy || undefined}>
              <FieldLabel htmlFor="deploy-target">面板目标</FieldLabel>
              <Select
                items={[{ value: 0, label: '在面板上重新创建' }, ...rebindCandidates.map((row) => ({ value: row.id, label: candidateLabel(row) }))]}
                value={rebindID}
                disabled={busy}
                onValueChange={(value) => {
                  setRebindID(value ?? 0)
                  void runPreview(value ?? 0)
                }}
              >
                <SelectTrigger id="deploy-target" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value={0}>在面板上重新创建</SelectItem>
                    {rebindCandidates.map((row) => (
                      <SelectItem key={row.id} value={row.id}>{candidateLabel(row)}</SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>面板数据库被覆盖后，同一入站可能换了编号并被发现为新入站。绑定会合并那条记录，并写回接管配置。</FieldDescription>
            </Field>
          )}
          {deployError && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>需要核对</AlertTitle>
              <AlertDescription>{deployError}</AlertDescription>
            </Alert>
          )}
          {pending === 'preview' && <FieldsSkeleton fields={3} />}
          {preview && (
            <div className="flex min-w-0 flex-col gap-4">
              <Alert>
                <CloudUploadIcon />
                <AlertTitle>
                  {preview.panelName} · {modeLabels[preview.mode]}{preview.remoteId ? `面板入站 #${preview.remoteId}` : '面板入站'}
                </AlertTitle>
                <AlertDescription className="break-all">{preview.baseUrl}</AlertDescription>
              </Alert>
              {preview.warnings.map((warning) => (
                <Alert key={warning} variant="warning">
                  <TriangleAlertIcon />
                  <AlertTitle>REALITY 回落限速</AlertTitle>
                  <AlertDescription>{warning}</AlertDescription>
                </Alert>
              ))}
              <ChangesTable changes={preview.changes} reveal={reveal} />
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={() => setDeploying(false)}>取消</Button>
            <Button type="button" variant="outline" disabled={busy} onClick={() => void runPreview(rebindID)}>
              {pending === 'preview' ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
              {pending === 'preview' ? '预览中…' : '重新预览'}
            </Button>
            <Button type="button" disabled={busy || !preview} onClick={() => void deploy()}>
              {pending === 'deploy' ? <Spinner data-icon="inline-start" /> : <CloudUploadIcon data-icon="inline-start" />}
              {pending === 'deploy' ? '下发中…' : '确认下发'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={templateName !== null} onOpenChange={(open) => !busy && !open && setTemplateName(null)}>
        <DialogContent>
          <form className="contents" noValidate onSubmit={saveTemplate}>
            <DialogHeader>
              <DialogTitle>另存为模板</DialogTitle>
              <DialogDescription>模板保留协议、加密、密钥、REALITY 目标与回落限速，去除监听地址、端口、PROXY 与回落路由等节点字段。</DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field data-invalid={!templateName?.trim() || undefined} data-disabled={busy || undefined}>
                <FieldLabel htmlFor="template-name">模板名称</FieldLabel>
                <Input id="template-name" value={templateName ?? ''} disabled={busy} aria-invalid={!templateName?.trim() || undefined} onChange={(event) => setTemplateName(event.target.value)} />
                {!templateName?.trim() && <FieldError>请输入模板名称。</FieldError>}
              </Field>
            </FieldGroup>
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={() => setTemplateName(null)}>取消</Button>
              <Button type="submit" disabled={busy || !templateName?.trim()}>
                {pending === 'template' ? <Spinner data-icon="inline-start" /> : <LayoutTemplateIcon data-icon="inline-start" />}
                {pending === 'template' ? '保存中…' : '保存模板'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={releasing}
        onOpenChange={setReleasing}
        title="释放接管？"
        description="FireX 将不再保存此入站的配置，也不再报告面板偏离。面板本身不会改动，下发记录会保留。"
        confirmLabel="释放接管"
        onConfirm={release}
      />
    </div>
  )
}

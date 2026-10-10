import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { BoxesIcon, FileCogIcon, LayersIcon, ListTreeIcon, PencilIcon, ServerIcon, Trash2Icon } from 'lucide-react'

import { api } from '@/api'
import type { Inbound } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { InboundDetails } from '@/components/inbound-details'
import { PageHeader } from '@/components/page-header'
import { ConfigStateBadge, StatusBadge } from '@/components/status-badge'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { Alert, AlertAction, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
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
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel, FieldTitle } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage } from '@/lib/format'
import { inboundLabel, inboundTitle } from '@/lib/inbound'
import { reportSyncError } from '@/lib/sync'

type Draft = {
  id: number
  name: string
  emoji: string
  sortOrder: number
  enabled: boolean
  udp: boolean
  vision: boolean | null
  publicAddress: string
  publicPort: number
}

type LoadState = 'loading' | 'ready' | 'error'
type PendingAction = 'bulk-enable' | 'bulk-disable' | 'save' | 'remove'

const visionLabels = { preserve: '保留现有流控', on: '开启 Vision', off: '关闭 Vision' }

export function InboundsPage() {
  const [inbounds, setInbounds] = useState<Inbound[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [draft, setDraft] = useState<Draft | null>(null)
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [pendingDelete, setPendingDelete] = useState<Inbound | null>(null)
  const [detailID, setDetailID] = useState<number | null>(null)
  const detailInbound = inbounds.find((inbound) => inbound.id === detailID) ?? null
  const editingInbound = inbounds.find((inbound) => inbound.id === draft?.id)

  const edit = (inbound: Inbound) => {
    setDetailID(null)
    setDraft({
      id: inbound.id, name: inbound.name, emoji: inbound.emoji,
      sortOrder: inbound.sortOrder, enabled: inbound.enabled, udp: inbound.udp,
      vision: inbound.vision, publicAddress: inbound.publicAddress, publicPort: inbound.publicPort,
    })
  }

  const load = useCallback(async () => {
    setLoadState('loading')
    setLoadError('')
    setSelected(new Set())
    try {
      setInbounds(await api.get<Inbound[]>('/inbounds'))
      setLoadState('ready')
    } catch (err) {
      setLoadError(errorMessage(err, '无法加载入站，请稍后重试。'))
      setLoadState('error')
    }
  }, [])

  const refreshAfterMutation = useCallback(async () => {
    try {
      setInbounds(await api.get<Inbound[]>('/inbounds'))
      setSelected(new Set())
      setLoadError('')
      setLoadState('ready')
    } catch (err) {
      toast.error(`操作已完成，但列表刷新失败：${errorMessage(err, '未知错误')}`)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const enabledCount = useMemo(() => inbounds.filter((n) => n.enabled && !n.missing).length, [inbounds])
  const missingCount = useMemo(() => inbounds.filter((n) => n.missing).length, [inbounds])
  const orphanCount = useMemo(
    () => inbounds.filter((n) => n.enabled && !n.missing && n.groupCount === 0).length,
    [inbounds],
  )

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const bulk = async (action: 'bulk-enable' | 'bulk-disable', enabled: boolean) => {
    if (selected.size === 0) return
    const count = selected.size
    setPendingAction(action)
    try {
      const result = await api.post<{ syncError: string }>('/inbounds/bulk', { ids: [...selected], enabled })
      toast.success(`已更新 ${count} 个入站`)
      reportSyncError(result.syncError)
      setSelected(new Set())
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '操作失败'))
    } finally {
      setPendingAction(null)
    }
  }

  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || pendingAction) return
    if (!event.currentTarget.reportValidity()) return
    setPendingAction('save')
    try {
      const result = await api.put<{ syncError: string }>(`/inbounds/${draft.id}`, draft)
      toast.success('已保存')
      reportSyncError(result.syncError)
      setDraft(null)
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setPendingAction(null)
    }
  }

  const removeMissing = async (inbound: Inbound) => {
    setPendingAction('remove')
    try {
      const result = await api.del<{ syncError: string }>(`/inbounds/${inbound.id}`)
      toast.success('已移除')
      reportSyncError(result.syncError)
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '移除失败'))
      throw err
    } finally {
      setPendingAction(null)
    }
  }

  const allSelected = inbounds.length > 0 && selected.size === inbounds.length
  const isBulkPending = pendingAction?.startsWith('bulk-') ?? false

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="入站"
        description="点击名称查看完整参数，编辑入站设置 Vision 流控。新入站默认停用，启用并加入节点组后即可下发给用户。"
      />

      {orphanCount > 0 && (
        <Alert>
          <BoxesIcon />
          <AlertTitle>{orphanCount} 个已启用的入站不属于任何节点组</AlertTitle>
          <AlertDescription>
            用户能用到哪些入站只由分流方案里的节点组决定，不在任何分组里的入站不会出现在任何订阅中。
          </AlertDescription>
          <AlertAction>
            <Button variant="outline" size="sm" render={<a href="#/node-groups" />} nativeButton={false}>
              <BoxesIcon data-icon="inline-start" />
              去分组
            </Button>
          </AlertAction>
        </Alert>
      )}

      {selected.size > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>批量管理入站</CardTitle>
            <CardDescription>只会修改当前选中的入站，操作完成后会自动刷新列表。</CardDescription>
            <CardAction>
              <Badge variant="secondary">已选 {selected.size} 个</Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <Field>
              <FieldTitle>启用状态</FieldTitle>
              <span className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pendingAction !== null}
                  onClick={() => void bulk('bulk-enable', true)}
                >
                  {pendingAction === 'bulk-enable' && <Spinner data-icon="inline-start" />}
                  {pendingAction === 'bulk-enable' ? '启用中…' : '启用'}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={pendingAction !== null}
                  onClick={() => void bulk('bulk-disable', false)}
                >
                  {pendingAction === 'bulk-disable' && <Spinner data-icon="inline-start" />}
                  {pendingAction === 'bulk-disable' ? '停用中…' : '停用'}
                </Button>
              </span>
              <FieldDescription>停用会把入站从所有订阅中移除，并同步到面板。</FieldDescription>
            </Field>
          </CardContent>
          <CardFooter className="justify-end">
            <Button type="button" variant="ghost" size="sm" disabled={isBulkPending} onClick={() => setSelected(new Set())}>
              取消选择
            </Button>
          </CardFooter>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>入站列表</CardTitle>
          <CardDescription>
            {loadState === 'ready' ? `共 ${inbounds.length} 个入站。` : '查看所有面板发现的入站。'}
          </CardDescription>
          <CardAction>
            <RefreshButton loading={loadState === 'loading'} disabled={pendingAction !== null} onClick={() => void load()} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loadState === 'loading' ? (
            <TableSkeleton
              columns={[
                { label: '', className: 'w-10', width: 'w-4' },
                { label: '名称', width: 'w-32' },
                { label: '面板 / 入站', className: 'hidden lg:table-cell', width: 'w-28' },
                { label: '协议', className: 'hidden md:table-cell', width: 'w-16' },
                { label: '端口', className: 'hidden xl:table-cell', width: 'w-12' },
                { label: '所属分组', className: 'hidden lg:table-cell', width: 'w-8' },
                { label: '状态', width: 'w-14' },
                { width: 'w-16' },
              ]}
            />
          ) : loadState === 'error' ? (
            <div className="px-4">
              <LoadErrorAlert title="入站加载失败" message={loadError} onRetry={() => void load()} />
            </div>
          ) : inbounds.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <LayersIcon />
                </EmptyMedia>
                <EmptyTitle>还没有入站</EmptyTitle>
                <EmptyDescription>先添加面板，FireX 会自动拉取它的入站。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button render={<a href="#/panels" />} nativeButton={false}>
                  <ServerIcon data-icon="inline-start" />
                  前往面板
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10">
                    <Checkbox
                      checked={allSelected}
                      indeterminate={selected.size > 0 && !allSelected}
                      disabled={isBulkPending}
                      aria-label="全选入站"
                      onCheckedChange={(checked) =>
                        setSelected(checked ? new Set(inbounds.map((n) => n.id)) : new Set())
                      }
                    />
                  </TableHead>
                  <TableHead>名称</TableHead>
                  <TableHead className="hidden lg:table-cell">面板 / 入站</TableHead>
                  <TableHead className="hidden md:table-cell">协议</TableHead>
                  <TableHead className="hidden xl:table-cell">端口</TableHead>
                  <TableHead className="hidden lg:table-cell">所属分组</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>
                    <span className="sr-only">操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {inbounds.map((inbound) => (
                  <TableRow key={inbound.id} data-state={selected.has(inbound.id) ? 'selected' : undefined}>
                    <TableCell>
                      <Checkbox
                        checked={selected.has(inbound.id)}
                        disabled={isBulkPending}
                        aria-label={`选择 ${inboundLabel(inbound)}`}
                        onCheckedChange={() => toggle(inbound.id)}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button variant="link" className="h-auto justify-start p-0 text-left whitespace-normal" onClick={() => setDetailID(inbound.id)}>
                          {inboundTitle(inbound)}
                        </Button>
                        {!inbound.name && (
                          <span className="hidden text-xs text-muted-foreground 2xl:inline">沿用面板备注</span>
                        )}
                      </div>
                      <div className="flex flex-wrap items-center gap-1.5 pt-1 md:hidden">
                        <span className="text-xs text-muted-foreground">{[inbound.network, inbound.security].filter(Boolean).join(' · ').toUpperCase()}</span>
                        {inbound.vision && <StatusBadge tone={inbound.visionSupported ? 'idle' : 'bad'}>Vision</StatusBadge>}
                        {inbound.vision === null && <Badge variant="outline">保留现有流控</Badge>}
                      </div>
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground lg:table-cell">
                      {inbound.panelName} #{inbound.remoteId}
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <div className="flex flex-col items-start gap-1.5">
                        <div className="flex flex-wrap gap-1">
                          <Badge variant="outline">{inbound.protocol.toUpperCase()}</Badge>
                          {inbound.vision && <StatusBadge tone={inbound.visionSupported ? 'idle' : 'bad'}>Vision{!inbound.visionSupported && ' · 不兼容'}</StatusBadge>}
                          {inbound.vision === null && <Badge variant="outline">保留现有流控</Badge>}
                        </div>
                        <span className="text-xs text-muted-foreground">
                          {[inbound.network, inbound.security].filter(Boolean).join(' · ').toUpperCase() || '等待同步参数'}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="hidden tabular-nums text-muted-foreground xl:table-cell">
                      <EndpointCell inbound={inbound} />
                    </TableCell>
                    <TableCell className="hidden lg:table-cell">
                      {inbound.groupCount > 0 ? (
                        <span className="tabular-nums text-muted-foreground">{inbound.groupCount}</span>
                      ) : (
                        <StatusBadge tone="warn">未分组</StatusBadge>
                      )}
                    </TableCell>
                    <TableCell>
                      <InboundStatus inbound={inbound} />
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap justify-end gap-1">
                        <Button variant="ghost" size="sm" aria-label={`管理 ${inboundLabel(inbound)} 的面板配置`} render={<a href={`#/inbound-configs?id=${inbound.id}`} />} nativeButton={false}>
                          <FileCogIcon data-icon="inline-start" />
                          <span className="hidden sm:inline">配置</span>
                        </Button>
                        <Button type="button" variant="ghost" size="sm" aria-label={`查看 ${inboundLabel(inbound)} 的完整参数`} onClick={() => setDetailID(inbound.id)}>
                          <ListTreeIcon data-icon="inline-start" />
                          <span className="hidden sm:inline">参数</span>
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          aria-label={`编辑 ${inboundLabel(inbound)}`}
                          disabled={pendingAction !== null}
                          onClick={() => edit(inbound)}
                        >
                          <PencilIcon data-icon="inline-start" />
                          <span className="hidden sm:inline">编辑</span>
                        </Button>
                        {inbound.missing && (
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            aria-label={`移除 ${inboundLabel(inbound)}`}
                            disabled={pendingAction !== null}
                            onClick={() => setPendingDelete(inbound)}
                          >
                            <Trash2Icon data-icon="inline-start" />
                            <span className="hidden sm:inline">移除</span>
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
        <CardFooter className="flex-wrap justify-between gap-2">
          <span className="text-muted-foreground">
            {loadState === 'ready' ? `${enabledCount} 个入站已启用` : '入站状态会在刷新后更新'}
          </span>
          <span className="text-muted-foreground">{loadState === 'ready' ? `${missingCount} 个已失联` : '—'}</span>
        </CardFooter>
      </Card>

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => {
          if (!open && pendingAction !== 'save') setDraft(null)
        }}
      >
        <DialogContent className="flex flex-col overflow-hidden sm:max-w-xl">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader className="shrink-0">
                <DialogTitle>编辑入站</DialogTitle>
                <DialogDescription>
                  {editingInbound ? `${inboundLabel(editingInbound)} · ${editingInbound.panelName}` : '入站设置'}。设置保存后生效，重新同步面板会保留。
                </DialogDescription>
              </DialogHeader>
              <FieldGroup className="-mx-4 min-h-0 overflow-y-auto px-4 py-1">
                <Field data-disabled={pendingAction === 'save' || undefined}>
                  <FieldLabel htmlFor="inbound-vision">Vision 流控</FieldLabel>
                  <Select
                    items={visionLabels}
                    value={draft.vision === null ? 'preserve' : draft.vision ? 'on' : 'off'}
                    disabled={pendingAction === 'save'}
                    onValueChange={(value) => {
                      if (value !== null) setDraft({ ...draft, vision: value === 'preserve' ? null : value === 'on' })
                    }}
                  >
                    <SelectTrigger id="inbound-vision" aria-describedby="inbound-vision-description" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {editingInbound?.vision === null && <SelectItem value="preserve">保留现有流控</SelectItem>}
                        <SelectItem value="on" disabled={!editingInbound?.visionSupported}>开启 Vision</SelectItem>
                        <SelectItem value="off">关闭 Vision</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription id="inbound-vision-description">
                    {draft.vision === null
                      ? '保留每位用户在面板中的现有流控。选择开启或关闭后，将统一应用到此入站的用户。'
                      : editingInbound?.visionSupported
                        ? '同步到服务端和客户端订阅，保存后请刷新订阅。'
                        : editingInbound?.visionReason || '请先同步面板参数'}
                  </FieldDescription>
                </Field>
                <FieldGroup className="sm:grid sm:grid-cols-[minmax(0,1fr)_8rem]">
                  <Field data-disabled={pendingAction === 'save' || undefined}>
                    <FieldLabel htmlFor="inbound-name">显示名称</FieldLabel>
                    <Input
                      id="inbound-name"
                      disabled={pendingAction === 'save'}
                      value={draft.name}
                      onChange={(event) => setDraft({ ...draft, name: event.target.value })}
                    />
                    <FieldDescription>留空则沿用面板上的入站备注</FieldDescription>
                  </Field>
                  <Field data-disabled={pendingAction === 'save' || undefined}>
                    <FieldLabel htmlFor="inbound-emoji">Emoji</FieldLabel>
                    <Input
                      id="inbound-emoji"
                      disabled={pendingAction === 'save'}
                      value={draft.emoji}
                      onChange={(event) => setDraft({ ...draft, emoji: event.target.value })}
                    />
                  </Field>
                </FieldGroup>
                <Field data-disabled={pendingAction === 'save' || undefined}>
                  <FieldLabel htmlFor="inbound-sort">排序</FieldLabel>
                  <Input
                    id="inbound-sort"
                    disabled={pendingAction === 'save'}
                    type="number"
                    step={1}
                    value={draft.sortOrder}
                    onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })}
                  />
                  <FieldDescription>数字小的排在前面，决定客户端里代理的先后顺序</FieldDescription>
                </Field>
                <FieldGroup className="sm:grid sm:grid-cols-[minmax(0,1fr)_8rem]">
                  <Field data-disabled={pendingAction === 'save' || undefined}>
                    <FieldLabel htmlFor="inbound-public-address">公网地址</FieldLabel>
                    <Input
                      id="inbound-public-address"
                      disabled={pendingAction === 'save'}
                      placeholder="沿用面板地址"
                      value={draft.publicAddress}
                      onChange={(event) => setDraft({ ...draft, publicAddress: event.target.value })}
                    />
                    <FieldDescription>
                      客户端实际连接的地址和端口。入站藏在回环端口、由 443 上的 SNI 分流或中转接入时填这里；留空或 0 沿用面板给出的值。
                    </FieldDescription>
                  </Field>
                  <Field data-disabled={pendingAction === 'save' || undefined}>
                    <FieldLabel htmlFor="inbound-public-port">公网端口</FieldLabel>
                    <Input
                      id="inbound-public-port"
                      disabled={pendingAction === 'save'}
                      type="number"
                      min={0}
                      max={65535}
                      step={1}
                      placeholder="沿用"
                      value={draft.publicPort || ''}
                      onChange={(event) => setDraft({ ...draft, publicPort: Number(event.target.value) || 0 })}
                    />
                  </Field>
                </FieldGroup>
                <Field orientation="horizontal" data-disabled={pendingAction === 'save' || undefined}>
                  <FieldLabel htmlFor="inbound-enabled">启用（停用后从所有订阅中移除）</FieldLabel>
                  <Switch
                    id="inbound-enabled"
                    disabled={pendingAction === 'save'}
                    checked={draft.enabled}
                    onCheckedChange={(value) => setDraft({ ...draft, enabled: value })}
                  />
                </Field>
                <Field orientation="horizontal" data-disabled={pendingAction === 'save' || undefined}>
                  <FieldLabel htmlFor="inbound-udp">允许 UDP</FieldLabel>
                  <Switch
                    id="inbound-udp"
                    disabled={pendingAction === 'save'}
                    checked={draft.udp}
                    onCheckedChange={(value) => setDraft({ ...draft, udp: value })}
                  />
                </Field>
              </FieldGroup>
              <DialogFooter className="shrink-0">
                <Button type="button" variant="outline" disabled={pendingAction === 'save'} onClick={() => setDraft(null)}>
                  取消
                </Button>
                <Button type="submit" disabled={pendingAction === 'save'}>
                  {pendingAction === 'save' && <Spinner data-icon="inline-start" />}
                  {pendingAction === 'save' ? '保存中…' : '保存'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <InboundDetails inbound={detailInbound} onClose={() => setDetailID(null)} onEdit={edit} onRefresh={refreshAfterMutation} />

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title="移除已失联的入站？"
        description={`「${pendingDelete ? inboundLabel(pendingDelete) : ''}」在面板上已经消失，移除后它在各节点组里的成员关系也会一并删除。`}
        confirmLabel="移除"
        onConfirm={async () => {
          if (pendingDelete) await removeMissing(pendingDelete)
        }}
      />
    </div>
  )
}

/** The port column: the panel's port, or the public endpoint when one overrides it. */
function EndpointCell({ inbound }: { inbound: Inbound }) {
  if (!inbound.publicAddress && !inbound.publicPort) return <>{inbound.port}</>
  const port = inbound.publicPort || inbound.port
  return (
    <span className="flex flex-col leading-tight">
      <span className="text-foreground">
        {inbound.publicAddress ? `${inbound.publicAddress}:${port}` : port}
      </span>
      <span className="text-xs">面板 {inbound.port}</span>
    </span>
  )
}

function InboundStatus({ inbound }: { inbound: Inbound }) {
  // Only problems with a managed configuration are worth a second badge here.
  const config = inbound.configState === 'pending' || inbound.configState === 'drifted' ? <ConfigStateBadge state={inbound.configState} /> : null
  let status = <StatusBadge tone="idle">停用</StatusBadge>
  if (inbound.missing) status = <StatusBadge tone="bad">已失联</StatusBadge>
  else if (!inbound.remoteEnabled) status = <StatusBadge tone="warn">面板已禁用</StatusBadge>
  else if (inbound.enabled) status = <StatusBadge tone="good">启用</StatusBadge>
  return config ? <div className="flex flex-wrap gap-1">{status}{config}</div> : status
}

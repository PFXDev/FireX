import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { BoxesIcon, LayersIcon, PencilIcon, RefreshCwIcon, ServerIcon, Trash2Icon, TriangleAlertIcon } from 'lucide-react'

import { api } from '@/api'
import type { Inbound } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
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
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage } from '@/lib/format'

type Draft = {
  id: number
  name: string
  emoji: string
  sortOrder: number
  enabled: boolean
  udp: boolean
}

type LoadState = 'loading' | 'ready' | 'error'
type PendingAction = 'bulk-enable' | 'bulk-disable' | 'save' | 'remove'

function label(inbound: Inbound): string {
  return inbound.name || inbound.remoteRemark || inbound.inboundTag
}

export function InboundsPage() {
  const [inbounds, setInbounds] = useState<Inbound[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [draft, setDraft] = useState<Draft | null>(null)
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [pendingDelete, setPendingDelete] = useState<Inbound | null>(null)

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
      await api.post('/inbounds/bulk', { ids: [...selected], enabled })
      toast.success(`已更新 ${count} 个入站`)
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
    if (!draft) return
    setPendingAction('save')
    try {
      await api.put(`/inbounds/${draft.id}`, draft)
      toast.success('已保存')
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
      await api.del(`/inbounds/${inbound.id}`)
      toast.success('已移除')
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
        description="入站由面板自动发现。新入站默认停用，确认信息后启用，再到「节点组」里挑进分组才会有人用到。"
      />

      {orphanCount > 0 && (
        <Alert>
          <BoxesIcon />
          <AlertTitle>{orphanCount} 个已启用的入站不属于任何节点组</AlertTitle>
          <AlertDescription>
            用户能用到哪些入站只由分流方案里的节点组决定，不在任何分组里的入站不会出现在任何订阅中。
          </AlertDescription>
          <AlertAction>
            <Button variant="outline" size="sm" onClick={() => (window.location.hash = '#/node-groups')}>
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
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={loadState === 'loading' || pendingAction !== null}
              onClick={() => void load()}
            >
              {loadState === 'loading' ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
              {loadState === 'loading' ? '加载中…' : '刷新'}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loadState === 'loading' ? (
            <InboundsTableSkeleton />
          ) : loadState === 'error' ? (
            <div className="px-4">
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>入站加载失败</AlertTitle>
                <AlertDescription>{loadError}</AlertDescription>
                <AlertAction>
                  <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
                    <RefreshCwIcon data-icon="inline-start" />
                    重试
                  </Button>
                </AlertAction>
              </Alert>
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
                <Button type="button" onClick={() => (window.location.hash = '#/panels')}>
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
                        aria-label={`选择 ${label(inbound)}`}
                        onCheckedChange={() => toggle(inbound.id)}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-medium">
                          {inbound.emoji && `${inbound.emoji} `}
                          {label(inbound)}
                        </span>
                        {!inbound.name && (
                          <span className="hidden text-xs text-muted-foreground 2xl:inline">沿用面板备注</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="hidden text-muted-foreground lg:table-cell">
                      {inbound.panelName} #{inbound.remoteId}
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <Badge variant="outline">{inbound.protocol}</Badge>
                    </TableCell>
                    <TableCell className="hidden tabular-nums text-muted-foreground xl:table-cell">
                      {inbound.port}
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
                      <div className="flex justify-end gap-1">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          aria-label={`编辑 ${label(inbound)}`}
                          disabled={pendingAction !== null}
                          onClick={() =>
                            setDraft({
                              id: inbound.id,
                              name: inbound.name,
                              emoji: inbound.emoji,
                              sortOrder: inbound.sortOrder,
                              enabled: inbound.enabled,
                              udp: inbound.udp,
                            })
                          }
                        >
                          <PencilIcon data-icon="inline-start" />
                          <span className="hidden sm:inline">编辑</span>
                        </Button>
                        {inbound.missing && (
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            aria-label={`移除 ${label(inbound)}`}
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
        <DialogContent className="sm:max-w-lg">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>编辑入站</DialogTitle>
                <DialogDescription>
                  这些字段由 FireX 持有，重新拉取面板不会覆盖。地区、线路这类分类信息填在节点组上。
                </DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <FieldGroup className="sm:grid sm:grid-cols-[1fr_120px]">
                  <Field>
                    <FieldLabel htmlFor="inbound-name">显示名称</FieldLabel>
                    <Input
                      id="inbound-name"
                      value={draft.name}
                      onChange={(event) => setDraft({ ...draft, name: event.target.value })}
                    />
                    <FieldDescription>留空则沿用面板上的入站备注</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="inbound-emoji">Emoji</FieldLabel>
                    <Input
                      id="inbound-emoji"
                      value={draft.emoji}
                      onChange={(event) => setDraft({ ...draft, emoji: event.target.value })}
                    />
                  </Field>
                </FieldGroup>
                <Field>
                  <FieldLabel htmlFor="inbound-sort">排序</FieldLabel>
                  <Input
                    id="inbound-sort"
                    type="number"
                    step={1}
                    value={draft.sortOrder}
                    onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })}
                  />
                  <FieldDescription>数字小的排在前面，决定客户端里代理的先后顺序</FieldDescription>
                </Field>
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="inbound-enabled">启用（停用后从所有订阅中移除）</FieldLabel>
                  <Switch
                    id="inbound-enabled"
                    checked={draft.enabled}
                    onCheckedChange={(value) => setDraft({ ...draft, enabled: value })}
                  />
                </Field>
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="inbound-udp">允许 UDP</FieldLabel>
                  <Switch
                    id="inbound-udp"
                    checked={draft.udp}
                    onCheckedChange={(value) => setDraft({ ...draft, udp: value })}
                  />
                </Field>
              </FieldGroup>
              <DialogFooter>
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

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title="移除已失联的入站？"
        description={`「${pendingDelete ? label(pendingDelete) : ''}」在面板上已经消失，移除后它在各节点组里的成员关系也会一并删除。`}
        confirmLabel="移除"
        onConfirm={async () => {
          if (pendingDelete) await removeMissing(pendingDelete)
        }}
      />
    </div>
  )
}

function InboundsTableSkeleton() {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10">
            <span className="sr-only">选择</span>
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
        {Array.from({ length: 4 }).map((_, index) => (
          <TableRow key={index}>
            <TableCell>
              <Skeleton className="size-4" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-4 w-32" />
            </TableCell>
            <TableCell className="hidden lg:table-cell">
              <Skeleton className="h-4 w-28" />
            </TableCell>
            <TableCell className="hidden md:table-cell">
              <Skeleton className="h-5 w-16" />
            </TableCell>
            <TableCell className="hidden xl:table-cell">
              <Skeleton className="h-4 w-12" />
            </TableCell>
            <TableCell className="hidden lg:table-cell">
              <Skeleton className="h-4 w-8" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-5 w-14" />
            </TableCell>
            <TableCell>
              <Skeleton className="ml-auto h-7 w-16" />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function InboundStatus({ inbound }: { inbound: Inbound }) {
  if (inbound.missing) return <StatusBadge tone="bad">已失联</StatusBadge>
  if (!inbound.remoteEnabled) return <StatusBadge tone="warn">面板已禁用</StatusBadge>
  if (inbound.enabled) return <StatusBadge tone="good">启用</StatusBadge>
  return <StatusBadge tone="idle">停用</StatusBadge>
}

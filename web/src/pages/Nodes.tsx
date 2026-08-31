import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { LayersIcon, PencilIcon, RefreshCwIcon, ServerIcon, Trash2Icon, TriangleAlertIcon } from 'lucide-react'

import { api } from '@/api'
import type { Node } from '@/api'
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
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldTitle } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage } from '@/lib/format'

type Draft = {
  id: number
  name: string
  region: string
  emoji: string
  tags: string
  sortOrder: number
  enabled: boolean
  udp: boolean
  multiplier: number
}

type LoadState = 'loading' | 'ready' | 'error'
type PendingAction = 'bulk-enable' | 'bulk-disable' | 'bulk-region' | 'save' | 'remove'
type BulkAction = Extract<PendingAction, `bulk-${string}`>

export function NodesPage() {
  const [nodes, setNodes] = useState<Node[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [draft, setDraft] = useState<Draft | null>(null)
  const [bulkRegion, setBulkRegion] = useState('')
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [pendingDelete, setPendingDelete] = useState<Node | null>(null)

  const load = useCallback(async () => {
    setLoadState('loading')
    setLoadError('')
    setSelected(new Set())
    try {
      const next = await api.get<Node[]>('/nodes')
      setNodes(next)
      setSelected(new Set())
      setLoadState('ready')
    } catch (err) {
      setLoadError(errorMessage(err, '无法加载节点，请稍后重试。'))
      setLoadState('error')
    }
  }, [])

  const refreshAfterMutation = useCallback(async () => {
    try {
      const next = await api.get<Node[]>('/nodes')
      setNodes(next)
      setSelected(new Set())
      setLoadError('')
      setLoadState('ready')
    } catch (err) {
      toast.error(`操作已完成，但节点列表刷新失败：${errorMessage(err, '未知错误')}`)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const regions = useMemo(() => {
    const set = new Set<string>()
    nodes.forEach((node) => node.region && set.add(node.region))
    return [...set].sort()
  }, [nodes])

  const enabledCount = useMemo(() => nodes.filter((node) => node.enabled && !node.missing).length, [nodes])
  const missingCount = useMemo(() => nodes.filter((node) => node.missing).length, [nodes])

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const bulk = async (action: BulkAction, body: Record<string, unknown>) => {
    if (selected.size === 0) return
    const count = selected.size
    setPendingAction(action)
    try {
      await api.post('/nodes/bulk', { ids: [...selected], ...body })
      toast.success(`已更新 ${count} 个节点`)
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
    if (!draft || !Number.isFinite(draft.multiplier) || draft.multiplier < 0) return
    setPendingAction('save')
    try {
      await api.put(`/nodes/${draft.id}`, {
        ...draft,
        tags: draft.tags
          .split(',')
          .map((tag) => tag.trim())
          .filter(Boolean),
      })
      toast.success('已保存')
      setDraft(null)
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setPendingAction(null)
    }
  }

  const removeMissing = async (node: Node) => {
    setPendingAction('remove')
    try {
      await api.del(`/nodes/${node.id}`)
      toast.success('已移除')
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '移除失败'))
      throw err
    } finally {
      setPendingAction(null)
    }
  }

  const openEditor = (node: Node) => {
    setDraft({
      id: node.id,
      name: node.name,
      region: node.region,
      emoji: node.emoji,
      tags: node.tags,
      sortOrder: node.sortOrder,
      enabled: node.enabled,
      udp: node.udp,
      multiplier: node.multiplier,
    })
  }

  const allSelected = nodes.length > 0 && selected.size === nodes.length
  const partiallySelected = selected.size > 0 && !allSelected
  const isBulkPending = pendingAction?.startsWith('bulk-') ?? false
  const multiplierInvalid = draft !== null && (!Number.isFinite(draft.multiplier) || draft.multiplier < 0)

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="节点"
        description="节点由面板的入站自动发现。新节点默认停用，确认信息后再启用并加入套餐。"
      />

      {selected.size > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>批量管理节点</CardTitle>
            <CardDescription>只会修改当前选中的节点，操作完成后会自动刷新列表。</CardDescription>
            <CardAction>
              <Badge variant="secondary">已选 {selected.size} 个</Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <FieldGroup className="sm:grid sm:grid-cols-2">
              <Field>
                <FieldTitle>节点状态</FieldTitle>
                <span className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pendingAction !== null}
                    onClick={() => void bulk('bulk-enable', { enabled: true })}
                  >
                    {pendingAction === 'bulk-enable' && <Spinner data-icon="inline-start" />}
                    {pendingAction === 'bulk-enable' ? '启用中…' : '启用'}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={pendingAction !== null}
                    onClick={() => void bulk('bulk-disable', { enabled: false })}
                  >
                    {pendingAction === 'bulk-disable' && <Spinner data-icon="inline-start" />}
                    {pendingAction === 'bulk-disable' ? '停用中…' : '停用'}
                  </Button>
                </span>
              </Field>
              <Field data-disabled={pendingAction !== null || undefined}>
                <FieldLabel htmlFor="bulk-node-region">节点地区</FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id="bulk-node-region"
                    list="firex-regions"
                    placeholder="例如：🇭🇰 香港"
                    value={bulkRegion}
                    disabled={pendingAction !== null}
                    onChange={(event) => setBulkRegion(event.target.value)}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton
                      variant="outline"
                      disabled={pendingAction !== null || !bulkRegion.trim()}
                      onClick={() => void bulk('bulk-region', { region: bulkRegion.trim() })}
                    >
                      {pendingAction === 'bulk-region' && <Spinner data-icon="inline-start" />}
                      {pendingAction === 'bulk-region' ? '应用中…' : '应用地区'}
                    </InputGroupButton>
                  </InputGroupAddon>
                </InputGroup>
                <FieldDescription>可输入新地区，也可从已有地区中选择。</FieldDescription>
              </Field>
            </FieldGroup>
          </CardContent>
          <CardFooter className="justify-end">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={isBulkPending}
              onClick={() => setSelected(new Set())}
            >
              取消选择
            </Button>
          </CardFooter>
        </Card>
      )}

      <datalist id="firex-regions">
        {regions.map((region) => (
          <option key={region} value={region} />
        ))}
      </datalist>

      <Card>
        <CardHeader>
          <CardTitle>节点列表</CardTitle>
          <CardDescription>
            {loadState === 'ready' ? `共 ${nodes.length} 个节点，可批量调整启用状态与地区。` : '查看所有面板发现的入站节点。'}
          </CardDescription>
          <CardAction>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={loadState === 'loading' || pendingAction !== null}
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
            <NodesTableSkeleton />
          ) : loadState === 'error' ? (
            <div className="px-4">
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>节点加载失败</AlertTitle>
                <AlertDescription>{loadError}</AlertDescription>
                <AlertAction>
                  <Button type="button" variant="outline" size="sm" onClick={() => void load()}>
                    <RefreshCwIcon data-icon="inline-start" />
                    重试
                  </Button>
                </AlertAction>
              </Alert>
            </div>
          ) : nodes.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <LayersIcon />
                </EmptyMedia>
                <EmptyTitle>还没有节点</EmptyTitle>
                <EmptyDescription>先添加面板，FireX 会自动拉取它的入站作为节点。</EmptyDescription>
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
                      indeterminate={partiallySelected}
                      disabled={isBulkPending}
                      aria-label="全选节点"
                      onCheckedChange={(checked) =>
                        setSelected(checked ? new Set(nodes.map((node) => node.id)) : new Set())
                      }
                    />
                  </TableHead>
                  <TableHead>名称</TableHead>
                  <TableHead className="hidden sm:table-cell">地区</TableHead>
                  <TableHead className="hidden lg:table-cell">面板 / 入站</TableHead>
                  <TableHead className="hidden md:table-cell">协议</TableHead>
                  <TableHead className="hidden xl:table-cell">端口</TableHead>
                  <TableHead className="hidden xl:table-cell">标签</TableHead>
                  <TableHead className="hidden lg:table-cell">套餐</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>
                    <span className="sr-only">操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.map((node) => {
                  const tags = node.tags
                    .split(',')
                    .map((tag) => tag.trim())
                    .filter(Boolean)

                  return (
                    <TableRow key={node.id} data-state={selected.has(node.id) ? 'selected' : undefined}>
                      <TableCell>
                        <Checkbox
                          checked={selected.has(node.id)}
                          disabled={isBulkPending}
                          aria-label={`选择 ${node.name || node.remoteRemark || node.inboundTag}`}
                          onCheckedChange={() => toggle(node.id)}
                        />
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <span className="font-medium">
                            {node.emoji && `${node.emoji} `}
                            {node.name || node.remoteRemark || node.inboundTag}
                          </span>
                          {!node.name && <span className="hidden text-xs text-muted-foreground 2xl:inline">沿用面板备注</span>}
                        </div>
                      </TableCell>
                      <TableCell className="hidden sm:table-cell">
                        {node.region || <span className="text-muted-foreground">未分组</span>}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground lg:table-cell">
                        {node.panelName} #{node.inboundId}
                      </TableCell>
                      <TableCell className="hidden md:table-cell">
                        <Badge variant="outline">{node.protocol}</Badge>
                      </TableCell>
                      <TableCell className="hidden tabular-nums text-muted-foreground xl:table-cell">{node.port}</TableCell>
                      <TableCell className="hidden xl:table-cell">
                        {tags.length > 0 ? (
                          <span className="flex flex-wrap gap-1">
                            {tags.map((tag) => (
                              <Badge key={tag} variant="secondary">
                                {tag}
                              </Badge>
                            ))}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="hidden tabular-nums text-muted-foreground lg:table-cell">{node.planCount}</TableCell>
                      <TableCell>
                        <NodeStatus node={node} />
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            aria-label={`编辑 ${node.name || node.remoteRemark || node.inboundTag}`}
                            disabled={pendingAction !== null}
                            onClick={() => openEditor(node)}
                          >
                            <PencilIcon data-icon="inline-start" />
                            <span className="hidden sm:inline">编辑</span>
                          </Button>
                          {node.missing && (
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              aria-label={`移除 ${node.name || node.remoteRemark || node.inboundTag}`}
                              disabled={pendingAction !== null}
                              onClick={() => setPendingDelete(node)}
                            >
                              <Trash2Icon data-icon="inline-start" />
                              <span className="hidden sm:inline">移除</span>
                            </Button>
                          )}
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
            {loadState === 'ready' ? `${enabledCount} 个节点已启用` : '节点状态会在刷新后更新'}
          </span>
          <span className="text-muted-foreground">
            {loadState === 'ready' ? `${missingCount} 个节点已失联` : '—'}
          </span>
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
                <DialogTitle>编辑节点</DialogTitle>
                <DialogDescription>这些字段由 FireX 持有，重新拉取面板不会覆盖。</DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <FieldGroup className="sm:grid sm:grid-cols-[1fr_120px]">
                  <Field>
                    <FieldLabel htmlFor="node-name">显示名称</FieldLabel>
                    <Input
                      id="node-name"
                      value={draft.name}
                      onChange={(event) => setDraft({ ...draft, name: event.target.value })}
                    />
                    <FieldDescription>留空则沿用面板上的入站备注</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="node-emoji">Emoji</FieldLabel>
                    <Input
                      id="node-emoji"
                      value={draft.emoji}
                      onChange={(event) => setDraft({ ...draft, emoji: event.target.value })}
                    />
                  </Field>
                </FieldGroup>
                <Field>
                  <FieldLabel htmlFor="node-region">地区</FieldLabel>
                  <Input
                    id="node-region"
                    list="firex-regions"
                    placeholder="🇭🇰 香港"
                    value={draft.region}
                    onChange={(event) => setDraft({ ...draft, region: event.target.value })}
                  />
                  <FieldDescription>同地区的节点会自动生成一个 Clash 分组，分组名就是这里填的文本</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="node-tags">标签</FieldLabel>
                  <Input
                    id="node-tags"
                    value={draft.tags}
                    onChange={(event) => setDraft({ ...draft, tags: event.target.value })}
                  />
                  <FieldDescription>逗号分隔，可在 Clash 模板里用 &lt;TAG:名称&gt; 引用</FieldDescription>
                </Field>
                <FieldGroup className="sm:grid sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="node-sort">排序</FieldLabel>
                    <Input
                      id="node-sort"
                      type="number"
                      step={1}
                      value={draft.sortOrder}
                      onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })}
                    />
                    <FieldDescription>数字小的排在前面</FieldDescription>
                  </Field>
                  <Field data-invalid={multiplierInvalid || undefined}>
                    <FieldLabel htmlFor="node-multiplier">倍率</FieldLabel>
                    <Input
                      id="node-multiplier"
                      type="number"
                      min={0}
                      step="0.1"
                      value={draft.multiplier}
                      aria-invalid={multiplierInvalid || undefined}
                      onChange={(event) => setDraft({ ...draft, multiplier: Number(event.target.value) })}
                    />
                    {multiplierInvalid ? (
                      <FieldError>倍率必须是大于或等于 0 的数字。</FieldError>
                    ) : (
                      <FieldDescription>仅作展示记录，不参与计费</FieldDescription>
                    )}
                  </Field>
                </FieldGroup>
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="node-enabled">启用（停用后从所有订阅中移除）</FieldLabel>
                  <Switch
                    id="node-enabled"
                    checked={draft.enabled}
                    onCheckedChange={(value) => setDraft({ ...draft, enabled: value })}
                  />
                </Field>
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="node-udp">允许 UDP</FieldLabel>
                  <Switch
                    id="node-udp"
                    checked={draft.udp}
                    onCheckedChange={(value) => setDraft({ ...draft, udp: value })}
                  />
                </Field>
              </FieldGroup>
              <DialogFooter>
                <Button type="button" variant="outline" disabled={pendingAction === 'save'} onClick={() => setDraft(null)}>
                  取消
                </Button>
                <Button type="submit" disabled={pendingAction === 'save' || multiplierInvalid}>
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
        title="移除已失联的节点？"
        description={`「${pendingDelete?.name || pendingDelete?.remoteRemark || ''}」的入站已在面板上消失，移除后套餐关联也会一并删除。`}
        confirmLabel="移除"
        onConfirm={async () => {
          if (pendingDelete) await removeMissing(pendingDelete)
        }}
      />
    </div>
  )
}

function NodesTableSkeleton() {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10"><span className="sr-only">选择</span></TableHead>
          <TableHead>名称</TableHead>
          <TableHead className="hidden sm:table-cell">地区</TableHead>
          <TableHead className="hidden lg:table-cell">面板 / 入站</TableHead>
          <TableHead className="hidden md:table-cell">协议</TableHead>
          <TableHead className="hidden xl:table-cell">端口</TableHead>
          <TableHead className="hidden xl:table-cell">标签</TableHead>
          <TableHead className="hidden lg:table-cell">套餐</TableHead>
          <TableHead>状态</TableHead>
          <TableHead><span className="sr-only">操作</span></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {Array.from({ length: 4 }).map((_, index) => (
          <TableRow key={index}>
            <TableCell><Skeleton className="size-4" /></TableCell>
            <TableCell><Skeleton className="h-4 w-32" /></TableCell>
            <TableCell className="hidden sm:table-cell"><Skeleton className="h-4 w-20" /></TableCell>
            <TableCell className="hidden lg:table-cell"><Skeleton className="h-4 w-28" /></TableCell>
            <TableCell className="hidden md:table-cell"><Skeleton className="h-5 w-16" /></TableCell>
            <TableCell className="hidden xl:table-cell"><Skeleton className="h-4 w-12" /></TableCell>
            <TableCell className="hidden xl:table-cell"><Skeleton className="h-5 w-24" /></TableCell>
            <TableCell className="hidden lg:table-cell"><Skeleton className="h-4 w-8" /></TableCell>
            <TableCell><Skeleton className="h-5 w-14" /></TableCell>
            <TableCell><Skeleton className="ml-auto h-7 w-16" /></TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function NodeStatus({ node }: { node: Node }) {
  if (node.missing) return <StatusBadge tone="bad">已失联</StatusBadge>
  if (!node.remoteEnabled) return <StatusBadge tone="warn">面板已禁用</StatusBadge>
  if (node.enabled) return <StatusBadge tone="good">启用</StatusBadge>
  return <StatusBadge tone="idle">停用</StatusBadge>
}

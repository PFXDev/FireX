import { useCallback, useEffect, useMemo, useState } from 'react'
import { BoxesIcon, PlusIcon, RefreshCwIcon, TriangleAlertIcon, WandSparklesIcon } from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { Node, NodeGroup } from '@/api'
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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage } from '@/lib/format'

const GROUP_TYPES = [
  { value: 'url-test', label: 'url-test（自动选延迟最低）' },
  { value: 'select', label: 'select（客户端手动选）' },
  { value: 'fallback', label: 'fallback（按顺序故障转移）' },
  { value: 'load-balance', label: 'load-balance（负载均衡）' },
]

type Draft = {
  id?: number
  name: string
  emoji: string
  region: string
  line: string
  type: string
  testUrl: string
  interval: number
  tolerance: number
  sortOrder: number
  enabled: boolean
  remark: string
  nodeIds: number[]
}

const emptyDraft: Draft = {
  name: '',
  emoji: '',
  region: '',
  line: '',
  type: 'url-test',
  testUrl: '',
  interval: 300,
  tolerance: 50,
  sortOrder: 100,
  enabled: true,
  remark: '',
  nodeIds: [],
}

export function NodeGroupsPage() {
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [nodes, setNodes] = useState<Node[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<NodeGroup | null>(null)

  const load = useCallback(async () => {
    try {
      const [nextGroups, nextNodes] = await Promise.all([
        api.get<NodeGroup[]>('/node-groups'),
        api.get<Node[]>('/nodes'),
      ])
      setGroups(nextGroups)
      setNodes(nextNodes)
      setLoadError(null)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '分组数据加载失败'))
      return false
    }
  }, [])

  useEffect(() => {
    void load().finally(() => setLoading(false))
  }, [load])

  const refresh = async () => {
    setRefreshing(true)
    await load()
    setRefreshing(false)
  }

  const revalidate = async () => {
    if (!(await load())) toast.error('操作已完成，但列表刷新失败，请手动重试')
  }

  // Members are picked per panel: an operator who creates dedicated FireX_*
  // inbounds recognises them by which panel they live on, not by region text.
  const nodesByPanel = useMemo(() => {
    const byPanel = new Map<string, Node[]>()
    nodes
      .filter((node) => !node.missing)
      .forEach((node) => {
        const key = node.panelName || `面板 #${node.panelId}`
        byPanel.set(key, [...(byPanel.get(key) ?? []), node])
      })
    return [...byPanel.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [nodes])

  const nodeName = useCallback(
    (node: Node) => `${node.emoji ? `${node.emoji} ` : ''}${node.name || node.remoteRemark || node.inboundTag}`,
    [],
  )

  const openCreate = () => {
    setShowErrors(false)
    setDraft({ ...emptyDraft, sortOrder: 100 + groups.length })
  }

  const openEdit = (group: NodeGroup) => {
    setShowErrors(false)
    setDraft({
      id: group.id,
      name: group.name,
      emoji: group.emoji,
      region: group.region,
      line: group.line,
      type: group.type,
      testUrl: group.testUrl,
      interval: group.interval,
      tolerance: group.tolerance,
      sortOrder: group.sortOrder,
      enabled: group.enabled,
      remark: group.remark,
      nodeIds: group.nodeIds,
    })
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft) return
    setShowErrors(true)
    if (!draft.name.trim() || draft.name.includes(',')) return

    setSaving(true)
    const body = {
      name: draft.name.trim(),
      emoji: draft.emoji.trim(),
      region: draft.region.trim(),
      line: draft.line.trim(),
      type: draft.type,
      testUrl: draft.testUrl.trim(),
      interval: draft.interval,
      tolerance: draft.tolerance,
      sortOrder: draft.sortOrder,
      enabled: draft.enabled,
      remark: draft.remark,
      nodeIds: draft.nodeIds,
    }

    try {
      if (draft.id) {
        const result = await api.put<{ rewrittenRules: number }>(`/node-groups/${draft.id}`, body)
        if (result.rewrittenRules > 0) {
          toast.success(`分组已保存，同步更新了 ${result.rewrittenRules} 处分流引用`)
        } else {
          toast.success('分组已保存')
        }
      } else {
        await api.post('/node-groups', body)
        toast.success('分组已创建')
      }
      setDraft(null)
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (group: NodeGroup) => {
    try {
      const result = await api.del<{ droppedRules: number }>(`/node-groups/${group.id}`)
      if (result.droppedRules > 0) {
        toast.success(`分组已删除，同时移除了 ${result.droppedRules} 条引用它的分流规则`)
      } else {
        toast.success('分组已删除')
      }
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  const generate = async () => {
    setGenerating(true)
    try {
      const result = await api.post<{ created: number; regions: number }>('/node-groups/generate')
      if (result.created === 0) {
        toast.info(result.regions === 0 ? '没有可用的地区信息，请先给节点填写地区' : '所有地区都已经有同名分组了')
      } else {
        toast.success(`已按地区生成 ${result.created} 个分组`)
      }
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '生成失败'))
    } finally {
      setGenerating(false)
    }
  }

  const toggleNode = (id: number) => {
    if (!draft) return
    const has = draft.nodeIds.includes(id)
    setDraft({
      ...draft,
      nodeIds: has ? draft.nodeIds.filter((nodeId) => nodeId !== id) : [...draft.nodeIds, id],
    })
  }

  const togglePanel = (group: Node[]) => {
    if (!draft) return
    const ids = group.map((node) => node.id)
    const allSelected = ids.every((id) => draft.nodeIds.includes(id))
    setDraft({
      ...draft,
      nodeIds: allSelected
        ? draft.nodeIds.filter((id) => !ids.includes(id))
        : [...new Set([...draft.nodeIds, ...ids])],
    })
  }

  const nameInvalid = Boolean(showErrors && draft && !draft.name.trim())
  const commaInvalid = Boolean(showErrors && draft && draft.name.includes(','))
  const probeGroup = draft?.type !== 'select'
  const enabledGroups = groups.filter((group) => group.enabled).length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="分组" description="把不同面板的入站聚合成客户端看到的策略组，通常按地区与线路划分。">
        <Button variant="outline" disabled={generating || loading} onClick={() => void generate()}>
          {generating ? <Spinner data-icon="inline-start" /> : <WandSparklesIcon data-icon="inline-start" />}
          按地区生成
        </Button>
        <Button onClick={openCreate}>
          <PlusIcon data-icon="inline-start" />
          新建分组
        </Button>
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>分组列表</CardTitle>
          <CardDescription>
            每个分组渲染成一个 mihomo 策略组；订阅只会包含用户套餐内的成员，成员为空的分组会被自动省略。
          </CardDescription>
          <CardAction className="flex items-center gap-2">
            {!loading && <Badge variant="secondary">{enabledGroups} 个启用</Badge>}
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="刷新分组列表"
              disabled={loading || refreshing}
              onClick={refresh}
            >
              {refreshing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading ? (
            <div className="flex flex-col gap-3 px-(--card-spacing)" aria-label="分组加载中">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </div>
          ) : loadError && groups.length === 0 ? (
            <div className="px-(--card-spacing)">
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>无法加载分组</AlertTitle>
                <AlertDescription>{loadError}</AlertDescription>
                <AlertAction>
                  <Button variant="outline" size="sm" onClick={refresh} disabled={refreshing}>
                    {refreshing && <Spinner data-icon="inline-start" />}
                    重试
                  </Button>
                </AlertAction>
              </Alert>
            </div>
          ) : groups.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BoxesIcon />
                </EmptyMedia>
                <EmptyTitle>还没有分组</EmptyTitle>
                <EmptyDescription>
                  在没有任何分组时，订阅会按节点的「地区」字段自动分组。建好分组后，这个兜底就不再生效。
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent className="flex-row gap-2">
                <Button variant="outline" disabled={generating} onClick={() => void generate()}>
                  {generating ? <Spinner data-icon="inline-start" /> : <WandSparklesIcon data-icon="inline-start" />}
                  按地区生成
                </Button>
                <Button onClick={openCreate}>
                  <PlusIcon data-icon="inline-start" />
                  新建分组
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <div className="flex flex-col gap-3">
              {loadError && (
                <div className="px-(--card-spacing)">
                  <Alert variant="warning">
                    <TriangleAlertIcon />
                    <AlertTitle>列表可能不是最新状态</AlertTitle>
                    <AlertDescription>{loadError}</AlertDescription>
                  </Alert>
                </div>
              )}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>分组</TableHead>
                    <TableHead className="hidden md:table-cell">地区</TableHead>
                    <TableHead className="hidden md:table-cell">线路</TableHead>
                    <TableHead className="hidden lg:table-cell">类型</TableHead>
                    <TableHead>节点</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>
                      <span className="sr-only">操作</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {groups.map((group) => (
                    <TableRow key={group.id}>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <strong>
                            {group.emoji && `${group.emoji} `}
                            {group.name}
                          </strong>
                          {group.remark && <span className="text-muted-foreground">{group.remark}</span>}
                        </div>
                      </TableCell>
                      <TableCell className="hidden md:table-cell">{group.region || '—'}</TableCell>
                      <TableCell className="hidden md:table-cell">{group.line || '—'}</TableCell>
                      <TableCell className="hidden lg:table-cell">
                        <Badge variant="outline">{group.type}</Badge>
                      </TableCell>
                      <TableCell>
                        {group.enabledNodes === group.nodeIds.length
                          ? group.nodeIds.length
                          : `${group.enabledNodes}/${group.nodeIds.length}`}
                      </TableCell>
                      <TableCell>
                        {group.enabled ? (
                          <StatusBadge tone="good">启用</StatusBadge>
                        ) : (
                          <StatusBadge tone="idle">停用</StatusBadge>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-2">
                          <Button variant="outline" size="sm" onClick={() => openEdit(group)}>
                            编辑
                          </Button>
                          <Button
                            variant="destructive"
                            size="sm"
                            aria-label={`删除分组 ${group.name}`}
                            onClick={() => setPendingDelete(group)}
                          >
                            删除
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
        <CardFooter className="flex-wrap justify-between gap-2">
          <span className="text-muted-foreground">
            {loading ? '正在读取分组' : `共 ${groups.length} 个分组`}
          </span>
          <span className="text-muted-foreground">
            {loading ? '—' : `覆盖 ${new Set(groups.flatMap((group) => group.nodeIds)).size} 个节点`}
          </span>
        </CardFooter>
      </Card>

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => {
          if (!open && !saving) {
            setDraft(null)
            setShowErrors(false)
          }
        }}
      >
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{draft?.id ? '编辑分组' : '新建分组'}</DialogTitle>
            <DialogDescription>分组名会直接作为客户端里的策略组名称显示。</DialogDescription>
          </DialogHeader>
          {draft && (
            <form className="flex min-h-0 flex-col gap-4" noValidate onSubmit={save}>
              <FieldGroup>
                <FieldGroup className="grid gap-4 sm:grid-cols-[6rem_1fr]">
                  <Field>
                    <FieldLabel htmlFor="group-emoji">图标</FieldLabel>
                    <Input
                      id="group-emoji"
                      placeholder="🇭🇰"
                      value={draft.emoji}
                      onChange={(event) => setDraft({ ...draft, emoji: event.target.value })}
                    />
                  </Field>
                  <Field data-invalid={nameInvalid || commaInvalid}>
                    <FieldLabel htmlFor="group-name">名称</FieldLabel>
                    <Input
                      id="group-name"
                      placeholder="香港 IEPL"
                      value={draft.name}
                      required
                      aria-invalid={nameInvalid || commaInvalid}
                      onChange={(event) => setDraft({ ...draft, name: event.target.value })}
                    />
                    {nameInvalid && <FieldError>请输入分组名称。</FieldError>}
                    {commaInvalid && <FieldError>名称不能包含英文逗号，分流规则以逗号分隔。</FieldError>}
                  </Field>
                </FieldGroup>

                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="group-region">地区</FieldLabel>
                    <Input
                      id="group-region"
                      placeholder="香港"
                      value={draft.region}
                      onChange={(event) => setDraft({ ...draft, region: event.target.value })}
                    />
                    <FieldDescription>仅用于列表筛选与排序，不参与渲染。</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="group-line">线路</FieldLabel>
                    <Input
                      id="group-line"
                      placeholder="IEPL / 中转 / 直连"
                      value={draft.line}
                      onChange={(event) => setDraft({ ...draft, line: event.target.value })}
                    />
                  </Field>
                </FieldGroup>

                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="group-type">策略组类型</FieldLabel>
                    <Select
                      items={GROUP_TYPES}
                      value={draft.type}
                      onValueChange={(value) => setDraft({ ...draft, type: String(value) })}
                    >
                      <SelectTrigger id="group-type" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {GROUP_TYPES.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="group-sort">排序</FieldLabel>
                    <Input
                      id="group-sort"
                      type="number"
                      value={draft.sortOrder}
                      onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })}
                    />
                    <FieldDescription>数值小的排在前面。</FieldDescription>
                  </Field>
                </FieldGroup>

                {probeGroup && (
                  <FieldGroup className="grid gap-4 sm:grid-cols-3">
                    <Field className="sm:col-span-3">
                      <FieldLabel htmlFor="group-test-url">测速地址</FieldLabel>
                      <Input
                        id="group-test-url"
                        placeholder="https://www.gstatic.com/generate_204"
                        value={draft.testUrl}
                        onChange={(event) => setDraft({ ...draft, testUrl: event.target.value })}
                      />
                      <FieldDescription>留空则使用默认地址。</FieldDescription>
                    </Field>
                    <Field>
                      <FieldLabel htmlFor="group-interval">测速间隔 (秒)</FieldLabel>
                      <Input
                        id="group-interval"
                        type="number"
                        min={0}
                        value={draft.interval}
                        onChange={(event) => setDraft({ ...draft, interval: Number(event.target.value) })}
                      />
                    </Field>
                    {draft.type === 'url-test' && (
                      <Field>
                        <FieldLabel htmlFor="group-tolerance">容差 (毫秒)</FieldLabel>
                        <Input
                          id="group-tolerance"
                          type="number"
                          min={0}
                          value={draft.tolerance}
                          onChange={(event) => setDraft({ ...draft, tolerance: Number(event.target.value) })}
                        />
                      </Field>
                    )}
                  </FieldGroup>
                )}

                <FieldSet>
                  <FieldLegend variant="label">包含节点（已选 {draft.nodeIds.length} 个）</FieldLegend>
                  <FieldDescription>一个节点可以同时属于多个分组。</FieldDescription>
                  <ScrollArea className="h-64 rounded-lg border">
                    {nodesByPanel.length === 0 ? (
                      <Empty>
                        <EmptyHeader>
                          <EmptyMedia variant="icon">
                            <BoxesIcon />
                          </EmptyMedia>
                          <EmptyTitle>还没有可用节点</EmptyTitle>
                          <EmptyDescription>先连接面板并完成入站发现。</EmptyDescription>
                        </EmptyHeader>
                      </Empty>
                    ) : (
                      <FieldGroup className="gap-4 p-3">
                        {nodesByPanel.map(([panel, group]) => {
                          const selectedCount = group.filter((node) => draft.nodeIds.includes(node.id)).length
                          const allSelected = selectedCount === group.length
                          return (
                            <FieldSet key={panel}>
                              <FieldLegend className="sr-only">{panel} 的节点</FieldLegend>
                              <Field orientation="horizontal">
                                <Checkbox
                                  id={`group-panel-${panel}`}
                                  checked={allSelected}
                                  indeterminate={selectedCount > 0 && !allSelected}
                                  onCheckedChange={() => togglePanel(group)}
                                />
                                <FieldLabel htmlFor={`group-panel-${panel}`}>
                                  {panel}
                                  <Badge variant="outline">
                                    {selectedCount}/{group.length}
                                  </Badge>
                                </FieldLabel>
                              </Field>
                              <FieldGroup className="gap-2 pl-6">
                                {group.map((node) => (
                                  <Field key={node.id} orientation="horizontal">
                                    <Checkbox
                                      id={`group-node-${node.id}`}
                                      checked={draft.nodeIds.includes(node.id)}
                                      onCheckedChange={() => toggleNode(node.id)}
                                    />
                                    <FieldLabel htmlFor={`group-node-${node.id}`}>
                                      <span>{nodeName(node)}</span>
                                      <span className="text-muted-foreground">
                                        {node.inboundTag || node.protocol}:{node.port}
                                      </span>
                                      {!node.enabled && <StatusBadge tone="idle">未启用</StatusBadge>}
                                    </FieldLabel>
                                  </Field>
                                ))}
                              </FieldGroup>
                            </FieldSet>
                          )
                        })}
                      </FieldGroup>
                    )}
                  </ScrollArea>
                </FieldSet>

                <Field orientation="horizontal">
                  <FieldLabel htmlFor="group-enabled">启用分组</FieldLabel>
                  <Switch
                    id="group-enabled"
                    checked={draft.enabled}
                    onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
                  />
                </Field>

                <Field>
                  <FieldLabel htmlFor="group-remark">备注</FieldLabel>
                  <Input
                    id="group-remark"
                    value={draft.remark}
                    onChange={(event) => setDraft({ ...draft, remark: event.target.value })}
                  />
                </Field>
              </FieldGroup>

              <DialogFooter>
                <Button type="button" variant="outline" disabled={saving} onClick={() => setDraft(null)}>
                  取消
                </Button>
                <Button type="submit" disabled={saving}>
                  {saving && <Spinner data-icon="inline-start" />}
                  {saving ? '保存中…' : '保存分组'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除分组「${pendingDelete?.name ?? ''}」？`}
        description="节点本身不受影响。分流配置里引用了这个分组的策略成员和规则会一并移除。"
        confirmLabel="删除分组"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

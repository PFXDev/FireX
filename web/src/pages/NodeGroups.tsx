import { useCallback, useEffect, useMemo, useState } from 'react'
import { BoxesIcon, EyeIcon, PencilIcon, PlusIcon, RouteIcon, Trash2Icon, TriangleAlertIcon, XIcon } from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { Inbound, NodeGroup, NodeGroupTag } from '@/api'
import { CodeText } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
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
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { errorMessage } from '@/lib/format'
import { inboundTitle } from '@/lib/inbound'
import { useSession } from '@/lib/session'
import { reportSyncError } from '@/lib/sync'

const GROUP_TYPES = [
  { value: 'url-test', label: 'url-test（自动选延迟最低）' },
  { value: 'select', label: 'select（客户端手动选）' },
  { value: 'fallback', label: 'fallback（按顺序故障转移）' },
  { value: 'load-balance', label: 'load-balance（负载均衡）' },
]

/** Tag keys the editor offers first; an operator may type any other. */
const SUGGESTED_TAG_KEYS = ['地区', '线路', '落地']

type Draft = {
  id?: number
  name: string
  emoji: string
  type: string
  testUrl: string
  interval: number
  tolerance: number
  multiplier: number
  sortOrder: number
  enabled: boolean
  remark: string
  tags: NodeGroupTag[]
  inboundIds: number[]
}

const emptyDraft: Draft = {
  name: '',
  emoji: '',
  type: 'url-test',
  testUrl: '',
  interval: 300,
  tolerance: 50,
  multiplier: 1,
  sortOrder: 100,
  enabled: true,
  remark: '',
  tags: [],
  inboundIds: [],
}

export function NodeGroupsPage() {
  const [groups, setGroups] = useState<NodeGroup[]>([])
  const [inbounds, setInbounds] = useState<Inbound[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<NodeGroup | null>(null)
  const [tagFilter, setTagFilter] = useState<{ key: string; value: string } | null>(null)
  const { readOnly } = useSession()

  const load = useCallback(async () => {
    try {
      const [nextGroups, nextInbounds] = await Promise.all([
        api.get<NodeGroup[]>('/node-groups'),
        api.get<Inbound[]>('/inbounds'),
      ])
      setGroups(nextGroups)
      setInbounds(nextInbounds)
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

  // Members are picked per panel: an operator recognises an inbound by which
  // machine it lives on, not by any text on the inbound itself.
  const inboundsByPanel = useMemo(() => {
    const byPanel = new Map<string, Inbound[]>()
    inbounds
      .filter((inbound) => inbound.enabled && !inbound.missing)
      .forEach((inbound) => {
        const key = inbound.panelName || `面板 #${inbound.panelId}`
        byPanel.set(key, [...(byPanel.get(key) ?? []), inbound])
      })
    return [...byPanel.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [inbounds])

  /** Every key/value pair in use, so the filter bar can be built from the data. */
  const tagIndex = useMemo(() => {
    const byKey = new Map<string, Set<string>>()
    groups.forEach((group) =>
      group.tags.forEach((tag) => {
        if (!byKey.has(tag.key)) byKey.set(tag.key, new Set())
        byKey.get(tag.key)!.add(tag.value)
      }),
    )
    return [...byKey.entries()]
      .map(([key, values]) => [key, [...values].sort()] as const)
      .sort((a, b) => a[0].localeCompare(b[0]))
  }, [groups])

  const visibleGroups = useMemo(() => {
    if (!tagFilter) return groups
    return groups.filter((group) =>
      group.tags.some((tag) => tag.key === tagFilter.key && tag.value === tagFilter.value),
    )
  }, [groups, tagFilter])

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
      type: group.type,
      testUrl: group.testUrl,
      interval: group.interval,
      tolerance: group.tolerance,
      multiplier: group.multiplier,
      sortOrder: group.sortOrder,
      enabled: group.enabled,
      remark: group.remark,
      tags: group.tags.map((tag) => ({ ...tag })),
      inboundIds: group.inboundIds,
    })
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || saving) return
    setShowErrors(true)
    if (!draft.name.trim() || draft.name.includes(',')) return
    if (!event.currentTarget.reportValidity()) return

    setSaving(true)
    const body = {
      ...draft,
      name: draft.name.trim(),
      emoji: draft.emoji.trim(),
      testUrl: draft.testUrl.trim(),
      tags: draft.tags.filter((tag) => tag.key.trim() && tag.value.trim()),
    }

    try {
      if (draft.id) {
        const result = await api.put<{ rewrittenMembers: number; syncError: string }>(
          `/node-groups/${draft.id}`,
          body,
        )
        toast.success(
          result.rewrittenMembers > 0
            ? `分组已保存，同步更新了 ${result.rewrittenMembers} 处分流引用`
            : '分组已保存',
        )
        reportSyncError(result.syncError)
      } else {
        const result = await api.post<{ syncError: string }>('/node-groups', body)
        toast.success('分组已创建')
        reportSyncError(result.syncError)
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
      const result = await api.del<{ droppedMembers: number; syncError: string }>(`/node-groups/${group.id}`)
      toast.success(
        result.droppedMembers > 0
          ? `分组已删除，同时移除了 ${result.droppedMembers} 处引用它的分流成员`
          : '分组已删除',
      )
      reportSyncError(result.syncError)
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  const toggleInbound = (id: number) => {
    if (!draft) return
    const has = draft.inboundIds.includes(id)
    setDraft({
      ...draft,
      inboundIds: has ? draft.inboundIds.filter((inboundId) => inboundId !== id) : [...draft.inboundIds, id],
    })
  }

  const togglePanel = (group: Inbound[]) => {
    if (!draft) return
    const ids = group.map((inbound) => inbound.id)
    const allSelected = ids.every((id) => draft.inboundIds.includes(id))
    setDraft({
      ...draft,
      inboundIds: allSelected
        ? draft.inboundIds.filter((id) => !ids.includes(id))
        : [...new Set([...draft.inboundIds, ...ids])],
    })
  }

  const patchTag = (index: number, patch: Partial<NodeGroupTag>) => {
    if (!draft) return
    setDraft({ ...draft, tags: draft.tags.map((tag, i) => (i === index ? { ...tag, ...patch } : tag)) })
  }

  const nameInvalid = Boolean(showErrors && draft && !draft.name.trim())
  const commaInvalid = Boolean(showErrors && draft && draft.name.includes(','))
  // Operators open the editor only to read a group's settings and members.
  const locked = saving || readOnly
  const probeGroup = draft?.type !== 'select'
  const selectedInboundCount = inboundsByPanel.reduce(
    (count, [, panelInbounds]) => count + panelInbounds.filter((inbound) => draft?.inboundIds.includes(inbound.id)).length,
    0,
  )
  const enabledGroups = groups.filter((group) => group.enabled).length
  const unreachable = groups.filter((group) => group.enabled && group.profileCount === 0).length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="节点组" description="把不同面板的入站聚合成客户端看到的一组线路，用标签按地区、线路管理。">
        {readOnly ? (
          <Badge variant="outline">
            <EyeIcon data-icon="inline-start" />
            只读
          </Badge>
        ) : (
          <Button onClick={openCreate}>
            <PlusIcon data-icon="inline-start" />
            新建节点组
          </Button>
        )}
      </PageHeader>

      {unreachable > 0 && (
        <Alert>
          <TriangleAlertIcon />
          <AlertTitle>{unreachable} 个节点组没有被任何分流方案选中</AlertTitle>
          <AlertDescription>方案的可用节点组决定用户能用到什么，没被选中的分组不会出现在任何订阅里。</AlertDescription>
          <AlertAction>
            <Button variant="outline" size="sm" render={<a href="#/routing" />} nativeButton={false}>
              <RouteIcon data-icon="inline-start" />
              去分流
            </Button>
          </AlertAction>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>分组列表</CardTitle>
          <CardDescription>
            {loading || (loadError && groups.length === 0) ? '' : `${enabledGroups} 个启用。`}
            每个分组渲染成一个 mihomo 策略组；订阅只包含用户方案覆盖的成员，成员为空的分组会被自动省略。
          </CardDescription>
          <CardAction>
            <RefreshButton loading={loading || refreshing} disabled={saving} onClick={() => void refresh()} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading ? (
            <TableSkeleton
              columns={[
                { label: '分组', width: 'w-28' },
                { label: '标签', className: 'hidden md:table-cell', width: 'w-24' },
                { label: '类型', className: 'hidden lg:table-cell', width: 'w-16' },
                { label: '入站', width: 'w-8' },
                { label: '方案', className: 'hidden lg:table-cell', width: 'w-8' },
                { label: '状态', width: 'w-14' },
                { width: 'w-24' },
              ]}
            />
          ) : loadError && groups.length === 0 ? (
            <div className="px-4">
              <LoadErrorAlert title="分组加载失败" message={loadError} retrying={refreshing} onRetry={() => void refresh()} />
            </div>
          ) : groups.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BoxesIcon />
                </EmptyMedia>
                <EmptyTitle>还没有节点组</EmptyTitle>
                <EmptyDescription>
                  节点组是管理的最小单位：分流方案挑节点组，套餐绑分流方案，用户绑套餐。没有分组就没人能用到任何入站。
                </EmptyDescription>
              </EmptyHeader>
              {!readOnly && (
                <EmptyContent>
                  <Button onClick={openCreate}>
                    <PlusIcon data-icon="inline-start" />
                    新建节点组
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : (
            <div className="flex flex-col gap-3">
              {loadError && (
                <div className="px-4">
                  <Alert variant="warning">
                    <TriangleAlertIcon />
                    <AlertTitle>列表可能不是最新状态</AlertTitle>
                    <AlertDescription>{loadError}</AlertDescription>
                  </Alert>
                </div>
              )}
              {tagIndex.length > 0 && (
                <div className="px-4">
                  <ToggleGroup
                    aria-label="按标签筛选"
                    variant="outline"
                    size="sm"
                    className="flex-wrap"
                    value={[tagFilter ? JSON.stringify([tagFilter.key, tagFilter.value]) : 'all']}
                    onValueChange={(value) => {
                      const next = value[0]
                      if (!next || next === 'all') {
                        setTagFilter(null)
                        return
                      }
                      const [key, tagValue] = JSON.parse(next) as [string, string]
                      setTagFilter({ key, value: tagValue })
                    }}
                  >
                    <ToggleGroupItem value="all">全部</ToggleGroupItem>
                    {tagIndex.map(([key, values]) =>
                      values.map((value) => (
                        <ToggleGroupItem key={`${key}:${value}`} value={JSON.stringify([key, value])}>
                          {key}: {value}
                        </ToggleGroupItem>
                      )),
                    )}
                  </ToggleGroup>
                </div>
              )}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>分组</TableHead>
                    <TableHead className="hidden md:table-cell">标签</TableHead>
                    <TableHead className="hidden lg:table-cell">类型</TableHead>
                    <TableHead>入站</TableHead>
                    <TableHead className="hidden lg:table-cell">方案</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead>
                      <span className="sr-only">操作</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visibleGroups.map((group) => (
                    <TableRow key={group.id}>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <span className="font-medium">
                            {group.emoji && `${group.emoji} `}
                            {group.name}
                          </span>
                          {group.remark && <span className="text-xs text-muted-foreground">{group.remark}</span>}
                        </div>
                      </TableCell>
                      <TableCell className="hidden md:table-cell">
                        {group.tags.length > 0 ? (
                          <span className="flex flex-wrap gap-1">
                            {group.tags.map((tag) => (
                              <Badge key={tag.key} variant="secondary">
                                {tag.key}: {tag.value}
                              </Badge>
                            ))}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="hidden lg:table-cell">
                        <Badge variant="outline">{group.type}</Badge>
                      </TableCell>
                      <TableCell className="tabular-nums">
                        {group.usableInbounds === group.inboundIds.length
                          ? group.inboundIds.length
                          : `${group.usableInbounds}/${group.inboundIds.length}`}
                      </TableCell>
                      <TableCell className="hidden lg:table-cell">
                        {group.profileCount > 0 ? (
                          <span className="tabular-nums text-muted-foreground">{group.profileCount}</span>
                        ) : (
                          <StatusBadge tone="warn">未被选中</StatusBadge>
                        )}
                      </TableCell>
                      <TableCell>
                        {group.enabled ? (
                          <StatusBadge tone="good">启用</StatusBadge>
                        ) : (
                          <StatusBadge tone="idle">停用</StatusBadge>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            aria-label={`${readOnly ? '查看' : '编辑'} ${group.name}`}
                            onClick={() => openEdit(group)}
                          >
                            {readOnly ? <EyeIcon data-icon="inline-start" /> : <PencilIcon data-icon="inline-start" />}
                            <span className="hidden sm:inline">{readOnly ? '查看' : '编辑'}</span>
                          </Button>
                          {!readOnly && (
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              aria-label={`删除 ${group.name}`}
                              onClick={() => setPendingDelete(group)}
                            >
                              <Trash2Icon data-icon="inline-start" />
                              <span className="hidden sm:inline">删除</span>
                            </Button>
                          )}
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
            {loading ? '正在读取分组' : loadError && groups.length === 0 ? '分组统计暂不可用' : `共 ${groups.length} 个分组`}
          </span>
          <span className="text-muted-foreground">
            {loading || (loadError && groups.length === 0) ? '—' : `覆盖 ${new Set(groups.flatMap((group) => group.inboundIds)).size} 个入站`}
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
        <DialogContent className="flex flex-col overflow-hidden sm:max-w-2xl">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader className="shrink-0">
                <DialogTitle>{readOnly ? '查看节点组' : draft.id ? '编辑节点组' : '新建节点组'}</DialogTitle>
                <DialogDescription>分组名会直接作为客户端里的策略组名称显示。</DialogDescription>
              </DialogHeader>
              <FieldGroup className="-mx-4 min-h-0 overflow-y-auto px-4 py-1">
                <FieldGroup className="sm:grid sm:grid-cols-[6rem_1fr]">
                  <Field data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="group-emoji">图标</FieldLabel>
                    <Input
                      id="group-emoji"
                      disabled={locked}
                      placeholder="🇭🇰"
                      value={draft.emoji}
                      onChange={(event) => setDraft({ ...draft, emoji: event.target.value })}
                    />
                  </Field>
                  <Field data-invalid={nameInvalid || commaInvalid || undefined} data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="group-name">名称</FieldLabel>
                    <Input
                      id="group-name"
                      placeholder="香港 IEPL"
                      value={draft.name}
                      required
                      aria-invalid={nameInvalid || commaInvalid || undefined}
                      disabled={locked}
                      onChange={(event) => setDraft({ ...draft, name: event.target.value })}
                    />
                    {nameInvalid && <FieldError>请输入分组名称。</FieldError>}
                    {commaInvalid && <FieldError>名称不能包含英文逗号，分流规则以逗号分隔。</FieldError>}
                  </Field>
                </FieldGroup>

                <FieldSet>
                  <FieldLegend variant="label">标签</FieldLegend>
                  <FieldDescription>只用于筛选和排序，不参与渲染。常用键：{SUGGESTED_TAG_KEYS.join('、')}。</FieldDescription>
                  <FieldGroup className="gap-2">
                    {draft.tags.map((tag, index) => (
                      <div key={index} className="flex items-center gap-2">
                        <Input
                          aria-label={`标签 ${index + 1} 的键`}
                          className="basis-2/5"
                          disabled={locked}
                          list="firex-tag-keys"
                          placeholder="地区"
                          value={tag.key}
                          onChange={(event) => patchTag(index, { key: event.target.value })}
                        />
                        <Input
                          aria-label={`标签 ${index + 1} 的值`}
                          placeholder="香港"
                          disabled={locked}
                          value={tag.value}
                          onChange={(event) => patchTag(index, { value: event.target.value })}
                        />
                        {!readOnly && (
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            aria-label={`删除标签 ${index + 1}`}
                            disabled={saving}
                            onClick={() => setDraft({ ...draft, tags: draft.tags.filter((_, i) => i !== index) })}
                          >
                            <XIcon />
                          </Button>
                        )}
                      </div>
                    ))}
                    {!readOnly && (
                      <div>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={saving}
                          onClick={() => setDraft({ ...draft, tags: [...draft.tags, { key: '', value: '' }] })}
                        >
                          <PlusIcon data-icon="inline-start" />
                          添加标签
                        </Button>
                      </div>
                    )}
                  </FieldGroup>
                  <datalist id="firex-tag-keys">
                    {SUGGESTED_TAG_KEYS.map((key) => (
                      <option key={key} value={key} />
                    ))}
                  </datalist>
                </FieldSet>

                <FieldGroup className="sm:grid sm:grid-cols-3">
                  <Field data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="group-type">选择方式</FieldLabel>
                    <Select
                      items={GROUP_TYPES}
                      value={draft.type}
                      disabled={locked}
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
                  <Field data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="group-multiplier">倍率</FieldLabel>
                    <Input
                      id="group-multiplier"
                      disabled={locked}
                      type="number"
                      min={0}
                      step="0.1"
                      value={draft.multiplier}
                      onChange={(event) => setDraft({ ...draft, multiplier: Number(event.target.value) })}
                    />
                    <FieldDescription>仅作展示记录，不参与计费。</FieldDescription>
                  </Field>
                  <Field data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="group-sort">排序</FieldLabel>
                    <Input
                      id="group-sort"
                      disabled={locked}
                      type="number"
                      value={draft.sortOrder}
                      onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })}
                    />
                    <FieldDescription>数值小的排在前面。</FieldDescription>
                  </Field>
                </FieldGroup>

                {probeGroup && (
                  <FieldGroup className="sm:grid sm:grid-cols-3">
                    <Field className="sm:col-span-3" data-disabled={locked || undefined}>
                      <FieldLabel htmlFor="group-test-url">测速地址</FieldLabel>
                      <Input
                        id="group-test-url"
                        disabled={locked}
                        placeholder="https://www.gstatic.com/generate_204"
                        value={draft.testUrl}
                        onChange={(event) => setDraft({ ...draft, testUrl: event.target.value })}
                      />
                      <FieldDescription>留空则使用默认地址。</FieldDescription>
                    </Field>
                    <Field data-disabled={locked || undefined}>
                      <FieldLabel htmlFor="group-interval">测速间隔 (秒)</FieldLabel>
                      <Input
                        id="group-interval"
                        disabled={locked}
                        type="number"
                        min={0}
                        value={draft.interval}
                        onChange={(event) => setDraft({ ...draft, interval: Number(event.target.value) })}
                      />
                    </Field>
                    {draft.type === 'url-test' && (
                      <Field data-disabled={locked || undefined}>
                        <FieldLabel htmlFor="group-tolerance">容差 (毫秒)</FieldLabel>
                        <Input
                          id="group-tolerance"
                          disabled={locked}
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
                  <FieldLegend variant="label">包含入站（已选 {selectedInboundCount} 个）</FieldLegend>
                  <FieldDescription>一个入站可以同时属于多个分组。</FieldDescription>
                  <ScrollArea className="h-64 rounded-lg border">
                    {inboundsByPanel.length === 0 ? (
                      <Empty>
                        <EmptyHeader>
                          <EmptyMedia variant="icon">
                            <BoxesIcon />
                          </EmptyMedia>
                          <EmptyTitle>还没有可用入站</EmptyTitle>
                          <EmptyDescription>先连接面板、完成入站发现并启用入站。</EmptyDescription>
                        </EmptyHeader>
                      </Empty>
                    ) : (
                      <FieldGroup className="gap-4 p-3">
                        {inboundsByPanel.map(([panel, group]) => {
                          const selectedCount = group.filter((inbound) => draft.inboundIds.includes(inbound.id)).length
                          const allSelected = selectedCount === group.length
                          return (
                            <FieldSet key={panel}>
                              <FieldLegend className="sr-only">{panel} 的入站</FieldLegend>
                              <Field orientation="horizontal">
                                <Checkbox
                                  id={`group-panel-${panel}`}
                                  checked={allSelected}
                                  indeterminate={selectedCount > 0 && !allSelected}
                                  disabled={locked}
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
                                {group.map((inbound) => (
                                  <Field key={inbound.id} orientation="horizontal">
                                    <Checkbox
                                      id={`group-inbound-${inbound.id}`}
                                      checked={draft.inboundIds.includes(inbound.id)}
                                      disabled={locked}
                                      onCheckedChange={() => toggleInbound(inbound.id)}
                                    />
                                    <FieldLabel htmlFor={`group-inbound-${inbound.id}`}>
                                      <span>{inboundTitle(inbound)}</span>
                                      <CodeText>
                                        {inbound.inboundTag || inbound.protocol}:{inbound.port}
                                      </CodeText>
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

                <Field orientation="horizontal" data-disabled={locked || undefined}>
                  <FieldLabel htmlFor="group-enabled">启用分组</FieldLabel>
                  <Switch
                    id="group-enabled"
                    disabled={locked}
                    checked={draft.enabled}
                    onCheckedChange={(enabled) => setDraft({ ...draft, enabled })}
                  />
                </Field>

                <Field data-disabled={locked || undefined}>
                  <FieldLabel htmlFor="group-remark">备注</FieldLabel>
                  <Input
                    id="group-remark"
                    disabled={locked}
                    value={draft.remark}
                    onChange={(event) => setDraft({ ...draft, remark: event.target.value })}
                  />
                </Field>
              </FieldGroup>

              <DialogFooter className="shrink-0">
                <Button type="button" variant="outline" disabled={saving} onClick={() => setDraft(null)}>
                  {readOnly ? '关闭' : '取消'}
                </Button>
                {!readOnly && (
                  <Button type="submit" disabled={saving}>
                    {saving && <Spinner data-icon="inline-start" />}
                    {saving ? '保存中…' : '保存分组'}
                  </Button>
                )}
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除节点组「${pendingDelete?.name ?? ''}」？`}
        description="入站本身不受影响。分流方案里对它的选中，以及出口里引用它的成员，都会一并移除。"
        confirmLabel="删除分组"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

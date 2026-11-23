import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  EyeIcon,
  ListIcon,
  MoreHorizontalIcon,
  PencilIcon,
  PlusIcon,
  PowerIcon,
  RefreshCwIcon,
  RssIcon,
  SearchXIcon,
  Trash2Icon,
  TriangleAlertIcon,
  XIcon,
} from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { ExternalSubscription, ExternalSubscriptionDetail } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { InfoPopover } from '@/components/info-popover'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { NumberInput } from '@/components/number-input'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { SearchInput, TableToolbar } from '@/components/table-toolbar'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useDirtyDialog } from '@/hooks/use-unsaved-guard'
import { errorMessage, formatTime } from '@/lib/format'
import { useSession } from '@/lib/session'

type Draft = {
  id?: number
  name: string
  url: string
  enabled: boolean
  updateInterval: number
  remark: string
}

type MutationResponse = {
  subscription: ExternalSubscription
  refreshError: string
}

type StatusFilter = 'all' | 'ok' | 'warning' | 'disabled'

const STATUS_FILTER_ITEMS: Array<{ value: StatusFilter; label: string }> = [
  { value: 'all', label: '全部状态' },
  { value: 'ok', label: '正常' },
  { value: 'warning', label: '有告警或失败' },
  { value: 'disabled', label: '已停用' },
]

const emptyDraft: Draft = {
  name: '',
  url: '',
  enabled: true,
  updateInterval: 3600,
  remark: '',
}

function validHTTPURL(raw: string) {
  try {
    const url = new URL(raw)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function intervalLabel(seconds: number) {
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

export function ExternalSubscriptionsPage() {
  const [items, setItems] = useState<ExternalSubscription[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [refreshingAll, setRefreshingAll] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [draft, setDraft] = useState<Draft | null>(null)
  const [initialDraft, setInitialDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [refreshingIDs, setRefreshingIDs] = useState<Set<number>>(new Set())
  const [toggling, setToggling] = useState<Set<number>>(new Set())
  const [pendingDelete, setPendingDelete] = useState<ExternalSubscription | null>(null)
  const [detail, setDetail] = useState<ExternalSubscriptionDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const { readOnly } = useSession()

  const load = useCallback(async () => {
    try {
      setItems(await api.get<ExternalSubscription[]>('/external-subscriptions'))
      setLoadError(null)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '外部订阅加载失败'))
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

  const draftDirty = Boolean(!readOnly && draft && initialDraft && JSON.stringify(draft) !== JSON.stringify(initialDraft))
  const closeDraft = useCallback(() => {
    setDraft(null)
    setShowErrors(false)
  }, [])
  const onDraftOpenChange = useDirtyDialog(draftDirty, saving, closeDraft)

  const openCreate = () => {
    const next = { ...emptyDraft }
    setShowErrors(false)
    setInitialDraft(next)
    setDraft(next)
  }

  const openEdit = (item: ExternalSubscription) => {
    const next: Draft = {
      id: item.id,
      name: item.name,
      url: item.url,
      enabled: item.enabled,
      updateInterval: item.updateInterval,
      remark: item.remark,
    }
    setShowErrors(false)
    setInitialDraft(next)
    setDraft(next)
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || saving) return
    setShowErrors(true)
    if (!draft.name.trim() || !validHTTPURL(draft.url.trim()) || draft.updateInterval < 60) return
    setSaving(true)
    const body = { ...draft, name: draft.name.trim(), url: draft.url.trim() }
    try {
      const result = draft.id
        ? await api.put<MutationResponse>(`/external-subscriptions/${draft.id}`, body)
        : await api.post<MutationResponse>('/external-subscriptions', body)
      if (result.refreshError) toast.warning(`已保存，但本次刷新失败：${result.refreshError}`)
      else toast.success(`已保存并解析 ${result.subscription.nodeCount} 个节点`)
      setDraft(null)
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const toggleEnabled = async (item: ExternalSubscription) => {
    setToggling((current) => new Set(current).add(item.id))
    try {
      const result = await api.put<MutationResponse>(`/external-subscriptions/${item.id}`, {
        name: item.name,
        url: item.url,
        enabled: !item.enabled,
        updateInterval: item.updateInterval,
        remark: item.remark,
      })
      setItems((current) => current.map((row) => (row.id === item.id ? result.subscription : row)))
      toast.success(item.enabled ? `已停用「${item.name}」` : `已启用「${item.name}」`)
    } catch (err) {
      toast.error(errorMessage(err, '切换状态失败'))
    } finally {
      setToggling((current) => {
        const next = new Set(current)
        next.delete(item.id)
        return next
      })
    }
  }

  const refreshOne = async (item: ExternalSubscription) => {
    if (refreshingIDs.has(item.id)) return
    setRefreshingIDs((current) => new Set(current).add(item.id))
    try {
      const result = await api.post<MutationResponse>(`/external-subscriptions/${item.id}/refresh`)
      setItems((current) => current.map((row) => (row.id === item.id ? result.subscription : row)))
      if (result.refreshError) toast.warning(`刷新失败，已保留旧节点：${result.refreshError}`)
      else toast.success(`「${item.name}」已更新 ${result.subscription.nodeCount} 个节点`)
    } catch (err) {
      toast.error(errorMessage(err, `刷新「${item.name}」失败`))
    } finally {
      setRefreshingIDs((current) => {
        const next = new Set(current)
        next.delete(item.id)
        return next
      })
    }
  }

  const refreshAll = async () => {
    const targets = items.filter((item) => item.enabled)
    if (targets.length === 0) {
      toast.info('没有已启用的外部订阅')
      return
    }
    setRefreshingAll(true)
    setRefreshingIDs((current) => {
      const next = new Set(current)
      targets.forEach((item) => next.add(item.id))
      return next
    })
    let ok = 0
    let failed = 0
    try {
      await Promise.all(
        targets.map(async (item) => {
          try {
            const result = await api.post<MutationResponse>(`/external-subscriptions/${item.id}/refresh`)
            setItems((current) => current.map((row) => (row.id === item.id ? result.subscription : row)))
            if (result.refreshError) failed++
            else ok++
          } catch {
            failed++
          } finally {
            setRefreshingIDs((current) => {
              const next = new Set(current)
              next.delete(item.id)
              return next
            })
          }
        }),
      )
      if (failed === 0) toast.success(`全部刷新完成，共更新 ${ok} 个订阅`)
      else toast.warning(`刷新完成：${ok} 个成功，${failed} 个有告警或失败`)
    } finally {
      setRefreshingAll(false)
    }
  }

  const remove = async (item: ExternalSubscription) => {
    try {
      await api.del(`/external-subscriptions/${item.id}`)
      toast.success(item.groupCount > 0 ? `已删除，并解除 ${item.groupCount} 处节点组关联` : '外部订阅已删除')
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  const viewNodes = async (item: ExternalSubscription) => {
    setDetail({ subscription: item, nodes: [] })
    setDetailLoading(true)
    try {
      setDetail(await api.get<ExternalSubscriptionDetail>(`/external-subscriptions/${item.id}`))
    } catch (err) {
      toast.error(errorMessage(err, '节点明细加载失败'))
      setDetail(null)
    } finally {
      setDetailLoading(false)
    }
  }

  const visibleItems = useMemo(() => {
    const keyword = query.trim().toLocaleLowerCase('zh-CN')
    return items.filter((item) => {
      if (statusFilter === 'ok' && (!item.enabled || Boolean(item.lastError) || item.lastUpdateAt === 0)) return false
      if (statusFilter === 'warning' && (!item.enabled || !item.lastError)) return false
      if (statusFilter === 'disabled' && item.enabled) return false
      if (!keyword) return true
      return [item.name, item.url, item.remark].some((text) => text.toLocaleLowerCase('zh-CN').includes(keyword))
    })
  }, [items, query, statusFilter])

  const hasFilters = query.trim() !== '' || statusFilter !== 'all'
  const locked = saving || readOnly
  const nameInvalid = Boolean(showErrors && draft && !draft.name.trim())
  const urlInvalid = Boolean(showErrors && draft && !validHTTPURL(draft.url.trim()))
  const enabledCount = items.filter((item) => item.enabled).length
  const nodeCount = items.reduce((sum, item) => sum + (item.enabled ? item.nodeCount : 0), 0)

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="外部订阅" description="集中抓取并缓存机场或其他服务的节点，再把一个或多个订阅加入节点组。">
        {readOnly ? (
          <Badge variant="outline">
            <EyeIcon data-icon="inline-start" />
            只读
          </Badge>
        ) : (
          <>
            {items.length > 0 && (
              <Button type="button" variant="outline" disabled={refreshingAll || saving} onClick={() => void refreshAll()}>
                {refreshingAll ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
                {refreshingAll ? '刷新中…' : '全部刷新'}
              </Button>
            )}
            <Button onClick={openCreate}>
              <PlusIcon data-icon="inline-start" />
              添加订阅
            </Button>
          </>
        )}
      </PageHeader>

      <Alert>
        <RssIcon />
        <AlertTitle>支持分享链接与 mihomo / Clash YAML</AlertTitle>
        <AlertDescription>
          默认每小时自动更新。刷新失败会保留上一份可用节点；用户只收到节点，不会看到原始订阅地址。
        </AlertDescription>
      </Alert>

      <Card>
        <CardHeader>
          <CardTitle>订阅列表</CardTitle>
          <CardDescription>分享链接可用于全部输出格式；仅存在于 YAML 的节点会用于 mihomo 输出。</CardDescription>
          <CardAction>
            <RefreshButton loading={loading || refreshing} disabled={saving || refreshingAll} onClick={() => void refresh()} />
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 px-0">
          {items.length > 0 && (
            <TableToolbar>
              <SearchInput
                value={query}
                onChange={setQuery}
                placeholder="按名称、地址或备注搜索…"
                ariaLabel="搜索外部订阅"
              />
              <Select
                items={STATUS_FILTER_ITEMS}
                value={statusFilter}
                onValueChange={(value) => setStatusFilter((value as StatusFilter) || 'all')}
              >
                <SelectTrigger aria-label="按状态筛选" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {STATUS_FILTER_ITEMS.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {hasFilters && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setQuery('')
                    setStatusFilter('all')
                  }}
                >
                  <XIcon data-icon="inline-start" />
                  清除筛选
                </Button>
              )}
            </TableToolbar>
          )}
          {loading ? (
            <TableSkeleton
              columns={[
                { label: '订阅', width: 'w-40' },
                { label: '节点', width: 'w-10' },
                { label: '节点组', className: 'hidden md:table-cell', width: 'w-10' },
                { label: '更新周期', className: 'hidden lg:table-cell', width: 'w-16' },
                { label: '最近成功', className: 'hidden lg:table-cell', width: 'w-28' },
                { label: '状态', width: 'w-16' },
                { width: 'w-48' },
              ]}
            />
          ) : loadError && items.length === 0 ? (
            <div className="px-4">
              <LoadErrorAlert title="订阅加载失败" message={loadError} retrying={refreshing} onRetry={() => void refresh()} />
            </div>
          ) : items.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <RssIcon />
                </EmptyMedia>
                <EmptyTitle>还没有外部订阅</EmptyTitle>
                <EmptyDescription>添加订阅后会立即抓取解析，之后可在节点组编辑器中将它加入一个或多个分组。</EmptyDescription>
              </EmptyHeader>
              {!readOnly && (
                <EmptyContent>
                  <Button onClick={openCreate}>
                    <PlusIcon data-icon="inline-start" />
                    添加订阅
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : visibleItems.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchXIcon />
                </EmptyMedia>
                <EmptyTitle>没有匹配的外部订阅</EmptyTitle>
                <EmptyDescription>换一个关键词或状态筛选条件试试。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setQuery('')
                    setStatusFilter('all')
                  }}
                >
                  <XIcon data-icon="inline-start" />
                  清除筛选
                </Button>
              </EmptyContent>
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
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>订阅</TableHead>
                    <TableHead>节点</TableHead>
                    <TableHead className="hidden md:table-cell">节点组</TableHead>
                    <TableHead className="hidden lg:table-cell">更新周期</TableHead>
                    <TableHead className="hidden lg:table-cell">最近成功</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead><span className="sr-only">操作</span></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visibleItems.map((item) => {
                    const rowRefreshing = refreshingIDs.has(item.id)
                    const rowToggling = toggling.has(item.id)
                    return (
                      <TableRow key={item.id}>
                        <TableCell>
                          <div className="flex min-w-0 flex-col gap-1">
                            <span className="font-medium">{item.name}</span>
                            <span className="max-w-64 truncate text-xs text-muted-foreground" title={item.url}>{item.url}</span>
                            {item.remark && <span className="text-xs text-muted-foreground">{item.remark}</span>}
                          </div>
                        </TableCell>
                        <TableCell>
                          <Button type="button" variant="link" size="sm" className="px-0 tabular-nums" onClick={() => void viewNodes(item)}>
                            {item.nodeCount}
                          </Button>
                        </TableCell>
                        <TableCell className="hidden tabular-nums md:table-cell">{item.groupCount}</TableCell>
                        <TableCell className="hidden lg:table-cell">{intervalLabel(item.updateInterval)}</TableCell>
                        <TableCell className="hidden text-muted-foreground lg:table-cell">{formatTime(item.lastUpdateAt)}</TableCell>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            {!readOnly && (
                              <Switch
                                size="sm"
                                aria-label={`${item.enabled ? '停用' : '启用'} ${item.name}`}
                                checked={item.enabled}
                                disabled={rowToggling || rowRefreshing}
                                onCheckedChange={() => void toggleEnabled(item)}
                              />
                            )}
                            {!item.enabled ? (
                              <StatusBadge tone="idle">停用</StatusBadge>
                            ) : item.lastError ? (
                              <InfoPopover
                                label={`查看 ${item.name} 的刷新错误`}
                                title={`${item.name} 刷新错误`}
                                content={item.lastError}
                                copyValue={item.lastError}
                              >
                                <StatusBadge tone={item.nodeCount > 0 ? 'warn' : 'bad'}>
                                  {item.nodeCount > 0 ? '有警告' : '刷新失败'}
                                </StatusBadge>
                              </InfoPopover>
                            ) : item.lastUpdateAt > 0 ? (
                              <StatusBadge tone="good">正常</StatusBadge>
                            ) : (
                              <StatusBadge tone="idle">待更新</StatusBadge>
                            )}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex justify-end gap-1">
                            {!readOnly && (
                              <Button type="button" variant="outline" size="sm" disabled={rowRefreshing} aria-label={`刷新 ${item.name}`} onClick={() => void refreshOne(item)}>
                                {rowRefreshing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
                                <span className="hidden sm:inline">刷新</span>
                              </Button>
                            )}
                            <Button type="button" variant="outline" size="sm" aria-label={`查看 ${item.name} 的节点`} onClick={() => void viewNodes(item)}>
                              <ListIcon data-icon="inline-start" />
                              <span className="hidden sm:inline">节点</span>
                            </Button>
                            {readOnly ? (
                              <Button type="button" variant="outline" size="sm" aria-label={`查看 ${item.name}`} onClick={() => openEdit(item)}>
                                <EyeIcon data-icon="inline-start" />
                                <span className="hidden sm:inline">查看</span>
                              </Button>
                            ) : (
                              <DropdownMenu>
                                <DropdownMenuTrigger
                                  render={
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="icon-sm"
                                      aria-label={`${item.name} 的更多操作`}
                                    />
                                  }
                                >
                                  <MoreHorizontalIcon />
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="end">
                                  <DropdownMenuGroup>
                                    <DropdownMenuItem onClick={() => openEdit(item)}>
                                      <PencilIcon />
                                      编辑
                                    </DropdownMenuItem>
                                    <DropdownMenuItem onClick={() => void toggleEnabled(item)}>
                                      <PowerIcon />
                                      {item.enabled ? '停用订阅' : '启用订阅'}
                                    </DropdownMenuItem>
                                  </DropdownMenuGroup>
                                  <DropdownMenuSeparator />
                                  <DropdownMenuGroup>
                                    <DropdownMenuItem variant="destructive" onClick={() => setPendingDelete(item)}>
                                      <Trash2Icon />
                                      删除订阅
                                    </DropdownMenuItem>
                                  </DropdownMenuGroup>
                                </DropdownMenuContent>
                              </DropdownMenu>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
        <CardFooter className="flex-wrap justify-between gap-2">
          <span className="text-muted-foreground">共 {items.length} 个订阅，{enabledCount} 个启用</span>
          <span className="text-muted-foreground">启用订阅缓存 {nodeCount} 个节点</span>
        </CardFooter>
      </Card>

      <Dialog open={draft !== null} onOpenChange={onDraftOpenChange}>
        <DialogContent className="flex flex-col overflow-hidden sm:max-w-xl">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{readOnly ? '查看外部订阅' : draft.id ? '编辑外部订阅' : '添加外部订阅'}</DialogTitle>
                <DialogDescription>保存后会立即刷新一次；自动更新只处理启用的订阅。</DialogDescription>
              </DialogHeader>
              <DialogBody>
                <FieldGroup>
                  <Field data-invalid={nameInvalid || undefined} data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="external-name">名称</FieldLabel>
                    <Input id="external-name" required disabled={locked} placeholder="机场 A" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
                    {nameInvalid && <FieldError>请输入订阅名称。</FieldError>}
                  </Field>
                  <Field data-invalid={urlInvalid || undefined} data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="external-url">订阅地址</FieldLabel>
                    <Input id="external-url" type="url" required disabled={locked} placeholder="https://example.com/sub?token=…" value={draft.url} onChange={(event) => setDraft({ ...draft, url: event.target.value })} />
                    <FieldDescription>支持 Base64、纯文本分享链接，以及包含 proxies 的 mihomo / Clash YAML。</FieldDescription>
                    {urlInvalid && <FieldError>请输入有效的 HTTP(S) 订阅地址。</FieldError>}
                  </Field>
                  <Field data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="external-interval">更新周期（秒）</FieldLabel>
                    <NumberInput id="external-interval" min={60} step={60} integer required disabled={locked} value={draft.updateInterval} onValueChange={(updateInterval) => setDraft({ ...draft, updateInterval })} />
                    <FieldDescription>默认 3600 秒（1 小时），最短 60 秒。</FieldDescription>
                  </Field>
                  <Field orientation="horizontal" data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="external-enabled">启用订阅</FieldLabel>
                    <Switch id="external-enabled" disabled={locked} checked={draft.enabled} onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} />
                  </Field>
                  <Field data-disabled={locked || undefined}>
                    <FieldLabel htmlFor="external-remark">备注</FieldLabel>
                    <Input id="external-remark" disabled={locked} value={draft.remark} onChange={(event) => setDraft({ ...draft, remark: event.target.value })} />
                  </Field>
                </FieldGroup>
              </DialogBody>
              <DialogFooter>
                <Button type="button" variant="outline" disabled={saving} onClick={closeDraft}>{readOnly ? '关闭' : '取消'}</Button>
                {!readOnly && (
                  <Button type="submit" disabled={saving}>
                    {saving && <Spinner data-icon="inline-start" />}
                    {saving ? '保存并刷新中…' : '保存并刷新'}
                  </Button>
                )}
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={detail !== null} onOpenChange={(open) => !open && !detailLoading && setDetail(null)}>
        <DialogContent className="flex flex-col overflow-hidden sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{detail?.subscription.name} · 节点明细</DialogTitle>
            <DialogDescription>这里展示最近一次成功解析的缓存，不会实时请求外部站点。</DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-4">
            {detail?.subscription.lastError && (
              <Alert variant="warning">
                <TriangleAlertIcon />
                <AlertTitle>最近刷新有警告</AlertTitle>
                <AlertDescription>{detail.subscription.lastError}</AlertDescription>
              </Alert>
            )}
            {detailLoading ? (
              <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground"><Spinner />正在读取节点…</div>
            ) : detail?.nodes.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>暂无可用节点</EmptyTitle>
                  <EmptyDescription>请检查订阅内容或最近刷新错误。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow><TableHead>名称</TableHead><TableHead>协议</TableHead><TableHead>服务器</TableHead><TableHead>输出</TableHead></TableRow>
                </TableHeader>
                <TableBody>
                  {detail?.nodes.map((node, index) => (
                    <TableRow key={`${node.name}-${index}`}>
                      <TableCell className="font-medium">{node.name}</TableCell>
                      <TableCell><Badge variant="outline">{node.protocol}</Badge></TableCell>
                      <TableCell className="text-muted-foreground">{node.server}{node.port ? `:${node.port}` : ''}</TableCell>
                      <TableCell>{node.hasShareLink ? '全部格式' : '仅 mihomo'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={detailLoading} onClick={() => setDetail(null)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除外部订阅「${pendingDelete?.name ?? ''}」？`}
        description={pendingDelete?.groupCount ? `它当前被 ${pendingDelete.groupCount} 个节点组引用。删除会解除这些关联，对应节点也会从用户订阅中消失。` : '删除后缓存节点也会被移除，此操作不可撤销。'}
        confirmLabel="删除订阅"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

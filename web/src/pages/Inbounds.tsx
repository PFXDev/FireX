import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  BoxesIcon,
  ChevronDownIcon,
  EyeIcon,
  FileCogIcon,
  FilterIcon,
  LayersIcon,
  ListTreeIcon,
  MoreHorizontalIcon,
  PencilIcon,
  SearchXIcon,
  ServerIcon,
  ShieldCheckIcon,
  Trash2Icon,
  XIcon,
} from 'lucide-react'

import { api } from '@/api'
import type { Inbound, NodeGroup } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { InboundDetails } from '@/components/inbound-details'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { NumberInput } from '@/components/number-input'
import { PageHeader } from '@/components/page-header'
import { ConfigStateBadge, StatusBadge } from '@/components/status-badge'
import { BulkActionBar, SearchInput, SortableHead, TableToolbar, type SortDirection } from '@/components/table-toolbar'
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
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useHashLocation } from '@/hooks/use-hash-route'
import { useDirtyDialog } from '@/hooks/use-unsaved-guard'
import { errorMessage } from '@/lib/format'
import { inboundLabel, inboundTitle } from '@/lib/inbound'
import { useSession } from '@/lib/session'
import { reportSyncError } from '@/lib/sync'

type Draft = {
  id: number
  name: string
  emoji: string
  sortOrder: number
  enabled: boolean
  udp: boolean
  vision: boolean | null
  xhttpTuning: boolean
  publicAddress: string
  publicPort: number
}

type LoadState = 'loading' | 'ready' | 'error'
type PendingAction = 'bulk-enable' | 'bulk-disable' | 'bulk-group' | 'bulk-vision' | 'bulk-remove' | 'save' | 'remove'
type StatusFilter = 'all' | 'enabled' | 'disabled' | 'ungrouped' | 'missing' | 'attention'
type SortKey = 'name' | 'panel' | 'groupCount' | 'status'

const STATUS_FILTER_ITEMS: Array<{ value: StatusFilter; label: string }> = [
  { value: 'all', label: '全部状态' },
  { value: 'enabled', label: '已启用' },
  { value: 'disabled', label: '已停用' },
  { value: 'ungrouped', label: '未分组' },
  { value: 'missing', label: '已失联' },
  { value: 'attention', label: '配置待处理' },
]

const VALID_STATUS_FILTERS = new Set<StatusFilter>(STATUS_FILTER_ITEMS.map((item) => item.value))
const visionLabels = { preserve: '保留现有流控', on: '开启 Vision', off: '关闭 Vision' }

export function InboundsPage() {
  const { readOnly } = useSession()
  const { params } = useHashLocation()
  const initialStatus = params.get('status') as StatusFilter | null

  const [inbounds, setInbounds] = useState<Inbound[]>([])
  const [nodeGroups, setNodeGroups] = useState<NodeGroup[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [query, setQuery] = useState('')
  const [panelFilter, setPanelFilter] = useState<string>('all')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>(() =>
    initialStatus && VALID_STATUS_FILTERS.has(initialStatus) ? initialStatus : 'all',
  )
  const [protocolFilter, setProtocolFilter] = useState<string>('all')
  const [sortKey, setSortKey] = useState<SortKey | null>(null)
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [draft, setDraft] = useState<Draft | null>(null)
  const [initialDraft, setInitialDraft] = useState<Draft | null>(null)
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [toggling, setToggling] = useState<Set<number>>(new Set())
  const [pendingDelete, setPendingDelete] = useState<Inbound | null>(null)
  const [pendingBulkRemove, setPendingBulkRemove] = useState(false)
  const [detailID, setDetailID] = useState<number | null>(null)

  useEffect(() => {
    const next = params.get('status') as StatusFilter | null
    if (next && VALID_STATUS_FILTERS.has(next)) setStatusFilter(next)
  }, [params])

  const detailInbound = inbounds.find((inbound) => inbound.id === detailID) ?? null
  const editingInbound = inbounds.find((inbound) => inbound.id === draft?.id)
  const draftDirty = Boolean(draft && initialDraft && JSON.stringify(draft) !== JSON.stringify(initialDraft))
  const closeDraft = useCallback(() => setDraft(null), [])
  const onDraftOpenChange = useDirtyDialog(draftDirty, pendingAction === 'save', closeDraft)

  const edit = (inbound: Inbound) => {
    setDetailID(null)
    const next: Draft = {
      id: inbound.id,
      name: inbound.name,
      emoji: inbound.emoji,
      sortOrder: inbound.sortOrder,
      enabled: inbound.enabled,
      udp: inbound.udp,
      vision: inbound.vision,
      xhttpTuning: inbound.xhttpTuning,
      publicAddress: inbound.publicAddress,
      publicPort: inbound.publicPort,
    }
    setInitialDraft(next)
    setDraft(next)
  }

  const load = useCallback(async () => {
    setLoadState('loading')
    setLoadError('')
    setSelected(new Set())
    try {
      const [nextInbounds, nextGroups] = await Promise.all([
        api.get<Inbound[]>('/inbounds'),
        api.get<NodeGroup[]>('/node-groups').catch(() => [] as NodeGroup[]),
      ])
      setInbounds(nextInbounds)
      setNodeGroups(nextGroups)
      setLoadState('ready')
    } catch (err) {
      setLoadError(errorMessage(err, '无法加载入站，请稍后重试。'))
      setLoadState('error')
    }
  }, [])

  const refreshAfterMutation = useCallback(async () => {
    try {
      const [nextInbounds, nextGroups] = await Promise.all([
        api.get<Inbound[]>('/inbounds'),
        api.get<NodeGroup[]>('/node-groups').catch(() => [] as NodeGroup[]),
      ])
      setInbounds(nextInbounds)
      setNodeGroups(nextGroups)
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

  const panelOptions = useMemo(() => {
    const names = [...new Set(inbounds.map((row) => row.panelName).filter(Boolean))].sort((a, b) =>
      a.localeCompare(b, 'zh-CN'),
    )
    return [{ value: 'all', label: '全部面板' }, ...names.map((name) => ({ value: name, label: name }))]
  }, [inbounds])

  const protocolOptions = useMemo(() => {
    const protos = [...new Set(inbounds.map((row) => row.protocol.toUpperCase()).filter(Boolean))].sort()
    return [{ value: 'all', label: '全部协议' }, ...protos.map((p) => ({ value: p, label: p }))]
  }, [inbounds])

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((current) => (current === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  const visibleInbounds = useMemo(() => {
    const keyword = query.trim().toLocaleLowerCase('zh-CN')
    const filtered = inbounds.filter((inbound) => {
      if (panelFilter !== 'all' && inbound.panelName !== panelFilter) return false
      if (protocolFilter !== 'all' && inbound.protocol.toUpperCase() !== protocolFilter) return false
      switch (statusFilter) {
        case 'enabled':
          if (!inbound.enabled || inbound.missing) return false
          break
        case 'disabled':
          if (inbound.enabled || inbound.missing) return false
          break
        case 'ungrouped':
          if (inbound.groupCount > 0 || inbound.missing) return false
          break
        case 'missing':
          if (!inbound.missing) return false
          break
        case 'attention':
          if (!['pending', 'drifted', 'missing'].includes(inbound.configState)) return false
          break
      }
      if (!keyword) return true
      return [
        inboundLabel(inbound),
        inboundTitle(inbound),
        inbound.panelName,
        inbound.inboundTag,
        inbound.publicAddress,
        inbound.shareAddress,
        String(inbound.port),
      ].some((value) => value.toLocaleLowerCase('zh-CN').includes(keyword))
    })
    if (!sortKey) return filtered
    const factor = sortDir === 'asc' ? 1 : -1
    return [...filtered].sort((a, b) => {
      switch (sortKey) {
        case 'name':
          return inboundLabel(a).localeCompare(inboundLabel(b), 'zh-CN') * factor
        case 'panel':
          return (a.panelName.localeCompare(b.panelName, 'zh-CN') || a.remoteId - b.remoteId) * factor
        case 'groupCount':
          return (a.groupCount - b.groupCount) * factor
        case 'status': {
          const score = (n: Inbound) => (n.missing ? 3 : !n.remoteEnabled ? 2 : n.enabled ? 0 : 1)
          return (score(a) - score(b)) * factor
        }
      }
    })
  }, [inbounds, query, panelFilter, statusFilter, protocolFilter, sortKey, sortDir])

  const hasFilters = query.trim() !== '' || panelFilter !== 'all' || statusFilter !== 'all' || protocolFilter !== 'all'
  const clearFilters = () => {
    setQuery('')
    setPanelFilter('all')
    setStatusFilter('all')
    setProtocolFilter('all')
  }

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleInboundEnabled = async (inbound: Inbound) => {
    setToggling((current) => new Set(current).add(inbound.id))
    try {
      const result = await api.post<{ syncError: string }>('/inbounds/bulk', {
        ids: [inbound.id],
        enabled: !inbound.enabled,
      })
      toast.success(inbound.enabled ? `已停用 ${inboundLabel(inbound)}` : `已启用 ${inboundLabel(inbound)}`)
      reportSyncError(result.syncError)
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '切换状态失败'))
    } finally {
      setToggling((current) => {
        const next = new Set(current)
        next.delete(inbound.id)
        return next
      })
    }
  }

  const runBulk = async (
    action: PendingAction,
    body: Record<string, unknown>,
    successMessage: string,
  ) => {
    if (selected.size === 0) return
    setPendingAction(action)
    try {
      const result = await api.post<{ syncError: string }>('/inbounds/bulk', {
        ids: [...selected],
        ...body,
      })
      toast.success(successMessage)
      reportSyncError(result.syncError)
      setSelected(new Set())
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '批量操作失败'))
      if (action === 'bulk-remove') throw err
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

  const visibleSelectedCount = visibleInbounds.filter((n) => selected.has(n.id)).length
  const allVisibleSelected = visibleInbounds.length > 0 && visibleSelectedCount === visibleInbounds.length
  const selectedRows = useMemo(() => inbounds.filter((n) => selected.has(n.id)), [inbounds, selected])
  const selectedMissingOnly = selectedRows.length > 0 && selectedRows.every((n) => n.missing)
  const isBulkPending = pendingAction?.startsWith('bulk-') ?? false

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="入站"
        description="点击名称查看完整参数，编辑入站设置 Vision 流控。新入站默认停用，启用并加入节点组后即可下发给用户。"
      >
        {readOnly && (
          <Badge variant="outline">
            <EyeIcon data-icon="inline-start" />
            只读
          </Badge>
        )}
      </PageHeader>

      {orphanCount > 0 && (
        <Alert>
          <BoxesIcon />
          <AlertTitle>{orphanCount} 个已启用的入站不属于任何节点组</AlertTitle>
          <AlertDescription>
            用户能用到哪些入站只由分流方案里的节点组决定，不在任何分组里的入站不会出现在任何订阅中。
          </AlertDescription>
          <AlertAction className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setStatusFilter('ungrouped')}>
              <FilterIcon data-icon="inline-start" />
              筛选未分组
            </Button>
            {!readOnly && (
              <Button variant="outline" size="sm" render={<a href="#/node-groups" />} nativeButton={false}>
                <BoxesIcon data-icon="inline-start" />
                去分组
              </Button>
            )}
          </AlertAction>
        </Alert>
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
        <CardContent className="flex flex-col gap-4 px-0">
          {inbounds.length > 0 && (
            <TableToolbar>
              <SearchInput
                value={query}
                onChange={setQuery}
                placeholder="按名称、端口、面板或地址搜索…"
                ariaLabel="搜索入站"
              />
              <Select
                items={panelOptions}
                value={panelFilter}
                onValueChange={(value) => setPanelFilter(String(value || 'all'))}
              >
                <SelectTrigger aria-label="按面板筛选" className="w-36">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {panelOptions.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Select
                items={STATUS_FILTER_ITEMS}
                value={statusFilter}
                onValueChange={(value) => setStatusFilter((value as StatusFilter) || 'all')}
              >
                <SelectTrigger aria-label="按状态筛选" className="w-32">
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
              <Select
                items={protocolOptions}
                value={protocolFilter}
                onValueChange={(value) => setProtocolFilter(String(value || 'all'))}
              >
                <SelectTrigger aria-label="按协议筛选" className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {protocolOptions.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {hasFilters && (
                <Button type="button" variant="ghost" size="sm" onClick={clearFilters}>
                  <XIcon data-icon="inline-start" />
                  清除筛选
                </Button>
              )}
            </TableToolbar>
          )}

          {!readOnly && (
            <BulkActionBar count={selected.size} disabled={isBulkPending} onClear={() => setSelected(new Set())}>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={pendingAction !== null}
                onClick={() => void runBulk('bulk-enable', { enabled: true }, `已启用 ${selected.size} 个入站`)}
              >
                {pendingAction === 'bulk-enable' && <Spinner data-icon="inline-start" />}
                {pendingAction === 'bulk-enable' ? '启用中…' : '启用'}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={pendingAction !== null}
                onClick={() => void runBulk('bulk-disable', { enabled: false }, `已停用 ${selected.size} 个入站`)}
              >
                {pendingAction === 'bulk-disable' && <Spinner data-icon="inline-start" />}
                {pendingAction === 'bulk-disable' ? '停用中…' : '停用'}
              </Button>
              {nodeGroups.length > 0 && (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button type="button" variant="outline" size="sm" disabled={pendingAction !== null}>
                        {pendingAction === 'bulk-group' ? (
                          <Spinner data-icon="inline-start" />
                        ) : (
                          <BoxesIcon data-icon="inline-start" />
                        )}
                        加入节点组
                        <ChevronDownIcon data-icon="inline-end" />
                      </Button>
                    }
                  />
                  <DropdownMenuContent align="start" className="max-h-72 min-w-48 overflow-y-auto">
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>选择目标节点组</DropdownMenuLabel>
                      {nodeGroups.map((group) => (
                        <DropdownMenuItem
                          key={group.id}
                          onClick={() =>
                            void runBulk(
                              'bulk-group',
                              { addToGroupId: group.id },
                              `已将选中的入站加入「${group.name}」`,
                            )
                          }
                        >
                          {group.emoji ? `${group.emoji} ${group.name}` : group.name}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button type="button" variant="outline" size="sm" disabled={pendingAction !== null}>
                      {pendingAction === 'bulk-vision' ? (
                        <Spinner data-icon="inline-start" />
                      ) : (
                        <ShieldCheckIcon data-icon="inline-start" />
                      )}
                      Vision 流控
                      <ChevronDownIcon data-icon="inline-end" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="start">
                  <DropdownMenuGroup>
                    <DropdownMenuItem
                      onClick={() =>
                        void runBulk('bulk-vision', { vision: true }, `已为 ${selected.size} 个入站开启 Vision`)
                      }
                    >
                      开启 Vision
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onClick={() =>
                        void runBulk('bulk-vision', { vision: false }, `已为 ${selected.size} 个入站关闭 Vision`)
                      }
                    >
                      关闭 Vision
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </DropdownMenuContent>
              </DropdownMenu>
              {selectedMissingOnly && (
                <Button
                  type="button"
                  variant="destructive"
                  size="sm"
                  disabled={pendingAction !== null}
                  onClick={() => setPendingBulkRemove(true)}
                >
                  <Trash2Icon data-icon="inline-start" />
                  移除失联
                </Button>
              )}
            </BulkActionBar>
          )}

          {loadState === 'loading' ? (
            <TableSkeleton
              columns={[
                ...(readOnly ? [] : [{ label: '', className: 'w-10', width: 'w-4' }]),
                { label: '名称', width: 'w-32' },
                { label: '面板 / 入站', className: 'hidden lg:table-cell', width: 'w-28' },
                { label: '协议', className: 'hidden md:table-cell', width: 'w-16' },
                { label: '地址', className: 'hidden xl:table-cell', width: 'w-28' },
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
              {!readOnly && (
                <EmptyContent>
                  <Button render={<a href="#/panels" />} nativeButton={false}>
                    <ServerIcon data-icon="inline-start" />
                    前往面板
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : visibleInbounds.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchXIcon />
                </EmptyMedia>
                <EmptyTitle>没有匹配的入站</EmptyTitle>
                <EmptyDescription>尝试调整搜索词或筛选条件。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button type="button" variant="outline" onClick={clearFilters}>
                  <XIcon data-icon="inline-start" />
                  清除筛选
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  {!readOnly && (
                    <TableHead className="w-10">
                      <Checkbox
                        checked={allVisibleSelected}
                        indeterminate={visibleSelectedCount > 0 && !allVisibleSelected}
                        disabled={isBulkPending}
                        aria-label="全选当前筛选的入站"
                        onCheckedChange={(checked) => {
                          setSelected((prev) => {
                            const next = new Set(prev)
                            if (checked) visibleInbounds.forEach((n) => next.add(n.id))
                            else visibleInbounds.forEach((n) => next.delete(n.id))
                            return next
                          })
                        }}
                      />
                    </TableHead>
                  )}
                  <SortableHead
                    label="名称"
                    active={sortKey === 'name'}
                    direction={sortDir}
                    onToggle={() => toggleSort('name')}
                  />
                  <SortableHead
                    label="面板 / 入站"
                    className="hidden lg:table-cell"
                    active={sortKey === 'panel'}
                    direction={sortDir}
                    onToggle={() => toggleSort('panel')}
                  />
                  <TableHead className="hidden md:table-cell">协议</TableHead>
                  <TableHead className="hidden xl:table-cell">地址</TableHead>
                  <SortableHead
                    label="所属分组"
                    className="hidden lg:table-cell"
                    active={sortKey === 'groupCount'}
                    direction={sortDir}
                    onToggle={() => toggleSort('groupCount')}
                  />
                  <SortableHead
                    label="状态"
                    active={sortKey === 'status'}
                    direction={sortDir}
                    onToggle={() => toggleSort('status')}
                  />
                  <TableHead>
                    <span className="sr-only">操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleInbounds.map((inbound) => {
                  const rowToggling = toggling.has(inbound.id)
                  return (
                    <TableRow key={inbound.id} data-state={selected.has(inbound.id) ? 'selected' : undefined}>
                      {!readOnly && (
                        <TableCell>
                          <Checkbox
                            checked={selected.has(inbound.id)}
                            disabled={isBulkPending}
                            aria-label={`选择 ${inboundLabel(inbound)}`}
                            onCheckedChange={() => toggle(inbound.id)}
                          />
                        </TableCell>
                      )}
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
                        <div className="flex flex-wrap items-center gap-2">
                          {!readOnly && !inbound.missing && (
                            <Switch
                              size="sm"
                              aria-label={`${inbound.enabled ? '停用' : '启用'} ${inboundLabel(inbound)}`}
                              checked={inbound.enabled}
                              disabled={rowToggling || pendingAction !== null}
                              onCheckedChange={() => void toggleInboundEnabled(inbound)}
                            />
                          )}
                          <InboundStatus inbound={inbound} />
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap justify-end gap-1">
                          {!readOnly && (
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
                          )}
                          <Button variant="outline" size="sm" aria-label={`管理 ${inboundLabel(inbound)} 的面板配置`} render={<a href={`#/inbound-configs?id=${inbound.id}`} />} nativeButton={false}>
                            <FileCogIcon data-icon="inline-start" />
                            <span className="hidden sm:inline">配置</span>
                          </Button>
                          <DropdownMenu>
                            <DropdownMenuTrigger
                              render={
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="icon-sm"
                                  aria-label={`${inboundLabel(inbound)} 的更多操作`}
                                />
                              }
                            >
                              <MoreHorizontalIcon />
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuGroup>
                                <DropdownMenuItem onClick={() => setDetailID(inbound.id)}>
                                  <ListTreeIcon />
                                  查看完整参数
                                </DropdownMenuItem>
                              </DropdownMenuGroup>
                              {!readOnly && inbound.missing && (
                                <>
                                  <DropdownMenuSeparator />
                                  <DropdownMenuGroup>
                                    <DropdownMenuItem variant="destructive" onClick={() => setPendingDelete(inbound)}>
                                      <Trash2Icon />
                                      移除失联入站
                                    </DropdownMenuItem>
                                  </DropdownMenuGroup>
                                </>
                              )}
                            </DropdownMenuContent>
                          </DropdownMenu>
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
            {loadState === 'ready' ? `${enabledCount} 个入站已启用` : '入站状态会在刷新后更新'}
          </span>
          <span className="text-muted-foreground">{loadState === 'ready' ? `${missingCount} 个已失联` : '—'}</span>
        </CardFooter>
      </Card>

      <Dialog open={draft !== null} onOpenChange={onDraftOpenChange}>
        <DialogContent className="flex flex-col overflow-hidden sm:max-w-xl">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>编辑入站</DialogTitle>
                <DialogDescription>
                  {editingInbound ? `${inboundLabel(editingInbound)} · ${editingInbound.panelName}` : '入站设置'}。设置保存后生效，重新同步面板会保留。
                </DialogDescription>
              </DialogHeader>
              <DialogBody>
                <FieldGroup>
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
                    <NumberInput
                      id="inbound-sort"
                      disabled={pendingAction === 'save'}
                      step={1}
                      integer
                      value={draft.sortOrder}
                      onValueChange={(sortOrder) => setDraft({ ...draft, sortOrder })}
                    />
                    <FieldDescription>数字小的排在前面，决定客户端里代理的先后顺序</FieldDescription>
                  </Field>
                  <FieldGroup className="sm:grid sm:grid-cols-[minmax(0,1fr)_8rem]">
                    <Field data-disabled={pendingAction === 'save' || undefined}>
                      <FieldLabel htmlFor="inbound-public-address">公网地址</FieldLabel>
                      <Input
                        id="inbound-public-address"
                        disabled={pendingAction === 'save'}
                        placeholder={editingInbound?.shareAddress || '沿用面板地址'}
                        value={draft.publicAddress}
                        onChange={(event) => setDraft({ ...draft, publicAddress: event.target.value })}
                      />
                      <FieldDescription>
                        客户端实际连接的地址和端口。入站藏在回环端口、由 443 上的 SNI 分流或中转接入时填这里；留空或 0 沿用面板给出的值，面板改地址后订阅会跟着变。
                      </FieldDescription>
                    </Field>
                    <Field data-disabled={pendingAction === 'save' || undefined}>
                      <FieldLabel htmlFor="inbound-public-port">公网端口</FieldLabel>
                      <NumberInput
                        id="inbound-public-port"
                        disabled={pendingAction === 'save'}
                        min={0}
                        max={65535}
                        step={1}
                        integer
                        zeroAsEmpty
                        placeholder="沿用"
                        value={draft.publicPort}
                        onValueChange={(publicPort) => setDraft({ ...draft, publicPort })}
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
                  {editingInbound?.network === 'xhttp' && (
                    <Field orientation="horizontal" data-disabled={pendingAction === 'save' || undefined}>
                      <FieldContent>
                        <FieldLabel htmlFor="inbound-xhttp-tuning">XHTTP 客户端优化</FieldLabel>
                        <FieldDescription>
                          {isAutoXHTTP(editingInbound.xhttpMode)
                            ? 'mihomo 订阅改用 stream-up 并复用连接，上传和延迟明显好于 auto；经 Cloudflare 时需在 CF 打开 gRPC。分享链接不受影响。'
                            : `服务端 mode 为 ${editingInbound.xhttpMode}，此项只对 auto 模式生效。`}
                        </FieldDescription>
                      </FieldContent>
                      <Switch
                        id="inbound-xhttp-tuning"
                        disabled={pendingAction === 'save'}
                        checked={draft.xhttpTuning}
                        onCheckedChange={(value) => setDraft({ ...draft, xhttpTuning: value })}
                      />
                    </Field>
                  )}
                </FieldGroup>
              </DialogBody>
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

      <ConfirmDialog
        open={pendingBulkRemove}
        onOpenChange={setPendingBulkRemove}
        title={`批量移除 ${selected.size} 个失联入站？`}
        description="移除后它们在各节点组里的成员关系及接管配置会一并删除，无法恢复。"
        confirmLabel="确认移除"
        onConfirm={async () => {
          await runBulk('bulk-remove', { removeMissing: true }, `已移除 ${selected.size} 个失联入站`)
        }}
      />
    </div>
  )
}

function isAutoXHTTP(mode: string): boolean {
  return mode === '' || mode === 'auto'
}

/** host:port as it appears in a link; IPv6 literals need brackets. */
function hostPort(host: string, port: number): string {
  return host.includes(':') ? `[${host}]:${port}` : `${host}:${port}`
}

/**
 * The address column: where clients connect. A FireX override wins over the
 * panel's share address; the panel's own endpoint is shown beneath it.
 */
function EndpointCell({ inbound }: { inbound: Inbound }) {
  const panelEndpoint = inbound.shareAddress ? hostPort(inbound.shareAddress, inbound.port) : String(inbound.port)
  if (!inbound.publicAddress && !inbound.publicPort) {
    return <span className="block max-w-56 truncate" title={panelEndpoint}>{panelEndpoint}</span>
  }
  const host = inbound.publicAddress || inbound.shareAddress
  const port = inbound.publicPort || inbound.port
  const endpoint = host ? hostPort(host, port) : String(port)
  return (
    <span className="flex max-w-56 flex-col leading-tight">
      <span className="truncate text-foreground" title={endpoint}>{endpoint}</span>
      <span className="truncate text-xs" title={panelEndpoint}>面板 {panelEndpoint}</span>
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

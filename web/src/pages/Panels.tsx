import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  EyeIcon,
  MoreHorizontalIcon,
  PencilIcon,
  PlugZapIcon,
  PlusIcon,
  PowerIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  SearchXIcon,
  ServerOffIcon,
  Trash2Icon,
  TriangleAlertIcon,
  XIcon,
} from 'lucide-react'

import { api } from '@/api'
import type { Panel } from '@/api'
import { CodeText } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { InfoPopover } from '@/components/info-popover'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { NumberInput } from '@/components/number-input'
import { PageHeader } from '@/components/page-header'
import { PasswordInput } from '@/components/password-input'
import { PanelStatusBadge, StatusBadge } from '@/components/status-badge'
import { SearchInput, SortableHead, TableToolbar, type SortDirection } from '@/components/table-toolbar'
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
import { bytesToGb, errorMessage, formatBytes, formatQuota, formatTime, gbToBytes } from '@/lib/format'
import { useSession } from '@/lib/session'
import { reportSyncError } from '@/lib/sync'

type Draft = {
  id?: number
  name: string
  baseUrl: string
  apiToken: string
  skipTlsVerify: boolean
  enabled: boolean
  remark: string
  trafficLimitGb: number
  trafficResetDay: number
}

type LoadState = 'loading' | 'ready' | 'error'
type PendingAction = 'test' | 'save' | 'delete' | 'reset'
type StatusFilter = 'all' | 'online' | 'offline' | 'disabled' | 'overQuota'
type SortKey = 'name' | 'inbounds' | 'used' | 'lastSeenAt'
type DraftErrors = Partial<Record<'name' | 'baseUrl' | 'apiToken' | 'trafficLimitGb' | 'trafficResetDay', string>>

const STATUS_FILTER_ITEMS = [
  { value: 'all', label: '全部状态' },
  { value: 'online', label: '在线' },
  { value: 'offline', label: '离线' },
  { value: 'disabled', label: '已停用' },
  { value: 'overQuota', label: '已用满' },
]

const emptyDraft: Draft = {
  name: '',
  baseUrl: '',
  apiToken: '',
  skipTlsVerify: false,
  enabled: true,
  remark: '',
  trafficLimitGb: 0,
  trafficResetDay: 0,
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
  if (!Number.isFinite(draft.trafficLimitGb) || draft.trafficLimitGb < 0) {
    errors.trafficLimitGb = '流量上限不能为负数。'
  }
  if (!Number.isInteger(draft.trafficResetDay) || draft.trafficResetDay < 0 || draft.trafficResetDay > 31) {
    errors.trafficResetDay = '重置日需在 0 到 31 之间。'
  }
  return errors
}

export function PanelsPage() {
  const [panels, setPanels] = useState<Panel[]>([])
  const [loadState, setLoadState] = useState<LoadState>('loading')
  const [loadError, setLoadError] = useState('')
  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [sortKey, setSortKey] = useState<SortKey | null>(null)
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [draft, setDraft] = useState<Draft | null>(null)
  const [initialDraft, setInitialDraft] = useState<Draft | null>(null)
  const [validationAttempted, setValidationAttempted] = useState(false)
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [discovering, setDiscovering] = useState<Set<number>>(new Set())
  const [toggling, setToggling] = useState<Set<number>>(new Set())
  const [pendingDelete, setPendingDelete] = useState<Panel | null>(null)
  const [pendingReset, setPendingReset] = useState<Panel | null>(null)
  const { readOnly } = useSession()

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
  const draftDirty = Boolean(draft && initialDraft && JSON.stringify(draft) !== JSON.stringify(initialDraft))
  const closeDraft = useCallback(() => setDraft(null), [])
  const onDraftOpenChange = useDirtyDialog(draftDirty, dialogPending, closeDraft)

  const onlineCount = useMemo(() => panels.filter((panel) => panel.status === 'online').length, [panels])
  const enabledNodeCount = useMemo(() => panels.reduce((sum, panel) => sum + panel.enabledInbounds, 0), [panels])

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((current) => (current === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir(key === 'name' ? 'asc' : 'desc')
    }
  }

  const visiblePanels = useMemo(() => {
    const keyword = query.trim().toLocaleLowerCase('zh-CN')
    const filtered = panels.filter((panel) => {
      if (statusFilter === 'online' && panel.status !== 'online') return false
      if (statusFilter === 'offline' && panel.status !== 'offline') return false
      if (statusFilter === 'disabled' && panel.enabled) return false
      if (statusFilter === 'overQuota' && !panel.overQuota) return false
      if (!keyword) return true
      return [panel.name, panel.baseUrl, panel.remark].some((text) =>
        text.toLocaleLowerCase('zh-CN').includes(keyword),
      )
    })
    if (!sortKey) return filtered
    const factor = sortDir === 'asc' ? 1 : -1
    return [...filtered].sort((a, b) => {
      switch (sortKey) {
        case 'name':
          return a.name.localeCompare(b.name, 'zh-CN') * factor
        case 'inbounds':
          return (a.enabledInbounds - b.enabledInbounds) * factor
        case 'used':
          return (a.used - b.used) * factor
        case 'lastSeenAt':
          return (a.lastSeenAt - b.lastSeenAt) * factor
      }
    })
  }, [panels, query, statusFilter, sortKey, sortDir])

  const hasFilters = query.trim() !== '' || statusFilter !== 'all'

  const openCreate = () => {
    const next = { ...emptyDraft }
    setValidationAttempted(false)
    setInitialDraft(next)
    setDraft(next)
  }

  const openEditor = (panel: Panel) => {
    const next: Draft = {
      id: panel.id,
      name: panel.name,
      baseUrl: panel.baseUrl,
      apiToken: '',
      skipTlsVerify: panel.skipTlsVerify,
      enabled: panel.enabled,
      remark: panel.remark,
      trafficLimitGb: bytesToGb(panel.trafficLimit),
      trafficResetDay: panel.trafficResetDay,
    }
    setValidationAttempted(false)
    setInitialDraft(next)
    setDraft(next)
  }

  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft) return
    setValidationAttempted(true)
    if (Object.keys(validateDraft(draft)).length > 0) return

    setPendingAction('save')
    const { trafficLimitGb, ...rest } = draft
    const body = { ...rest, trafficLimit: gbToBytes(trafficLimitGb) }
    try {
      if (draft.id !== undefined) {
        const result = await api.put<{ syncError: string }>(`/panels/${draft.id}`, body)
        toast.success('已保存')
        reportSyncError(result.syncError)
      } else {
        const result = await api.post<{ discoverError: string }>('/panels', body)
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

  const toggleEnabled = async (panel: Panel) => {
    setToggling((current) => new Set(current).add(panel.id))
    try {
      const result = await api.put<{ syncError: string }>(`/panels/${panel.id}`, {
        name: panel.name,
        baseUrl: panel.baseUrl,
        skipTlsVerify: panel.skipTlsVerify,
        enabled: !panel.enabled,
        remark: panel.remark,
      })
      toast.success(panel.enabled ? `已停用 ${panel.name}` : `已启用 ${panel.name}`)
      reportSyncError(result.syncError)
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '切换状态失败'))
    } finally {
      setToggling((current) => {
        const next = new Set(current)
        next.delete(panel.id)
        return next
      })
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

  const resetTraffic = async (panel: Panel) => {
    setPendingAction('reset')
    try {
      const result = await api.post<{ syncError: string }>(`/panels/${panel.id}/resetTraffic`)
      if (result.syncError) toast.error(`已清零，但恢复客户端时出错：${result.syncError}`, { duration: 10000 })
      else toast.success('已清零本周期流量')
      await refreshAfterMutation()
    } catch (err) {
      toast.error(errorMessage(err, '清零失败'))
      throw err
    } finally {
      setPendingAction(null)
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
        {readOnly ? (
          <Badge variant="outline">
            <EyeIcon data-icon="inline-start" />
            只读
          </Badge>
        ) : (
          <Button type="button" onClick={openCreate}>
            <PlusIcon data-icon="inline-start" />
            添加面板
          </Button>
        )}
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>面板列表</CardTitle>
          <CardDescription>
            {loadState === 'ready' ? `共 ${panels.length} 个面板，集中查看连通性与节点同步状态。` : '管理 FireX 连接的 3x-ui 面板。'}
          </CardDescription>
          <CardAction>
            <RefreshButton
              loading={loadState === 'loading'}
              disabled={pendingAction !== null || discovering.size > 0}
              onClick={() => void load()}
            />
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 px-0">
          {panels.length > 0 && (
            <TableToolbar>
              <SearchInput
                value={query}
                onChange={setQuery}
                placeholder="按名称、地址或备注搜索…"
                ariaLabel="搜索面板"
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
          {loadState === 'loading' ? (
            <TableSkeleton
              columns={[
                { label: '名称', width: 'w-28' },
                { label: '地址', className: 'hidden lg:table-cell', width: 'w-48' },
                { label: '状态', width: 'w-14' },
                { label: '入站', className: 'hidden md:table-cell', width: 'w-12' },
                { label: '流量', width: 'w-24' },
                { label: 'Xray', className: 'hidden 2xl:table-cell', width: 'w-20' },
                { label: '最近连通', className: 'hidden lg:table-cell', width: 'w-32' },
                { width: 'w-24' },
              ]}
            />
          ) : loadState === 'error' ? (
            <div className="px-4">
              <LoadErrorAlert title="面板加载失败" message={loadError} onRetry={() => void load()} />
            </div>
          ) : panels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ServerOffIcon />
                </EmptyMedia>
                <EmptyTitle>还没有面板</EmptyTitle>
                <EmptyDescription>
                  {readOnly ? '超级管理员添加 3x-ui 后，它的入站会出现在这里。' : '添加一台 3x-ui，FireX 会自动拉取它的入站。'}
                </EmptyDescription>
              </EmptyHeader>
              {!readOnly && (
                <EmptyContent>
                  <Button type="button" onClick={openCreate}>
                    <PlusIcon data-icon="inline-start" />
                    添加面板
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : visiblePanels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SearchXIcon />
                </EmptyMedia>
                <EmptyTitle>没有匹配的面板</EmptyTitle>
                <EmptyDescription>换个关键词或状态条件试试。</EmptyDescription>
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
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableHead
                    label="名称"
                    active={sortKey === 'name'}
                    direction={sortDir}
                    onToggle={() => toggleSort('name')}
                  />
                  <TableHead className="hidden lg:table-cell">地址</TableHead>
                  <TableHead>状态</TableHead>
                  <SortableHead
                    label="入站"
                    className="hidden md:table-cell"
                    active={sortKey === 'inbounds'}
                    direction={sortDir}
                    onToggle={() => toggleSort('inbounds')}
                  />
                  <SortableHead
                    label="流量"
                    active={sortKey === 'used'}
                    direction={sortDir}
                    onToggle={() => toggleSort('used')}
                  />
                  <TableHead className="hidden 2xl:table-cell">Xray</TableHead>
                  <SortableHead
                    label="最近连通"
                    className="hidden lg:table-cell"
                    active={sortKey === 'lastSeenAt'}
                    direction={sortDir}
                    onToggle={() => toggleSort('lastSeenAt')}
                  />
                  {!readOnly && (
                    <TableHead>
                      <span className="sr-only">操作</span>
                    </TableHead>
                  )}
                </TableRow>
              </TableHeader>
              <TableBody>
                {visiblePanels.map((panel) => {
                  const discoveringPanel = discovering.has(panel.id)
                  const togglingPanel = toggling.has(panel.id)
                  const rowBusy = discoveringPanel || togglingPanel || pendingAction !== null
                  return (
                    <TableRow key={panel.id}>
                      <TableCell className="min-w-40 max-w-80 whitespace-normal [overflow-wrap:anywhere]">
                        <div className="flex flex-col gap-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="font-medium">{panel.name}</span>
                            {!panel.enabled && <StatusBadge tone="idle">已停用</StatusBadge>}
                          </div>
                          <CodeText className="block max-w-56 truncate lg:hidden">{panel.baseUrl}</CodeText>
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
                          <div className="flex items-center gap-1.5">
                            <PanelStatusBadge status={panel.status} />
                            {panel.lastError && (
                              <span className="md:hidden">
                                <InfoPopover
                                  label={`查看 ${panel.name} 的错误信息`}
                                  title={`${panel.name} 最近错误`}
                                  content={panel.lastError}
                                  copyValue={panel.lastError}
                                  className="text-destructive"
                                >
                                  <TriangleAlertIcon className="size-3.5" />
                                </InfoPopover>
                              </span>
                            )}
                          </div>
                          {panel.lastError && (
                            <span className="hidden md:inline-flex">
                              <InfoPopover
                                label={`查看 ${panel.name} 的错误信息`}
                                title={`${panel.name} 最近错误`}
                                content={panel.lastError}
                                copyValue={panel.lastError}
                              >
                                <CodeText className="max-w-40 truncate underline decoration-dotted underline-offset-4 xl:max-w-56">
                                  {panel.lastError}
                                </CodeText>
                              </InfoPopover>
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="hidden tabular-nums md:table-cell">
                        {panel.enabledInbounds} / {panel.inboundCount}
                      </TableCell>
                      <TableCell>
                        <PanelTraffic panel={panel} />
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground 2xl:table-cell">{panel.xrayVersion || '—'}</TableCell>
                      <TableCell className="hidden text-muted-foreground lg:table-cell">{formatTime(panel.lastSeenAt)}</TableCell>
                      {!readOnly && (
                        <TableCell>
                          <div className="flex justify-end gap-1">
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              aria-label={`拉取 ${panel.name} 的入站`}
                              disabled={rowBusy}
                              onClick={() => void discover(panel)}
                            >
                              {discoveringPanel ? (
                                <Spinner data-icon="inline-start" />
                              ) : (
                                <RefreshCwIcon data-icon="inline-start" />
                              )}
                              <span className="hidden xl:inline">{discoveringPanel ? '同步中…' : '拉取入站'}</span>
                            </Button>
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              aria-label={`编辑 ${panel.name}`}
                              disabled={rowBusy}
                              onClick={() => openEditor(panel)}
                            >
                              <PencilIcon data-icon="inline-start" />
                              <span className="hidden sm:inline">编辑</span>
                            </Button>
                            <DropdownMenu>
                              <DropdownMenuTrigger
                                render={
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon-sm"
                                    disabled={rowBusy}
                                    aria-label={`${panel.name} 的更多操作`}
                                  />
                                }
                              >
                                <MoreHorizontalIcon />
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuGroup>
                                  <DropdownMenuItem onClick={() => void toggleEnabled(panel)}>
                                    <PowerIcon />
                                    {panel.enabled ? '停用面板' : '启用面板'}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem disabled={panel.used <= 0} onClick={() => setPendingReset(panel)}>
                                    <RotateCcwIcon />
                                    清零本周期流量
                                  </DropdownMenuItem>
                                </DropdownMenuGroup>
                                <DropdownMenuSeparator />
                                <DropdownMenuGroup>
                                  <DropdownMenuItem variant="destructive" onClick={() => setPendingDelete(panel)}>
                                    <Trash2Icon />
                                    删除面板
                                  </DropdownMenuItem>
                                </DropdownMenuGroup>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </div>
                        </TableCell>
                      )}
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

      <Dialog open={draft !== null} onOpenChange={onDraftOpenChange}>
        <DialogContent className="flex flex-col overflow-hidden sm:max-w-lg">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{draft.id !== undefined ? '编辑面板' : '添加面板'}</DialogTitle>
                <DialogDescription>
                  地址需要包含协议、端口和 basePath，例如 https://1.2.3.4:2053/mypath，不要带 /panel。
                </DialogDescription>
              </DialogHeader>
              <DialogBody>
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
                    <PasswordInput
                      id="panel-token"
                      required={draft.id === undefined}
                      disabled={dialogPending}
                      autoComplete="off"
                      value={draft.apiToken}
                      aria-invalid={(validationAttempted && !!draftErrors.apiToken) || undefined}
                      onChangeValue={(apiToken) => setDraft({ ...draft, apiToken })}
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
                  <FieldGroup className="sm:grid sm:grid-cols-2">
                    <Field
                      data-invalid={(validationAttempted && !!draftErrors.trafficLimitGb) || undefined}
                      data-disabled={dialogPending || undefined}
                    >
                      <FieldLabel htmlFor="panel-traffic-limit">流量上限 (GB)</FieldLabel>
                      <NumberInput
                        id="panel-traffic-limit"
                        min={0}
                        step="any"
                        zeroAsEmpty
                        placeholder="不限"
                        disabled={dialogPending}
                        value={draft.trafficLimitGb}
                        aria-invalid={(validationAttempted && !!draftErrors.trafficLimitGb) || undefined}
                        onValueChange={(trafficLimitGb) => setDraft({ ...draft, trafficLimitGb })}
                      />
                      {validationAttempted && draftErrors.trafficLimitGb ? (
                        <FieldError>{draftErrors.trafficLimitGb}</FieldError>
                      ) : (
                        <FieldDescription>所有 FireX 用户在此面板的合计用量，留空或 0 表示不限。用满后停用全部客户端，直到下个周期。</FieldDescription>
                      )}
                    </Field>
                    <Field
                      data-invalid={(validationAttempted && !!draftErrors.trafficResetDay) || undefined}
                      data-disabled={dialogPending || undefined}
                    >
                      <FieldLabel htmlFor="panel-reset-day">每月重置日</FieldLabel>
                      <NumberInput
                        id="panel-reset-day"
                        min={0}
                        max={31}
                        step={1}
                        integer
                        disabled={dialogPending}
                        value={draft.trafficResetDay}
                        aria-invalid={(validationAttempted && !!draftErrors.trafficResetDay) || undefined}
                        onValueChange={(trafficResetDay) => setDraft({ ...draft, trafficResetDay })}
                      />
                      {validationAttempted && draftErrors.trafficResetDay ? (
                        <FieldError>{draftErrors.trafficResetDay}</FieldError>
                      ) : (
                        <FieldDescription>
                          每月这一天 0 点清零，0 表示只手动清零；小月按月末算。
                          {draft.id !== undefined &&
                            draft.trafficResetDay !== panels.find((panel) => panel.id === draft.id)?.trafficResetDay &&
                            ' 修改后从现在开始计算新周期。'}
                        </FieldDescription>
                      )}
                    </Field>
                  </FieldGroup>
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
              </DialogBody>
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
        open={pendingReset !== null}
        onOpenChange={(open) => !open && setPendingReset(null)}
        title={`清零面板「${pendingReset?.name ?? ''}」本周期的流量？`}
        description="面板本周期用量从 0 重新计算，历史累计保持不变；因面板用满而停用的客户端会恢复。用户自己的套餐用量不受影响。"
        confirmLabel="清零"
        onConfirm={async () => {
          if (pendingReset) await resetTraffic(pendingReset)
        }}
      />

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

function PanelTraffic({ panel }: { panel: Panel }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="whitespace-nowrap tabular-nums">
        {formatBytes(panel.used)}
        {panel.trafficLimit > 0 && <span className="text-muted-foreground"> / {formatQuota(panel.trafficLimit)}</span>}
      </span>
      {(panel.totalUsed > 0 || panel.nextTrafficReset > 0) && (
        <span className="whitespace-nowrap text-xs text-muted-foreground tabular-nums">
          累计 {formatBytes(panel.totalUsed)}
        </span>
      )}
      {panel.overQuota ? (
        <StatusBadge tone="bad" title="面板的入站已从所有订阅中移除，客户端已停用">
          已用满
        </StatusBadge>
      ) : (
        <span className="text-xs text-muted-foreground">{panel.userCount} 个用户</span>
      )}
      {panel.nextTrafficReset > 0 && (
        <span className="whitespace-nowrap text-xs text-muted-foreground">
          {new Date(panel.nextTrafficReset).toLocaleDateString('zh-CN')} 重置
        </span>
      )}
    </div>
  )
}

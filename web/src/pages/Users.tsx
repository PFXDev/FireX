import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  CopyIcon,
  EyeIcon,
  MoreHorizontalIcon,
  PencilIcon,
  PlusIcon,
  RadioTowerIcon,
  RotateCcwIcon,
  SearchIcon,
  SearchXIcon,
  ServerOffIcon,
  Trash2Icon,
  TriangleAlertIcon,
  UsersIcon,
  XIcon,
} from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { Plan, SubscriptionPreview, User, UserPanelUsage } from '@/api'
import { CodeText, CodeTextarea } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { Progress, ProgressLabel, ProgressValue } from '@/components/ui/progress'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  bytesToGb,
  copyText,
  dateInputToMs,
  dateInputValue,
  errorMessage,
  formatBytes,
  formatExpiry,
  formatQuota,
  formatTime,
  gbToBytes,
  TRAFFIC_RESET_LABELS,
} from '@/lib/format'
import { reportSyncError } from '@/lib/sync'

type Draft = {
  id?: number
  username: string
  uuid: string
  planId: number
  enabled: boolean
  expiresAt: number
  trafficGb: number
  remark: string
  // On create, each limit either follows the plan or is set by hand. An edit
  // always sets both: the user already has their own values.
  overrideTraffic: boolean
  overrideExpiry: boolean
}

type PendingAction = {
  user: User
  kind: 'delete' | 'resetTraffic'
}

type RowAction = {
  userId: number
  kind: 'resync' | 'preview'
}

const USERNAME_RE = /^[A-Za-z0-9._-]+$/
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function UsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [plans, setPlans] = useState<Plan[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [draft, setDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [preview, setPreview] = useState<{ user: User; data: SubscriptionPreview } | null>(null)
  const [pending, setPending] = useState<PendingAction | null>(null)
  const [rowAction, setRowAction] = useState<RowAction | null>(null)

  const load = useCallback(async () => {
    try {
      const [nextUsers, nextPlans] = await Promise.all([api.get<User[]>('/users'), api.get<Plan[]>('/plans')])
      setUsers(nextUsers)
      setPlans(nextPlans)
      setLoadError(null)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '用户数据加载失败'))
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

  const planItems = useMemo(
    () => [
      { label: '不分配（用户将没有节点）', value: 0 },
      ...plans.map((plan) => ({
        label: plan.enabled ? plan.name : `${plan.name}（已停用）`,
        value: plan.id,
      })),
    ],
    [plans],
  )

  const filteredUsers = useMemo(() => {
    const keyword = query.trim().toLocaleLowerCase('zh-CN')
    if (!keyword) return users
    return users.filter((user) =>
      [user.username, user.planName, user.remark].some((value) => value.toLocaleLowerCase('zh-CN').includes(keyword)),
    )
  }, [query, users])

  const newDraft = (): Draft => {
    const defaultPlan = plans.find((plan) => plan.enabled)
    return {
      username: '',
      uuid: '',
      planId: defaultPlan?.id ?? 0,
      enabled: true,
      expiresAt: 0,
      trafficGb: 0,
      remark: '',
      overrideTraffic: !defaultPlan,
      overrideExpiry: !defaultPlan,
    }
  }

  const openCreate = () => {
    setShowErrors(false)
    setDraft(newDraft())
  }

  const openEdit = (user: User) => {
    setShowErrors(false)
    setDraft({
      id: user.id,
      username: user.username,
      uuid: user.uuid,
      planId: user.planId,
      enabled: user.enabled,
      expiresAt: user.expiresAt,
      trafficGb: bytesToGb(user.trafficLimit),
      remark: user.remark,
      overrideTraffic: true,
      overrideExpiry: true,
    })
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || saving) return
    setShowErrors(true)
    const usernameValid = draft.username.trim().length > 0 && USERNAME_RE.test(draft.username)
    const uuidValid = !draft.uuid || UUID_RE.test(draft.uuid)
    if (!usernameValid || !uuidValid) return
    if (!event.currentTarget.reportValidity()) return

    setSaving(true)
    const body: Record<string, unknown> = {
      username: draft.username.trim(),
      uuid: draft.uuid || undefined,
      planId: draft.planId,
      enabled: draft.enabled,
      remark: draft.remark,
    }
    // Leaving a field out of the create request is what makes the server
    // take the plan's value for it.
    if (draft.id || draft.overrideExpiry) body.expiresAt = draft.expiresAt
    if (draft.id || draft.overrideTraffic) body.trafficLimit = gbToBytes(draft.trafficGb)

    try {
      const result = draft.id
        ? await api.put<{ syncError: string }>(`/users/${draft.id}`, body)
        : await api.post<{ syncError: string }>('/users', body)
      if (result.syncError) toast.success(draft.id ? '用户已保存' : '用户已创建')
      else toast.success(draft.id ? '用户已保存并下发' : '用户已创建并下发')
      reportSyncError(result.syncError)
      setDraft(null)
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const runResync = async (user: User) => {
    setRowAction({ userId: user.id, kind: 'resync' })
    try {
      await api.post(`/users/${user.id}/resync`)
      toast.success('重新下发完成')
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '重新下发失败'))
    } finally {
      setRowAction(null)
    }
  }

  const confirmAction = async () => {
    if (!pending) return
    try {
      if (pending.kind === 'delete') {
        await api.del(`/users/${pending.user.id}`)
        toast.success('用户已删除')
      } else {
        const result = await api.post<{ remoteFailures: number; syncError: string }>(`/users/${pending.user.id}/resetTraffic`)
        if (result.remoteFailures > 0) {
          toast.warning(`流量统计已清空，${result.remoteFailures} 个面板暂未响应，稍后会自动重试`)
        } else {
          toast.success('流量统计已清空')
        }
        reportSyncError(result.syncError)
      }
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, pending.kind === 'delete' ? '删除失败' : '流量重置失败'))
      throw err
    }
  }

  const openPreview = async (user: User) => {
    setRowAction({ userId: user.id, kind: 'preview' })
    try {
      const data = await api.get<SubscriptionPreview>(`/users/${user.id}/subscription`)
      setPreview({ user, data })
    } catch (err) {
      toast.error(errorMessage(err, '获取订阅失败'))
    } finally {
      setRowAction(null)
    }
  }

  const copy = async (text: string, what: string) => {
    if (await copyText(text)) toast.success(`${what}已复制`)
    else toast.error('复制失败，请手动复制')
  }

  const activeUsers = users.filter((user) => user.enabled && !user.depleted && (!user.expiresAt || user.expiresAt > Date.now())).length
  const failedSyncs = users.filter((user) => user.syncState === 'failed').length
  const selectedPlan = draft ? plans.find((plan) => plan.id === draft.planId) : undefined
  // Only a new user on a real plan has defaults to fall back on.
  const canInherit = Boolean(draft && !draft.id && selectedPlan)
  // Seeds the date picker when a plan's term is switched to a custom one, so
  // the operator adjusts from the default instead of from a blank field.
  const planExpiry =
    selectedPlan && selectedPlan.durationDays > 0
      ? dateInputToMs(dateInputValue(Date.now() + selectedPlan.durationDays * 86400000))
      : 0
  const usernameInvalid = Boolean(showErrors && draft && (!draft.username.trim() || !USERNAME_RE.test(draft.username)))
  const uuidInvalid = Boolean(showErrors && draft?.uuid && !UUID_RE.test(draft.uuid))

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="用户" description="统一管理账号、套餐、流量与订阅交付状态。">
        <Button type="button" onClick={openCreate}>
          <PlusIcon data-icon="inline-start" />
          新建用户
        </Button>
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>用户列表</CardTitle>
          <CardDescription>
            {loading || (loadError && users.length === 0) ? '' : `${activeUsers}/${users.length} 个用户可用。`}
            每个用户在所有节点共用同一 UUID，并通过一条订阅链接接收配置。
          </CardDescription>
          <CardAction>
            <RefreshButton loading={loading || refreshing} disabled={saving} onClick={() => void refresh()} />
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 px-0">
          {users.length > 0 && (
            <div className="px-4">
              <InputGroup className="sm:max-w-64">
                <InputGroupInput
                  aria-label="搜索用户"
                  placeholder="按用户名、套餐或备注搜索…"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
                <InputGroupAddon>
                  <SearchIcon />
                </InputGroupAddon>
                {query && (
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton size="icon-xs" aria-label="清除搜索" onClick={() => setQuery('')}>
                      <XIcon />
                    </InputGroupButton>
                  </InputGroupAddon>
                )}
              </InputGroup>
            </div>
          )}
          {loading ? (
            <TableSkeleton
              columns={[
                { label: '用户', width: 'w-24' },
                { label: '套餐', width: 'w-16' },
                { label: '流量', width: 'w-28' },
                { label: '到期', className: 'hidden md:table-cell', width: 'w-20' },
                { label: '节点', className: 'hidden lg:table-cell', width: 'w-8' },
                { label: '状态', width: 'w-14' },
                { label: '最近拉取', className: 'hidden xl:table-cell', width: 'w-28' },
                { width: 'w-28' },
              ]}
            />
          ) : loadError && users.length === 0 ? (
            <div className="px-4">
              <LoadErrorAlert title="用户加载失败" message={loadError} retrying={refreshing} onRetry={() => void refresh()} />
            </div>
          ) : users.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon"><UsersIcon /></EmptyMedia>
                <EmptyTitle>还没有用户</EmptyTitle>
                <EmptyDescription>创建用户后，FireX 会把客户端下发到套餐涉及的每个面板。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={openCreate}>
                  <PlusIcon data-icon="inline-start" />
                  新建用户
                </Button>
              </EmptyContent>
            </Empty>
          ) : filteredUsers.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon"><SearchXIcon /></EmptyMedia>
                <EmptyTitle>没有匹配的用户</EmptyTitle>
                <EmptyDescription>换一个关键词，或清除当前搜索条件。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button type="button" variant="outline" onClick={() => setQuery('')}>
                  <XIcon data-icon="inline-start" />
                  清除搜索
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
                    <TableHead>用户</TableHead>
                    <TableHead>套餐</TableHead>
                    <TableHead>流量</TableHead>
                    <TableHead className="hidden md:table-cell">到期</TableHead>
                    <TableHead className="hidden lg:table-cell">节点</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead className="hidden xl:table-cell">最近拉取</TableHead>
                    <TableHead><span className="sr-only">操作</span></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filteredUsers.map((user) => {
                    const usage = user.trafficLimit > 0 ? Math.min(100, (user.used / user.trafficLimit) * 100) : 0
                    const previewing = rowAction?.userId === user.id && rowAction.kind === 'preview'
                    const resyncing = rowAction?.userId === user.id && rowAction.kind === 'resync'
                    return (
                      <TableRow key={user.id}>
                        <TableCell>
                          <div className="flex flex-col gap-1">
                            <span className="font-medium">{user.username}</span>
                            {user.remark && <span className="text-xs text-muted-foreground">{user.remark}</span>}
                          </div>
                        </TableCell>
                        <TableCell>
                          {user.planName || <span className="text-muted-foreground">未分配</span>}
                        </TableCell>
                        <TableCell className="min-w-40">
                          <div className="flex flex-col gap-1.5">
                            <span className="tabular-nums">{formatBytes(user.used)} / {formatQuota(user.trafficLimit)}</span>
                            {user.trafficLimit > 0 && (
                              <Progress value={usage}>
                                <ProgressLabel className="sr-only">{user.username} 流量使用率</ProgressLabel>
                                <ProgressValue className="sr-only" />
                              </Progress>
                            )}
                            {user.nextTrafficReset > 0 && (
                              <span className="text-xs text-muted-foreground">
                                {new Date(user.nextTrafficReset).toLocaleDateString('zh-CN')} 重置
                              </span>
                            )}
                            <PanelUsage user={user} />
                          </div>
                        </TableCell>
                        <TableCell className="hidden text-muted-foreground md:table-cell">{formatExpiry(user.expiresAt)}</TableCell>
                        <TableCell className="hidden tabular-nums text-muted-foreground lg:table-cell">{user.inboundCount}</TableCell>
                        <TableCell><UserStatuses user={user} /></TableCell>
                        <TableCell className="hidden text-muted-foreground xl:table-cell">{formatTime(user.lastSubAt)}</TableCell>
                        <TableCell>
                          <div className="flex justify-end gap-1">
                            <Tooltip>
                              <TooltipTrigger
                                render={
                                  <Button
                                    type="button"
                                    variant="outline"
                                    size="icon-sm"
                                    aria-label={`复制 ${user.username} 的订阅链接`}
                                    onClick={() => void copy(user.subUrl, '订阅链接')}
                                  />
                                }
                              >
                                <CopyIcon />
                              </TooltipTrigger>
                              <TooltipContent>复制订阅链接</TooltipContent>
                            </Tooltip>
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              aria-label={`查看 ${user.username} 的订阅`}
                              disabled={previewing}
                              onClick={() => void openPreview(user)}
                            >
                              {previewing ? <Spinner data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
                              <span className="hidden sm:inline">{previewing ? '加载中…' : '查看'}</span>
                            </Button>
                            <DropdownMenu>
                              <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" aria-label={`${user.username} 的更多操作`} />}>
                                <MoreHorizontalIcon />
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuGroup>
                                  <DropdownMenuItem onClick={() => openEdit(user)}>
                                    <PencilIcon />
                                    编辑
                                  </DropdownMenuItem>
                                  <DropdownMenuItem disabled={resyncing} onClick={() => void runResync(user)}>
                                    {resyncing ? <Spinner /> : <RadioTowerIcon />}
                                    {resyncing ? '下发中…' : '重新下发'}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem onClick={() => setPending({ user, kind: 'resetTraffic' })}>
                                    <RotateCcwIcon />
                                    清空流量
                                  </DropdownMenuItem>
                                </DropdownMenuGroup>
                                <DropdownMenuSeparator />
                                <DropdownMenuGroup>
                                  <DropdownMenuItem variant="destructive" onClick={() => setPending({ user, kind: 'delete' })}>
                                    <Trash2Icon />
                                    删除
                                  </DropdownMenuItem>
                                </DropdownMenuGroup>
                              </DropdownMenuContent>
                            </DropdownMenu>
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
          <span className="text-muted-foreground">
            {loading ? '正在读取用户状态' : loadError && users.length === 0 ? '用户统计暂不可用' : `共 ${users.length} 个用户`}
          </span>
          <span className="text-muted-foreground">
            {loading || (loadError && users.length === 0)
              ? '—'
              : users.length === 0
                ? '暂无下发记录'
              : failedSyncs > 0
                ? `${failedSyncs} 个用户下发失败`
                : '全部用户下发状态正常'}
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
        <DialogContent className="sm:max-w-lg">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{draft.id ? `编辑用户 ${draft.username}` : '新建用户'}</DialogTitle>
                <DialogDescription>保存后会立即同步到套餐涉及的所有面板。</DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <Field data-disabled={draft.id || saving ? true : undefined} data-invalid={usernameInvalid || undefined}>
                  <FieldLabel htmlFor="user-name">用户名</FieldLabel>
                  <Input id="user-name" value={draft.username} required disabled={Boolean(draft.id) || saving} aria-invalid={usernameInvalid || undefined} onChange={(event) => setDraft({ ...draft, username: event.target.value })} />
                  <FieldDescription>{draft.id ? '用户名与面板客户端标识关联，创建后不可修改。' : '仅允许字母、数字、连字符、下划线与句点。'}</FieldDescription>
                  {usernameInvalid && <FieldError>请输入有效用户名。</FieldError>}
                </Field>

                <Field data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="user-plan">套餐</FieldLabel>
                  <Select items={planItems} value={draft.planId} disabled={saving} onValueChange={(value) => setDraft({ ...draft, planId: Number(value) })}>
                    <SelectTrigger id="user-plan" className="w-full"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {planItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  {draft.planId === 0 && <FieldDescription>未分配套餐的用户不会获得任何节点。</FieldDescription>}
                </Field>

                <FieldGroup className="sm:grid sm:grid-cols-2">
                  <Field data-disabled={saving || undefined}>
                    {canInherit ? (
                      <Field orientation="horizontal">
                        <FieldLabel htmlFor="user-override-traffic">自定义流量上限</FieldLabel>
                        <Switch id="user-override-traffic" disabled={saving} checked={draft.overrideTraffic} onCheckedChange={(overrideTraffic) => setDraft({ ...draft, overrideTraffic, trafficGb: overrideTraffic && !draft.overrideTraffic ? bytesToGb(selectedPlan?.trafficBytes ?? 0) : draft.trafficGb })} />
                      </Field>
                    ) : (
                      <FieldLabel htmlFor="user-traffic">流量上限 (GB)</FieldLabel>
                    )}
                    {!canInherit || draft.overrideTraffic ? (
                      <>
                        <Input id="user-traffic" aria-label={canInherit ? '流量上限 (GB)' : undefined} disabled={saving} type="number" min={0} step="any" value={draft.trafficGb} onChange={(event) => setDraft({ ...draft, trafficGb: Number(event.target.value) })} />
                        <FieldDescription>单位 GB，0 表示不限。</FieldDescription>
                      </>
                    ) : (
                      <FieldDescription>沿用套餐：{formatQuota(selectedPlan?.trafficBytes ?? 0)}</FieldDescription>
                    )}
                  </Field>
                  <Field data-disabled={saving || undefined}>
                    {canInherit ? (
                      <Field orientation="horizontal">
                        <FieldLabel htmlFor="user-override-expiry">自定义到期日</FieldLabel>
                        <Switch id="user-override-expiry" disabled={saving} checked={draft.overrideExpiry} onCheckedChange={(overrideExpiry) => setDraft({ ...draft, overrideExpiry, expiresAt: overrideExpiry && !draft.overrideExpiry ? planExpiry : draft.expiresAt })} />
                      </Field>
                    ) : (
                      <FieldLabel htmlFor="user-expiry">到期日</FieldLabel>
                    )}
                    {!canInherit || draft.overrideExpiry ? (
                      <>
                        <Input id="user-expiry" aria-label={canInherit ? '到期日' : undefined} disabled={saving} type="date" value={dateInputValue(draft.expiresAt)} onChange={(event) => setDraft({ ...draft, expiresAt: dateInputToMs(event.target.value) })} />
                        <FieldDescription>留空表示永久。</FieldDescription>
                      </>
                    ) : (
                      <FieldDescription>沿用套餐：{selectedPlan && selectedPlan.durationDays > 0 ? `${selectedPlan.durationDays} 天后到期` : '永久'}</FieldDescription>
                    )}
                  </Field>
                </FieldGroup>

                {selectedPlan && selectedPlan.trafficReset !== 'none' && (
                  <FieldDescription>
                    该套餐流量{TRAFFIC_RESET_LABELS[selectedPlan.trafficReset]}重置，上限为每个周期的额度。
                  </FieldDescription>
                )}

                <Field data-invalid={uuidInvalid || undefined} data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="user-uuid">UUID</FieldLabel>
                  <Input id="user-uuid" className="font-mono" spellCheck={false} disabled={saving} value={draft.uuid} aria-invalid={uuidInvalid || undefined} onChange={(event) => setDraft({ ...draft, uuid: event.target.value })} />
                  <FieldDescription>留空自动生成；所有节点共用同一个 UUID。</FieldDescription>
                  {uuidInvalid && <FieldError>请输入标准 UUID，或留空自动生成。</FieldError>}
                </Field>

                <Field data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="user-remark">备注</FieldLabel>
                  <Input id="user-remark" disabled={saving} value={draft.remark} onChange={(event) => setDraft({ ...draft, remark: event.target.value })} />
                </Field>

                <Field orientation="horizontal" data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="user-enabled">启用用户</FieldLabel>
                  <Switch id="user-enabled" disabled={saving} checked={draft.enabled} onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} />
                </Field>
              </FieldGroup>

              <DialogFooter>
                <Button type="button" variant="outline" disabled={saving} onClick={() => setDraft(null)}>取消</Button>
                <Button type="submit" disabled={saving}>
                  {saving && <Spinner data-icon="inline-start" />}
                  {saving ? '下发中…' : '保存并下发'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={preview !== null} onOpenChange={(open) => !open && setPreview(null)}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{preview?.user.username} 的订阅</DialogTitle>
            <DialogDescription>订阅默认返回 mihomo（Clash）配置；旧版客户端可请求 base64 分享链接列表。</DialogDescription>
          </DialogHeader>
          {preview && (
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="sub-url">订阅链接</FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id="sub-url"
                    readOnly
                    className="font-mono text-xs"
                    value={preview.data.subUrl}
                    onFocus={(event) => event.target.select()}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupButton aria-label="复制订阅链接" onClick={() => void copy(preview.data.subUrl, '订阅链接')}>
                      <CopyIcon />
                      复制
                    </InputGroupButton>
                  </InputGroupAddon>
                </InputGroup>
              </Field>

              {preview.data.warnings.length > 0 && (
                <Alert variant="warning">
                  <TriangleAlertIcon />
                  <AlertTitle>生成订阅时有警告</AlertTitle>
                  <AlertDescription>
                    <ul className="flex flex-col gap-1">
                      {preview.data.warnings.map((warning) => <li key={warning}>{warning}</li>)}
                    </ul>
                  </AlertDescription>
                </Alert>
              )}

              <Tabs defaultValue="nodes">
                <TabsList>
                  <TabsTrigger value="nodes">节点 ({preview.data.entries.length})</TabsTrigger>
                  <TabsTrigger value="clash">Clash 配置</TabsTrigger>
                </TabsList>
                <TabsContent value="nodes">
                  {preview.data.entries.length === 0 ? (
                    <Empty>
                      <EmptyHeader>
                        <EmptyMedia variant="icon"><ServerOffIcon /></EmptyMedia>
                        <EmptyTitle>没有可用节点</EmptyTitle>
                        <EmptyDescription>检查该用户套餐绑定的分流方案里有没有选中节点组。</EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  ) : (
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>名称</TableHead>
                          <TableHead>面板</TableHead>
                          <TableHead>协议</TableHead>
                          <TableHead>Clash</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {preview.data.entries.map((entry) => (
                          <TableRow key={`${entry.panelId}-${entry.name}-${entry.link}`}>
                            <TableCell className="font-medium">{entry.name}</TableCell>
                            <TableCell className="tabular-nums text-muted-foreground">#{entry.panelId}</TableCell>
                            <TableCell><CodeText>{entry.protocol}</CodeText></TableCell>
                            <TableCell>{entry.clashSupported ? <StatusBadge tone="good">支持</StatusBadge> : <StatusBadge tone="bad">不支持</StatusBadge>}</TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  )}
                </TabsContent>
                <TabsContent value="clash">
                  {preview.data.renderError ? (
                    <Alert variant="destructive">
                      <TriangleAlertIcon />
                      <AlertTitle>Clash 配置渲染失败</AlertTitle>
                      <AlertDescription>{preview.data.renderError}</AlertDescription>
                    </Alert>
                  ) : (
                    <CodeTextarea aria-label="Clash 配置" readOnly rows={14} className="max-h-[50vh]" value={preview.data.clash} />
                  )}
                </TabsContent>
              </Tabs>
            </FieldGroup>
          )}
          <DialogFooter className="flex-wrap">
            <Button type="button" variant="outline" onClick={() => setPreview(null)}>
              关闭
            </Button>
            <Button
              type="button"
              disabled={!preview?.data.clash || Boolean(preview?.data.renderError)}
              onClick={() => preview && void copy(preview.data.clash, 'Clash 配置')}
            >
              <CopyIcon data-icon="inline-start" />
              复制配置
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={pending?.kind === 'delete' ? `删除用户「${pending.user.username}」？` : `清空「${pending?.user.username ?? ''}」的流量统计？`}
        description={
          pending?.kind === 'delete'
            ? '会同时从所有面板移除该用户的客户端。'
            : pending && pending.user.nextTrafficReset > 0
              ? '面板侧计数会一并重置，用户会重新可用。自动重置日不变。'
              : '面板侧计数会一并重置，用户会重新可用。'
        }
        confirmLabel={pending?.kind === 'delete' ? '删除用户' : '清空流量'}
        onConfirm={confirmAction}
      />
    </div>
  )
}

function UserStatuses({ user }: { user: User }) {
  const accountStatus = !user.enabled ? (
    <StatusBadge tone="idle">已停用</StatusBadge>
  ) : user.depleted ? (
    <StatusBadge tone="bad">流量耗尽</StatusBadge>
  ) : user.expiresAt > 0 && user.expiresAt <= Date.now() ? (
    <StatusBadge tone="bad">已过期</StatusBadge>
  ) : (
    <StatusBadge tone="good">正常</StatusBadge>
  )

  return (
    <div className="flex flex-wrap gap-1">
      {accountStatus}
      {user.syncState === 'failed' && (
        <Tooltip>
          <TooltipTrigger
            render={<span tabIndex={0} className="rounded-4xl outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50" />}
            aria-label={`${user.username} 下发失败：${user.syncErrors.join('；') || '面板下发失败'}`}
          >
            <StatusBadge tone="bad">下发失败</StatusBadge>
          </TooltipTrigger>
          <TooltipContent>{user.syncErrors.join('；') || '面板下发失败'}</TooltipContent>
        </Tooltip>
      )}
      {user.syncState === 'pending' && <StatusBadge tone="warn">待下发</StatusBadge>}
    </div>
  )
}

const PANEL_BLOCKED_LABELS: Record<string, string> = {
  'panel-quota': '面板总流量已用满',
  'user-panel-quota': '已用满套餐在该面板的限额',
}

/**
 * PanelUsage summarizes the user's usage on each panel in one line and puts
 * the per-panel breakdown in a tooltip, so a closed panel shows in the list.
 */
function PanelUsage({ user }: { user: User }) {
  const panels = user.panels ?? []
  if (panels.length === 0) return null
  const blocked = panels.filter((panel) => panel.blocked)
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            className="w-fit text-left text-xs text-muted-foreground underline decoration-dotted underline-offset-2"
            aria-label={`${user.username} 在各面板的用量`}
          />
        }
      >
        {panels.length} 个面板
        {blocked.length > 0 && <span className="text-destructive">，{blocked.length} 个已停用</span>}
      </TooltipTrigger>
      <TooltipContent side="bottom" className="max-w-sm">
        <ul className="flex flex-col gap-1.5">
          {panels.map((panel) => (
            <PanelUsageLine key={panel.panelId} panel={panel} />
          ))}
        </ul>
      </TooltipContent>
    </Tooltip>
  )
}

function PanelUsageLine({ panel }: { panel: UserPanelUsage }) {
  return (
    <li className="flex flex-col">
      <span className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-medium">{panel.panelName || `#${panel.panelId}`}</span>
        <span className="tabular-nums">
          {formatBytes(panel.upload + panel.download)}
          {panel.limit > 0 && ` / ${formatQuota(panel.limit)}`}
        </span>
      </span>
      {panel.blocked && <span>{PANEL_BLOCKED_LABELS[panel.blocked] ?? panel.blocked}，已停用</span>}
    </li>
  )
}

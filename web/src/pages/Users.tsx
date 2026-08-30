import { useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { CopyIcon, MoreHorizontalIcon, PlusIcon, TriangleAlertIcon, UsersIcon } from 'lucide-react'

import { api } from '@/api'
import type { Plan, SubscriptionPreview, User } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
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
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
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
} from '@/lib/format'

type Draft = {
  id?: number
  username: string
  uuid: string
  planId: number
  enabled: boolean
  expiresAt: number
  trafficGb: number
  remark: string
  /** Unset lets the backend inherit the plan's defaults on create. */
  inheritPlan: boolean
}

type PendingAction = {
  user: User
  kind: 'delete' | 'resetTraffic'
}

export function UsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [plans, setPlans] = useState<Plan[]>([])
  const [draft, setDraft] = useState<Draft | null>(null)
  const [preview, setPreview] = useState<{ user: User; data: SubscriptionPreview } | null>(null)
  const [pending, setPending] = useState<PendingAction | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    const [u, p] = await Promise.all([api.get<User[]>('/users'), api.get<Plan[]>('/plans')])
    setUsers(u)
    setPlans(p)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const planItems = useMemo(
    () => [
      { label: '不分配（用户将没有任何节点）', value: 0 },
      ...plans.map((p) => ({ label: p.name, value: p.id })),
    ],
    [plans],
  )

  const save = async () => {
    if (!draft) return
    setBusy(true)
    const body: Record<string, unknown> = {
      username: draft.username,
      uuid: draft.uuid || undefined,
      planId: draft.planId,
      enabled: draft.enabled,
      remark: draft.remark,
    }
    if (!draft.inheritPlan || draft.id) {
      body.expiresAt = draft.expiresAt
      body.trafficLimit = gbToBytes(draft.trafficGb)
    }
    try {
      const res = draft.id
        ? await api.put<{ syncError: string }>(`/users/${draft.id}`, body)
        : await api.post<{ syncError: string }>('/users', body)
      if (res.syncError) toast.error(`已保存，但下发到面板时出错：${res.syncError}`)
      else toast.success('已保存并下发')
      setDraft(null)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setBusy(false)
    }
  }

  const act = async (label: string, fn: () => Promise<unknown>) => {
    try {
      await fn()
      toast.success(`${label}完成`)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, `${label}失败`))
    }
  }

  const openPreview = async (u: User) => {
    try {
      const data = await api.get<SubscriptionPreview>(`/users/${u.id}/subscription`)
      setPreview({ user: u, data })
    } catch (err) {
      toast.error(errorMessage(err, '获取订阅失败'))
    }
  }

  const copySub = async (u: User) => {
    const ok = await copyText(u.subUrl)
    if (ok) toast.success('订阅链接已复制')
    else toast.error('复制失败，请手动复制')
  }

  const newDraft = (): Draft => ({
    username: '',
    uuid: '',
    planId: plans[0]?.id ?? 0,
    enabled: true,
    expiresAt: 0,
    trafficGb: 0,
    remark: '',
    inheritPlan: true,
  })

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="用户" description="一个用户在所有可用节点上共用同一 UUID，订阅链接聚合全部面板。">
        <Button onClick={() => setDraft(newDraft())} disabled={plans.length === 0}>
          <PlusIcon data-icon="inline-start" />
          {plans.length === 0 ? '请先创建套餐' : '新建用户'}
        </Button>
      </PageHeader>

      <Card>
        <CardContent>
          {users.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <UsersIcon />
                </EmptyMedia>
                <EmptyTitle>还没有用户</EmptyTitle>
                <EmptyDescription>创建用户后，FireX 会把客户端下发到套餐涉及的每个面板。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户名</TableHead>
                  <TableHead>套餐</TableHead>
                  <TableHead>流量</TableHead>
                  <TableHead>到期</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最近拉取</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <span className="font-medium">{u.username}</span>
                        {u.remark && <span className="text-xs text-muted-foreground">{u.remark}</span>}
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{u.planName || '未分配'}</TableCell>
                    <TableCell className="min-w-40">
                      <div className="flex flex-col gap-1.5">
                        <span className="whitespace-nowrap tabular-nums">
                          {formatBytes(u.used)} / {formatQuota(u.trafficLimit)}
                        </span>
                        {u.trafficLimit > 0 && (
                          <Progress value={Math.min(100, (u.used / u.trafficLimit) * 100)} />
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatExpiry(u.expiresAt)}
                    </TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">{u.nodeCount}</TableCell>
                    <TableCell>
                      <UserStatus user={u} />
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">{formatTime(u.lastSubAt)}</TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-2">
                        <Button variant="outline" size="sm" onClick={() => copySub(u)}>
                          <CopyIcon data-icon="inline-start" />
                          复制订阅
                        </Button>
                        <Button variant="outline" size="sm" onClick={() => openPreview(u)}>
                          查看
                        </Button>
                        <DropdownMenu>
                          <DropdownMenuTrigger
                            render={<Button variant="ghost" size="icon-sm" aria-label="更多操作" />}
                          >
                            <MoreHorizontalIcon />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuGroup>
                              <DropdownMenuItem
                                onClick={() =>
                                  setDraft({
                                    id: u.id,
                                    username: u.username,
                                    uuid: u.uuid,
                                    planId: u.planId,
                                    enabled: u.enabled,
                                    expiresAt: u.expiresAt,
                                    trafficGb: bytesToGb(u.trafficLimit),
                                    remark: u.remark,
                                    inheritPlan: false,
                                  })
                                }
                              >
                                编辑
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() => act('重新下发', () => api.post(`/users/${u.id}/resync`))}
                              >
                                重新下发
                              </DropdownMenuItem>
                              <DropdownMenuItem onClick={() => setPending({ user: u, kind: 'resetTraffic' })}>
                                清空流量
                              </DropdownMenuItem>
                            </DropdownMenuGroup>
                            <DropdownMenuSeparator />
                            <DropdownMenuGroup>
                              <DropdownMenuItem
                                variant="destructive"
                                onClick={() => setPending({ user: u, kind: 'delete' })}
                              >
                                删除
                              </DropdownMenuItem>
                            </DropdownMenuGroup>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={draft !== null} onOpenChange={(open) => !open && setDraft(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{draft?.id ? `编辑用户 ${draft.username}` : '新建用户'}</DialogTitle>
            <DialogDescription>保存后会立即同步到套餐涉及的所有面板。</DialogDescription>
          </DialogHeader>
          {draft && (
            <FieldGroup>
              <Field data-disabled={draft.id ? true : undefined}>
                <FieldLabel htmlFor="user-name">用户名</FieldLabel>
                <Input
                  id="user-name"
                  value={draft.username}
                  disabled={!!draft.id}
                  onChange={(e) => setDraft({ ...draft, username: e.target.value })}
                />
                <FieldDescription>
                  {draft.id
                    ? '用户名对应面板上的客户端邮箱，创建后不可修改'
                    : '将作为面板上的客户端邮箱 <用户名>@firex，只允许字母、数字、- _ .'}
                </FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor="user-plan">套餐</FieldLabel>
                <Select
                  items={planItems}
                  value={draft.planId}
                  onValueChange={(value) => setDraft({ ...draft, planId: Number(value) })}
                >
                  <SelectTrigger id="user-plan" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {planItems.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>

              {!draft.id && (
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="user-inherit">流量与到期时间沿用套餐默认值</FieldLabel>
                  <Switch
                    id="user-inherit"
                    checked={draft.inheritPlan}
                    onCheckedChange={(v) => setDraft({ ...draft, inheritPlan: v })}
                  />
                </Field>
              )}

              {(draft.id || !draft.inheritPlan) && (
                <div className="grid gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="user-traffic">流量上限 (GB)</FieldLabel>
                    <Input
                      id="user-traffic"
                      type="number"
                      min={0}
                      step="0.1"
                      value={draft.trafficGb}
                      onChange={(e) => setDraft({ ...draft, trafficGb: Number(e.target.value) })}
                    />
                    <FieldDescription>0 表示不限</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="user-expiry">到期日</FieldLabel>
                    <Input
                      id="user-expiry"
                      type="date"
                      value={dateInputValue(draft.expiresAt)}
                      onChange={(e) => setDraft({ ...draft, expiresAt: dateInputToMs(e.target.value) })}
                    />
                    <FieldDescription>留空表示永久</FieldDescription>
                  </Field>
                </div>
              )}

              <Field>
                <FieldLabel htmlFor="user-uuid">UUID</FieldLabel>
                <Input
                  id="user-uuid"
                  className="font-mono"
                  value={draft.uuid}
                  onChange={(e) => setDraft({ ...draft, uuid: e.target.value })}
                />
                <FieldDescription>留空自动生成；所有节点共用这一个 UUID</FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="user-remark">备注</FieldLabel>
                <Input
                  id="user-remark"
                  value={draft.remark}
                  onChange={(e) => setDraft({ ...draft, remark: e.target.value })}
                />
              </Field>
              <Field orientation="horizontal">
                <FieldLabel htmlFor="user-enabled">启用</FieldLabel>
                <Switch
                  id="user-enabled"
                  checked={draft.enabled}
                  onCheckedChange={(v) => setDraft({ ...draft, enabled: v })}
                />
              </Field>
            </FieldGroup>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setDraft(null)}>
              取消
            </Button>
            <Button onClick={save} disabled={busy}>
              {busy && <Spinner data-icon="inline-start" />}
              保存并下发
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={preview !== null} onOpenChange={(open) => !open && setPreview(null)}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{preview?.user.username} 的订阅</DialogTitle>
            <DialogDescription>
              Clash 类客户端自动识别；其他客户端可加 <code>?target=base64</code> 强制取分享链接列表。
            </DialogDescription>
          </DialogHeader>
          {preview && (
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="sub-url">订阅链接</FieldLabel>
                <Input
                  id="sub-url"
                  className="font-mono"
                  readOnly
                  value={preview.data.subUrl}
                  onFocus={(e) => e.target.select()}
                />
              </Field>

              {preview.data.warnings.length > 0 && (
                <Alert variant="warning">
                  <TriangleAlertIcon />
                  <AlertTitle>生成订阅时有警告</AlertTitle>
                  <AlertDescription>
                    <ul className="flex flex-col gap-1">
                      {preview.data.warnings.map((w, i) => (
                        <li key={i} className="font-mono text-xs">
                          {w}
                        </li>
                      ))}
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
                        <EmptyTitle>没有可用节点</EmptyTitle>
                        <EmptyDescription>检查该用户的套餐是否包含已启用的节点。</EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  ) : (
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>名称</TableHead>
                          <TableHead>地区</TableHead>
                          <TableHead>协议</TableHead>
                          <TableHead>Clash</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {preview.data.entries.map((e, i) => (
                          <TableRow key={i}>
                            <TableCell className="font-medium">{e.name}</TableCell>
                            <TableCell className="text-muted-foreground">{e.region || '—'}</TableCell>
                            <TableCell className="text-muted-foreground">{e.protocol}</TableCell>
                            <TableCell>
                              {e.clashSupported ? (
                                <StatusBadge tone="good">支持</StatusBadge>
                              ) : (
                                <StatusBadge tone="bad">不支持</StatusBadge>
                              )}
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  )}
                </TabsContent>
                <TabsContent value="clash">
                  <Textarea
                    readOnly
                    rows={14}
                    className="max-h-[50vh] font-mono text-xs"
                    value={preview.data.renderError || preview.data.clash}
                  />
                </TabsContent>
              </Tabs>
            </FieldGroup>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={async () => {
                if (!preview) return
                const ok = await copyText(preview.data.clash)
                if (ok) toast.success('已复制配置')
                else toast.error('复制失败')
              }}
            >
              <CopyIcon data-icon="inline-start" />
              复制配置
            </Button>
            <Button onClick={() => setPreview(null)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => !open && setPending(null)}
        title={
          pending?.kind === 'delete'
            ? `删除用户「${pending.user.username}」？`
            : `清空「${pending?.user.username ?? ''}」的流量统计？`
        }
        description={
          pending?.kind === 'delete'
            ? '会同时从所有面板移除该用户的客户端。'
            : '面板侧的计数也会一并重置，用户会因此重新可用。'
        }
        confirmLabel={pending?.kind === 'delete' ? '删除' : '清空'}
        onConfirm={async () => {
          if (!pending) return
          if (pending.kind === 'delete') {
            await act('删除', () => api.del(`/users/${pending.user.id}`))
          } else {
            await act('流量重置', () => api.post(`/users/${pending.user.id}/resetTraffic`))
          }
        }}
      />
    </div>
  )
}

function UserStatus({ user }: { user: User }) {
  if (!user.enabled) return <StatusBadge tone="idle">已停用</StatusBadge>
  if (user.depleted) return <StatusBadge tone="bad">流量耗尽</StatusBadge>
  if (user.expiresAt > 0 && user.expiresAt <= Date.now()) return <StatusBadge tone="bad">已过期</StatusBadge>
  if (user.syncState === 'failed') {
    return (
      <StatusBadge tone="bad" title={user.syncErrors.join('\n')}>
        下发失败
      </StatusBadge>
    )
  }
  if (user.syncState === 'pending') return <StatusBadge tone="warn">待下发</StatusBadge>
  return <StatusBadge tone="good">正常</StatusBadge>
}

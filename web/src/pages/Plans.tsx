import { useCallback, useEffect, useMemo, useState } from 'react'
import { PlusIcon, RefreshCwIcon, TicketIcon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { Plan, Profile } from '@/api'
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { bytesToGb, errorMessage, formatQuota, gbToBytes } from '@/lib/format'

type Draft = {
  id?: number
  name: string
  trafficGb: number
  durationDays: number
  deviceLimit: number
  speedNote: string
  enabled: boolean
  sortOrder: number
  remark: string
  profileId: number
}

const emptyDraft: Draft = {
  name: '',
  trafficGb: 100,
  durationDays: 30,
  deviceLimit: 3,
  speedNote: '',
  enabled: true,
  sortOrder: 100,
  remark: '',
  profileId: 0,
}

export function PlansPage() {
  const [plans, setPlans] = useState<Plan[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Plan | null>(null)

  const load = useCallback(async () => {
    try {
      const [nextPlans, nextProfiles] = await Promise.all([
        api.get<Plan[]>('/plans'),
        api.get<Profile[]>('/profiles'),
      ])
      setPlans(nextPlans)
      setProfiles(nextProfiles)
      setLoadError(null)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '套餐数据加载失败'))
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

  const openCreate = () => {
    setShowErrors(false)
    setDraft({ ...emptyDraft })
  }

  const openEdit = (plan: Plan) => {
    setShowErrors(false)
    setDraft({
      id: plan.id,
      name: plan.name,
      trafficGb: bytesToGb(plan.trafficBytes),
      durationDays: plan.durationDays,
      deviceLimit: plan.deviceLimit,
      speedNote: plan.speedNote,
      enabled: plan.enabled,
      sortOrder: plan.sortOrder,
      remark: plan.remark,
      profileId: plan.profileId,
    })
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft) return
    setShowErrors(true)
    if (!draft.name.trim()) return

    setSaving(true)
    const body = {
      name: draft.name.trim(),
      trafficBytes: gbToBytes(draft.trafficGb),
      durationDays: draft.durationDays,
      deviceLimit: draft.deviceLimit,
      speedNote: draft.speedNote,
      enabled: draft.enabled,
      sortOrder: draft.sortOrder,
      remark: draft.remark,
      profileId: draft.profileId,
    }

    try {
      if (draft.id) {
        const result = await api.put<{ syncError: string }>(`/plans/${draft.id}`, body)
        if (result.syncError) toast.error(`已保存，但下发到面板时出错：${result.syncError}`)
        else toast.success('已保存并同步到面板')
      } else {
        await api.post('/plans', body)
        toast.success('套餐已创建')
      }
      setDraft(null)
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (plan: Plan) => {
    try {
      await api.del(`/plans/${plan.id}`)
      toast.success('套餐已删除')
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  const profileItems = useMemo(
    () => [
      { value: '0', label: '不绑定（用户拿不到任何节点）' },
      ...profiles.map((profile) => ({ value: String(profile.id), label: profile.name })),
    ],
    [profiles],
  )

  const enabledPlans = plans.filter((plan) => plan.enabled).length
  const assignedUsers = plans.reduce((total, plan) => total + plan.userCount, 0)
  const nameInvalid = Boolean(showErrors && draft && !draft.name.trim())

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="套餐" description="把节点、流量与有效期组合成可复用的交付方案。">
        <Button onClick={openCreate}>
          <PlusIcon data-icon="inline-start" />
          新建套餐
        </Button>
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>套餐列表</CardTitle>
          <CardDescription>集中管理交付规则；修改套餐后，关联用户会自动重新下发。</CardDescription>
          <CardAction className="flex items-center gap-2">
            {!loading && <Badge variant="secondary">{enabledPlans} 个启用</Badge>}
            <Button variant="ghost" size="icon-sm" aria-label="刷新套餐列表" disabled={loading || refreshing} onClick={refresh}>
              {refreshing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading ? (
            <div className="flex flex-col gap-3 px-(--card-spacing)" aria-label="套餐加载中">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </div>
          ) : loadError && plans.length === 0 ? (
            <div className="px-(--card-spacing)">
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>无法加载套餐</AlertTitle>
                <AlertDescription>{loadError}</AlertDescription>
                <AlertAction>
                  <Button variant="outline" size="sm" onClick={refresh} disabled={refreshing}>
                    {refreshing && <Spinner data-icon="inline-start" />}
                    重试
                  </Button>
                </AlertAction>
              </Alert>
            </div>
          ) : plans.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <TicketIcon />
                </EmptyMedia>
                <EmptyTitle>还没有套餐</EmptyTitle>
                <EmptyDescription>创建第一个套餐，给它绑一个分流方案，再把用户放进来。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={openCreate}>
                  <PlusIcon data-icon="inline-start" />
                  新建套餐
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
                    <TableHead>名称</TableHead>
                    <TableHead>流量</TableHead>
                    <TableHead className="hidden md:table-cell">时长</TableHead>
                    <TableHead className="hidden lg:table-cell">设备数</TableHead>
                    <TableHead className="hidden sm:table-cell">分流方案</TableHead>
                    <TableHead>用户</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead><span className="sr-only">操作</span></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {plans.map((plan) => (
                    <TableRow key={plan.id}>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <strong>{plan.name}</strong>
                          {plan.remark && <span className="text-muted-foreground">{plan.remark}</span>}
                          {plan.speedNote && <span className="text-muted-foreground">{plan.speedNote}</span>}
                        </div>
                      </TableCell>
                      <TableCell>{formatQuota(plan.trafficBytes)}</TableCell>
                      <TableCell className="hidden md:table-cell">
                        {plan.durationDays > 0 ? `${plan.durationDays} 天` : '永久'}
                      </TableCell>
                      <TableCell className="hidden lg:table-cell">
                        {plan.deviceLimit > 0 ? plan.deviceLimit : '不限'}
                      </TableCell>
                      <TableCell className="hidden sm:table-cell">
                        {plan.profileId === 0 ? (
                          <StatusBadge tone="warn">未绑定</StatusBadge>
                        ) : (
                          <span>
                            {plan.profileName}
                            <span className="ml-1 text-muted-foreground tabular-nums">({plan.usableInbounds})</span>
                          </span>
                        )}
                      </TableCell>
                      <TableCell>{plan.userCount}</TableCell>
                      <TableCell>
                        {plan.enabled ? <StatusBadge tone="good">启用</StatusBadge> : <StatusBadge tone="idle">停用</StatusBadge>}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-2">
                          <Button variant="outline" size="sm" onClick={() => openEdit(plan)}>编辑</Button>
                          <Button
                            variant="destructive"
                            size="sm"
                            disabled={plan.userCount > 0}
                            aria-label={plan.userCount > 0 ? `套餐 ${plan.name} 仍有用户，不能删除` : `删除套餐 ${plan.name}`}
                            onClick={() => setPendingDelete(plan)}
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
            {loading ? '正在读取套餐状态' : loadError && plans.length === 0 ? '套餐统计暂不可用' : `共 ${plans.length} 个套餐`}
          </span>
          <span className="text-muted-foreground">
            {loading || (loadError && plans.length === 0) ? '—' : `覆盖 ${assignedUsers} 个用户`}
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
            <DialogTitle>{draft?.id ? '编辑套餐' : '新建套餐'}</DialogTitle>
            <DialogDescription>套餐只管配额与时长，用户能用哪些节点由绑定的分流方案决定。保存后会立即同步到面板。</DialogDescription>
          </DialogHeader>
          {draft && (
            <form className="flex min-h-0 flex-col gap-4" noValidate onSubmit={save}>
              <FieldGroup>
                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <Field data-invalid={nameInvalid}>
                    <FieldLabel htmlFor="plan-name">名称</FieldLabel>
                    <Input id="plan-name" value={draft.name} required aria-invalid={nameInvalid} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
                    {nameInvalid && <FieldError>请输入套餐名称。</FieldError>}
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="plan-remark">备注</FieldLabel>
                    <Input id="plan-remark" value={draft.remark} onChange={(event) => setDraft({ ...draft, remark: event.target.value })} />
                  </Field>
                </FieldGroup>

                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="plan-traffic">流量 (GB)</FieldLabel>
                    <Input id="plan-traffic" type="number" min={0} step="0.1" value={draft.trafficGb} onChange={(event) => setDraft({ ...draft, trafficGb: Number(event.target.value) })} />
                    <FieldDescription>0 表示不限；新建用户时作为默认值。</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="plan-duration">时长 (天)</FieldLabel>
                    <Input id="plan-duration" type="number" min={0} value={draft.durationDays} onChange={(event) => setDraft({ ...draft, durationDays: Number(event.target.value) })} />
                    <FieldDescription>0 表示永久；用于计算新用户到期时间。</FieldDescription>
                  </Field>
                </FieldGroup>

                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="plan-devices">设备数上限</FieldLabel>
                    <Input id="plan-devices" type="number" min={0} value={draft.deviceLimit} onChange={(event) => setDraft({ ...draft, deviceLimit: Number(event.target.value) })} />
                    <FieldDescription>0 表示不限，对应 3x-ui 的 IP 限制。</FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="plan-sort">排序</FieldLabel>
                    <Input id="plan-sort" type="number" value={draft.sortOrder} onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })} />
                  </Field>
                </FieldGroup>

                <Field>
                  <FieldLabel htmlFor="plan-speed-note">速率说明</FieldLabel>
                  <Input id="plan-speed-note" placeholder="例如：峰值 500 Mbps" value={draft.speedNote} onChange={(event) => setDraft({ ...draft, speedNote: event.target.value })} />
                  <FieldDescription>仅用于记录与辨识套餐，不参与限速。</FieldDescription>
                </Field>

                <Field>
                  <FieldLabel htmlFor="plan-profile">分流方案</FieldLabel>
                  <Select
                    items={profileItems}
                    value={String(draft.profileId)}
                    onValueChange={(value) => setDraft({ ...draft, profileId: Number(value) })}
                  >
                    <SelectTrigger id="plan-profile" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="0">不绑定（用户拿不到任何节点）</SelectItem>
                        {profiles.map((profile) => (
                          <SelectItem key={profile.id} value={String(profile.id)}>
                            {profile.name}
                            <span className="text-muted-foreground">{profile.usableInbounds} 个入站</span>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {profiles.length === 0
                      ? '还没有分流方案，先到「分流」页建一个。'
                      : '方案的可用节点组决定这个套餐的用户能连上哪些入站。'}
                  </FieldDescription>
                </Field>

                <Field orientation="horizontal">
                  <FieldLabel htmlFor="plan-enabled">启用套餐</FieldLabel>
                  <Switch id="plan-enabled" checked={draft.enabled} onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} />
                </Field>
              </FieldGroup>

              <DialogFooter>
                <Button type="button" variant="outline" disabled={saving} onClick={() => setDraft(null)}>取消</Button>
                <Button type="submit" disabled={saving}>
                  {saving && <Spinner data-icon="inline-start" />}
                  {saving ? '保存中…' : '保存套餐'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除套餐「${pendingDelete?.name ?? ''}」？`}
        description="删除后无法恢复。只有没有用户关联的套餐才可以删除。"
        confirmLabel="删除套餐"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

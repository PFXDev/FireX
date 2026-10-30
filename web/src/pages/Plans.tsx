import { useCallback, useEffect, useMemo, useState } from 'react'
import { PencilIcon, PlusIcon, TicketIcon, Trash2Icon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { NodeGroup, Panel, Plan, Profile, TrafficReset } from '@/api'
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
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { bytesToGb, errorMessage, formatQuota, gbToBytes, TRAFFIC_RESET_LABELS } from '@/lib/format'
import { reportSyncError } from '@/lib/sync'
import { useSession } from '@/lib/session'

type Draft = {
  id?: number
  name: string
  trafficGb: number
  durationDays: number
  deviceLimit: number
  trafficReset: TrafficReset
  speedNote: string
  enabled: boolean
  sortOrder: number
  remark: string
  profileId: number
  /** Per-user cap on each panel in GB, keyed by panel id; 0 or absent = none. */
  panelLimitsGb: Record<number, number>
  /** Per-user quota on each node group in GB, keyed by group id; 0 or absent = none. */
  groupLimitsGb: Record<number, number>
}

const trafficResetItems = (Object.keys(TRAFFIC_RESET_LABELS) as TrafficReset[]).map((value) => ({
  value,
  label: TRAFFIC_RESET_LABELS[value],
}))

const emptyDraft: Draft = {
  name: '',
  trafficGb: 100,
  durationDays: 30,
  deviceLimit: 3,
  trafficReset: 'none',
  speedNote: '',
  enabled: true,
  sortOrder: 100,
  remark: '',
  profileId: 0,
  panelLimitsGb: {},
  groupLimitsGb: {},
}

export function PlansPage() {
  // Operators keep a read-only view: plans decide quota and routing, so only a
  // super admin may change them.
  const { isSuperAdmin } = useSession()
  const [plans, setPlans] = useState<Plan[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  // Null when the panel list is unavailable (an operator without config
  // access); the plan's caps are then shown but never sent back.
  const [panels, setPanels] = useState<Panel[] | null>(null)
  const [groups, setGroups] = useState<NodeGroup[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Plan | null>(null)

  const load = useCallback(async () => {
    try {
      const [nextPlans, nextProfiles, nextPanels, nextGroups] = await Promise.all([
        api.get<Plan[]>('/plans'),
        api.get<Profile[]>('/profiles'),
        api.get<Panel[]>('/panels', { unauthorized: 'ignore' }).catch(() => null),
        api.get<NodeGroup[]>('/node-groups', { unauthorized: 'ignore' }).catch(() => null),
      ])
      setPlans(nextPlans)
      setProfiles(nextProfiles)
      setPanels(nextPanels)
      setGroups(nextGroups)
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
      trafficReset: plan.trafficReset || 'none',
      speedNote: plan.speedNote,
      enabled: plan.enabled,
      sortOrder: plan.sortOrder,
      remark: plan.remark,
      profileId: plan.profileId,
      panelLimitsGb: Object.fromEntries(plan.panelLimits.map((limit) => [limit.panelId, bytesToGb(limit.trafficBytes)])),
      groupLimitsGb: Object.fromEntries(plan.groupLimits.map((limit) => [limit.groupId, bytesToGb(limit.trafficBytes)])),
    })
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || saving) return
    setShowErrors(true)
    if (!draft.name.trim()) return
    if (!event.currentTarget.reportValidity()) return

    setSaving(true)
    const body = {
      name: draft.name.trim(),
      trafficBytes: gbToBytes(draft.trafficGb),
      durationDays: draft.durationDays,
      deviceLimit: draft.deviceLimit,
      trafficReset: draft.trafficReset,
      speedNote: draft.speedNote,
      enabled: draft.enabled,
      sortOrder: draft.sortOrder,
      remark: draft.remark,
      profileId: draft.profileId,
      // Only panels that still exist; the server rejects unknown ids and
      // drops zero caps.
      panelLimits: panels
        ?.map((panel) => ({ panelId: panel.id, trafficBytes: gbToBytes(draft.panelLimitsGb[panel.id] ?? 0) }))
        .filter((limit) => limit.trafficBytes > 0),
      groupLimits: grantedGroups
        ?.map((group) => ({ groupId: group.id, trafficBytes: gbToBytes(draft.groupLimitsGb[group.id] ?? 0) }))
        .filter((limit) => limit.trafficBytes > 0),
    }

    try {
      if (draft.id) {
        const result = await api.put<{ syncError: string }>(`/plans/${draft.id}`, body)
        toast.success(result.syncError ? '套餐已保存' : '套餐已保存并同步到面板')
        reportSyncError(result.syncError)
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
  // Only the groups the chosen profile grants can carry a quota; null when
  // the group list is unavailable.
  const grantedGroups = useMemo(() => {
    if (!groups || !draft) return null
    const profile = profiles.find((item) => item.id === draft.profileId)
    if (!profile || !profile.enabled) return []
    return groups.filter((group) => group.enabled && (profile.allGroups || profile.groupIds.includes(group.id)))
  }, [groups, profiles, draft])

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="套餐" description="把节点、流量与有效期组合成可复用的交付方案。">
        {isSuperAdmin && (
          <Button onClick={openCreate}>
            <PlusIcon data-icon="inline-start" />
            新建套餐
          </Button>
        )}
      </PageHeader>

      <Card>
        <CardHeader>
          <CardTitle>套餐列表</CardTitle>
          <CardDescription>
            {loading || (loadError && plans.length === 0)
              ? '集中管理交付规则；修改套餐后，关联用户会自动重新下发。'
              : `共 ${plans.length} 个套餐，${enabledPlans} 个启用。修改套餐后，关联用户会自动重新下发。`}
          </CardDescription>
          <CardAction>
            <RefreshButton loading={loading || refreshing} disabled={saving} onClick={() => void refresh()} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading ? (
            <TableSkeleton
              columns={[
                { label: '名称', width: 'w-28' },
                { label: '流量', width: 'w-16' },
                { label: '时长', className: 'hidden md:table-cell', width: 'w-12' },
                { label: '设备数', className: 'hidden lg:table-cell', width: 'w-10' },
                { label: '分流方案', className: 'hidden sm:table-cell', width: 'w-24' },
                { label: '用户', width: 'w-8' },
                { label: '状态', width: 'w-14' },
                { width: 'w-24' },
              ]}
            />
          ) : loadError && plans.length === 0 ? (
            <div className="px-4">
              <LoadErrorAlert title="套餐加载失败" message={loadError} retrying={refreshing} onRetry={() => void refresh()} />
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
              {isSuperAdmin && (
                <EmptyContent>
                  <Button onClick={openCreate}>
                    <PlusIcon data-icon="inline-start" />
                    新建套餐
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
                          <span className="font-medium">{plan.name}</span>
                          {plan.remark && <span className="text-xs text-muted-foreground">{plan.remark}</span>}
                          {plan.speedNote && <span className="text-xs text-muted-foreground">{plan.speedNote}</span>}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col gap-1">
                          <span className="tabular-nums">{formatQuota(plan.trafficBytes)}</span>
                          {plan.trafficReset && plan.trafficReset !== 'none' && (
                            <span className="text-xs text-muted-foreground">{TRAFFIC_RESET_LABELS[plan.trafficReset]}重置</span>
                          )}
                          {plan.groupLimits.length > 0 && (
                            <span
                              className="text-xs text-muted-foreground"
                              title={plan.groupLimits
                                .map((limit) => `${groups?.find((group) => group.id === limit.groupId)?.name ?? `#${limit.groupId}`}：${formatQuota(limit.trafficBytes)}`)
                                .join('\n')}
                            >
                              {plan.groupLimits.length} 个节点组限额
                            </span>
                          )}
                          {plan.panelLimits.length > 0 && (
                            <span
                              className="text-xs text-muted-foreground"
                              title={plan.panelLimits
                                .map((limit) => `${panels?.find((panel) => panel.id === limit.panelId)?.name ?? `#${limit.panelId}`}：${formatQuota(limit.trafficBytes)}`)
                                .join('\n')}
                            >
                              {plan.panelLimits.length} 个面板单独限额
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="hidden tabular-nums text-muted-foreground md:table-cell">
                        {plan.durationDays > 0 ? `${plan.durationDays} 天` : '永久'}
                      </TableCell>
                      <TableCell className="hidden tabular-nums text-muted-foreground lg:table-cell">
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
                      <TableCell className="tabular-nums">{plan.userCount}</TableCell>
                      <TableCell>
                        {plan.enabled ? <StatusBadge tone="good">启用</StatusBadge> : <StatusBadge tone="idle">停用</StatusBadge>}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          {isSuperAdmin && (
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              aria-label={`编辑 ${plan.name}`}
                              onClick={() => openEdit(plan)}
                            >
                              <PencilIcon data-icon="inline-start" />
                              <span className="hidden sm:inline">编辑</span>
                            </Button>
                          )}
                          {!isSuperAdmin ? null : plan.userCount > 0 ? (
                            <Tooltip>
                              <TooltipTrigger render={<span tabIndex={0} />}>
                                <Button type="button" variant="ghost" size="sm" disabled aria-label={`删除 ${plan.name}`}>
                                  <Trash2Icon data-icon="inline-start" />
                                  <span className="hidden sm:inline">删除</span>
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>仍有 {plan.userCount} 个用户使用此套餐，先把他们换到其他套餐</TooltipContent>
                            </Tooltip>
                          ) : (
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              aria-label={`删除 ${plan.name}`}
                              onClick={() => setPendingDelete(plan)}
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
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{draft.id ? '编辑套餐' : '新建套餐'}</DialogTitle>
                <DialogDescription>套餐只管配额与时长，用户能用哪些节点由绑定的分流方案决定。保存后会立即同步到面板。</DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <FieldGroup className="sm:grid sm:grid-cols-2">
                  <Field data-invalid={nameInvalid || undefined} data-disabled={saving || undefined}>
                    <FieldLabel htmlFor="plan-name">名称</FieldLabel>
                    <Input id="plan-name" value={draft.name} required aria-invalid={nameInvalid || undefined} disabled={saving} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
                    {nameInvalid && <FieldError>请输入套餐名称。</FieldError>}
                  </Field>
                  <Field data-disabled={saving || undefined}>
                    <FieldLabel htmlFor="plan-remark">备注</FieldLabel>
                    <Input id="plan-remark" disabled={saving} value={draft.remark} onChange={(event) => setDraft({ ...draft, remark: event.target.value })} />
                  </Field>
                </FieldGroup>

                <FieldGroup className="sm:grid sm:grid-cols-2">
                  <Field data-disabled={saving || undefined}>
                    <FieldLabel htmlFor="plan-traffic">流量 (GB)</FieldLabel>
                    <Input id="plan-traffic" disabled={saving} type="number" min={0} step="any" value={draft.trafficGb} onChange={(event) => setDraft({ ...draft, trafficGb: Number(event.target.value) })} />
                    <FieldDescription>0 表示不限；新建用户时作为默认值。</FieldDescription>
                  </Field>
                  <Field data-disabled={saving || undefined}>
                    <FieldLabel htmlFor="plan-duration">时长 (天)</FieldLabel>
                    <Input id="plan-duration" disabled={saving} type="number" min={0} value={draft.durationDays} onChange={(event) => setDraft({ ...draft, durationDays: Number(event.target.value) })} />
                    <FieldDescription>0 表示永久；用于计算新用户到期时间。</FieldDescription>
                  </Field>
                </FieldGroup>

                <Field data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="plan-reset">流量重置</FieldLabel>
                  <Select
                    items={trafficResetItems}
                    value={draft.trafficReset}
                    disabled={saving}
                    onValueChange={(value) => setDraft({ ...draft, trafficReset: value as TrafficReset })}
                  >
                    <SelectTrigger id="plan-reset" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {trafficResetItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {draft.trafficReset === 'none'
                      ? '流量上限在整个有效期内只算一次，用完即止。'
                      : draft.trafficReset === 'monthly'
                        ? '每月在用户开通日的 0 点清零已用流量（31 日开通的在小月按月末算），流量耗尽的用户会自动恢复。'
                        : '每月 1 日 0 点清零所有用户的已用流量，流量耗尽的用户会自动恢复。'}
                    {draft.id && draft.trafficReset !== (plans.find((plan) => plan.id === draft.id)?.trafficReset || 'none') &&
                      ' 修改后从现在开始按新规则计算，不会立即清零现有用量。'}
                  </FieldDescription>
                </Field>

                {grantedGroups && grantedGroups.length > 0 && (
                  <FieldSet data-disabled={saving || undefined}>
                    <FieldLegend variant="label">节点组流量额度 (GB)</FieldLegend>
                    <FieldDescription>
                      每个用户在该节点组的用量上限，按节点组里设好的比例拆到各面板；0 表示只受套餐总流量约束。用满一个面板只停用那个面板，随用户流量一起重置。
                    </FieldDescription>
                    <FieldGroup className="gap-3">
                      {grantedGroups.map((group) => {
                        const gb = draft.groupLimitsGb[group.id] ?? 0
                        return (
                          <Field key={group.id} data-disabled={saving || undefined}>
                            <div className="flex items-center gap-3">
                              <FieldLabel htmlFor={`plan-group-limit-${group.id}`} className="min-w-0 flex-1 truncate" title={group.name}>
                                {group.emoji && `${group.emoji} `}
                                {group.name}
                              </FieldLabel>
                              <Input
                                id={`plan-group-limit-${group.id}`}
                                className="w-28"
                                type="number"
                                min={0}
                                step="any"
                                disabled={saving}
                                value={gb}
                                onChange={(event) =>
                                  setDraft({
                                    ...draft,
                                    groupLimitsGb: { ...draft.groupLimitsGb, [group.id]: Number(event.target.value) },
                                  })
                                }
                              />
                            </div>
                            {gb > 0 && group.panelShares.length > 0 && (
                              <FieldDescription>
                                {group.panelShares
                                  .map(
                                    (share) =>
                                      `${panels?.find((panel) => panel.id === share.panelId)?.name ?? `#${share.panelId}`} ${share.weight}% ≈ ${formatQuota(Math.max(1, Math.floor((gbToBytes(gb) * share.weight) / 100)))}`,
                                  )
                                  .join('，')}
                              </FieldDescription>
                            )}
                          </Field>
                        )
                      })}
                    </FieldGroup>
                  </FieldSet>
                )}

                {panels && panels.length > 0 && (
                  <FieldSet data-disabled={saving || undefined}>
                    <FieldLegend variant="label">单面板流量上限 (GB)</FieldLegend>
                    <FieldDescription>
                      可选，按面板单独再收紧：和节点组拆出来的额度同时存在时取较小值。0 表示不额外限制。
                    </FieldDescription>
                    <FieldGroup className="gap-3 sm:grid sm:grid-cols-2">
                      {panels.map((panel) => (
                        <Field key={panel.id} orientation="horizontal" data-disabled={saving || undefined}>
                          <FieldLabel htmlFor={`plan-panel-limit-${panel.id}`} className="min-w-0 flex-1 truncate" title={panel.name}>
                            {panel.name}
                          </FieldLabel>
                          <Input
                            id={`plan-panel-limit-${panel.id}`}
                            className="w-28"
                            type="number"
                            min={0}
                            step="any"
                            disabled={saving}
                            value={draft.panelLimitsGb[panel.id] ?? 0}
                            onChange={(event) =>
                              setDraft({
                                ...draft,
                                panelLimitsGb: { ...draft.panelLimitsGb, [panel.id]: Number(event.target.value) },
                              })
                            }
                          />
                        </Field>
                      ))}
                    </FieldGroup>
                  </FieldSet>
                )}

                <FieldGroup className="sm:grid sm:grid-cols-2">
                  <Field data-disabled={saving || undefined}>
                    <FieldLabel htmlFor="plan-devices">设备数上限</FieldLabel>
                    <Input id="plan-devices" disabled={saving} type="number" min={0} value={draft.deviceLimit} onChange={(event) => setDraft({ ...draft, deviceLimit: Number(event.target.value) })} />
                    <FieldDescription>0 表示不限，对应 3x-ui 的 IP 限制。</FieldDescription>
                  </Field>
                  <Field data-disabled={saving || undefined}>
                    <FieldLabel htmlFor="plan-sort">排序</FieldLabel>
                    <Input id="plan-sort" disabled={saving} type="number" value={draft.sortOrder} onChange={(event) => setDraft({ ...draft, sortOrder: Number(event.target.value) })} />
                  </Field>
                </FieldGroup>

                <Field data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="plan-speed-note">速率说明</FieldLabel>
                  <Input id="plan-speed-note" disabled={saving} placeholder="例如：峰值 500 Mbps" value={draft.speedNote} onChange={(event) => setDraft({ ...draft, speedNote: event.target.value })} />
                  <FieldDescription>仅用于记录与辨识套餐，不参与限速。</FieldDescription>
                </Field>

                <Field data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="plan-profile">分流方案</FieldLabel>
                  <Select
                    items={profileItems}
                    value={String(draft.profileId)}
                    disabled={saving}
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

                <Field orientation="horizontal" data-disabled={saving || undefined}>
                  <FieldLabel htmlFor="plan-enabled">启用套餐</FieldLabel>
                  <Switch id="plan-enabled" disabled={saving} checked={draft.enabled} onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} />
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

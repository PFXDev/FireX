import { useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { PlusIcon, TicketIcon } from 'lucide-react'

import { api } from '@/api'
import type { Node, Plan } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
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
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
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
  nodeIds: number[]
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
  nodeIds: [],
}

export function PlansPage() {
  const [plans, setPlans] = useState<Plan[]>([])
  const [nodes, setNodes] = useState<Node[]>([])
  const [draft, setDraft] = useState<Draft | null>(null)
  const [busy, setBusy] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Plan | null>(null)

  const load = useCallback(async () => {
    const [p, n] = await Promise.all([api.get<Plan[]>('/plans'), api.get<Node[]>('/nodes')])
    setPlans(p)
    setNodes(n)
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const nodesByRegion = useMemo(() => {
    const groups = new Map<string, Node[]>()
    nodes
      .filter((n) => !n.missing)
      .forEach((n) => {
        const key = n.region || '未分组'
        groups.set(key, [...(groups.get(key) ?? []), n])
      })
    return [...groups.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [nodes])

  const save = async () => {
    if (!draft) return
    setBusy(true)
    const body = {
      name: draft.name,
      trafficBytes: gbToBytes(draft.trafficGb),
      durationDays: draft.durationDays,
      deviceLimit: draft.deviceLimit,
      speedNote: draft.speedNote,
      enabled: draft.enabled,
      sortOrder: draft.sortOrder,
      remark: draft.remark,
      nodeIds: draft.nodeIds,
    }
    try {
      if (draft.id) {
        const res = await api.put<{ syncError: string }>(`/plans/${draft.id}`, body)
        if (res.syncError) toast.error(`已保存，但下发到面板时出错：${res.syncError}`)
        else toast.success('已保存并同步到面板')
      } else {
        await api.post('/plans', body)
        toast.success('已创建')
      }
      setDraft(null)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setBusy(false)
    }
  }

  const remove = async (p: Plan) => {
    try {
      await api.del(`/plans/${p.id}`)
      toast.success('已删除')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
    }
  }

  const toggleNode = (id: number) => {
    if (!draft) return
    const has = draft.nodeIds.includes(id)
    setDraft({
      ...draft,
      nodeIds: has ? draft.nodeIds.filter((x) => x !== id) : [...draft.nodeIds, id],
    })
  }

  const toggleRegion = (group: Node[]) => {
    if (!draft) return
    const ids = group.map((n) => n.id)
    const allIn = ids.every((id) => draft.nodeIds.includes(id))
    setDraft({
      ...draft,
      nodeIds: allIn ? draft.nodeIds.filter((id) => !ids.includes(id)) : [...new Set([...draft.nodeIds, ...ids])],
    })
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="套餐" description="套餐决定用户能用哪些节点，以及默认的流量、时长和设备数。">
        <Button onClick={() => setDraft({ ...emptyDraft })}>
          <PlusIcon data-icon="inline-start" />
          新建套餐
        </Button>
      </PageHeader>

      <Card>
        <CardContent>
          {plans.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <TicketIcon />
                </EmptyMedia>
                <EmptyTitle>还没有套餐</EmptyTitle>
                <EmptyDescription>至少建一个套餐，用户才能拿到节点。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={() => setDraft({ ...emptyDraft })}>
                  <PlusIcon data-icon="inline-start" />
                  新建套餐
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>流量</TableHead>
                  <TableHead>时长</TableHead>
                  <TableHead>设备数</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>用户</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {plans.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <span className="font-medium">{p.name}</span>
                        {p.remark && <span className="text-xs text-muted-foreground">{p.remark}</span>}
                      </div>
                    </TableCell>
                    <TableCell className="tabular-nums">{formatQuota(p.trafficBytes)}</TableCell>
                    <TableCell className="tabular-nums">{p.durationDays > 0 ? `${p.durationDays} 天` : '永久'}</TableCell>
                    <TableCell className="tabular-nums">{p.deviceLimit > 0 ? p.deviceLimit : '不限'}</TableCell>
                    <TableCell className="tabular-nums">{p.nodeIds.length}</TableCell>
                    <TableCell className="tabular-nums">{p.userCount}</TableCell>
                    <TableCell>
                      {p.enabled ? <StatusBadge tone="good">启用</StatusBadge> : <StatusBadge tone="idle">停用</StatusBadge>}
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            setDraft({
                              id: p.id,
                              name: p.name,
                              trafficGb: bytesToGb(p.trafficBytes),
                              durationDays: p.durationDays,
                              deviceLimit: p.deviceLimit,
                              speedNote: p.speedNote,
                              enabled: p.enabled,
                              sortOrder: p.sortOrder,
                              remark: p.remark,
                              nodeIds: p.nodeIds,
                            })
                          }
                        >
                          编辑
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setPendingDelete(p)}>
                          删除
                        </Button>
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
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{draft?.id ? '编辑套餐' : '新建套餐'}</DialogTitle>
            <DialogDescription>改动会立即下发给该套餐下的所有用户。</DialogDescription>
          </DialogHeader>
          {draft && (
            <FieldGroup>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="plan-name">名称</FieldLabel>
                  <Input
                    id="plan-name"
                    value={draft.name}
                    onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="plan-remark">备注</FieldLabel>
                  <Input
                    id="plan-remark"
                    value={draft.remark}
                    onChange={(e) => setDraft({ ...draft, remark: e.target.value })}
                  />
                </Field>
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="plan-traffic">流量 (GB)</FieldLabel>
                  <Input
                    id="plan-traffic"
                    type="number"
                    min={0}
                    step="0.1"
                    value={draft.trafficGb}
                    onChange={(e) => setDraft({ ...draft, trafficGb: Number(e.target.value) })}
                  />
                  <FieldDescription>0 表示不限；新建用户时作为默认值</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="plan-duration">时长 (天)</FieldLabel>
                  <Input
                    id="plan-duration"
                    type="number"
                    min={0}
                    value={draft.durationDays}
                    onChange={(e) => setDraft({ ...draft, durationDays: Number(e.target.value) })}
                  />
                  <FieldDescription>0 表示永久；新建用户时用于计算到期时间</FieldDescription>
                </Field>
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="plan-devices">设备数上限</FieldLabel>
                  <Input
                    id="plan-devices"
                    type="number"
                    min={0}
                    value={draft.deviceLimit}
                    onChange={(e) => setDraft({ ...draft, deviceLimit: Number(e.target.value) })}
                  />
                  <FieldDescription>0 表示不限，对应 3x-ui 的 IP 限制</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="plan-sort">排序</FieldLabel>
                  <Input
                    id="plan-sort"
                    type="number"
                    value={draft.sortOrder}
                    onChange={(e) => setDraft({ ...draft, sortOrder: Number(e.target.value) })}
                  />
                </Field>
              </div>

              <FieldSet>
                <FieldLegend variant="label">包含节点（已选 {draft.nodeIds.length} 个）</FieldLegend>
                <FieldDescription>用户能看到的节点，就是套餐勾选的这一批里当前启用的部分。</FieldDescription>
                <ScrollArea className="h-64 rounded-lg border">
                  <div className="flex flex-col gap-3 p-3">
                    {nodesByRegion.length === 0 && (
                      <span className="text-sm text-muted-foreground">还没有可用节点</span>
                    )}
                    {nodesByRegion.map(([region, group]) => (
                      <div key={region} className="flex flex-col gap-2">
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-medium text-muted-foreground">{region}</span>
                          <Button type="button" variant="ghost" size="sm" onClick={() => toggleRegion(group)}>
                            全选 / 取消
                          </Button>
                        </div>
                        <FieldGroup className="gap-2">
                          {group.map((n) => (
                            <Field key={n.id} orientation="horizontal">
                              <Checkbox
                                id={`plan-node-${n.id}`}
                                checked={draft.nodeIds.includes(n.id)}
                                onCheckedChange={() => toggleNode(n.id)}
                              />
                              <FieldLabel htmlFor={`plan-node-${n.id}`} className="font-normal">
                                <span className="flex flex-wrap items-center gap-2">
                                  <span>
                                    {n.emoji && `${n.emoji} `}
                                    {n.name || n.remoteRemark || n.inboundTag}
                                  </span>
                                  <span className="text-xs text-muted-foreground">
                                    {n.panelName} · {n.protocol}:{n.port}
                                  </span>
                                  {!n.enabled && <StatusBadge tone="idle">节点未启用</StatusBadge>}
                                </span>
                              </FieldLabel>
                            </Field>
                          ))}
                        </FieldGroup>
                      </div>
                    ))}
                  </div>
                </ScrollArea>
              </FieldSet>

              <Field orientation="horizontal">
                <FieldLabel htmlFor="plan-enabled">启用</FieldLabel>
                <Switch
                  id="plan-enabled"
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
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除套餐「${pendingDelete?.name ?? ''}」？`}
        description="只有没有用户在用的套餐才能删除。"
        confirmLabel="删除"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

import { useCallback, useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { LayersIcon } from 'lucide-react'

import { api } from '@/api'
import type { Node } from '@/api'
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
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage } from '@/lib/format'

type Draft = {
  id: number
  name: string
  region: string
  emoji: string
  tags: string
  sortOrder: number
  enabled: boolean
  udp: boolean
  multiplier: number
}

export function NodesPage() {
  const [nodes, setNodes] = useState<Node[]>([])
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [draft, setDraft] = useState<Draft | null>(null)
  const [bulkRegion, setBulkRegion] = useState('')
  const [pendingDelete, setPendingDelete] = useState<Node | null>(null)

  const load = useCallback(async () => {
    setNodes(await api.get<Node[]>('/nodes'))
    setSelected(new Set())
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const regions = useMemo(() => {
    const set = new Set<string>()
    nodes.forEach((n) => n.region && set.add(n.region))
    return [...set].sort()
  }, [nodes])

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const bulk = async (body: Record<string, unknown>) => {
    if (selected.size === 0) return
    try {
      await api.post('/nodes/bulk', { ids: [...selected], ...body })
      toast.success(`已更新 ${selected.size} 个节点`)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '操作失败'))
    }
  }

  const save = async () => {
    if (!draft) return
    try {
      await api.put(`/nodes/${draft.id}`, {
        ...draft,
        tags: draft.tags
          .split(',')
          .map((t) => t.trim())
          .filter(Boolean),
      })
      toast.success('已保存')
      setDraft(null)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    }
  }

  const removeMissing = async (n: Node) => {
    try {
      await api.del(`/nodes/${n.id}`)
      toast.success('已移除')
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '移除失败'))
    }
  }

  const allSelected = nodes.length > 0 && selected.size === nodes.length

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="节点"
        description="节点由面板的入站自动发现。新节点默认停用，确认信息后再启用并加入套餐。"
      />

      {selected.size > 0 && (
        <Card>
          <CardContent className="flex flex-wrap items-center gap-3">
            <span className="text-sm font-medium">已选 {selected.size} 个</span>
            <Button variant="outline" size="sm" onClick={() => bulk({ enabled: true })}>
              启用
            </Button>
            <Button variant="outline" size="sm" onClick={() => bulk({ enabled: false })}>
              停用
            </Button>
            <Input
              className="w-56"
              list="firex-regions"
              placeholder="批量设置地区，如 🇭🇰 香港"
              value={bulkRegion}
              onChange={(e) => setBulkRegion(e.target.value)}
            />
            <Button variant="outline" size="sm" disabled={!bulkRegion} onClick={() => bulk({ region: bulkRegion })}>
              应用地区
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
              取消选择
            </Button>
          </CardContent>
        </Card>
      )}

      <datalist id="firex-regions">
        {regions.map((r) => (
          <option key={r} value={r} />
        ))}
      </datalist>

      <Card>
        <CardContent>
          {nodes.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <LayersIcon />
                </EmptyMedia>
                <EmptyTitle>还没有节点</EmptyTitle>
                <EmptyDescription>先在「面板」页添加面板，FireX 会自动拉取入站。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10">
                    <Checkbox
                      checked={allSelected}
                      aria-label="全选"
                      onCheckedChange={(checked) =>
                        setSelected(checked ? new Set(nodes.map((n) => n.id)) : new Set())
                      }
                    />
                  </TableHead>
                  <TableHead>名称</TableHead>
                  <TableHead>地区</TableHead>
                  <TableHead>面板 / 入站</TableHead>
                  <TableHead>协议</TableHead>
                  <TableHead>端口</TableHead>
                  <TableHead>标签</TableHead>
                  <TableHead>套餐</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.map((n) => (
                  <TableRow key={n.id}>
                    <TableCell>
                      <Checkbox
                        checked={selected.has(n.id)}
                        aria-label={`选择 ${n.name || n.remoteRemark}`}
                        onCheckedChange={() => toggle(n.id)}
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-medium">
                          {n.emoji && `${n.emoji} `}
                          {n.name || n.remoteRemark || n.inboundTag}
                        </span>
                        {!n.name && <span className="text-xs text-muted-foreground">沿用面板备注</span>}
                      </div>
                    </TableCell>
                    <TableCell>{n.region || <span className="text-muted-foreground">未分组</span>}</TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {n.panelName} #{n.inboundId}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{n.protocol}</TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">{n.port}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{n.tags || '—'}</TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">{n.planCount}</TableCell>
                    <TableCell>
                      <NodeStatus node={n} />
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            setDraft({
                              id: n.id,
                              name: n.name,
                              region: n.region,
                              emoji: n.emoji,
                              tags: n.tags,
                              sortOrder: n.sortOrder,
                              enabled: n.enabled,
                              udp: n.udp,
                              multiplier: n.multiplier,
                            })
                          }
                        >
                          编辑
                        </Button>
                        {n.missing && (
                          <Button variant="ghost" size="sm" onClick={() => setPendingDelete(n)}>
                            移除
                          </Button>
                        )}
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
            <DialogTitle>编辑节点</DialogTitle>
            <DialogDescription>这些字段由 FireX 持有，重新拉取面板不会覆盖。</DialogDescription>
          </DialogHeader>
          {draft && (
            <FieldGroup>
              <div className="grid gap-4 sm:grid-cols-[1fr_120px]">
                <Field>
                  <FieldLabel htmlFor="node-name">显示名称</FieldLabel>
                  <Input
                    id="node-name"
                    value={draft.name}
                    onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                  />
                  <FieldDescription>留空则沿用面板上的入站备注</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="node-emoji">Emoji</FieldLabel>
                  <Input
                    id="node-emoji"
                    value={draft.emoji}
                    onChange={(e) => setDraft({ ...draft, emoji: e.target.value })}
                  />
                </Field>
              </div>
              <Field>
                <FieldLabel htmlFor="node-region">地区</FieldLabel>
                <Input
                  id="node-region"
                  list="firex-regions"
                  placeholder="🇭🇰 香港"
                  value={draft.region}
                  onChange={(e) => setDraft({ ...draft, region: e.target.value })}
                />
                <FieldDescription>同地区的节点会自动生成一个 Clash 分组，分组名就是这里填的文本</FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="node-tags">标签</FieldLabel>
                <Input
                  id="node-tags"
                  value={draft.tags}
                  onChange={(e) => setDraft({ ...draft, tags: e.target.value })}
                />
                <FieldDescription>逗号分隔，可在 Clash 模板里用 &lt;TAG:名称&gt; 引用</FieldDescription>
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="node-sort">排序</FieldLabel>
                  <Input
                    id="node-sort"
                    type="number"
                    value={draft.sortOrder}
                    onChange={(e) => setDraft({ ...draft, sortOrder: Number(e.target.value) })}
                  />
                  <FieldDescription>数字小的排在前面</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="node-multiplier">倍率</FieldLabel>
                  <Input
                    id="node-multiplier"
                    type="number"
                    step="0.1"
                    value={draft.multiplier}
                    onChange={(e) => setDraft({ ...draft, multiplier: Number(e.target.value) })}
                  />
                  <FieldDescription>仅作展示记录，不参与计费</FieldDescription>
                </Field>
              </div>
              <Field orientation="horizontal">
                <FieldLabel htmlFor="node-enabled">启用（停用后从所有订阅中移除）</FieldLabel>
                <Switch
                  id="node-enabled"
                  checked={draft.enabled}
                  onCheckedChange={(v) => setDraft({ ...draft, enabled: v })}
                />
              </Field>
              <Field orientation="horizontal">
                <FieldLabel htmlFor="node-udp">允许 UDP</FieldLabel>
                <Switch id="node-udp" checked={draft.udp} onCheckedChange={(v) => setDraft({ ...draft, udp: v })} />
              </Field>
            </FieldGroup>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setDraft(null)}>
              取消
            </Button>
            <Button onClick={save}>保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title="移除已失联的节点？"
        description={`「${pendingDelete?.name || pendingDelete?.remoteRemark || ''}」的入站已在面板上消失，移除后套餐关联也会一并删除。`}
        confirmLabel="移除"
        onConfirm={async () => {
          if (pendingDelete) await removeMissing(pendingDelete)
        }}
      />
    </div>
  )
}

function NodeStatus({ node }: { node: Node }) {
  if (node.missing) return <StatusBadge tone="bad">已失联</StatusBadge>
  if (!node.remoteEnabled) return <StatusBadge tone="warn">面板已禁用</StatusBadge>
  if (node.enabled) return <StatusBadge tone="good">启用</StatusBadge>
  return <StatusBadge tone="idle">停用</StatusBadge>
}

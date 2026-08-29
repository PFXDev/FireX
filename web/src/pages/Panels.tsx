import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { PlugZapIcon, PlusIcon, RefreshCwIcon, ServerOffIcon } from 'lucide-react'

import { api } from '@/api'
import type { Panel } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { PanelStatusBadge, StatusBadge } from '@/components/status-badge'
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
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatTime } from '@/lib/format'

type Draft = {
  id?: number
  name: string
  baseUrl: string
  apiToken: string
  skipTlsVerify: boolean
  enabled: boolean
  remark: string
}

const emptyDraft: Draft = {
  name: '',
  baseUrl: '',
  apiToken: '',
  skipTlsVerify: false,
  enabled: true,
  remark: '',
}

export function PanelsPage() {
  const [panels, setPanels] = useState<Panel[]>([])
  const [draft, setDraft] = useState<Draft | null>(null)
  const [busy, setBusy] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<Panel | null>(null)

  const load = useCallback(async () => {
    setPanels(await api.get<Panel[]>('/panels'))
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const save = async () => {
    if (!draft) return
    setBusy(true)
    try {
      if (draft.id) {
        await api.put(`/panels/${draft.id}`, draft)
        toast.success('已保存')
      } else {
        const res = await api.post<{ discoverError: string }>('/panels', draft)
        if (res.discoverError) toast.error(`已添加，但拉取节点失败：${res.discoverError}`)
        else toast.success('已添加并同步节点')
      }
      setDraft(null)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setBusy(false)
    }
  }

  const test = async () => {
    if (!draft) return
    setBusy(true)
    try {
      const res = await api.post<{ xrayVersion: string; panelVersion: string; inbounds: number }>('/panels/test', draft)
      toast.success(`连接成功：面板 ${res.panelVersion || '?'} / Xray ${res.xrayVersion || '?'}，${res.inbounds} 个入站`)
    } catch (err) {
      toast.error(errorMessage(err, '连接失败'))
    } finally {
      setBusy(false)
    }
  }

  const discover = async (p: Panel) => {
    try {
      const res = await api.post<{ nodes: number }>(`/panels/${p.id}/discover`)
      toast.success(`已同步 ${res.nodes} 个入站`)
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '同步失败'))
    }
  }

  const remove = async (p: Panel) => {
    try {
      const res = await api.del<{ remoteCleanupFailures: number }>(`/panels/${p.id}`)
      if (res.remoteCleanupFailures > 0) {
        toast.error(`已删除，但有 ${res.remoteCleanupFailures} 个客户端未能从面板移除，需要手动清理`)
      } else {
        toast.success('已删除')
      }
      await load()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="面板"
        description="每个面板是一台独立的 3x-ui。FireX 用面板的 API Token（admin 作用域）下发配置。"
      >
        <Button onClick={() => setDraft({ ...emptyDraft })}>
          <PlusIcon data-icon="inline-start" />
          添加面板
        </Button>
      </PageHeader>

      <Card>
        <CardContent>
          {panels.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ServerOffIcon />
                </EmptyMedia>
                <EmptyTitle>还没有面板</EmptyTitle>
                <EmptyDescription>添加一台 3x-ui，FireX 会自动拉取它的入站作为节点。</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={() => setDraft({ ...emptyDraft })}>
                  <PlusIcon data-icon="inline-start" />
                  添加面板
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>地址</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>Xray</TableHead>
                  <TableHead>最近连通</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {panels.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{p.name}</span>
                          {!p.enabled && <StatusBadge tone="idle">已停用</StatusBadge>}
                        </div>
                        {p.remark && <span className="text-xs text-muted-foreground">{p.remark}</span>}
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">{p.baseUrl}</TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <PanelStatusBadge status={p.status} />
                        {p.lastError && (
                          <span className="max-w-72 truncate font-mono text-xs text-muted-foreground" title={p.lastError}>
                            {p.lastError}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap tabular-nums">
                      {p.enabledNodes} / {p.nodeCount}
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">{p.xrayVersion || '—'}</TableCell>
                    <TableCell className="whitespace-nowrap text-muted-foreground">{formatTime(p.lastSeenAt)}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <Button variant="outline" size="sm" onClick={() => discover(p)}>
                          <RefreshCwIcon data-icon="inline-start" />
                          拉取节点
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            setDraft({
                              id: p.id,
                              name: p.name,
                              baseUrl: p.baseUrl,
                              apiToken: '',
                              skipTlsVerify: p.skipTlsVerify,
                              enabled: p.enabled,
                              remark: p.remark,
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
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{draft?.id ? '编辑面板' : '添加面板'}</DialogTitle>
            <DialogDescription>
              地址需要包含协议、端口和 basePath，例如 https://1.2.3.4:2053/mypath，不要带 /panel。
            </DialogDescription>
          </DialogHeader>
          {draft && (
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="panel-name">名称</FieldLabel>
                <Input
                  id="panel-name"
                  value={draft.name}
                  onChange={(e) => setDraft({ ...draft, name: e.target.value })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="panel-url">面板地址</FieldLabel>
                <Input
                  id="panel-url"
                  placeholder="https://panel.example.com:2053"
                  value={draft.baseUrl}
                  onChange={(e) => setDraft({ ...draft, baseUrl: e.target.value })}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="panel-token">API Token</FieldLabel>
                <Input
                  id="panel-token"
                  type="password"
                  autoComplete="off"
                  value={draft.apiToken}
                  onChange={(e) => setDraft({ ...draft, apiToken: e.target.value })}
                />
                <FieldDescription>
                  {draft.id
                    ? '留空表示不修改。在 3x-ui 的「设置 → API 令牌」创建 admin 作用域的令牌。'
                    : '在 3x-ui 的「设置 → API 令牌」创建 admin 作用域的令牌。'}
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="panel-remark">备注</FieldLabel>
                <Input
                  id="panel-remark"
                  value={draft.remark}
                  onChange={(e) => setDraft({ ...draft, remark: e.target.value })}
                />
              </Field>
              <Field orientation="horizontal">
                <FieldLabel htmlFor="panel-skip-tls">跳过 TLS 证书校验</FieldLabel>
                <Switch
                  id="panel-skip-tls"
                  checked={draft.skipTlsVerify}
                  onCheckedChange={(v) => setDraft({ ...draft, skipTlsVerify: v })}
                />
              </Field>
              <Field orientation="horizontal">
                <FieldLabel htmlFor="panel-enabled">启用</FieldLabel>
                <Switch
                  id="panel-enabled"
                  checked={draft.enabled}
                  onCheckedChange={(v) => setDraft({ ...draft, enabled: v })}
                />
              </Field>
            </FieldGroup>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={test} disabled={busy}>
              <PlugZapIcon data-icon="inline-start" />
              测试连接
            </Button>
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
        title={`删除面板「${pendingDelete?.name ?? ''}」？`}
        description="FireX 会尽量先从该面板删除它创建的客户端，然后移除本地的节点、套餐关联和下发记录。"
        confirmLabel="删除"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

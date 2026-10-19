import { type FormEvent, useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { EyeIcon, FileCogIcon, LayoutTemplateIcon, MousePointerClickIcon, PlusIcon, SaveIcon, Trash2Icon } from 'lucide-react'

import { api, type InboundDeployment, type Panel } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { ConfigEditor, type Parameters } from '@/components/inbound-config-editor'
import { FieldsSkeleton, ListSkeleton, LoadErrorAlert, RefreshButton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { confirmUnsavedNavigation, useUnsavedGuard } from '@/hooks/use-unsaved-guard'
import { errorMessage, formatTime } from '@/lib/format'
import { hashID } from '@/lib/inbound'
import { useSession } from '@/lib/session'

type Template = { id: number; name: string; revision: number; updatedAt: number }
type Detail = { template: Template; parameters: Parameters }
type Pending = 'save' | 'open' | 'create'

type Node = {
  panelId: number | null
  listen: string
  port: number
  remark: string
  acceptProxyProtocol: boolean
  publicAddress: string
  publicPort: number
}

const blankNode: Node = { panelId: null, listen: '', port: 443, remark: '', acceptProxyProtocol: false, publicAddress: '', publicPort: 0 }

export function InboundTemplatesPage() {
  const { readOnly } = useSession()
  const [rows, setRows] = useState<Template[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [selectedID, setSelectedID] = useState<number | null>(hashID)
  const [detail, setDetail] = useState<Detail | null>(null)
  const [detailError, setDetailError] = useState('')
  const [name, setName] = useState('')
  const [draft, setDraft] = useState<Parameters>({})
  const [invalid, setInvalid] = useState(false)
  const [reveal, setReveal] = useState(false)
  const [pending, setPending] = useState<Pending | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [panels, setPanels] = useState<Panel[]>([])
  const [node, setNode] = useState<Node | null>(null)
  const [nodeAttempted, setNodeAttempted] = useState(false)
  const busy = pending !== null

  const dirty = !!detail && (invalid || name !== detail.template.name || JSON.stringify(draft) !== JSON.stringify(detail.parameters))
  useUnsavedGuard(dirty)

  const loadList = useCallback(async () => {
    setLoading(true)
    try {
      setRows(await api.get<Template[]>('/inbound-templates'))
      setLoadError('')
    } catch (err) {
      setLoadError(errorMessage(err, '无法加载模板'))
    } finally {
      setLoading(false)
    }
  }, [])

  // Re-fetches the list after a mutation without swapping it for a skeleton.
  const refreshList = useCallback(async () => {
    try {
      setRows(await api.get<Template[]>('/inbound-templates'))
      setLoadError('')
    } catch (err) {
      toast.error(`操作已完成，但模板列表刷新失败：${errorMessage(err, '未知错误')}`)
    }
  }, [])

  const adopt = (next: Detail) => {
    setDetail(next)
    setName(next.template.name)
    setDraft(next.parameters)
    setInvalid(false)
  }

  const loadDetail = useCallback(async (id: number) => {
    setDetailError('')
    try {
      adopt(await api.get<Detail>(`/inbound-templates/${id}`))
    } catch (err) {
      setDetailError(errorMessage(err, '无法打开模板'))
    }
  }, [])

  useEffect(() => {
    void loadList()
  }, [loadList])

  useEffect(() => {
    setDetail(null)
    setReveal(false)
    if (selectedID) void loadDetail(selectedID)
  }, [selectedID, loadDetail])

  const select = (id: number) => {
    if (id === selectedID || !confirmUnsavedNavigation()) return
    window.history.replaceState(window.history.state, '', `#/inbound-templates?id=${id}`)
    setSelectedID(id)
  }

  const save = async () => {
    if (!detail) return
    setPending('save')
    try {
      adopt(await api.put<Detail>(`/inbound-templates/${detail.template.id}`, { revision: detail.template.revision, name, parameters: draft }))
      toast.success('模板已保存，已创建的入站不受影响')
      await refreshList()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setPending(null)
    }
  }

  const remove = async () => {
    if (!detail) return
    try {
      await api.del(`/inbound-templates/${detail.template.id}`)
      toast.success('模板已删除')
      window.history.replaceState(window.history.state, '', '#/inbound-templates')
      setSelectedID(null)
      await refreshList()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  const openCreate = async () => {
    setPending('open')
    try {
      setPanels((await api.get<Panel[]>('/panels')).filter((panel) => panel.enabled))
      setNodeAttempted(false)
      setNode({ ...blankNode, remark: detail?.template.name ?? '' })
    } catch (err) {
      toast.error(errorMessage(err, '无法读取面板'))
    } finally {
      setPending(null)
    }
  }

  const create = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setNodeAttempted(true)
    if (!detail || !node?.panelId) return
    setPending('create')
    try {
      const result = await api.post<{ deployment: InboundDeployment }>(`/inbound-templates/${detail.template.id}/instantiate`, { ...node, revision: detail.template.revision })
      const { status, error, inboundId } = result.deployment
      if (status === 'verified') toast.success('入站已创建并由 FireX 接管，启用并加入节点组后用户即可使用')
      else toast.error(`创建结果待核对：${error}`, { duration: 10000 })
      setNode(null)
      if (inboundId) window.location.hash = `#/inbound-configs?id=${inboundId}`
    } catch (err) {
      toast.error(errorMessage(err, '创建失败'))
    } finally {
      setPending(null)
    }
  }

  const panelMissing = nodeAttempted && !node?.panelId
  const firstLoad = loading && rows.length === 0

  const listCard = (
    <Card className="min-w-0 xl:col-span-1">
      <CardHeader>
        <CardTitle>模板列表</CardTitle>
        <CardDescription>{firstLoad ? '读取已保存的模板。' : `共 ${rows.length} 个模板。`}</CardDescription>
        <CardAction>
          <RefreshButton loading={loading} disabled={busy} onClick={() => void loadList()} />
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {loadError && <LoadErrorAlert title="模板加载失败" message={loadError} retrying={loading} onRetry={() => void loadList()} />}
        {firstLoad ? (
          <ListSkeleton />
        ) : rows.length === 0 ? (
          !loadError && (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <LayoutTemplateIcon />
                </EmptyMedia>
                <EmptyTitle>还没有模板</EmptyTitle>
                <EmptyDescription>在「入站配置」中打开一个已接管的入站，选择「另存为模板」。</EmptyDescription>
              </EmptyHeader>
              {!readOnly && (
                <EmptyContent>
                  <Button render={<a href="#/inbound-configs" />} nativeButton={false}>
                    <FileCogIcon data-icon="inline-start" />
                    前往入站配置
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          )
        ) : (
          <nav className="flex min-w-0 flex-col gap-1" aria-label="模板">
            {rows.map((row) => (
              <Button
                key={row.id}
                type="button"
                variant={row.id === selectedID ? 'secondary' : 'ghost'}
                className="h-auto w-full flex-col items-start gap-0.5 py-2 text-left"
                aria-current={row.id === selectedID ? 'page' : undefined}
                disabled={busy}
                onClick={() => select(row.id)}
              >
                <span className="w-full truncate">{row.name}</span>
                <span className="w-full truncate text-xs font-normal text-muted-foreground">
                  版本 {row.revision} · {formatTime(row.updatedAt)}
                </span>
              </Button>
            ))}
          </nav>
        )}
      </CardContent>
    </Card>
  )

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="入站模板" description="模板保存多个节点共用的参数，例如 REALITY 设置。用模板创建入站时填写各节点的监听与 PROXY，创建后由 FireX 独立接管。">
        {readOnly && (
          <Badge variant="outline">
            <EyeIcon data-icon="inline-start" />
            只读
          </Badge>
        )}
      </PageHeader>

      {rows.length === 0 ? (
        listCard
      ) : (
        <div className="grid min-w-0 items-start gap-6 xl:grid-cols-4">
          {listCard}
          <Card className="min-w-0 xl:col-span-3">
            {!selectedID ? (
              <CardContent>
                <Empty>
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <MousePointerClickIcon />
                    </EmptyMedia>
                    <EmptyTitle>选择一个模板</EmptyTitle>
                    <EmptyDescription>{readOnly ? '在左侧列表中选择模板，查看它的共用参数。' : '在左侧列表中选择模板，查看和编辑它的共用参数。'}</EmptyDescription>
                  </EmptyHeader>
                </Empty>
              </CardContent>
            ) : detailError ? (
              <CardContent>
                <LoadErrorAlert title="模板打开失败" message={detailError} onRetry={() => void loadDetail(selectedID)} />
              </CardContent>
            ) : !detail ? (
              <CardContent>
                <FieldsSkeleton />
              </CardContent>
            ) : (
              <>
                <CardHeader>
                  <CardTitle className="truncate">{detail.template.name}</CardTitle>
                  <CardDescription className="flex flex-wrap items-center gap-2">
                    <Badge variant="outline">版本 {detail.template.revision}</Badge>
                    <span>更新于 {formatTime(detail.template.updatedAt)}</span>
                    {dirty && <Badge variant="secondary">未保存</Badge>}
                  </CardDescription>
                  {!readOnly && (
                    <CardAction className="flex flex-wrap justify-end gap-2">
                      <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => setDeleting(true)}>
                        <Trash2Icon data-icon="inline-start" />
                        删除
                      </Button>
                      <Button type="button" variant="outline" size="sm" disabled={busy || dirty} title={dirty ? '请先保存模板' : undefined} onClick={() => void openCreate()}>
                        {pending === 'open' ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                        用模板创建入站
                      </Button>
                      <Button type="button" size="sm" disabled={busy || !dirty || invalid || !name.trim()} onClick={() => void save()}>
                        {pending === 'save' ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
                        {pending === 'save' ? '保存中…' : '保存'}
                      </Button>
                    </CardAction>
                  )}
                </CardHeader>
                <CardContent>
                  <FieldGroup>
                    <Field data-invalid={!name.trim() || undefined} data-disabled={readOnly || busy || undefined}>
                      <FieldLabel htmlFor="template-name">模板名称</FieldLabel>
                      <Input id="template-name" value={name} disabled={readOnly || busy} aria-invalid={!name.trim() || undefined} onChange={(event) => setName(event.target.value)} />
                      {!name.trim() && <FieldError>请输入模板名称。</FieldError>}
                    </Field>
                    {!readOnly && (
                      <Field orientation="horizontal">
                        <FieldLabel htmlFor="reveal-template">显示密钥</FieldLabel>
                        <Switch id="reveal-template" checked={reveal} onCheckedChange={setReveal} />
                      </Field>
                    )}
                    <ConfigEditor key={detail.template.id} parameters={draft} onChange={setDraft} onInvalidChange={setInvalid} disabled={readOnly || busy} reveal={reveal} />
                  </FieldGroup>
                </CardContent>
              </>
            )}
          </Card>
        </div>
      )}

      <Dialog open={node !== null} onOpenChange={(open) => !busy && !open && setNode(null)}>
        <DialogContent className="sm:max-w-xl">
          {node && (
            <form className="contents" noValidate onSubmit={create}>
              <DialogHeader>
                <DialogTitle>用模板创建入站</DialogTitle>
                <DialogDescription>{detail?.template.name}。新入站默认不加入任何节点组，审核后再启用。</DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <Field data-invalid={panelMissing || undefined} data-disabled={busy || undefined}>
                  <FieldLabel htmlFor="node-panel">面板</FieldLabel>
                  <Select
                    items={panels.map((panel) => ({ value: panel.id, label: panel.name }))}
                    value={node.panelId}
                    disabled={busy}
                    onValueChange={(value) => setNode({ ...node, panelId: value })}
                  >
                    <SelectTrigger id="node-panel" className="w-full" aria-invalid={panelMissing || undefined}>
                      <SelectValue placeholder="选择面板" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {panels.map((panel) => (
                          <SelectItem key={panel.id} value={panel.id}>{panel.name}</SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  {panelMissing && <FieldError>{panels.length === 0 ? '没有已启用的面板，请先在「面板」中添加或启用。' : '请选择要创建入站的面板。'}</FieldError>}
                </Field>
                <FieldGroup className="grid sm:grid-cols-2">
                  <Field data-disabled={busy || undefined}>
                    <FieldLabel htmlFor="node-listen">监听地址</FieldLabel>
                    <Input id="node-listen" placeholder="留空监听所有地址" value={node.listen} disabled={busy} onChange={(event) => setNode({ ...node, listen: event.target.value })} />
                  </Field>
                  <Field data-disabled={busy || undefined}>
                    <FieldLabel htmlFor="node-port">监听端口</FieldLabel>
                    <Input id="node-port" type="number" min={1} max={65535} value={node.port} disabled={busy} onChange={(event) => setNode({ ...node, port: Number(event.target.value) })} />
                  </Field>
                </FieldGroup>
                <Field orientation="horizontal" data-disabled={busy || undefined}>
                  <FieldContent>
                    <FieldLabel htmlFor="node-proxy">接收 PROXY 协议</FieldLabel>
                    <FieldDescription>节点在反向代理之后时，通常监听 127.0.0.1 的内部端口并接收 PROXY 协议；直接对外的节点则都不需要。</FieldDescription>
                  </FieldContent>
                  <Switch id="node-proxy" checked={node.acceptProxyProtocol} disabled={busy} onCheckedChange={(value) => setNode({ ...node, acceptProxyProtocol: value })} />
                </Field>
                <Field data-disabled={busy || undefined}>
                  <FieldLabel htmlFor="node-remark">面板备注</FieldLabel>
                  <Input id="node-remark" value={node.remark} disabled={busy} onChange={(event) => setNode({ ...node, remark: event.target.value })} />
                </Field>
                <FieldGroup className="grid sm:grid-cols-2">
                  <Field data-disabled={busy || undefined}>
                    <FieldLabel htmlFor="node-public-address">客户端地址</FieldLabel>
                    <Input id="node-public-address" placeholder="留空使用面板地址" value={node.publicAddress} disabled={busy} onChange={(event) => setNode({ ...node, publicAddress: event.target.value })} />
                  </Field>
                  <Field data-disabled={busy || undefined}>
                    <FieldLabel htmlFor="node-public-port">客户端端口</FieldLabel>
                    <Input id="node-public-port" type="number" min={0} max={65535} value={node.publicPort} disabled={busy} onChange={(event) => setNode({ ...node, publicPort: Number(event.target.value) })} />
                    <FieldDescription>0 跟随监听端口。</FieldDescription>
                  </Field>
                </FieldGroup>
              </FieldGroup>
              <DialogFooter>
                <Button type="button" variant="outline" disabled={busy} onClick={() => setNode(null)}>取消</Button>
                <Button type="submit" disabled={busy || panelMissing}>
                  {pending === 'create' ? <Spinner data-icon="inline-start" /> : <PlusIcon data-icon="inline-start" />}
                  {pending === 'create' ? '创建中…' : '创建'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`删除模板「${detail?.template.name ?? ''}」？`}
        description="用此模板创建的入站由 FireX 独立接管，不受影响。"
        confirmLabel="删除"
        onConfirm={remove}
      />
    </div>
  )
}

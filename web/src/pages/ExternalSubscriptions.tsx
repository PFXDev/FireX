import { useCallback, useEffect, useState } from 'react'
import { EyeIcon, ListIcon, PencilIcon, PlusIcon, RefreshCwIcon, RssIcon, Trash2Icon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { api } from '@/api'
import type { ExternalSubscription, ExternalSubscriptionDetail } from '@/api'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { LoadErrorAlert, RefreshButton, TableSkeleton } from '@/components/load-state'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { errorMessage, formatTime } from '@/lib/format'
import { useSession } from '@/lib/session'

type Draft = {
  id?: number
  name: string
  url: string
  enabled: boolean
  updateInterval: number
  remark: string
}

type MutationResponse = {
  subscription: ExternalSubscription
  refreshError: string
}

const emptyDraft: Draft = {
  name: '',
  url: '',
  enabled: true,
  updateInterval: 3600,
  remark: '',
}

function validHTTPURL(raw: string) {
  try {
    const url = new URL(raw)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function intervalLabel(seconds: number) {
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}

export function ExternalSubscriptionsPage() {
  const [items, setItems] = useState<ExternalSubscription[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [showErrors, setShowErrors] = useState(false)
  const [saving, setSaving] = useState(false)
  const [refreshingID, setRefreshingID] = useState<number | null>(null)
  const [pendingDelete, setPendingDelete] = useState<ExternalSubscription | null>(null)
  const [detail, setDetail] = useState<ExternalSubscriptionDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const { readOnly } = useSession()

  const load = useCallback(async () => {
    try {
      setItems(await api.get<ExternalSubscription[]>('/external-subscriptions'))
      setLoadError(null)
      return true
    } catch (err) {
      setLoadError(errorMessage(err, '外部订阅加载失败'))
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

  const openEdit = (item: ExternalSubscription) => {
    setShowErrors(false)
    setDraft({
      id: item.id,
      name: item.name,
      url: item.url,
      enabled: item.enabled,
      updateInterval: item.updateInterval,
      remark: item.remark,
    })
  }

  const save = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || saving) return
    setShowErrors(true)
    if (!draft.name.trim() || !validHTTPURL(draft.url.trim()) || draft.updateInterval < 60) return
    setSaving(true)
    const body = { ...draft, name: draft.name.trim(), url: draft.url.trim() }
    try {
      const result = draft.id
        ? await api.put<MutationResponse>(`/external-subscriptions/${draft.id}`, body)
        : await api.post<MutationResponse>('/external-subscriptions', body)
      if (result.refreshError) toast.warning(`已保存，但本次刷新失败：${result.refreshError}`)
      else toast.success(`已保存并解析 ${result.subscription.nodeCount} 个节点`)
      setDraft(null)
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  const refreshOne = async (item: ExternalSubscription) => {
    if (refreshingID !== null) return
    setRefreshingID(item.id)
    try {
      const result = await api.post<MutationResponse>(`/external-subscriptions/${item.id}/refresh`)
      setItems((current) => current.map((row) => (row.id === item.id ? result.subscription : row)))
      if (result.refreshError) toast.warning(`刷新失败，已保留旧节点：${result.refreshError}`)
      else toast.success(`已更新 ${result.subscription.nodeCount} 个节点`)
    } catch (err) {
      toast.error(errorMessage(err, '刷新失败'))
    } finally {
      setRefreshingID(null)
    }
  }

  const remove = async (item: ExternalSubscription) => {
    try {
      await api.del(`/external-subscriptions/${item.id}`)
      toast.success(item.groupCount > 0 ? `已删除，并解除 ${item.groupCount} 处节点组关联` : '外部订阅已删除')
      await revalidate()
    } catch (err) {
      toast.error(errorMessage(err, '删除失败'))
      throw err
    }
  }

  const viewNodes = async (item: ExternalSubscription) => {
    setDetail({ subscription: item, nodes: [] })
    setDetailLoading(true)
    try {
      setDetail(await api.get<ExternalSubscriptionDetail>(`/external-subscriptions/${item.id}`))
    } catch (err) {
      toast.error(errorMessage(err, '节点明细加载失败'))
      setDetail(null)
    } finally {
      setDetailLoading(false)
    }
  }

  const locked = saving || readOnly
  const nameInvalid = Boolean(showErrors && draft && !draft.name.trim())
  const urlInvalid = Boolean(showErrors && draft && !validHTTPURL(draft.url.trim()))
  const enabledCount = items.filter((item) => item.enabled).length
  const nodeCount = items.reduce((sum, item) => sum + (item.enabled ? item.nodeCount : 0), 0)

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="外部订阅" description="集中抓取并缓存机场或其他服务的节点，再把一个或多个订阅加入节点组。">
        {readOnly ? (
          <Badge variant="outline">
            <EyeIcon data-icon="inline-start" />
            只读
          </Badge>
        ) : (
          <Button onClick={openCreate}>
            <PlusIcon data-icon="inline-start" />
            添加订阅
          </Button>
        )}
      </PageHeader>

      <Alert>
        <RssIcon />
        <AlertTitle>支持分享链接与 mihomo / Clash YAML</AlertTitle>
        <AlertDescription>
          默认每小时自动更新。刷新失败会保留上一份可用节点；用户只收到节点，不会看到原始订阅地址。
        </AlertDescription>
      </Alert>

      <Card>
        <CardHeader>
          <CardTitle>订阅列表</CardTitle>
          <CardDescription>分享链接可用于全部输出格式；仅存在于 YAML 的节点会用于 mihomo 输出。</CardDescription>
          <CardAction>
            <RefreshButton loading={loading || refreshing} disabled={saving || refreshingID !== null} onClick={() => void refresh()} />
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {loading ? (
            <TableSkeleton
              columns={[
                { label: '订阅', width: 'w-40' },
                { label: '节点', width: 'w-10' },
                { label: '节点组', className: 'hidden md:table-cell', width: 'w-10' },
                { label: '更新周期', className: 'hidden lg:table-cell', width: 'w-16' },
                { label: '最近成功', className: 'hidden lg:table-cell', width: 'w-28' },
                { label: '状态', width: 'w-16' },
                { width: 'w-48' },
              ]}
            />
          ) : loadError && items.length === 0 ? (
            <div className="px-4">
              <LoadErrorAlert title="订阅加载失败" message={loadError} retrying={refreshing} onRetry={() => void refresh()} />
            </div>
          ) : items.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <RssIcon />
                </EmptyMedia>
                <EmptyTitle>还没有外部订阅</EmptyTitle>
                <EmptyDescription>添加订阅后会立即抓取解析，之后可在节点组编辑器中将它加入一个或多个分组。</EmptyDescription>
              </EmptyHeader>
              {!readOnly && (
                <EmptyContent>
                  <Button onClick={openCreate}>
                    <PlusIcon data-icon="inline-start" />
                    添加订阅
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
                    <TableHead>订阅</TableHead>
                    <TableHead>节点</TableHead>
                    <TableHead className="hidden md:table-cell">节点组</TableHead>
                    <TableHead className="hidden lg:table-cell">更新周期</TableHead>
                    <TableHead className="hidden lg:table-cell">最近成功</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead><span className="sr-only">操作</span></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((item) => (
                    <TableRow key={item.id}>
                      <TableCell>
                        <div className="flex min-w-0 flex-col gap-1">
                          <span className="font-medium">{item.name}</span>
                          <span className="max-w-64 truncate text-xs text-muted-foreground" title={item.url}>{item.url}</span>
                          {item.remark && <span className="text-xs text-muted-foreground">{item.remark}</span>}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Button type="button" variant="link" size="sm" className="px-0 tabular-nums" onClick={() => void viewNodes(item)}>
                          {item.nodeCount}
                        </Button>
                      </TableCell>
                      <TableCell className="hidden tabular-nums md:table-cell">{item.groupCount}</TableCell>
                      <TableCell className="hidden lg:table-cell">{intervalLabel(item.updateInterval)}</TableCell>
                      <TableCell className="hidden text-muted-foreground lg:table-cell">{formatTime(item.lastUpdateAt)}</TableCell>
                      <TableCell>
                        {!item.enabled ? (
                          <StatusBadge tone="idle">停用</StatusBadge>
                        ) : item.lastError ? (
                          <StatusBadge tone={item.nodeCount > 0 ? 'warn' : 'bad'} title={item.lastError}>
                            {item.nodeCount > 0 ? '有警告' : '刷新失败'}
                          </StatusBadge>
                        ) : item.lastUpdateAt > 0 ? (
                          <StatusBadge tone="good">正常</StatusBadge>
                        ) : (
                          <StatusBadge tone="idle">待更新</StatusBadge>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button type="button" variant="outline" size="sm" aria-label={`查看 ${item.name} 的节点`} onClick={() => void viewNodes(item)}>
                            <ListIcon data-icon="inline-start" />
                            <span className="hidden xl:inline">节点</span>
                          </Button>
                          <Button type="button" variant="outline" size="sm" aria-label={`${readOnly ? '查看' : '编辑'} ${item.name}`} onClick={() => openEdit(item)}>
                            {readOnly ? <EyeIcon data-icon="inline-start" /> : <PencilIcon data-icon="inline-start" />}
                            <span className="hidden xl:inline">{readOnly ? '查看' : '编辑'}</span>
                          </Button>
                          {!readOnly && (
                            <>
                              <Button type="button" variant="outline" size="sm" disabled={refreshingID !== null} aria-label={`刷新 ${item.name}`} onClick={() => void refreshOne(item)}>
                                {refreshingID === item.id ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
                                <span className="hidden xl:inline">刷新</span>
                              </Button>
                              <Button type="button" variant="ghost" size="sm" aria-label={`删除 ${item.name}`} onClick={() => setPendingDelete(item)}>
                                <Trash2Icon data-icon="inline-start" />
                                <span className="hidden xl:inline">删除</span>
                              </Button>
                            </>
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
          <span className="text-muted-foreground">共 {items.length} 个订阅，{enabledCount} 个启用</span>
          <span className="text-muted-foreground">启用订阅缓存 {nodeCount} 个节点</span>
        </CardFooter>
      </Card>

      <Dialog open={draft !== null} onOpenChange={(open) => !open && !saving && setDraft(null)}>
        <DialogContent className="sm:max-w-xl">
          {draft && (
            <form className="contents" noValidate onSubmit={save}>
              <DialogHeader>
                <DialogTitle>{readOnly ? '查看外部订阅' : draft.id ? '编辑外部订阅' : '添加外部订阅'}</DialogTitle>
                <DialogDescription>保存后会立即刷新一次；自动更新只处理启用的订阅。</DialogDescription>
              </DialogHeader>
              <FieldGroup>
                <Field data-invalid={nameInvalid || undefined} data-disabled={locked || undefined}>
                  <FieldLabel htmlFor="external-name">名称</FieldLabel>
                  <Input id="external-name" required disabled={locked} placeholder="机场 A" value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
                  {nameInvalid && <FieldError>请输入订阅名称。</FieldError>}
                </Field>
                <Field data-invalid={urlInvalid || undefined} data-disabled={locked || undefined}>
                  <FieldLabel htmlFor="external-url">订阅地址</FieldLabel>
                  <Input id="external-url" type="url" required disabled={locked} placeholder="https://example.com/sub?token=…" value={draft.url} onChange={(event) => setDraft({ ...draft, url: event.target.value })} />
                  <FieldDescription>支持 Base64、纯文本分享链接，以及包含 proxies 的 mihomo / Clash YAML。</FieldDescription>
                  {urlInvalid && <FieldError>请输入有效的 HTTP(S) 订阅地址。</FieldError>}
                </Field>
                <Field data-disabled={locked || undefined}>
                  <FieldLabel htmlFor="external-interval">更新周期（秒）</FieldLabel>
                  <Input id="external-interval" type="number" min={60} step={60} required disabled={locked} value={draft.updateInterval} onChange={(event) => setDraft({ ...draft, updateInterval: Number(event.target.value) })} />
                  <FieldDescription>默认 3600 秒（1 小时），最短 60 秒。</FieldDescription>
                </Field>
                <Field orientation="horizontal" data-disabled={locked || undefined}>
                  <FieldLabel htmlFor="external-enabled">启用订阅</FieldLabel>
                  <Switch id="external-enabled" disabled={locked} checked={draft.enabled} onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} />
                </Field>
                <Field data-disabled={locked || undefined}>
                  <FieldLabel htmlFor="external-remark">备注</FieldLabel>
                  <Input id="external-remark" disabled={locked} value={draft.remark} onChange={(event) => setDraft({ ...draft, remark: event.target.value })} />
                </Field>
              </FieldGroup>
              <DialogFooter>
                <Button type="button" variant="outline" disabled={saving} onClick={() => setDraft(null)}>{readOnly ? '关闭' : '取消'}</Button>
                {!readOnly && (
                  <Button type="submit" disabled={saving}>
                    {saving && <Spinner data-icon="inline-start" />}
                    {saving ? '保存并刷新中…' : '保存并刷新'}
                  </Button>
                )}
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={detail !== null} onOpenChange={(open) => !open && !detailLoading && setDetail(null)}>
        <DialogContent className="flex flex-col overflow-hidden sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{detail?.subscription.name} · 节点明细</DialogTitle>
            <DialogDescription>这里展示最近一次成功解析的缓存，不会实时请求外部站点。</DialogDescription>
          </DialogHeader>
          {detail?.subscription.lastError && (
            <Alert variant="warning">
              <TriangleAlertIcon />
              <AlertTitle>最近刷新有警告</AlertTitle>
              <AlertDescription>{detail.subscription.lastError}</AlertDescription>
            </Alert>
          )}
          <div className="min-h-0 overflow-y-auto">
            {detailLoading ? (
              <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground"><Spinner />正在读取节点…</div>
            ) : detail?.nodes.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>暂无可用节点</EmptyTitle>
                  <EmptyDescription>请检查订阅内容或最近刷新错误。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow><TableHead>名称</TableHead><TableHead>协议</TableHead><TableHead>服务器</TableHead><TableHead>输出</TableHead></TableRow>
                </TableHeader>
                <TableBody>
                  {detail?.nodes.map((node, index) => (
                    <TableRow key={`${node.name}-${index}`}>
                      <TableCell className="font-medium">{node.name}</TableCell>
                      <TableCell><Badge variant="outline">{node.protocol}</Badge></TableCell>
                      <TableCell className="text-muted-foreground">{node.server}{node.port ? `:${node.port}` : ''}</TableCell>
                      <TableCell>{node.hasShareLink ? '全部格式' : '仅 mihomo'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={detailLoading} onClick={() => setDetail(null)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        title={`删除外部订阅「${pendingDelete?.name ?? ''}」？`}
        description={pendingDelete?.groupCount ? `它当前被 ${pendingDelete.groupCount} 个节点组引用。删除会解除这些关联，对应节点也会从用户订阅中消失。` : '删除后缓存节点也会被移除，此操作不可撤销。'}
        confirmLabel="删除订阅"
        onConfirm={async () => {
          if (pendingDelete) await remove(pendingDelete)
        }}
      />
    </div>
  )
}

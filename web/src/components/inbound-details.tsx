import { useCallback, useEffect, useRef, useState } from 'react'
import { CopyIcon, EyeIcon, EyeOffIcon, FileSearchIcon, PencilIcon, RefreshCwIcon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { api, type Inbound, type InboundDetail } from '@/api'
import { CodeBlock, CodeText } from '@/components/code-display'
import { FieldsSkeleton, LoadErrorAlert } from '@/components/load-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { copyText, errorMessage, formatTime } from '@/lib/format'
import { inboundTitle } from '@/lib/inbound'
import { parameterLabel } from '@/lib/inbound-labels'
import { concealed, isObject } from '@/lib/inbound-parameters'

type Props = {
  inbound: Inbound | null
  onClose: () => void
  onEdit: (inbound: Inbound) => void
  onRefresh: () => Promise<void>
}

async function copy(value: string) {
  if (await copyText(value)) toast.success('已复制')
  else toast.error('复制失败，请手动复制')
}

export function InboundDetails({ inbound, onClose, onEdit, onRefresh }: Props) {
  const [detail, setDetail] = useState<InboundDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState('')
  const [reveal, setReveal] = useState(false)
  const [tab, setTab] = useState('parameters')
  const id = inbound?.id
  const activeID = useRef(id)
  activeID.current = id
  const editAfterClose = useRef<Inbound | null>(null)

  const load = useCallback(async () => {
    if (!id) return
    setError('')
    setLoading(true)
    try {
      const result = await api.get<InboundDetail>(`/inbounds/${id}`)
      if (activeID.current === id) setDetail(result)
    } catch (err) {
      if (activeID.current === id) setError(errorMessage(err, '无法加载入站参数'))
    } finally {
      if (activeID.current === id) setLoading(false)
    }
  }, [id])

  useEffect(() => {
    setDetail(null)
    setReveal(false)
    setTab('parameters')
    setError('')
    setSyncing(false)
    if (!id) return
    let active = true
    setLoading(true)
    api.get<InboundDetail>(`/inbounds/${id}`)
      .then((result) => { if (active) setDetail(result) })
      .catch((err) => { if (active) setError(errorMessage(err, '无法加载入站参数')) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [id])

  const sync = async () => {
    if (!inbound) return
    setSyncing(true)
    try {
      await api.post(`/panels/${inbound.panelId}/discover`)
      await onRefresh()
      if (activeID.current === inbound.id) await load()
      toast.success('入站参数已同步')
    } catch (err) {
      toast.error(errorMessage(err, '同步面板失败'))
    } finally {
      if (activeID.current === inbound.id) setSyncing(false)
    }
  }

  const parameters = detail?.parameters
  const visibleParameters = (reveal ? parameters : concealed(parameters)) as Record<string, unknown> | null
  const basic = visibleParameters && Object.fromEntries(Object.entries(visibleParameters).filter(([key]) => !['settings', 'streamSettings', 'sniffing'].includes(key)))

  return (
    <Sheet
      open={inbound !== null}
      onOpenChange={(open) => { if (!open) onClose() }}
      onOpenChangeComplete={(open) => {
        if (!open && editAfterClose.current) {
          const next = editAfterClose.current
          editAfterClose.current = null
          onEdit(next)
        }
      }}
    >
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl">
        <SheetHeader className="gap-2 pr-12">
          <SheetTitle>{inbound ? inboundTitle(inbound) : '入站参数'}</SheetTitle>
          <SheetDescription>{inbound?.panelName} · 入站 #{inbound?.remoteId} · {inbound?.inboundTag || '无标签'}</SheetDescription>
          <div className="flex flex-wrap gap-1.5 pt-1">
            <Badge variant="outline">{inbound?.protocol.toUpperCase()}</Badge>
            {inbound?.network && <Badge variant="outline">{inbound.network.toUpperCase()}</Badge>}
            {inbound?.security && <Badge variant="outline">{inbound.security.toUpperCase()}</Badge>}
            <Badge variant={inbound?.vision ? 'secondary' : 'outline'}>{inbound?.vision == null ? '保留现有流控' : inbound.vision ? 'Vision 已开启' : 'Vision 未开启'}</Badge>
          </div>
        </SheetHeader>
        <Separator />
        <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto p-4">
          <dl className="grid grid-cols-2 gap-x-6 gap-y-4">
            <Summary label="客户端地址" value={inbound?.publicAddress || '沿用面板分享地址'} />
            <Summary label="客户端端口" value={String(inbound?.publicPort || inbound?.port || '—')} />
            <Summary label="监听地址" value={`${inbound?.listen || '所有地址'} : ${inbound?.port ?? '—'}`} />
            <Summary label="参数同步于" value={detail?.lastSeenAt ? formatTime(detail.lastSeenAt) : '尚未同步'} />
          </dl>
          {inbound?.vision && !inbound.visionSupported && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>Vision 设置与入站不兼容</AlertTitle>
              <AlertDescription>{inbound.visionReason}。可在编辑入站中关闭 Vision。</AlertDescription>
            </Alert>
          )}
          <Tabs value={tab} onValueChange={(value) => setTab(String(value))} className="gap-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <TabsList variant="line">
                <TabsTrigger value="parameters">参数一览</TabsTrigger>
                <TabsTrigger value="json">JSON</TabsTrigger>
              </TabsList>
              <Button type="button" size="sm" variant="ghost" disabled={!parameters} onClick={() => setReveal(!reveal)}>
                {reveal ? <EyeOffIcon data-icon="inline-start" /> : <EyeIcon data-icon="inline-start" />}
                {reveal ? '隐藏密钥' : '显示密钥'}
              </Button>
            </div>
            {loading ? (
              <FieldsSkeleton />
            ) : error ? (
              <LoadErrorAlert title="参数加载失败" message={error} onRetry={() => void load()} />
            ) : !parameters ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <FileSearchIcon />
                  </EmptyMedia>
                  <EmptyTitle>尚未同步入站参数</EmptyTitle>
                  <EmptyDescription>点击下方「同步参数」，读取面板中的完整入站配置。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <>
                <TabsContent value="parameters" className="flex flex-col gap-6">
                  <ParameterSection title="传输与安全" value={visibleParameters?.streamSettings} />
                  <Separator />
                  <ParameterSection title="协议设置" value={visibleParameters?.settings} />
                  <Separator />
                  <ParameterSection title="流量嗅探" value={visibleParameters?.sniffing} />
                  <Separator />
                  <ParameterSection title="连接与面板" value={basic} />
                </TabsContent>
                <TabsContent value="json" className="flex flex-col gap-3">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs text-muted-foreground">入站参数 · {reveal ? '密钥已显示' : '密钥已隐藏'}</span>
                    <Button type="button" size="sm" variant="outline" onClick={() => void copy(JSON.stringify(visibleParameters, null, 2))}>
                      <CopyIcon data-icon="inline-start" />复制 JSON
                    </Button>
                  </div>
                  <CodeBlock className="[overflow-wrap:anywhere]">{JSON.stringify(visibleParameters, null, 2)}</CodeBlock>
                </TabsContent>
              </>
            )}
          </Tabs>
        </div>
        <Separator />
        <SheetFooter className="flex-row justify-between">
          <Button type="button" variant="outline" disabled={loading || syncing} onClick={() => void sync()}>
            {syncing ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
            {syncing ? '同步中…' : '同步参数'}
          </Button>
          <Button type="button" disabled={syncing} onClick={() => {
            if (inbound) {
              editAfterClose.current = inbound
              onClose()
            }
          }}>
            <PencilIcon data-icon="inline-start" />编辑入站
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

function Summary({ label, value }: { label: string; value: string }) {
  return <div className="flex min-w-0 flex-col gap-1"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="text-sm [overflow-wrap:anywhere]">{value}</dd></div>
}

function ParameterSection({ title, value }: { title: string; value: unknown }) {
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <h3 className="text-sm font-medium">{title}</h3>
      {isObject(value) && Object.keys(value).length > 0 ? <ParameterTree value={value} /> : <p className="text-xs text-muted-foreground">{typeof value === 'string' && value ? value : '无额外参数'}</p>}
    </section>
  )
}

function ParameterTree({ value }: { value: Record<string, unknown> }) {
  return (
    <dl className="flex min-w-0 flex-col gap-3">
      {Object.entries(value).map(([key, item]) => {
        const nested = isObject(item) || (Array.isArray(item) && item.some(isObject))
        const text = item == null ? '—' : item === '' ? '未设置' : Array.isArray(item) ? item.length ? item.join('\n') : '[]' : String(item)
        return nested ? (
          <div key={key} className="flex min-w-0 flex-col gap-2">
            <dt className="text-xs font-medium">{parameterLabel(key)}</dt>
            <dd className="min-w-0 pl-3">
              <ParameterTree value={isObject(item) ? item : Object.fromEntries((item as unknown[]).map((child, index) => [String(index + 1), child]))} />
            </dd>
          </div>
        ) : (
          <div key={key} className="group grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)_auto] items-start gap-2">
            <dt className="pt-1 text-xs text-muted-foreground [overflow-wrap:anywhere]" title={key}>{parameterLabel(key)}</dt>
            <dd className="min-w-0 pt-1 leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere]">
              <CodeText className="text-foreground">{typeof item === 'boolean' ? item ? '是' : '否' : text}</CodeText>
            </dd>
            <dd>
              <Button type="button" size="icon-xs" variant="ghost" disabled={item === '••••••••'} aria-label={`复制 ${parameterLabel(key)}`} onClick={() => void copy(text)}>
                <CopyIcon />
              </Button>
            </dd>
          </div>
        )
      })}
    </dl>
  )
}

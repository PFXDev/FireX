import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { InfoIcon, RefreshCwIcon, SaveIcon, TriangleAlertIcon } from 'lucide-react'
import { toast } from 'sonner'

import { ApiError, api, type ServerConfig, type ServerConfigResponse } from '@/api'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { confirmUnsavedNavigation, useUnsavedGuard } from '@/hooks/use-unsaved-guard'
import { errorMessage } from '@/lib/format'

const channelItems = [
  { value: 'stable', label: 'stable · 正式版' },
  { value: 'dev', label: 'dev · 预发布' },
]
const sourceItems = [
  { value: 'github', label: 'GitHub 直连' },
  { value: 'proxy', label: '镜像代理' },
]

export function ServerConfigEditor() {
  const [saved, setSaved] = useState<ServerConfigResponse | null>(null)
  const [draft, setDraft] = useState<ServerConfig | null>(null)
  const [clearPassword, setClearPassword] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [saveError, setSaveError] = useState('')
  const [fields, setFields] = useState<Record<string, string>>({})
  const loadRequest = useRef<Promise<void> | null>(null)
  const dirty = !!saved && (JSON.stringify(draft) !== JSON.stringify(saved.config) || clearPassword)
  const busy = loading || saving
  useUnsavedGuard(dirty)

  const accept = (response: ServerConfigResponse) => {
    setSaved(response)
    setDraft(response.config)
    setClearPassword(false)
    setFields({})
    setSaveError('')
  }

  const load = useCallback(() => {
    if (loadRequest.current) return loadRequest.current
    const request = (async () => {
      setLoading(true)
      setLoadError('')
      try {
        accept(await api.get<ServerConfigResponse>('/settings/server'))
      } catch (err) {
        setLoadError(errorMessage(err, '加载服务端配置失败'))
      } finally {
        setLoading(false)
        loadRequest.current = null
      }
    })()
    loadRequest.current = request
    return request
  }, [])

  useEffect(() => { void load() }, [load])

  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!draft || !saved || busy || !dirty) return
    setSaving(true)
    setSaveError('')
    setFields({})
    // An untouched, redacted password must never clear an existing reset.
    const { adminPassword, ...values } = draft
    try {
      const response = await api.put<ServerConfigResponse>('/settings/server', {
        revision: saved.revision,
        config: {
          ...values,
          ...(clearPassword ? { adminPassword: '' } : adminPassword ? { adminPassword } : {}),
        },
      })
      accept(response)
      toast.success(response.restartRequired ? '配置已保存，重启 FireX 后生效' : '配置已保存，与当前运行配置一致')
    } catch (err) {
      setSaveError(errorMessage(err, '保存服务端配置失败'))
      if (err instanceof ApiError) setFields(err.fields)
    } finally {
      setSaving(false)
    }
  }

  const textField = (key: keyof Omit<ServerConfig, 'update' | 'debug'>, label: string, description: string, placeholder?: string) => (
    <ConfigTextField key={key} name={key} label={label} description={description} placeholder={placeholder}
      value={draft?.[key] ?? ''} error={fields[key]} disabled={busy || (key === 'adminPassword' && clearPassword)}
      password={key === 'adminPassword'}
      onChange={(value) => setDraft((current) => current && { ...current, [key]: value })} />
  )

  const updateTextField = (key: 'checkInterval' | 'proxyBaseUrl' | 'repo', label: string, description: string, placeholder?: string) => (
    <ConfigTextField name={`update.${key}`} label={label} description={description} placeholder={placeholder}
      value={draft?.update[key] ?? ''} error={fields[`update.${key}`]} disabled={busy}
      onChange={(value) => setDraft((current) => current && { ...current, update: { ...current.update, [key]: value } })} />
  )

  return (
    <form onSubmit={save} aria-busy={busy}>
      <Card>
        <CardHeader>
          <CardTitle>服务端配置</CardTitle>
          <CardDescription>
            编辑配置文件，保存后需重启 FireX 服务生效。
            {saved && <span className="block break-all">文件：{saved.path}</span>}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-6">
          {loadError && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>无法加载最新配置</AlertTitle>
              <AlertDescription>{loadError}</AlertDescription>
            </Alert>
          )}
          {!draft && loading && <Skeleton className="h-64 w-full" />}
          {draft && saved && (
            <>
              <Alert variant={saved.restartRequired ? 'warning' : 'default'}>
                <InfoIcon />
                <AlertTitle>{saved.restartRequired ? '有已保存的配置等待重启生效' : '保存后重启生效'}</AlertTitle>
                <AlertDescription>
                  请通过部署环境的服务管理工具重启 FireX。更改监听地址后需使用新地址访问；更改存储路径不会自动迁移数据库。
                </AlertDescription>
              </Alert>
              <FieldGroup>
                <FieldSet disabled={busy}>
                  <FieldLegend>基础设置</FieldLegend>
                  <FieldGroup className="grid sm:grid-cols-2">
                    {textField('listen', '监听地址', '支持 :端口、主机:端口或 [IPv6]:端口。', ':8080')}
                    {textField('subBaseUrl', '订阅公开地址', '留空时从请求推导；反向代理部署时填写公开地址。', 'https://sub.example.com')}
                    {textField('dataDir', '数据目录', '存放数据库和暂存更新；相对路径以进程工作目录为基准。', './data')}
                    {textField('dbPath', '数据库路径', '留空使用数据目录中的 firex.db；迁移前请先准备好数据库。', '留空跟随数据目录')}
                  </FieldGroup>
                  <FieldGroup>
                    <Field orientation="horizontal" data-disabled={busy || undefined}>
                      <FieldContent>
                        <FieldLabel htmlFor="server-debug">调试日志</FieldLabel>
                        <FieldDescription id="server-debug-description">开启详细日志和 SQL 跟踪。</FieldDescription>
                      </FieldContent>
                      <Switch id="server-debug" checked={draft.debug} disabled={busy} aria-describedby="server-debug-description"
                        onCheckedChange={(debug) => setDraft({ ...draft, debug })} />
                    </Field>
                  </FieldGroup>
                </FieldSet>
                <FieldSet disabled={busy}>
                  <FieldLegend>后台任务</FieldLegend>
                  <FieldDescription>时间格式支持 30s、2m、1h30m，必须大于 0。</FieldDescription>
                  <FieldGroup className="grid sm:grid-cols-3">
                    {textField('syncInterval', '用户同步间隔', '全量协调用户配置。', '2m')}
                    {textField('trafficInterval', '流量采集间隔', '汇总流量并执行配额限制。', '1m')}
                    {textField('discoverInterval', '入站发现间隔', '从面板发现和刷新入站。', '5m')}
                  </FieldGroup>
                </FieldSet>
                <FieldSet disabled={busy}>
                  <FieldLegend>应用更新</FieldLegend>
                  <FieldGroup>
                    <Field orientation="horizontal" data-disabled={busy || undefined}>
                      <FieldContent>
                        <FieldLabel htmlFor="server-update-enabled">自动检查更新</FieldLabel>
                        <FieldDescription id="server-update-enabled-description">stable 通道自动安装正式版；dev 通道下载后等待手动确认。</FieldDescription>
                      </FieldContent>
                      <Switch id="server-update-enabled" checked={draft.update.enabled} disabled={busy} aria-describedby="server-update-enabled-description"
                        onCheckedChange={(enabled) => setDraft({ ...draft, update: { ...draft.update, enabled } })} />
                    </Field>
                    <FieldGroup className="grid sm:grid-cols-2">
                      {(['channel', 'source'] as const).map((key) => {
                        const id = `server-update-${key}`
                        const options = key === 'channel' ? channelItems : sourceItems
                        const error = fields[`update.${key}`]
                        return (
                          <Field key={key} data-invalid={!!error || undefined} data-disabled={busy || undefined}>
                            <FieldLabel htmlFor={id}>{key === 'channel' ? '更新通道' : '下载来源'}</FieldLabel>
                            <Select items={options} value={draft.update[key]} disabled={busy}
                              onValueChange={(value) => { if (value) setDraft({ ...draft, update: { ...draft.update, [key]: value } }) }}>
                              <SelectTrigger id={id} className="w-full" aria-invalid={!!error || undefined} aria-describedby={error ? `${id}-error` : undefined}>
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent><SelectGroup>
                                {options.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
                              </SelectGroup></SelectContent>
                            </Select>
                            {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
                          </Field>
                        )
                      })}
                      {updateTextField('checkInterval', '更新检查间隔', '至少 1m；关闭自动检查时保留此设置。', '1h')}
                      {updateTextField('repo', '发布仓库', 'GitHub 仓库，格式为 owner/name。', 'PFXDev/FireX')}
                      {updateTextField('proxyBaseUrl', '镜像地址', '选择镜像代理时用于下载更新。', 'https://dl.repo.chycloud.top')}
                    </FieldGroup>
                  </FieldGroup>
                </FieldSet>
                <FieldSet disabled={busy}>
                  <FieldLegend>启动时的管理员设置</FieldLegend>
                  <FieldDescription>用于首次创建账号或下次启动时重置密码。立即修改密码请前往「设置 → 账号安全」。</FieldDescription>
                  <FieldGroup className="grid sm:grid-cols-2">
                    {textField('adminUser', '管理员用户名', '密码重置的目标账号，此项不会重命名已有账号。', 'admin')}
                    {textField('adminPassword', '下次启动时重置密码', '留空保留现状。填写后将在重启时应用并清空，所有旧会话失效。')}
                  </FieldGroup>
                  {saved.hasPendingAdminPassword && (
                    <FieldGroup>
                      <Field orientation="horizontal" data-disabled={busy || undefined}>
                        <FieldContent>
                          <FieldLabel htmlFor="server-clear-password">取消已保存的密码重置</FieldLabel>
                          <FieldDescription>文件中已有待应用的密码，不会回显。开启此项并保存即可清除。</FieldDescription>
                        </FieldContent>
                        <Switch id="server-clear-password" checked={clearPassword} disabled={busy} onCheckedChange={setClearPassword} />
                      </Field>
                    </FieldGroup>
                  )}
                </FieldSet>
              </FieldGroup>
            </>
          )}
          {saveError && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>配置未保存</AlertTitle>
              <AlertDescription>{saveError}</AlertDescription>
            </Alert>
          )}
        </CardContent>
        <CardFooter className="flex-col items-stretch gap-3 sm:flex-row sm:items-center">
          <Button type="submit" disabled={busy || !dirty}>
            {saving ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
            {saving ? '保存中…' : '保存配置'}
          </Button>
          <Button type="button" variant="outline" disabled={busy} onClick={() => {
            if (confirmUnsavedNavigation()) void load()
          }}>
            {loading ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
            重新加载配置
          </Button>
          {saved && <p role="status" className="text-sm text-muted-foreground">{dirty ? '有未保存的更改' : saved.restartRequired ? '已保存，等待重启' : '与当前运行配置一致'}</p>}
        </CardFooter>
      </Card>
    </form>
  )
}

function ConfigTextField({ name, label, description, placeholder, value, onChange, disabled, error, password = false }: {
  name: string
  label: string
  description: string
  placeholder?: string
  value: string
  onChange: (value: string) => void
  disabled: boolean
  error?: string
  password?: boolean
}) {
  const id = `server-${name}`
  return (
    <Field data-invalid={!!error || undefined} data-disabled={disabled || undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input id={id} value={value} type={password ? 'password' : 'text'} autoComplete={password ? 'new-password' : 'off'}
        spellCheck={false} placeholder={placeholder} disabled={disabled} onChange={(event) => onChange(event.target.value)}
        aria-invalid={!!error || undefined} aria-describedby={`${id}-description${error ? ` ${id}-error` : ''}`} />
      <FieldDescription id={`${id}-description`}>{description}</FieldDescription>
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </Field>
  )
}

import { type FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import {
  FileCode2Icon,
  InfoIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  SaveIcon,
  ShieldCheckIcon,
  TriangleAlertIcon,
} from 'lucide-react'

import { ApiError, api } from '@/api'
import { CodeTextarea } from '@/components/code-display'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { PageHeader } from '@/components/page-header'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { errorMessage } from '@/lib/format'

interface TemplateResponse {
  template: string
  isDefault: boolean
  default: string
}

export function SettingsPage() {
  const [tpl, setTpl] = useState<TemplateResponse | null>(null)
  const [text, setText] = useState('')
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [restoreOpen, setRestoreOpen] = useState(false)

  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [passwordAttempted, setPasswordAttempted] = useState(false)
  const [passwordBusy, setPasswordBusy] = useState(false)
  const loadPromiseRef = useRef<Promise<boolean> | null>(null)

  const load = useCallback(() => {
    if (loadPromiseRef.current) return loadPromiseRef.current

    const request = (async () => {
      setLoading(true)
      setLoadError(null)
      try {
        const res = await api.get<TemplateResponse>('/settings/clashTemplate')
        setTpl(res)
        setText(res.template || res.default)
        return true
      } catch (err) {
        setLoadError(errorMessage(err, '加载设置失败'))
        return false
      } finally {
        setLoading(false)
        loadPromiseRef.current = null
      }
    })()

    loadPromiseRef.current = request
    return request
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const saveTemplate = async (value: string) => {
    setSaving(true)
    try {
      try {
        await api.put('/settings/clashTemplate', { template: value })
      } catch (err) {
        toast.error(errorMessage(err, '保存失败'))
        return
      }

      const refreshed = await load()
      if (refreshed) toast.success('模板已保存')
      else toast.warning('模板已保存，但刷新最新设置失败，请重新加载。')
    } finally {
      setSaving(false)
    }
  }

  const restoreTemplate = async () => {
    setRestoring(true)
    try {
      try {
        await api.put('/settings/clashTemplate', { template: '' })
      } catch (err) {
        toast.error(errorMessage(err, '恢复默认模板失败'))
        throw err
      }

      const refreshed = await load()
      if (refreshed) toast.success('已恢复内置默认模板')
      else toast.warning('已恢复内置默认模板，但刷新最新设置失败，请重新加载。')
    } finally {
      setRestoring(false)
    }
  }

  const currentInvalid = passwordAttempted && current.length === 0
  const nextInvalid = passwordAttempted && next.length < 8

  const changePassword = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setPasswordAttempted(true)
    if (!current || next.length < 8) return

    setPasswordBusy(true)
    try {
      await api.post('/auth/password', { current, new: next })
      toast.success('密码已修改，请重新登录')
      setTimeout(() => window.location.reload(), 1200)
    } catch (err) {
      toast.error(err instanceof ApiError && err.status === 401 ? '当前密码不正确' : errorMessage(err, '修改失败'))
      setPasswordBusy(false)
    }
  }

  if (!tpl) {
    return (
      <div className="flex w-full max-w-5xl flex-col gap-6">
        <PageHeader title="设置" description="管理订阅模板与管理员账号安全" />
        {loadError ? (
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>无法加载设置</AlertTitle>
            <AlertDescription className="flex flex-col items-start gap-3">
              <p>{loadError}</p>
              <Button variant="outline" size="sm" disabled={loading} onClick={() => void load()}>
                {loading ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
                {loading ? '重试中…' : '重新加载'}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <div aria-busy="true" aria-label="正在加载设置">
            <Skeleton className="h-96 w-full" />
          </div>
        )}
      </div>
    )
  }

  const savedTemplate = tpl.template || tpl.default
  const dirty = text !== savedTemplate
  const canRestore = !tpl.isDefault || dirty
  const templateBusy = loading || saving || restoring

  return (
    <div className="flex w-full max-w-5xl flex-col gap-6">
      <PageHeader title="设置" description="管理订阅模板与管理员账号安全" />

      {loadError && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertTitle>最新设置加载失败</AlertTitle>
          <AlertDescription className="flex flex-col items-start gap-3">
            <p>{loadError}</p>
            <Button variant="outline" size="sm" disabled={loading} onClick={() => void load()}>
              {loading ? <Spinner data-icon="inline-start" /> : <RefreshCwIcon data-icon="inline-start" />}
              {loading ? '重试中…' : '重新加载'}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <Tabs defaultValue="template" className="flex flex-col gap-4">
        <TabsList className="w-full sm:w-fit">
          <TabsTrigger value="template">
            <FileCode2Icon />
            订阅模板
          </TabsTrigger>
          <TabsTrigger value="security">
            <ShieldCheckIcon />
            账号安全
          </TabsTrigger>
        </TabsList>

        <TabsContent value="template">
          <form
            aria-busy={templateBusy}
            onSubmit={(event) => {
              event.preventDefault()
              if (dirty && !templateBusy) void saveTemplate(text)
            }}
          >
            <Card>
              <CardHeader>
                <CardTitle>Clash 订阅模板</CardTitle>
                <CardDescription>
                  订阅的基础配置：DNS、嗅探、端口这些。保存前会渲染一遍验证，避免无效配置进入客户端。
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-4">
                <Alert>
                  <InfoIcon />
                  <AlertTitle>模板只管基础配置</AlertTitle>
                  <AlertDescription>
                    <p>
                      <code>proxies</code>、<code>proxy-groups</code> 和 <code>rules</code> 永远由
                      「分流」页的矩阵按用户的分流方案生成，写在模板里的同名内容会被覆盖。
                    </p>
                    <p className="mt-2">
                      成员为空的策略组会被自动移除，指向它的规则会改写到还存在的组上，所以配额用尽的用户拿到的
                      仍然是一份能加载的配置。
                    </p>
                  </AlertDescription>
                </Alert>

                <FieldGroup>
                  <Field>
                    <FieldLabel htmlFor="clash-template">
                      {tpl.isDefault ? '当前使用内置默认模板' : '当前使用自定义模板'}
                    </FieldLabel>
                    <CodeTextarea
                      id="clash-template"
                      rows={22}
                      className="max-h-[60vh]"
                      value={text}
                      disabled={templateBusy}
                      onChange={(event) => setText(event.target.value)}
                    />
                    <FieldDescription>
                      {dirty ? '有尚未保存的更改。' : '当前内容已保存。'}
                    </FieldDescription>
                  </Field>
                </FieldGroup>
              </CardContent>
              <CardFooter className="flex-col items-stretch gap-2 sm:flex-row sm:items-center">
                <Button type="submit" className="w-full sm:w-auto" disabled={templateBusy || !dirty}>
                  {saving ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
                  {saving ? '保存中…' : '保存模板'}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="w-full sm:w-auto"
                  disabled={templateBusy || !canRestore}
                  onClick={() => setRestoreOpen(true)}
                >
                  {restoring ? <Spinner data-icon="inline-start" /> : <RotateCcwIcon data-icon="inline-start" />}
                  {restoring ? '恢复中…' : '恢复默认'}
                </Button>
              </CardFooter>
            </Card>
          </form>
        </TabsContent>

        <TabsContent value="security">
          <form noValidate aria-busy={passwordBusy} onSubmit={changePassword}>
            <Card className="max-w-xl">
              <CardHeader>
                <CardTitle>修改管理员密码</CardTitle>
                <CardDescription>密码修改后，所有已登录会话都会失效并需要重新登录。</CardDescription>
              </CardHeader>
              <CardContent>
                <FieldGroup>
                  <Field data-invalid={currentInvalid || undefined}>
                    <FieldLabel htmlFor="pw-current">当前密码</FieldLabel>
                    <Input
                      id="pw-current"
                      type="password"
                      autoComplete="current-password"
                      required
                      aria-invalid={currentInvalid || undefined}
                      aria-describedby={currentInvalid ? 'pw-current-error' : undefined}
                      disabled={passwordBusy}
                      value={current}
                      onChange={(event) => setCurrent(event.target.value)}
                    />
                    {currentInvalid && <FieldError id="pw-current-error">请输入当前密码。</FieldError>}
                  </Field>
                  <Field data-invalid={nextInvalid || undefined}>
                    <FieldLabel htmlFor="pw-new">新密码</FieldLabel>
                    <Input
                      id="pw-new"
                      type="password"
                      autoComplete="new-password"
                      required
                      minLength={8}
                      aria-invalid={nextInvalid || undefined}
                      aria-describedby={nextInvalid ? 'pw-new-error' : 'pw-new-description'}
                      disabled={passwordBusy}
                      value={next}
                      onChange={(event) => setNext(event.target.value)}
                    />
                    {nextInvalid ? (
                      <FieldError id="pw-new-error">新密码至少需要 8 位。</FieldError>
                    ) : (
                      <FieldDescription id="pw-new-description">至少 8 位字符。</FieldDescription>
                    )}
                  </Field>
                </FieldGroup>
              </CardContent>
              <CardFooter className="flex-col items-stretch sm:flex-row sm:items-center">
                <Button type="submit" className="w-full sm:w-auto" disabled={passwordBusy}>
                  {passwordBusy && <Spinner data-icon="inline-start" />}
                  {passwordBusy ? '修改中…' : '修改密码'}
                </Button>
              </CardFooter>
            </Card>
          </form>
        </TabsContent>
      </Tabs>

      <ConfirmDialog
        open={restoreOpen}
        onOpenChange={setRestoreOpen}
        title="恢复内置默认模板？"
        description="当前自定义模板和未保存的编辑都会被替换。此操作会立即保存默认模板。"
        confirmLabel="恢复默认"
        onConfirm={restoreTemplate}
      />
    </div>
  )
}

import { useCallback, useEffect, useRef, useState } from 'react'
import { ThemeProvider } from 'next-themes'
import { CircleAlertIcon, FlameIcon } from 'lucide-react'

import { ApiError, FIREX_UNAUTHORIZED_EVENT, api } from '@/api'
import { AppSidebar } from '@/components/app-sidebar'
import { SiteHeader } from '@/components/site-header'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { Spinner } from '@/components/ui/spinner'
import { Toaster } from '@/components/ui/sonner'
import { useHashRoute } from '@/hooks/use-hash-route'
import { errorMessage } from '@/lib/format'
import type { RouteKey } from '@/lib/navigation'
import { OverviewPage } from '@/pages/Overview'
import { PanelsPage } from '@/pages/Panels'
import { InboundsPage } from '@/pages/Inbounds'
import { InboundConfigsPage } from '@/pages/InboundConfigs'
import { InboundTemplatesPage } from '@/pages/InboundTemplates'
import { NodeGroupsPage } from '@/pages/NodeGroups'
import { PlansPage } from '@/pages/Plans'
import { RoutingPage } from '@/pages/Routing'
import { UsersPage } from '@/pages/Users'
import { SettingsPage } from '@/pages/Settings'
import { SystemPage } from '@/pages/System'

/** Restores the collapsed state that the sidebar persists in its cookie. */
function sidebarDefaultOpen() {
  return !document.cookie.split('; ').includes('sidebar_state=false')
}

export function App() {
  return (
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem={false} disableTransitionOnChange>
      <Shell />
      <Toaster position="bottom-right" />
    </ThemeProvider>
  )
}

function Shell() {
  const [username, setUsername] = useState<string | null>(null)
  const [checking, setChecking] = useState(true)
  const route = useHashRoute()
  const insetRef = useRef<HTMLElement>(null)

  // Each page starts at its top; the inset scrolls on desktop, the window on mobile.
  useEffect(() => {
    insetRef.current?.scrollTo(0, 0)
    window.scrollTo(0, 0)
  }, [route])

  const check = useCallback(async () => {
    try {
      const me = await api.get<{ username: string }>('/auth/me', {
        unauthorized: 'session-invalid',
      })
      setUsername(me.username)
    } catch (err) {
      setUsername(null)
      throw err
    } finally {
      setChecking(false)
    }
  }, [])

  useEffect(() => {
    void check().catch(() => undefined)
  }, [check])

  useEffect(() => {
    const onUnauthorized = () => setUsername(null)
    window.addEventListener(FIREX_UNAUTHORIZED_EVENT, onUnauthorized)
    return () => window.removeEventListener(FIREX_UNAUTHORIZED_EVENT, onUnauthorized)
  }, [])

  if (checking) {
    return (
      <div className="flex min-h-svh items-center justify-center bg-background">
        {/* Held back briefly so a fast session check never flashes the spinner. */}
        <div className="flex flex-col items-center gap-3 text-center animate-in fade-in-0 duration-300 delay-200 fill-mode-backwards motion-reduce:animate-none">
          <div className="flex size-10 items-center justify-center rounded-xl bg-brand text-brand-foreground">
            <FlameIcon />
          </div>
          <Spinner />
          <p className="text-sm text-muted-foreground">正在验证管理会话…</p>
        </div>
      </div>
    )
  }

  if (!username) return <Login onSignedIn={check} />

  return (
    <SidebarProvider defaultOpen={sidebarDefaultOpen()}>
      <AppSidebar route={route} username={username} onSignedOut={() => setUsername(null)} />
      {/* On desktop the inset panel is its own scroll container, so the sticky
          header stays inside the rounded frame instead of riding over its margin. */}
      <SidebarInset
        ref={insetRef}
        className="md:h-[calc(100svh-1rem)] md:overflow-y-auto md:border md:border-sidebar-border"
      >
        <SiteHeader route={route} />
        <div
          key={route}
          className="mx-auto flex w-full max-w-screen-2xl flex-1 flex-col gap-6 p-4 animate-in fade-in-0 slide-in-from-bottom-2 duration-300 ease-out motion-reduce:animate-none md:p-6 lg:p-8"
        >
          <Page route={route} />
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

function Page({ route }: { route: RouteKey }) {
  switch (route) {
    case 'panels':
      return <PanelsPage />
    case 'inbounds':
      return <InboundsPage />
    case 'inbound-configs':
      return <InboundConfigsPage />
    case 'inbound-templates':
      return <InboundTemplatesPage />
    case 'node-groups':
      return <NodeGroupsPage />
    case 'plans':
      return <PlansPage />
    case 'users':
      return <UsersPage />
    case 'routing':
      return <RoutingPage />
    case 'settings':
      return <SettingsPage />
    case 'system':
      return <SystemPage />
    default:
      return <OverviewPage />
  }
}

type LoginErrors = {
  username?: string
  password?: string
}

function Login({ onSignedIn }: { onSignedIn: () => Promise<void> }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [errors, setErrors] = useState<LoginErrors>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()

    const nextErrors: LoginErrors = {}
    if (!username.trim()) nextErrors.username = '请输入用户名'
    if (!password) nextErrors.password = '请输入密码'

    if (Object.keys(nextErrors).length > 0) {
      setErrors(nextErrors)
      setFormError(null)
      return
    }

    setErrors({})
    setFormError(null)
    setBusy(true)
    try {
      await api.post(
        '/auth/login',
        { username: username.trim(), password },
        { unauthorized: 'ignore' },
      )
      try {
        await onSignedIn()
      } catch (err) {
        setFormError(
          err instanceof ApiError && err.status === 401
            ? '登录成功，但服务端未建立有效会话，请重新登录。'
            : '登录成功，但暂时无法验证会话，请检查网络后重试。',
        )
      }
    } catch (err) {
      setFormError(
        err instanceof ApiError && err.status === 401
          ? '用户名或密码不正确，请重新输入。'
          : errorMessage(err, '登录失败，请稍后重试。'),
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="grid min-h-svh bg-background">
      <section className="relative flex items-center justify-center overflow-hidden p-4 sm:p-8 lg:p-12">
        <div className="pointer-events-none absolute inset-x-8 top-0 h-40 rounded-full bg-brand/10 blur-3xl lg:hidden" />
        <div className="relative flex w-full max-w-md flex-col gap-6 animate-in fade-in-0 slide-in-from-bottom-2 duration-300 ease-out motion-reduce:animate-none">
          <div className="flex items-center gap-3">
            <div className="flex size-10 items-center justify-center rounded-xl bg-brand text-brand-foreground">
              <FlameIcon />
            </div>
            <div>
              <p className="font-heading font-semibold">FireX</p>
              <p className="text-sm text-muted-foreground">3X-UI 控制平面</p>
            </div>
          </div>

          <form onSubmit={submit} noValidate>
            <Card className="w-full">
              <CardHeader>
                <CardTitle>欢迎回来</CardTitle>
                <CardDescription>使用管理员账户登录 FireX 控制台。</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-5">
                {formError && (
                  <Alert variant="destructive">
                    <CircleAlertIcon />
                    <AlertTitle>登录失败</AlertTitle>
                    <AlertDescription>{formError}</AlertDescription>
                  </Alert>
                )}
                <FieldGroup>
                  <Field data-invalid={Boolean(errors.username) || undefined} data-disabled={busy || undefined}>
                    <FieldLabel htmlFor="login-username">用户名</FieldLabel>
                    <Input
                      id="login-username"
                      value={username}
                      autoFocus
                      autoComplete="username"
                      required
                      disabled={busy}
                      aria-invalid={Boolean(errors.username)}
                      aria-describedby={errors.username ? 'login-username-error' : undefined}
                      onChange={(event) => {
                        setUsername(event.target.value)
                        if (errors.username) setErrors((current) => ({ ...current, username: undefined }))
                      }}
                    />
                    <FieldError id="login-username-error">{errors.username}</FieldError>
                  </Field>
                  <Field data-invalid={Boolean(errors.password) || undefined} data-disabled={busy || undefined}>
                    <FieldLabel htmlFor="login-password">密码</FieldLabel>
                    <Input
                      id="login-password"
                      type="password"
                      autoComplete="current-password"
                      required
                      disabled={busy}
                      aria-invalid={Boolean(errors.password)}
                      aria-describedby={errors.password ? 'login-password-error' : undefined}
                      onChange={(event) => {
                        setPassword(event.target.value)
                        if (errors.password) setErrors((current) => ({ ...current, password: undefined }))
                      }}
                      value={password}
                    />
                    <FieldError id="login-password-error">{errors.password}</FieldError>
                  </Field>
                </FieldGroup>
              </CardContent>
              <CardFooter>
                <Button type="submit" size="lg" className="w-full" disabled={busy}>
                  {busy && <Spinner data-icon="inline-start" />}
                  {busy ? '正在登录…' : '进入控制台'}
                </Button>
              </CardFooter>
            </Card>
          </form>

          <p className="text-center text-xs text-muted-foreground">会话凭据仅通过安全 Cookie 保存</p>
        </div>
      </section>
    </main>
  )
}

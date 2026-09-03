import { useCallback, useEffect, useState } from 'react'
import { ThemeProvider, useTheme } from 'next-themes'
import { toast } from 'sonner'
import {
  BoxesIcon,
  ChevronsUpDownIcon,
  CircleAlertIcon,
  CpuIcon,
  FlameIcon,
  GaugeIcon,
  LayersIcon,
  LogOutIcon,
  MoonIcon,
  ServerIcon,
  SettingsIcon,
  ShieldCheckIcon,
  SplitIcon,
  SunIcon,
  TicketIcon,
  UsersIcon,
} from 'lucide-react'

import { ApiError, FIREX_UNAUTHORIZED_EVENT, api } from '@/api'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from '@/components/ui/sidebar'
import { Spinner } from '@/components/ui/spinner'
import { Toaster } from '@/components/ui/sonner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { errorMessage } from '@/lib/format'
import { OverviewPage } from '@/pages/Overview'
import { PanelsPage } from '@/pages/Panels'
import { InboundsPage } from '@/pages/Inbounds'
import { NodeGroupsPage } from '@/pages/NodeGroups'
import { PlansPage } from '@/pages/Plans'
import { RoutingPage } from '@/pages/Routing'
import { UsersPage } from '@/pages/Users'
import { SettingsPage } from '@/pages/Settings'
import { SystemPage } from '@/pages/System'

const ROUTES = [
  { key: 'overview', label: '总览', group: '工作台', icon: GaugeIcon },
  { key: 'panels', label: '面板', group: '资源管理', icon: ServerIcon },
  { key: 'inbounds', label: '入站', group: '资源管理', icon: LayersIcon },
  { key: 'node-groups', label: '节点组', group: '资源管理', icon: BoxesIcon },
  { key: 'plans', label: '套餐', group: '资源管理', icon: TicketIcon },
  { key: 'users', label: '用户', group: '资源管理', icon: UsersIcon },
  { key: 'routing', label: '分流', group: '运维配置', icon: SplitIcon },
  { key: 'settings', label: '设置', group: '运维配置', icon: SettingsIcon },
  { key: 'system', label: '系统', group: '运维配置', icon: CpuIcon },
] as const

const ROUTE_GROUPS = ['工作台', '资源管理', '运维配置'] as const

type RouteKey = (typeof ROUTES)[number]['key']

function currentRoute(): RouteKey {
  const hash = window.location.hash.replace('#/', '')
  return (ROUTES.find((route) => route.key === hash)?.key ?? 'overview') as RouteKey
}

export function App() {
  return (
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem={false}>
      <Shell />
      <Toaster position="bottom-right" />
    </ThemeProvider>
  )
}

function Shell() {
  const [username, setUsername] = useState<string | null>(null)
  const [checking, setChecking] = useState(true)
  const [route, setRoute] = useState<RouteKey>(currentRoute)

  useEffect(() => {
    const onHash = () => setRoute(currentRoute())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

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
        <div className="flex flex-col items-center gap-3 text-center">
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

  const activeRoute = ROUTES.find((item) => item.key === route) ?? ROUTES[0]

  return (
    <SidebarProvider>
      <AppSidebar route={route} username={username} onSignedOut={() => setUsername(null)} />
      <SidebarInset>
        <header className="sticky top-0 z-10 flex h-12 shrink-0 items-center gap-2 border-b bg-background/85 px-4 backdrop-blur-md md:px-6">
          <SidebarTrigger className="-ml-1" aria-label="切换侧边栏" />
          <Separator orientation="vertical" className="mr-1 h-4" />
          <div className="flex min-w-0 items-center gap-2">
            <span className="hidden text-sm text-muted-foreground sm:inline">{activeRoute.group}</span>
            <span className="hidden text-muted-foreground sm:inline" aria-hidden="true">
              /
            </span>
            <span className="truncate text-sm font-medium">{activeRoute.label}</span>
          </div>
          <div className="ml-auto flex items-center gap-1">
            <ThemeToggle />
          </div>
        </header>
        <div className="mx-auto flex w-full max-w-screen-2xl flex-1 flex-col gap-6 p-4 md:p-6 lg:p-8">
          <Page route={route} />
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

function AppSidebar({
  route,
  username,
  onSignedOut,
}: {
  route: RouteKey
  username: string
  onSignedOut: () => void
}) {
  const { isMobile, setOpenMobile } = useSidebar()
  const closeMobile = () => {
    if (isMobile) setOpenMobile(false)
  }

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              size="lg"
              render={
                <a
                  href="#/overview"
                  aria-current={route === 'overview' ? 'page' : undefined}
                  onClick={closeMobile}
                />
              }
            >
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-brand text-brand-foreground">
                <FlameIcon />
              </div>
              <div className="grid flex-1 text-left leading-tight">
                <span className="truncate font-heading font-semibold">FireX</span>
                <span className="truncate text-xs text-sidebar-foreground/70">3X-UI 控制平面</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <nav aria-label="主导航">
          {ROUTE_GROUPS.map((group) => (
            <SidebarGroup key={group}>
              <SidebarGroupLabel>{group}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {ROUTES.filter((item) => item.group === group).map((item) => (
                    <SidebarMenuItem key={item.key}>
                      <SidebarMenuButton
                        isActive={route === item.key}
                        tooltip={item.label}
                        render={
                          <a
                            href={`#/${item.key}`}
                            aria-current={route === item.key ? 'page' : undefined}
                            onClick={closeMobile}
                          />
                        }
                      >
                        <item.icon />
                        <span>{item.label}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ))}
        </nav>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <NavUser username={username} onSignedOut={onSignedOut} />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

function NavUser({ username, onSignedOut }: { username: string; onSignedOut: () => void }) {
  const { isMobile, setOpenMobile } = useSidebar()

  const signOut = async () => {
    try {
      await api.post('/auth/logout')
      onSignedOut()
    } catch (err) {
      toast.error(errorMessage(err, '退出登录失败'))
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<SidebarMenuButton size="lg" />}>
        <Avatar size="sm">
          <AvatarFallback>{username.slice(0, 2).toUpperCase()}</AvatarFallback>
        </Avatar>
        <div className="grid flex-1 text-left leading-tight">
          <span className="truncate font-medium">{username}</span>
          <span className="truncate text-xs text-sidebar-foreground/70">管理员</span>
        </div>
        <ChevronsUpDownIcon className="ml-auto" />
      </DropdownMenuTrigger>
      <DropdownMenuContent side={isMobile ? 'bottom' : 'right'} align="end" className="min-w-52">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{username}</DropdownMenuLabel>
          <DropdownMenuItem
            onClick={() => {
              window.location.hash = '#/settings'
              if (isMobile) setOpenMobile(false)
            }}
          >
            <SettingsIcon />
            设置
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem variant="destructive" onClick={() => void signOut()}>
            <LogOutIcon />
            退出登录
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme()
  const dark = resolvedTheme !== 'light'
  const label = dark ? '切换到浅色主题' : '切换到深色主题'
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={label}
            onClick={() => setTheme(dark ? 'light' : 'dark')}
          />
        }
      >
        {dark ? <SunIcon data-icon="inline-start" /> : <MoonIcon data-icon="inline-start" />}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function Page({ route }: { route: RouteKey }) {
  switch (route) {
    case 'panels':
      return <PanelsPage />
    case 'inbounds':
      return <InboundsPage />
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
    <main className="grid min-h-svh bg-background lg:grid-cols-2">
      <section className="relative hidden overflow-hidden border-r bg-muted/30 p-10 lg:flex lg:flex-col xl:p-14">
        <div className="pointer-events-none absolute -left-24 top-1/4 size-80 rounded-full bg-brand/10 blur-3xl" />
        <div className="pointer-events-none absolute -bottom-32 right-0 size-96 rounded-full bg-brand/10 blur-3xl" />

        <div className="relative flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-xl bg-brand text-brand-foreground">
            <FlameIcon />
          </div>
          <div>
            <p className="font-heading font-semibold">FireX</p>
            <p className="text-sm text-muted-foreground">3X-UI 控制平面</p>
          </div>
        </div>

        <div className="relative my-auto flex max-w-xl flex-col gap-6">
          <Badge variant="outline">统一管理 · 清晰掌控</Badge>
          <div className="flex flex-col gap-3">
            <h1 className="font-heading text-4xl font-semibold tracking-tight xl:text-5xl">
              一个入口，掌握所有面板与节点。
            </h1>
            <p className="max-w-lg text-base leading-relaxed text-muted-foreground">
              集中查看健康状态、交付套餐与订阅，并让同步、流量和系统维护保持高效可控。
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Badge variant="secondary">
              <ServerIcon data-icon="inline-start" />
              多面板管理
            </Badge>
            <Badge variant="secondary">
              <LayersIcon data-icon="inline-start" />
              节点自动发现
            </Badge>
            <Badge variant="secondary">
              <ShieldCheckIcon data-icon="inline-start" />
              安全会话
            </Badge>
          </div>
        </div>

        <p className="relative text-sm text-muted-foreground">可靠地运行在你的基础设施中</p>
      </section>

      <section className="relative flex items-center justify-center overflow-hidden p-4 sm:p-8 lg:p-12">
        <div className="pointer-events-none absolute inset-x-8 top-0 h-40 rounded-full bg-brand/10 blur-3xl lg:hidden" />
        <div className="relative flex w-full max-w-md flex-col gap-6">
          <div className="flex items-center gap-3 lg:hidden">
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

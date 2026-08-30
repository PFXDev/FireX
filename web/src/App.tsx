import { useCallback, useEffect, useState } from 'react'
import { ThemeProvider, useTheme } from 'next-themes'
import { toast } from 'sonner'
import {
  ChevronsUpDownIcon,
  FlameIcon,
  GaugeIcon,
  LayersIcon,
  LogOutIcon,
  MoonIcon,
  ServerIcon,
  SettingsIcon,
  SunIcon,
  TicketIcon,
  UsersIcon,
} from 'lucide-react'

import { ApiError, api } from '@/api'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
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
  SidebarTrigger,
} from '@/components/ui/sidebar'
import { Spinner } from '@/components/ui/spinner'
import { Toaster } from '@/components/ui/sonner'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { errorMessage } from '@/lib/format'
import { OverviewPage } from '@/pages/Overview'
import { PanelsPage } from '@/pages/Panels'
import { NodesPage } from '@/pages/Nodes'
import { PlansPage } from '@/pages/Plans'
import { UsersPage } from '@/pages/Users'
import { SettingsPage } from '@/pages/Settings'

const ROUTES = [
  { key: 'overview', label: '总览', icon: GaugeIcon },
  { key: 'panels', label: '面板', icon: ServerIcon },
  { key: 'nodes', label: '节点', icon: LayersIcon },
  { key: 'plans', label: '套餐', icon: TicketIcon },
  { key: 'users', label: '用户', icon: UsersIcon },
  { key: 'settings', label: '设置', icon: SettingsIcon },
] as const

type RouteKey = (typeof ROUTES)[number]['key']

function currentRoute(): RouteKey {
  const hash = window.location.hash.replace('#/', '')
  return (ROUTES.find((r) => r.key === hash)?.key ?? 'overview') as RouteKey
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
      const me = await api.get<{ username: string }>('/auth/me')
      setUsername(me.username)
    } catch {
      setUsername(null)
    } finally {
      setChecking(false)
    }
  }, [])

  useEffect(() => {
    void check()
  }, [check])

  // A 401 anywhere means the session lapsed; drop straight back to the login
  // screen instead of leaving half-loaded pages behind.
  useEffect(() => {
    const onRejection = (e: PromiseRejectionEvent) => {
      if (e.reason instanceof ApiError && e.reason.status === 401) setUsername(null)
    }
    window.addEventListener('unhandledrejection', onRejection)
    return () => window.removeEventListener('unhandledrejection', onRejection)
  }, [])

  if (checking) {
    return (
      <div className="flex min-h-svh items-center justify-center">
        <Spinner />
      </div>
    )
  }
  if (!username) return <Login onSignedIn={check} />

  return (
    <SidebarProvider>
      <Sidebar collapsible="icon">
        <SidebarHeader>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton size="lg" render={<a href="#/overview" />}>
                <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
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
          <SidebarGroup>
            <SidebarGroupLabel>管理</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {ROUTES.map((r) => (
                  <SidebarMenuItem key={r.key}>
                    <SidebarMenuButton
                      isActive={route === r.key}
                      tooltip={r.label}
                      render={<a href={`#/${r.key}`} />}
                    >
                      <r.icon />
                      <span>{r.label}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
        <SidebarFooter>
          <SidebarMenu>
            <SidebarMenuItem>
              <NavUser username={username} onSignedOut={() => setUsername(null)} />
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
      </Sidebar>
      <SidebarInset>
        <header className="sticky top-0 z-10 flex h-12 shrink-0 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur-md">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-1 h-4" />
          <span className="text-sm font-medium">{ROUTES.find((r) => r.key === route)?.label}</span>
          <div className="ml-auto flex items-center gap-1">
            <ThemeToggle />
          </div>
        </header>
        <div className="flex flex-1 flex-col gap-6 p-4 md:p-6">
          <Page route={route} />
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

function NavUser({ username, onSignedOut }: { username: string; onSignedOut: () => void }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<SidebarMenuButton size="lg" />}>
        <Avatar size="sm">
          <AvatarFallback className="bg-primary/10 font-medium text-primary uppercase">
            {username.slice(0, 2)}
          </AvatarFallback>
        </Avatar>
        <div className="grid flex-1 text-left leading-tight">
          <span className="truncate font-medium">{username}</span>
          <span className="truncate text-xs text-sidebar-foreground/70">管理员</span>
        </div>
        <ChevronsUpDownIcon className="ml-auto" />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="right" align="end" className="min-w-52">
        <DropdownMenuLabel>{username}</DropdownMenuLabel>
        <DropdownMenuGroup>
          <DropdownMenuItem
            onClick={() => {
              window.location.hash = '#/settings'
            }}
          >
            <SettingsIcon />
            设置
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem
            variant="destructive"
            onClick={async () => {
              await api.post('/auth/logout')
              onSignedOut()
            }}
          >
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
        {dark ? <SunIcon /> : <MoonIcon />}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function Page({ route }: { route: RouteKey }) {
  switch (route) {
    case 'panels':
      return <PanelsPage />
    case 'nodes':
      return <NodesPage />
    case 'plans':
      return <PlansPage />
    case 'users':
      return <UsersPage />
    case 'settings':
      return <SettingsPage />
    default:
      return <OverviewPage />
  }
}

function Login({ onSignedIn }: { onSignedIn: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/auth/login', { username, password })
      onSignedIn()
    } catch (err) {
      toast.error(errorMessage(err, '登录失败'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-6">
      <Card className="w-full max-w-sm">
        <CardHeader className="justify-items-center text-center">
          <div className="flex size-11 items-center justify-center rounded-xl bg-primary text-primary-foreground">
            <FlameIcon className="size-5" />
          </div>
          <CardTitle className="text-xl">FireX</CardTitle>
          <CardDescription>3X-UI 多面板统一管理</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="login-username">用户名</FieldLabel>
                <Input
                  id="login-username"
                  value={username}
                  autoFocus
                  autoComplete="username"
                  onChange={(e) => setUsername(e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="login-password">密码</FieldLabel>
                <Input
                  id="login-password"
                  type="password"
                  autoComplete="current-password"
                  onChange={(e) => setPassword(e.target.value)}
                  value={password}
                />
              </Field>
              <Button type="submit" size="lg" disabled={busy}>
                {busy && <Spinner data-icon="inline-start" />}
                {busy ? '登录中…' : '登录'}
              </Button>
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}

import { useCallback, useEffect, useState } from 'react'
import { ThemeProvider, useTheme } from 'next-themes'
import { toast } from 'sonner'
import {
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
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
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
              <SidebarMenuButton size="lg" className="pointer-events-none">
                <FlameIcon />
                <span className="font-heading text-base font-semibold">FireX</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {ROUTES.map((r) => (
                  <SidebarMenuItem key={r.key}>
                    <SidebarMenuButton
                      isActive={route === r.key}
                      tooltip={r.label}
                      onClick={() => {
                        window.location.hash = `#/${r.key}`
                      }}
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
              <ThemeToggleButton />
            </SidebarMenuItem>
            <SidebarMenuItem>
              <SidebarMenuButton
                tooltip="退出登录"
                onClick={async () => {
                  await api.post('/auth/logout')
                  setUsername(null)
                }}
              >
                <LogOutIcon />
                <span>{username}</span>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarFooter>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger />
          <span className="text-sm text-muted-foreground">
            {ROUTES.find((r) => r.key === route)?.label}
          </span>
        </header>
        <div className="flex flex-1 flex-col gap-6 p-6">
          <Page route={route} />
        </div>
      </SidebarInset>
    </SidebarProvider>
  )
}

function ThemeToggleButton() {
  const { resolvedTheme, setTheme } = useTheme()
  const dark = resolvedTheme !== 'light'
  return (
    <SidebarMenuButton
      tooltip={dark ? '切换到浅色' : '切换到深色'}
      onClick={() => setTheme(dark ? 'light' : 'dark')}
    >
      {dark ? <SunIcon /> : <MoonIcon />}
      <span>{dark ? '浅色主题' : '深色主题'}</span>
    </SidebarMenuButton>
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
    <div className="flex min-h-svh items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <CardTitle className="flex items-center justify-center gap-2 text-2xl">
            <FlameIcon className="size-6 text-primary" />
            FireX
          </CardTitle>
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
                  value={password}
                  autoComplete="current-password"
                  onChange={(e) => setPassword(e.target.value)}
                />
              </Field>
              <Button type="submit" disabled={busy}>
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

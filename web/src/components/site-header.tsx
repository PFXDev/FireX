import { Fragment } from 'react'
import { useTheme } from 'next-themes'
import { MonitorIcon, MoonIcon, SunIcon } from 'lucide-react'

import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Kbd, KbdGroup } from '@/components/ui/kbd'
import { Separator } from '@/components/ui/separator'
import { SidebarTrigger } from '@/components/ui/sidebar'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { routeHref, routeTrail, type RouteKey } from '@/lib/navigation'

const MOD_KEY = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'

export function SiteHeader({ route }: { route: RouteKey }) {
  const trail = routeTrail(route)
  const page = trail[trail.length - 1]

  return (
    <header className="sticky top-0 z-10 flex h-12 shrink-0 items-center gap-2 border-b bg-background/85 px-4 backdrop-blur-md md:rounded-t-xl md:px-6">
      <Tooltip>
        <TooltipTrigger render={<SidebarTrigger className="-ml-1" aria-label="切换侧边栏" />} />
        <TooltipContent side="bottom" align="start">
          切换侧边栏
          <KbdGroup>
            <Kbd>{MOD_KEY}</Kbd>
            <Kbd>B</Kbd>
          </KbdGroup>
        </TooltipContent>
      </Tooltip>
      <Separator orientation="vertical" className="mr-2 data-vertical:h-4 data-vertical:self-auto" />
      <Breadcrumb className="min-w-0">
        <BreadcrumbList className="flex-nowrap">
          {trail.slice(0, -1).map((crumb) => (
            <Fragment key={crumb.title}>
              <BreadcrumbItem className="hidden md:inline-flex">
                {crumb.route ? (
                  <BreadcrumbLink href={routeHref(crumb.route)}>{crumb.title}</BreadcrumbLink>
                ) : (
                  crumb.title
                )}
              </BreadcrumbItem>
              <BreadcrumbSeparator className="hidden md:inline-flex" />
            </Fragment>
          ))}
          <BreadcrumbItem className="min-w-0">
            <BreadcrumbPage className="truncate">{page.title}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <div className="ml-auto flex items-center gap-1">
        <ThemeToggle />
      </div>
    </header>
  )
}

function ThemeToggle() {
  const { theme = 'dark', resolvedTheme, setTheme } = useTheme()
  const icon = theme === 'system' ? <MonitorIcon /> : resolvedTheme === 'light' ? <SunIcon /> : <MoonIcon />
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon-sm" aria-label="切换外观主题" title="切换外观主题" />}
      >
        {icon}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-36">
        <DropdownMenuGroup>
          <DropdownMenuLabel>外观主题</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={theme} onValueChange={setTheme}>
            <DropdownMenuRadioItem value="system">
              <MonitorIcon />
              跟随系统
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="light">
              <SunIcon />
              浅色主题
            </DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="dark">
              <MoonIcon />
              深色主题
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

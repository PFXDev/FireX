import type { ComponentProps } from 'react'
import { FlameIcon } from 'lucide-react'

import { NavMain } from '@/components/nav-main'
import { NavUser } from '@/components/nav-user'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar'
import { DEFAULT_ROUTE, NAV_GROUPS, routeHref, type RouteKey } from '@/lib/navigation'

export function AppSidebar({
  route,
  username,
  onSignedOut,
  ...props
}: ComponentProps<typeof Sidebar> & {
  route: RouteKey
  username: string
  onSignedOut: () => void
}) {
  const { isMobile, setOpenMobile } = useSidebar()

  return (
    <Sidebar collapsible="icon" variant="inset" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              size="lg"
              tooltip="FireX 总览"
              render={
                <a
                  href={routeHref(DEFAULT_ROUTE)}
                  onClick={() => {
                    if (isMobile) setOpenMobile(false)
                  }}
                />
              }
            >
              <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-brand text-brand-foreground">
                <FlameIcon />
              </div>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-heading font-semibold">FireX</span>
                <span className="truncate text-xs text-muted-foreground">3X-UI 控制平面</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent role="navigation" aria-label="主导航">
        <NavMain groups={NAV_GROUPS} route={route} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser username={username} onSignedOut={onSignedOut} />
      </SidebarFooter>
      <SidebarRail aria-label="切换侧边栏" title="切换侧边栏" />
    </Sidebar>
  )
}

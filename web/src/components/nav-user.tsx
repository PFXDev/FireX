import { toast } from 'sonner'
import { ChevronsUpDownIcon, LogOutIcon, SettingsIcon } from 'lucide-react'

import { api } from '@/api'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem, useSidebar } from '@/components/ui/sidebar'
import { confirmUnsavedNavigation } from '@/hooks/use-unsaved-guard'
import { errorMessage } from '@/lib/format'
import { routeHref } from '@/lib/navigation'
import { useSession, type Admin } from '@/lib/session'

export function NavUser({ admin, onSignedOut }: { admin: Admin; onSignedOut: () => void }) {
  const { isMobile, setOpenMobile } = useSidebar()
  const { isSuperAdmin } = useSession()
  const initials = admin.username.slice(0, 2).toUpperCase()

  const signOut = async () => {
    if (!confirmUnsavedNavigation()) return
    try {
      await api.post('/auth/logout')
      onSignedOut()
    } catch (err) {
      toast.error(errorMessage(err, '退出登录失败'))
    }
  }

  const identity = (
    <>
      <Avatar className="rounded-lg after:rounded-lg">
        <AvatarFallback className="rounded-lg bg-sidebar-accent font-medium text-sidebar-accent-foreground">
          {initials}
        </AvatarFallback>
      </Avatar>
      <div className="grid flex-1 text-left text-sm leading-tight">
        <span className="truncate font-medium">{admin.username}</span>
        <span className="truncate text-xs text-muted-foreground">
          {isSuperAdmin ? '超级管理员' : '普通管理员'}
        </span>
      </div>
    </>
  )

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger render={<SidebarMenuButton size="lg" className="aria-expanded:bg-sidebar-accent aria-expanded:text-sidebar-accent-foreground" />}>
            {identity}
            <ChevronsUpDownIcon className="ml-auto text-muted-foreground" />
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="min-w-56 rounded-lg"
            side={isMobile ? 'bottom' : 'right'}
            align="end"
            sideOffset={4}
          >
            <DropdownMenuGroup>
              <DropdownMenuLabel className="p-0 font-normal">
                <div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">{identity}</div>
              </DropdownMenuLabel>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem
                render={
                  <a
                    href={routeHref('settings')}
                    onClick={() => {
                      if (isMobile) setOpenMobile(false)
                    }}
                  />
                }
              >
                <SettingsIcon />
                {isSuperAdmin ? '设置' : '账号安全'}
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
      </SidebarMenuItem>
    </SidebarMenu>
  )
}

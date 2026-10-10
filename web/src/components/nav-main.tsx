import { useEffect, useState } from 'react'
import { ChevronRightIcon } from 'lucide-react'

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { isRouteWithin, routeHref, type NavGroup, type NavItem, type RouteKey } from '@/lib/navigation'

export function NavMain({ groups, route }: { groups: NavGroup[]; route: RouteKey }) {
  return (
    <>
      {groups.map((group) => (
        <SidebarGroup key={group.title}>
          <SidebarGroupLabel>{group.title}</SidebarGroupLabel>
          <SidebarMenu className="gap-0.5">
            {group.items.map((item) => (
              <NavMainItem key={item.route} item={item} route={route} />
            ))}
          </SidebarMenu>
        </SidebarGroup>
      ))}
    </>
  )
}

function NavMainItem({ item, route }: { item: NavItem; route: RouteKey }) {
  const { isMobile, setOpenMobile, state } = useSidebar()
  const within = isRouteWithin(item, route)
  const [open, setOpen] = useState(within)

  // Reveal the sub-menu whenever one of its pages becomes active, including
  // navigation from in-page links and breadcrumbs.
  useEffect(() => {
    if (within) setOpen(true)
  }, [within])

  const link = (target: RouteKey) => (
    <a
      href={routeHref(target)}
      aria-current={route === target ? 'page' : undefined}
      onClick={() => {
        if (isMobile) setOpenMobile(false)
      }}
    />
  )

  if (!item.items?.length) {
    return (
      <SidebarMenuItem>
        <SidebarMenuButton isActive={route === item.route} tooltip={item.title} render={link(item.route)}>
          <item.icon />
          <span>{item.title}</span>
        </SidebarMenuButton>
      </SidebarMenuItem>
    )
  }

  // Icon mode hides the inline sub-menu, so the sub-pages move into a flyout
  // and the parent carries the active state.
  if (state === 'collapsed' && !isMobile) {
    return (
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger render={<SidebarMenuButton isActive={within} aria-label={item.title} />}>
            <item.icon />
            <span>{item.title}</span>
          </DropdownMenuTrigger>
          <DropdownMenuContent side="right" align="start" sideOffset={8} className="w-auto min-w-40">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{item.title}</DropdownMenuLabel>
              {item.items.map((sub) => (
                <DropdownMenuItem
                  key={sub.route}
                  render={link(sub.route)}
                  className="aria-[current=page]:bg-accent aria-[current=page]:font-medium aria-[current=page]:text-accent-foreground"
                >
                  {sub.title}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    )
  }

  return (
    <Collapsible open={open} onOpenChange={setOpen} render={<SidebarMenuItem />}>
      <CollapsibleTrigger render={<SidebarMenuButton />}>
        <item.icon />
        <span>{item.title}</span>
        <ChevronRightIcon
          aria-hidden
          className="ml-auto text-muted-foreground transition-transform duration-200 group-aria-expanded/menu-button:rotate-90 motion-reduce:transition-none"
        />
      </CollapsibleTrigger>
      <CollapsibleContent className="h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-200 ease-out data-ending-style:h-0 data-starting-style:h-0 motion-reduce:transition-none">
        <SidebarMenuSub className="mt-0.5">
          {item.items.map((sub) => (
            <SidebarMenuSubItem key={sub.route}>
              <SidebarMenuSubButton isActive={route === sub.route} render={link(sub.route)}>
                <span>{sub.title}</span>
              </SidebarMenuSubButton>
            </SidebarMenuSubItem>
          ))}
        </SidebarMenuSub>
      </CollapsibleContent>
    </Collapsible>
  )
}

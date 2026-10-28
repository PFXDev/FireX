import {
  BotIcon,
  BoxesIcon,
  CpuIcon,
  DatabaseIcon,
  HistoryIcon,
  GaugeIcon,
  LayersIcon,
  ServerIcon,
  SettingsIcon,
  ShieldIcon,
  SplitIcon,
  TicketIcon,
  UsersIcon,
  type LucideIcon,
} from 'lucide-react'

export type RouteKey =
  | 'overview'
  | 'admins'
  | 'panels'
  | 'inbounds'
  | 'inbound-configs'
  | 'inbound-templates'
  | 'node-groups'
  | 'plans'
  | 'users'
  | 'routing'
  | 'agent'
  | 'audit'
  | 'settings'
  | 'backup'
  | 'system'

export type NavSubItem = { route: RouteKey; title: string }

export type NavItem = {
  route: RouteKey
  title: string
  icon: LucideIcon
  items?: NavSubItem[]
  /** Super-admin-only pages are hidden from operators everywhere in the UI. */
  superAdminOnly?: boolean
  /** Configuration pages an operator may open read-only when granted. */
  configView?: boolean
}

export type NavGroup = { title: string; items: NavItem[] }

export type Crumb = { title: string; route?: RouteKey }

export const NAV_GROUPS: NavGroup[] = [
  {
    title: '工作台',
    items: [{ route: 'overview', title: '总览', icon: GaugeIcon }],
  },
  {
    title: '资源管理',
    items: [
      { route: 'panels', title: '面板', icon: ServerIcon, superAdminOnly: true, configView: true },
      {
        route: 'inbounds',
        title: '入站',
        icon: LayersIcon,
        superAdminOnly: true,
        configView: true,
        items: [
          { route: 'inbounds', title: '入站列表' },
          { route: 'inbound-configs', title: '入站配置' },
          { route: 'inbound-templates', title: '入站模板' },
        ],
      },
      { route: 'node-groups', title: '节点组', icon: BoxesIcon, superAdminOnly: true, configView: true },
      { route: 'plans', title: '套餐', icon: TicketIcon },
      { route: 'users', title: '用户', icon: UsersIcon },
    ],
  },
  {
    title: '运维配置',
    items: [
      { route: 'routing', title: '分流', icon: SplitIcon, superAdminOnly: true, configView: true },
      { route: 'agent', title: 'Agent 修改', icon: BotIcon, superAdminOnly: true },
      { route: 'audit', title: '操作日志', icon: HistoryIcon, superAdminOnly: true },
      { route: 'settings', title: '设置', icon: SettingsIcon },
      { route: 'backup', title: '备份', icon: DatabaseIcon, superAdminOnly: true },
      { route: 'system', title: '系统', icon: CpuIcon, superAdminOnly: true },
    ],
  },
  {
    title: '账号',
    items: [{ route: 'admins', title: '管理员', icon: ShieldIcon, superAdminOnly: true }],
  },
]

export const DEFAULT_ROUTE: RouteKey = 'overview'

const ROUTE_KEYS = new Set<string>(
  NAV_GROUPS.flatMap((group) =>
    group.items.flatMap((item) => [item.route, ...(item.items ?? []).map((sub) => sub.route)]),
  ),
)

export function routeHref(route: RouteKey) {
  return `#/${route}`
}

/** Resolves a hash such as `#/inbound-configs?id=3` to its page; unknown hashes fall back to the overview. */
export function parseRoute(hash: string): RouteKey {
  const key = hash.replace(/^#\/?/, '').split('?')[0]
  return ROUTE_KEYS.has(key) ? (key as RouteKey) : DEFAULT_ROUTE
}

/** Whether the route is the item itself or one of its sub-pages. */
export function isRouteWithin(item: NavItem, route: RouteKey) {
  return item.route === route || Boolean(item.items?.some((sub) => sub.route === route))
}

export type Access = { isSuperAdmin: boolean; canViewConfig: boolean }

/**
 * The navigation one admin sees. Operators keep the overview, plans (read-only)
 * and users, plus the configuration pages read-only when granted; every other
 * infrastructure page is dropped entirely rather than shown disabled, since the
 * server would refuse it anyway.
 */
export function navGroupsFor({ isSuperAdmin, canViewConfig }: Access): NavGroup[] {
  if (isSuperAdmin) return NAV_GROUPS
  return NAV_GROUPS.map((group) => ({
    ...group,
    items: group.items.filter((item) => !item.superAdminOnly || (canViewConfig && item.configView)),
  })).filter((group) => group.items.length > 0)
}

/** Whether an admin may open a route; used to bounce a hand-typed hash. */
export function canAccess(route: RouteKey, access: Access): boolean {
  return navGroupsFor(access).some((group) =>
    group.items.some((item) => isRouteWithin(item, route)),
  )
}

/** Breadcrumb trail for the header: group, then parent menu (linked) for sub-pages, then the page. */
export function trailIn(groups: NavGroup[], route: RouteKey): Crumb[] {
  for (const group of groups) {
    for (const item of group.items) {
      const sub = item.items?.find((candidate) => candidate.route === route)
      if (sub) return [{ title: group.title }, { title: item.title, route: item.route }, { title: sub.title }]
      if (item.route === route) return [{ title: group.title }, { title: item.title }]
    }
  }
  return trailIn(NAV_GROUPS, DEFAULT_ROUTE)
}

export function routeTrail(route: RouteKey): Crumb[] {
  return trailIn(NAV_GROUPS, route)
}

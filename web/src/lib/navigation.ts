import {
  BotIcon,
  BoxesIcon,
  CpuIcon,
  HistoryIcon,
  GaugeIcon,
  LayersIcon,
  ServerIcon,
  SettingsIcon,
  SplitIcon,
  TicketIcon,
  UsersIcon,
  type LucideIcon,
} from 'lucide-react'

export type RouteKey =
  | 'overview'
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
  | 'system'

export type NavSubItem = { route: RouteKey; title: string }

export type NavItem = {
  route: RouteKey
  title: string
  icon: LucideIcon
  items?: NavSubItem[]
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
      { route: 'panels', title: '面板', icon: ServerIcon },
      {
        route: 'inbounds',
        title: '入站',
        icon: LayersIcon,
        items: [
          { route: 'inbounds', title: '入站列表' },
          { route: 'inbound-configs', title: '入站配置' },
          { route: 'inbound-templates', title: '入站模板' },
        ],
      },
      { route: 'node-groups', title: '节点组', icon: BoxesIcon },
      { route: 'plans', title: '套餐', icon: TicketIcon },
      { route: 'users', title: '用户', icon: UsersIcon },
    ],
  },
  {
    title: '运维配置',
    items: [
      { route: 'routing', title: '分流', icon: SplitIcon },
      { route: 'agent', title: 'Agent 修改', icon: BotIcon },
      { route: 'audit', title: '操作日志', icon: HistoryIcon },
      { route: 'settings', title: '设置', icon: SettingsIcon },
      { route: 'system', title: '系统', icon: CpuIcon },
    ],
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

/** Breadcrumb trail for the header: group, then parent menu (linked) for sub-pages, then the page. */
export function routeTrail(route: RouteKey): Crumb[] {
  for (const group of NAV_GROUPS) {
    for (const item of group.items) {
      const sub = item.items?.find((candidate) => candidate.route === route)
      if (sub) return [{ title: group.title }, { title: item.title, route: item.route }, { title: sub.title }]
      if (item.route === route) return [{ title: group.title }, { title: item.title }]
    }
  }
  return routeTrail(DEFAULT_ROUTE)
}

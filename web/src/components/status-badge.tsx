import type { ConfigState } from '@/api'
import { Badge } from '@/components/ui/badge'

/**
 * Tone maps a FireX state onto a Badge variant. The theme carries dedicated
 * `success` and `warning` tokens alongside `destructive`, so healthy states
 * read green rather than competing with the brand-blue primary buttons.
 */
export type Tone = 'good' | 'warn' | 'idle' | 'bad'

const VARIANT: Record<Tone, 'success' | 'warning' | 'outline' | 'destructive'> = {
  good: 'success',
  warn: 'warning',
  idle: 'outline',
  bad: 'destructive',
}

export function StatusBadge({
  tone,
  children,
  title,
}: {
  tone: Tone
  children: React.ReactNode
  title?: string
}) {
  return (
    <Badge variant={VARIANT[tone]} title={title}>
      {children}
    </Badge>
  )
}

export function PanelStatusBadge({ status }: { status: string }) {
  if (status === 'online') return <StatusBadge tone="good">在线</StatusBadge>
  if (status === 'offline') return <StatusBadge tone="bad">离线</StatusBadge>
  return <StatusBadge tone="idle">未知</StatusBadge>
}

const CONFIG_STATES: Record<ConfigState, { tone: Tone; label: string; title: string }> = {
  '': { tone: 'idle', label: '未接管', title: 'FireX 不管理此入站的面板配置' },
  synced: { tone: 'good', label: '已同步', title: '面板与接管配置一致' },
  pending: { tone: 'warn', label: '待下发', title: '接管配置有改动尚未下发' },
  drifted: { tone: 'bad', label: '面板已偏离', title: '面板配置在 FireX 之外被修改' },
  missing: { tone: 'bad', label: '面板缺失', title: '面板上已找不到此入站' },
}

export function ConfigStateBadge({ state }: { state: ConfigState }) {
  const { tone, label, title } = CONFIG_STATES[state] ?? CONFIG_STATES['']
  return <StatusBadge tone={tone} title={title}>{label}</StatusBadge>
}

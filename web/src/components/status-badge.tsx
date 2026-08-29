import { Badge } from '@/components/ui/badge'

/**
 * Tone maps a FireX state onto the badge variants the theme provides: `good`
 * is the brand accent, `idle` is neutral, `bad` is destructive. The palette
 * has no dedicated success colour, so "healthy" reads as branded rather than
 * green.
 */
export type Tone = 'good' | 'idle' | 'bad'

const VARIANT: Record<Tone, 'default' | 'outline' | 'destructive'> = {
  good: 'default',
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

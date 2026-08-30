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

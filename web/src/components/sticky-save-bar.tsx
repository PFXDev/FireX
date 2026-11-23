import type { ReactNode } from 'react'
import { RotateCcwIcon, SaveIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { cn } from '@/lib/utils'

/**
 * StickySaveBar keeps a clear save/discard action anchored at the bottom of
 * long draft-based configuration pages so operators never have to scroll back up.
 */
export function StickySaveBar({
  visible,
  message = '有未保存的改动',
  saving = false,
  disabled = false,
  saveLabel = '保存',
  savingLabel = '保存中…',
  saveType = 'button',
  discardLabel = '放弃改动',
  onSave,
  onDiscard,
  extraActions,
  className,
}: {
  visible: boolean
  message?: ReactNode
  saving?: boolean
  disabled?: boolean
  saveLabel?: string
  savingLabel?: string
  saveType?: 'button' | 'submit'
  discardLabel?: string
  onSave?: () => void
  onDiscard?: () => void
  extraActions?: ReactNode
  className?: string
}) {
  if (!visible) return null

  return (
    <div
      role="region"
      aria-label="未保存改动操作栏"
      className={cn(
        'sticky bottom-4 z-20 flex flex-wrap items-center justify-between gap-3 rounded-xl border bg-popover/95 px-4 py-3 shadow-lg backdrop-blur-md animate-in fade-in-0 slide-in-from-bottom-2 duration-200 motion-reduce:animate-none',
        className
      )}
    >
      <div className="flex items-center gap-2 text-sm font-medium">
        <span className="size-2 shrink-0 rounded-full bg-warning" aria-hidden />
        <span>{message}</span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {extraActions}
        {onDiscard && (
          <Button type="button" variant="outline" size="sm" disabled={saving || disabled} onClick={onDiscard}>
            <RotateCcwIcon data-icon="inline-start" />
            {discardLabel}
          </Button>
        )}
        {(onSave || saveType === 'submit') && (
          <Button type={saveType} size="sm" disabled={saving || disabled} onClick={onSave}>
            {saving ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
            {saving ? savingLabel : saveLabel}
          </Button>
        )}
      </div>
    </div>
  )
}

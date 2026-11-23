import type { ReactElement, ReactNode } from 'react'
import { CopyIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { copyText } from '@/lib/format'
import { cn } from '@/lib/utils'

/**
 * InfoPopover presents full multi-line details or error messages on tap/click
 * as well as keyboard focus, avoiding desktop-only native title tooltips.
 */
export function InfoPopover({
  title,
  content,
  copyValue,
  trigger,
  nativeButton = true,
  children,
  label,
  className,
  contentClassName,
  align = 'start',
  side = 'bottom',
}: {
  title?: string
  content: ReactNode
  copyValue?: string
  trigger?: ReactElement
  nativeButton?: boolean
  children?: ReactNode
  label?: string
  className?: string
  contentClassName?: string
  align?: 'start' | 'center' | 'end'
  side?: 'top' | 'bottom' | 'left' | 'right'
}) {
  return (
    <Popover>
      {trigger ? (
        <PopoverTrigger render={trigger} nativeButton={nativeButton} aria-label={label} />
      ) : (
        <PopoverTrigger
          aria-label={label}
          className={cn(
            'inline-flex max-w-full cursor-pointer items-center gap-1 rounded-sm text-left outline-none focus-visible:ring-2 focus-visible:ring-ring',
            className
          )}
        >
          {children}
        </PopoverTrigger>
      )}
      <PopoverContent align={align} side={side} className={cn('w-80', contentClassName)}>
        {(title || copyValue) && (
          <div className="mb-1.5 flex items-center justify-between gap-2">
            {title ? <span className="text-xs font-medium text-foreground">{title}</span> : <span />}
            {copyValue && (
              <Button
                type="button"
                variant="ghost"
                size="xs"
                onClick={() => {
                  void copyText(copyValue).then((ok) => {
                    if (ok) toast.success('已复制')
                    else toast.error('复制失败，请手动复制')
                  })
                }}
              >
                <CopyIcon data-icon="inline-start" />
                复制
              </Button>
            )}
          </div>
        )}
        <div className="max-h-60 overflow-y-auto text-xs leading-relaxed break-all whitespace-pre-wrap text-muted-foreground">
          {content}
        </div>
      </PopoverContent>
    </Popover>
  )
}

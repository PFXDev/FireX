import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

export function CodeTextarea({
  className,
  ...props
}: Omit<React.ComponentProps<typeof Textarea>, 'variant'>) {
  return (
    <Textarea
      variant="code"
      className={className}
      spellCheck={false}
      {...props}
    />
  )
}

export function CodeBlock({ className, ...props }: React.ComponentProps<'pre'>) {
  return (
    <pre
      className={cn(
        'overflow-auto rounded-lg bg-muted/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap',
        className,
      )}
      {...props}
    />
  )
}

export function CodeText({ className, ...props }: React.ComponentProps<'span'>) {
  return <span className={cn('font-mono text-xs text-muted-foreground', className)} {...props} />
}

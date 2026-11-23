import { useEffect, useRef, useState } from 'react'

import { Input } from '@/components/ui/input'

function formatNumberValue(value: number, zeroAsEmpty: boolean): string {
  if (!Number.isFinite(value)) return zeroAsEmpty ? '' : '0'
  if (zeroAsEmpty && value === 0) return ''
  return String(value)
}

/**
 * NumberInput buffers the active text while the user types so clearing the last
 * digit or typing decimals never snaps back mid-edit, and optionally renders
 * zero as an empty field with a placeholder such as "不限".
 */
export function NumberInput({
  value,
  onValueChange,
  zeroAsEmpty = false,
  integer = false,
  min,
  max,
  onFocus,
  onBlur,
  ...props
}: Omit<React.ComponentProps<typeof Input>, 'type' | 'value' | 'onChange'> & {
  value: number
  onValueChange: (value: number) => void
  zeroAsEmpty?: boolean
  integer?: boolean
}) {
  const [text, setText] = useState(() => formatNumberValue(value, zeroAsEmpty))
  const focusedRef = useRef(false)

  useEffect(() => {
    if (!focusedRef.current) {
      setText(formatNumberValue(value, zeroAsEmpty))
    }
  }, [value, zeroAsEmpty])

  return (
    <Input
      {...props}
      type="number"
      min={min}
      max={max}
      value={text}
      onFocus={(event) => {
        focusedRef.current = true
        onFocus?.(event)
      }}
      onBlur={(event) => {
        focusedRef.current = false
        const trimmed = text.trim()
        if (trimmed === '' && zeroAsEmpty) {
          onValueChange(0)
          setText('')
        } else {
          setText(formatNumberValue(value, zeroAsEmpty))
        }
        onBlur?.(event)
      }}
      onChange={(event) => {
        const next = event.target.value
        setText(next)
        const trimmed = next.trim()
        if (trimmed === '') {
          if (zeroAsEmpty) onValueChange(0)
          return
        }
        const parsed = Number(trimmed)
        if (!Number.isFinite(parsed)) return
        if (integer && !Number.isInteger(parsed)) return
        onValueChange(parsed)
      }}
    />
  )
}

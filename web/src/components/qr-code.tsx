import { useMemo } from 'react'

import { encodeQrSvgData } from '@/lib/qr'
import { cn } from '@/lib/utils'

export { encodeQrSvgData }

/**
 * QrCode renders a high-contrast SVG QR code inside a white quiet zone so it
 * scans reliably in both light and dark themes.
 */
export function QrCode({
  value,
  size = 180,
  label = '订阅二维码',
  className,
}: {
  value: string
  size?: number
  label?: string
  className?: string
}) {
  const encoded = useMemo(() => (value ? encodeQrSvgData(value) : null), [value])
  if (!encoded) return null

  return (
    <div
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-xl bg-white p-2.5 ring-1 ring-border',
        className
      )}
    >
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${encoded.size} ${encoded.size}`}
        shapeRendering="crispEdges"
        role="img"
        aria-label={label}
      >
        <rect width={encoded.size} height={encoded.size} fill="#ffffff" />
        <path d={encoded.path} fill="#000000" />
      </svg>
    </div>
  )
}

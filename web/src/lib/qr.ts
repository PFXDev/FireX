import { encode } from 'uqr'

export function encodeQrSvgData(value: string): { size: number; path: string } {
  const qr = encode(value, { ecc: 'M', border: 2 })
  const segments: string[] = []
  for (let y = 0; y < qr.size; y++) {
    const row = qr.data[y]
    for (let x = 0; x < qr.size; x++) {
      if (row[x]) segments.push(`M${x},${y}h1v1h-1z`)
    }
  }
  return { size: qr.size, path: segments.join('') }
}

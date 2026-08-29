// Shared formatting for byte counts, quotas and timestamps used across the
// admin pages.

const GB = 1024 ** 3

export function formatBytes(n: number): string {
  if (n <= 0) return '0'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let value = n
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${value.toFixed(value < 10 && i > 0 ? 2 : 0)} ${units[i]}`
}

export function formatQuota(n: number): string {
  return n <= 0 ? '不限' : formatBytes(n)
}

export function gbToBytes(gb: number): number {
  return Math.round(gb * GB)
}

export function bytesToGb(bytes: number): number {
  return bytes <= 0 ? 0 : Math.round((bytes / GB) * 100) / 100
}

export function formatTime(ms: number): string {
  if (!ms) return '—'
  return new Date(ms).toLocaleString('zh-CN', { hour12: false })
}

export function formatExpiry(ms: number): string {
  if (!ms) return '永久'
  const days = Math.ceil((ms - Date.now()) / 86400000)
  const date = new Date(ms).toLocaleDateString('zh-CN')
  if (days < 0) return `${date} (已过期)`
  return `${date} (${days} 天)`
}

/** dateInputValue renders an epoch-ms timestamp for <input type="date">. */
export function dateInputValue(ms: number): string {
  if (!ms) return ''
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** Expiry is stored as end-of-day so a user keeps the whole final day. */
export function dateInputToMs(value: string): number {
  if (!value) return 0
  const d = new Date(`${value}T23:59:59`)
  return Number.isNaN(d.getTime()) ? 0 : d.getTime()
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    // The Clipboard API needs a secure context; a panel reached over plain
    // HTTP still has to be able to hand out subscription links.
    const el = document.createElement('textarea')
    el.value = text
    el.style.position = 'fixed'
    el.style.opacity = '0'
    document.body.appendChild(el)
    el.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(el)
    return ok
  }
}

export function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback
}

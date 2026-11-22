import type { LivePanelStat, LiveSnapshot, LiveTrendPoint, SystemLoadLevel } from '../api'

export const LIVE_HISTORY_CAP = 30

export function mergeLiveHistory(
  prev: LiveTrendPoint[],
  snap: LiveSnapshot,
  cap = LIVE_HISTORY_CAP,
): LiveTrendPoint[] {
  let next: LiveTrendPoint[]
  if (snap.history && snap.history.length > 0) {
    next = [...snap.history]
    const last = next[next.length - 1]
    if (!last || last.t !== snap.point.t) {
      next.push(snap.point)
    } else {
      next[next.length - 1] = snap.point
    }
  } else if (prev.length === 0) {
    next = [snap.point]
  } else if (prev[prev.length - 1].t === snap.point.t) {
    next = [...prev.slice(0, -1), snap.point]
  } else {
    next = [...prev, snap.point]
  }
  return next.length > cap ? next.slice(next.length - cap) : next
}

export type SparklineOptions = {
  min?: number
  max?: number
  padTop?: number
  padBottom?: number
}

export function buildSparklinePath(
  values: (number | null)[],
  width: number,
  height: number,
  options: SparklineOptions = {},
): { line: string; area: string } {
  if (values.length === 0 || width <= 0 || height <= 0) {
    return { line: '', area: '' }
  }

  const padTop = options.padTop ?? 6
  const padBottom = options.padBottom ?? 2
  const usableHeight = Math.max(1, height - padTop - padBottom)

  const measured = values.filter((v): v is number => v !== null && Number.isFinite(v))
  if (measured.length === 0) return { line: '', area: '' }
  let minVal = options.min ?? Math.min(...measured)
  let maxVal = options.max ?? Math.max(...measured)
  if (!Number.isFinite(minVal)) minVal = 0
  if (!Number.isFinite(maxVal)) maxVal = 1
  if (maxVal <= minVal) {
    if (maxVal === 0) {
      minVal = 0
      maxVal = 1
    } else {
      minVal -= 1
      maxVal += 1
    }
  }

  const series = values.length === 1 ? [values[0], values[0]] : values
  const span = maxVal - minVal
  const groups: { x: number; y: number }[][] = []
  let points: { x: number; y: number }[] = []
  for (const [index, value] of series.entries()) {
    if (value === null || !Number.isFinite(value)) {
      points = []
      continue
    }
    if (points.length === 0) groups.push(points)
    const ratio = (Math.min(maxVal, Math.max(minVal, value)) - minVal) / span
    points.push({ x: index * width / (series.length - 1), y: padTop + (1 - ratio) * usableHeight })
  }
  const paths = groups.map((group) => sparklineSegment(group, height))
  return { line: paths.map((p) => p.line).join(' '), area: paths.map((p) => p.area).join(' ') }
}

function sparklineSegment(pts: { x: number; y: number }[], height: number) {
  let line = `M ${pts[0].x.toFixed(1)} ${pts[0].y.toFixed(1)}`
  if (pts.length === 1) {
    line += ' l 0 0'
  } else if (pts.length === 2) {
    line += ` L ${pts[1].x.toFixed(1)} ${pts[1].y.toFixed(1)}`
  } else {
    for (let i = 1; i < pts.length - 2; i++) {
      const mx = (pts[i].x + pts[i + 1].x) / 2
      const my = (pts[i].y + pts[i + 1].y) / 2
      line += ` Q ${pts[i].x.toFixed(1)} ${pts[i].y.toFixed(1)} ${mx.toFixed(1)} ${my.toFixed(1)}`
    }
    const ctrl = pts[pts.length - 2]
    const last = pts[pts.length - 1]
    line += ` Q ${ctrl.x.toFixed(1)} ${ctrl.y.toFixed(1)} ${last.x.toFixed(1)} ${last.y.toFixed(1)}`
  }

  const last = pts[pts.length - 1]
  const area = `${line} L ${last.x.toFixed(1)} ${height.toFixed(1)} L ${pts[0].x.toFixed(1)} ${height.toFixed(1)} Z`
  return { line, area }
}

export function sortPanelsBySpeed(panels: LivePanelStat[]): LivePanelStat[] {
  return [...panels].sort((a, b) => {
    if (a.online !== b.online) return a.online ? -1 : 1
    const speedDiff = b.netUp + b.netDown - (a.netUp + a.netDown)
    if (speedDiff !== 0) return speedDiff
    const nameCmp = a.name.localeCompare(b.name, 'zh-CN')
    if (nameCmp !== 0) return nameCmp
    return a.id - b.id
  })
}

export function loadLevelMeta(level: SystemLoadLevel): {
  label: string
  tone: 'good' | 'idle' | 'warn' | 'bad'
  colorVar: string
} {
  switch (level) {
    case 'unknown':
      return { label: '暂无数据', tone: 'idle', colorVar: 'var(--muted-foreground)' }
    case 'idle':
      return { label: '空闲', tone: 'idle', colorVar: 'var(--brand)' }
    case 'normal':
      return { label: '平稳', tone: 'good', colorVar: 'var(--success)' }
    case 'busy':
      return { label: '繁忙', tone: 'warn', colorVar: 'var(--warning)' }
    case 'high':
      return { label: '高负载', tone: 'bad', colorVar: 'var(--destructive)' }
  }
}

import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { LivePanelStat, LiveSnapshot, LiveSummary, LiveTrendPoint } from '../src/api.ts'
import { formatSpeed } from '../src/lib/format.ts'
import {
  buildSparklinePath,
  loadLevelMeta,
  mergeLiveHistory,
  sortPanelsBySpeed,
} from '../src/lib/live-stats.ts'

function makePoint(t: number, netDown = 100): LiveTrendPoint {
  return {
    t,
    netUp: 50,
    netDown,
    cpuAvg: 20,
    memAvg: 40,
    loadAvg: 25,
    systemLoad: 30,
    connections: 12,
    onlineUsers: 3,
  }
}

function makeSummary(): LiveSummary {
  const zeroExtreme = { value: 0, panelId: 1, panelName: 'p1' }
  return {
    totalPanels: 1,
    onlinePanels: 1,
    activePanels: 1,
    totalNetUp: 50,
    totalNetDown: 100,
    totalTcp: 10,
    totalUdp: 2,
    totalConnections: 12,
    onlineUsers: 3,
    userSamplePanels: 1,
    onlineUserSessions: 3,
    onlineClients: 3,
    totalCpuCores: 4,
    totalMemUsed: 1000,
    totalMemTotal: 4000,
    fleetLoad1: 0.4,
    fleetLoad5: 0.3,
    fleetLoad15: 0.2,
    systemLoadIndex: 30,
    systemLoadLevel: 'idle',
    cpu: { samples: 1, avg: 20, min: zeroExtreme, max: zeroExtreme },
    mem: { samples: 1, avg: 40, min: zeroExtreme, max: zeroExtreme },
    loadIndex: { samples: 1, avg: 25, min: zeroExtreme, max: zeroExtreme },
  }
}

test('formatSpeed formats bytes per second across units', () => {
  assert.equal(formatSpeed(0), '0 B/s')
  assert.equal(formatSpeed(-10), '0 B/s')
  assert.equal(formatSpeed(512), '512 B/s')
  assert.equal(formatSpeed(12.4 * 1024 * 1024), '12.4 MB/s')
  assert.equal(formatSpeed(1.25 * 1024 ** 3), '1.25 GB/s')
  assert.equal(formatSpeed(256 * 1024), '256 KB/s')
})

test('mergeLiveHistory seeds from catch-up history, deduplicates timestamps, and caps length', () => {
  const p1 = makePoint(1000, 100)
  const p2 = makePoint(2000, 200)
  const snapWithHistory: LiveSnapshot = {
    timestamp: 2000,
    intervalMs: 2000,
    summary: makeSummary(),
    panels: [],
    point: makePoint(2000, 250),
    history: [p1, p2],
  }

  const seeded = mergeLiveHistory([], snapWithHistory, 5)
  assert.equal(seeded.length, 2)
  assert.equal(seeded[1].netDown, 250)

  let acc = seeded
  for (let i = 3; i <= 7; i++) {
    acc = mergeLiveHistory(
      acc,
      {
        timestamp: i * 1000,
        intervalMs: 2000,
        summary: makeSummary(),
        panels: [],
        point: makePoint(i * 1000, i * 100),
      },
      5,
    )
  }
  assert.equal(acc.length, 5)
  assert.deepEqual(
    acc.map((p) => p.t),
    [3000, 4000, 5000, 6000, 7000],
  )
})

test('buildSparklinePath handles empty, single, flat, and multi-point series', () => {
  assert.deepEqual(buildSparklinePath([], 240, 80), { line: '', area: '' })

  const single = buildSparklinePath([50], 240, 80, { min: 0, max: 100 })
  assert.ok(single.line.startsWith('M 0.0 '))
  assert.ok(single.line.includes(' L 240.0 '))
  assert.ok(single.area.endsWith('L 240.0 80.0 L 0.0 80.0 Z'))

  const flatZero = buildSparklinePath([0, 0, 0], 240, 80)
  assert.ok(flatZero.line.includes(' Q '))

  const multi = buildSparklinePath([10, 80, 30, 90], 240, 80, { min: 0, max: 100 })
  assert.ok(multi.line.startsWith('M 0.0 '))
  assert.ok(multi.line.includes(' Q '))
  assert.ok(multi.area.endsWith('L 240.0 80.0 L 0.0 80.0 Z'))
})

test('sortPanelsBySpeed orders online panels first and then by total throughput descending', () => {
  const base: LivePanelStat = {
    id: 1,
    name: 'alpha',
    online: true,
    rttMs: 10,
    cpu: 10,
    cpuCores: 2,
    memUsed: 100,
    memTotal: 1000,
    memPercent: 10,
    diskUsed: 100,
    diskTotal: 1000,
    load1: 0.1,
    load5: 0.1,
    load15: 0.1,
    loadIndex: 10,
    netUp: 100,
    netDown: 200,
    tcpCount: 5,
    udpCount: 1,
    onlineUsers: 1,
    onlineClients: 1,
    xrayState: 'running',
    xrayVersion: '25.1.1',
    uptime: 100,
    sampledAt: 1000,
  }

  const sorted = sortPanelsBySpeed([
    { ...base, id: 1, name: 'slow', netUp: 10, netDown: 20 },
    { ...base, id: 2, name: 'offline-fast', online: false, netUp: 9999, netDown: 9999 },
    { ...base, id: 3, name: 'fast', netUp: 500, netDown: 1000 },
  ])

  assert.deepEqual(
    sorted.map((p) => p.name),
    ['fast', 'slow', 'offline-fast'],
  )
})

test('loadLevelMeta returns expected labels and tones', () => {
  assert.equal(loadLevelMeta('idle').label, '空闲')
  assert.equal(loadLevelMeta('normal').tone, 'good')
  assert.equal(loadLevelMeta('busy').tone, 'warn')
  assert.equal(loadLevelMeta('high').tone, 'bad')
})

test('missing CPU measurements leave gaps in trend lines', () => {
  const path = buildSparklinePath([60, 60, null, 0, 0], 100, 40, { min: 0, max: 100 })
  assert.equal(path.line.split('M ').length - 1, 2)
  assert.match(path.line, /M 75\.0 /)
  assert.equal(buildSparklinePath([null, null], 100, 40).line, '')
})

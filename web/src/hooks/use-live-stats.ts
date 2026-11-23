import { useCallback, useEffect, useState } from 'react'

import type { LiveSnapshot, LiveTrendPoint } from '@/api'
import { subscribeLiveStats } from '@/lib/live-stream'
import { mergeLiveHistory } from '@/lib/live-stats'

const STORAGE_KEY = 'firex.overview.live'
const INTERVAL_STORAGE_KEY = 'firex.overview.liveInterval'

export type LiveInterval = 2 | 5 | 10
export type LiveStatus = 'off' | 'connecting' | 'live' | 'reconnecting' | 'paused'

export interface LiveStatsState {
  enabled: boolean
  setEnabled: (next: boolean) => void
  interval: LiveInterval
  setInterval: (next: LiveInterval) => void
  status: LiveStatus
  snapshot: LiveSnapshot | null
  history: LiveTrendPoint[]
}

function readStoredEnabled(): boolean {
  if (typeof window === 'undefined') return false
  try {
    return window.localStorage.getItem(STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

function readStoredInterval(): LiveInterval {
  if (typeof window === 'undefined') return 2
  try {
    const raw = Number(window.localStorage.getItem(INTERVAL_STORAGE_KEY))
    if (raw === 5 || raw === 10) return raw
  } catch {
    // Ignore storage failures.
  }
  return 2
}

export function useLiveStats(): LiveStatsState {
  const [enabled, setEnabledState] = useState<boolean>(readStoredEnabled)
  const [interval, setIntervalState] = useState<LiveInterval>(readStoredInterval)
  const [visible, setVisible] = useState<boolean>(() =>
    typeof document === 'undefined' ? true : document.visibilityState === 'visible',
  )
  const [status, setStatus] = useState<LiveStatus>(() => (readStoredEnabled() ? 'connecting' : 'off'))
  const [snapshot, setSnapshot] = useState<LiveSnapshot | null>(null)
  const [history, setHistory] = useState<LiveTrendPoint[]>([])

  useEffect(() => {
    if (typeof document === 'undefined') return
    const onVisibilityChange = () => {
      setVisible(document.visibilityState === 'visible')
    }
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => document.removeEventListener('visibilitychange', onVisibilityChange)
  }, [])

  const setEnabled = useCallback((next: boolean) => {
    try {
      window.localStorage.setItem(STORAGE_KEY, String(next))
    } catch {
      // Ignore storage failures (e.g. private browsing restrictions).
    }
    setEnabledState(next)
    if (!next) {
      setStatus('off')
      setSnapshot(null)
      setHistory([])
    } else {
      setStatus(typeof document !== 'undefined' && document.visibilityState !== 'visible' ? 'paused' : 'connecting')
    }
  }, [])

  const setInterval = useCallback((next: LiveInterval) => {
    try {
      window.localStorage.setItem(INTERVAL_STORAGE_KEY, String(next))
    } catch {
      // Ignore storage failures.
    }
    setIntervalState(next)
  }, [])

  useEffect(() => {
    if (!enabled) {
      setStatus('off')
      return
    }
    if (!visible) {
      setStatus('paused')
      return
    }

    setStatus('connecting')
    return subscribeLiveStats(
      (next) => {
        setSnapshot(next)
        setHistory((prev) => mergeLiveHistory(prev, next))
        setStatus('live')
      },
      () => setStatus('reconnecting'),
      interval,
    )
  }, [enabled, visible, interval])

  return {
    enabled,
    setEnabled,
    interval,
    setInterval,
    status,
    snapshot,
    history,
  }
}

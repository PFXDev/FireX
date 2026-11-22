import { useCallback, useEffect, useState } from 'react'

import { verifySession, type LiveSnapshot, type LiveTrendPoint } from '@/api'
import { mergeLiveHistory } from '@/lib/live-stats'

const STORAGE_KEY = 'firex.overview.live'

export type LiveStatus = 'off' | 'connecting' | 'live' | 'reconnecting' | 'paused'

export interface LiveStatsState {
  enabled: boolean
  setEnabled: (next: boolean) => void
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

export function useLiveStats(): LiveStatsState {
  const [enabled, setEnabledState] = useState<boolean>(readStoredEnabled)
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

  useEffect(() => {
    if (!enabled) {
      setStatus('off')
      return
    }
    if (!visible) {
      setStatus('paused')
      return
    }

    let closed = false
    setStatus('connecting')

    const es = new EventSource('/api/overview/live', { withCredentials: true })

    const onSnapshot = (event: Event) => {
      if (closed) return
      const msg = event as MessageEvent<string>
      try {
        const next = JSON.parse(msg.data) as LiveSnapshot
        setSnapshot(next)
        setHistory((prev) => mergeLiveHistory(prev, next))
        setStatus('live')
      } catch {
        // Ignore malformed payloads and wait for the next tick.
      }
    }

    es.addEventListener('snapshot', onSnapshot)
    es.onerror = () => {
      if (closed) return
      setStatus('reconnecting')
      verifySession('/overview/live')
    }

    return () => {
      closed = true
      es.removeEventListener('snapshot', onSnapshot)
      es.close()
    }
  }, [enabled, visible])

  return {
    enabled,
    setEnabled,
    status,
    snapshot,
    history,
  }
}

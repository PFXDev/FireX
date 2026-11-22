import { verifySession, type LiveSnapshot } from '../api.ts'

export function subscribeLiveStats(onSnapshot: (snapshot: LiveSnapshot) => void, onError: () => void) {
  let source: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | undefined
  let delay = 1000
  let stopped = false

  function connect() {
    const es = new EventSource('/api/overview/live', { withCredentials: true })
    source = es
    es.addEventListener('snapshot', (event: MessageEvent<string>) => {
      if (source !== es) return
      let snapshot: LiveSnapshot
      try {
        snapshot = JSON.parse(event.data)
      } catch {
        return
      }
      delay = 1000
      onSnapshot(snapshot)
    })
    es.onerror = async () => {
      if (source !== es) return
      onError()
      // CONNECTING means the browser already owns the next attempt.
      if (es.readyState !== EventSource.CLOSED) return
      es.close()
      source = null
      if (!await verifySession('/overview/live') || stopped) return
      retryTimer = setTimeout(connect, delay)
      delay = Math.min(delay * 2, 30000)
    }
  }

  connect()
  return () => {
    stopped = true
    clearTimeout(retryTimer)
    source?.close()
    source = null
  }
}

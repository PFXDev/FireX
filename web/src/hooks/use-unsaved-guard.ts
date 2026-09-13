import { useEffect } from 'react'

const guards = new Map<symbol, string>()

export function confirmUnsavedNavigation() {
  const message = guards.values().next().value
  return !message || window.confirm(message)
}

/**
 * Asks before the page is closed or another console page is opened while
 * there are edits that have not been saved. The shell checks the same guard
 * before committing any hash navigation, including browser back/forward.
 */
export function useUnsavedGuard(dirty: boolean, message = '有未保存的改动，离开后会丢失。确定离开？') {
  useEffect(() => {
    if (!dirty) return
    const key = Symbol()
    guards.set(key, message)
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => {
      guards.delete(key)
      window.removeEventListener('beforeunload', onBeforeUnload)
    }
  }, [dirty, message])
}

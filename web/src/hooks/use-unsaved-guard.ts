import { useEffect } from 'react'

/**
 * Asks before the page is closed or another console page is opened while
 * there are edits that have not been saved. The console routes by hash, so a
 * sidebar link is a plain anchor; intercepting the click is the only moment
 * left to ask before the page unmounts and the edits go with it.
 */
export function useUnsavedGuard(dirty: boolean, message = '有未保存的改动，离开后会丢失。确定离开？') {
  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    const onClick = (event: MouseEvent) => {
      if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey) return
      const anchor = (event.target as Element | null)?.closest('a[href^="#/"]')
      if (!anchor) return
      if (anchor.getAttribute('href') === window.location.hash) return
      if (!window.confirm(message)) event.preventDefault()
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    document.addEventListener('click', onClick, true)
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload)
      document.removeEventListener('click', onClick, true)
    }
  }, [dirty, message])
}

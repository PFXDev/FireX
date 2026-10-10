import { useEffect, useState } from 'react'

import { confirmUnsavedNavigation } from '@/hooks/use-unsaved-guard'
import { parseRoute, type RouteKey } from '@/lib/navigation'

/**
 * Tracks the console page encoded in the URL hash. Navigation that would
 * discard unsaved edits (links, back/forward, manual edits) is rolled back
 * when the user declines the guard prompt.
 */
export function useHashRoute(): RouteKey {
  const [route, setRoute] = useState<RouteKey>(() => parseRoute(window.location.hash))

  useEffect(() => {
    let acceptedUrl = window.location.href
    let acceptedRoute = parseRoute(window.location.hash)
    const onHashChange = () => {
      const nextRoute = parseRoute(window.location.hash)
      if (nextRoute !== acceptedRoute && !confirmUnsavedNavigation()) {
        window.history.replaceState(window.history.state, '', acceptedUrl)
        return
      }
      acceptedUrl = window.location.href
      acceptedRoute = nextRoute
      setRoute(nextRoute)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  return route
}

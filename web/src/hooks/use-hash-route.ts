import { useSyncExternalStore } from 'react'

import { hasUnsavedGuard, requestUnsavedConfirmation } from '@/hooks/use-unsaved-guard'
import { DEFAULT_ROUTE, parseHashLocation, routeHref, type RouteKey } from '@/lib/navigation'

export type HashLocation = {
  route: RouteKey
  params: URLSearchParams
  hash: string
}

const listeners = new Set<() => void>()

let initialized = false
let acceptedHash = ''
let currentLocation: HashLocation = {
  route: DEFAULT_ROUTE,
  params: new URLSearchParams(),
  hash: routeHref(DEFAULT_ROUTE),
}

function readBrowserHash(): string {
  if (typeof window === 'undefined') return routeHref(DEFAULT_ROUTE)
  return window.location.hash || routeHref(DEFAULT_ROUTE)
}

function commitHash(nextHash: string, updateBrowserUrl: boolean) {
  acceptedHash = nextHash
  currentLocation = parseHashLocation(nextHash)
  if (updateBrowserUrl && typeof window !== 'undefined') {
    window.history.replaceState(window.history.state, '', nextHash)
  }
  listeners.forEach((listener) => listener())
}

function handleExternalHashChange() {
  const nextHash = readBrowserHash()
  if (nextHash === acceptedHash) return
  if (hasUnsavedGuard()) {
    // Keep the current page mounted and restore the URL until the user confirms.
    window.history.replaceState(window.history.state, '', acceptedHash)
    void requestUnsavedConfirmation().then((ok) => {
      if (ok) commitHash(nextHash, true)
    })
    return
  }
  commitHash(nextHash, false)
}

function ensureInit() {
  if (initialized || typeof window === 'undefined') return
  initialized = true
  acceptedHash = readBrowserHash()
  currentLocation = parseHashLocation(acceptedHash)
  window.addEventListener('hashchange', handleExternalHashChange)
  window.addEventListener('popstate', handleExternalHashChange)
}

function subscribe(listener: () => void) {
  ensureInit()
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function getLocationSnapshot(): HashLocation {
  ensureInit()
  return currentLocation
}

function normalizeHash(target: string): string {
  if (target.startsWith('#/')) return target
  if (target.startsWith('#')) return `#/${target.slice(1)}`
  if (target.startsWith('/')) return `#${target}`
  return `#/${target}`
}

/**
 * Navigates to another hash route or query string, honoring any active
 * unsaved-changes guard before updating browser history.
 */
export async function navigateHash(
  target: string,
  options?: { replace?: boolean },
): Promise<boolean> {
  ensureInit()
  const nextHash = normalizeHash(target)
  if (nextHash === acceptedHash) return true
  if (hasUnsavedGuard()) {
    const ok = await requestUnsavedConfirmation()
    if (!ok) return false
  }
  if (options?.replace) {
    commitHash(nextHash, true)
  } else {
    commitHash(nextHash, false)
    window.location.hash = nextHash
  }
  return true
}

/**
 * Silently replaces the URL hash (e.g. syncing a selected tab or bouncing an
 * unauthorized hash) without triggering an unsaved-changes confirmation.
 */
export function replaceHash(target: string) {
  ensureInit()
  const nextHash = normalizeHash(target)
  if (nextHash === acceptedHash) return
  commitHash(nextHash, true)
}

export function useHashLocation(): HashLocation {
  return useSyncExternalStore(subscribe, getLocationSnapshot, getLocationSnapshot)
}

/**
 * Tracks the console page encoded in the URL hash. Navigation that would
 * discard unsaved edits (links, back/forward, manual edits) is rolled back
 * when the user declines the guard prompt.
 */
export function useHashRoute(): RouteKey {
  return useHashLocation().route
}

import { useCallback, useEffect, useSyncExternalStore } from 'react'
import { TriangleAlertIcon } from 'lucide-react'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

const DEFAULT_MESSAGE = '有未保存的改动，离开后会丢失。确定离开？'

const guards = new Map<symbol, string>()

type PromptState = {
  message: string
  resolve: (confirmed: boolean) => void
} | null

let activePrompt: PromptState = null
let activePromise: Promise<boolean> | null = null
const listeners = new Set<() => void>()

function emit() {
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function getSnapshot() {
  return activePrompt
}

function settlePrompt(confirmed: boolean) {
  const current = activePrompt
  activePrompt = null
  activePromise = null
  emit()
  current?.resolve(confirmed)
}

export function hasUnsavedGuard(): boolean {
  return guards.size > 0
}

/**
 * Prompts the user with a non-blocking themed AlertDialog before discarding
 * unsaved edits. Concurrent callers share the active confirmation prompt.
 */
export function requestUnsavedConfirmation(overrideMessage?: string): Promise<boolean> {
  const message = overrideMessage ?? guards.values().next().value
  if (!message) return Promise.resolve(true)
  if (activePromise) return activePromise

  activePromise = new Promise<boolean>((resolve) => {
    activePrompt = { message, resolve }
    emit()
  })
  return activePromise
}

/**
 * Global AlertDialog mounted once at the app shell to handle non-blocking confirmation
 * whenever leaving a dirty page or closing a dirty form dialog.
 */
export function UnsavedChangesDialog() {
  const prompt = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)

  return (
    <AlertDialog
      open={prompt !== null}
      onOpenChange={(open) => {
        if (!open) settlePrompt(false)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <TriangleAlertIcon />
          </AlertDialogMedia>
          <AlertDialogTitle>放弃未保存的改动？</AlertDialogTitle>
          <AlertDialogDescription>{prompt?.message}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => settlePrompt(false)}>继续编辑</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => settlePrompt(true)}>
            放弃改动
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

/**
 * Guards modal dialogs against accidental dismissal via backdrop click or Escape
 * when the form holds unsaved edits.
 */
export function useDirtyDialog(dirty: boolean, busy: boolean, onClose: () => void) {
  return useCallback(
    (open: boolean) => {
      if (open || busy) return
      if (!dirty) {
        onClose()
        return
      }
      void requestUnsavedConfirmation('表单有未保存的改动，关闭后会丢失。确定关闭？').then((ok) => {
        if (ok) onClose()
      })
    },
    [dirty, busy, onClose],
  )
}

/**
 * Asks before the page is closed or another console page is opened while
 * there are edits that have not been saved. The shell checks the same guard
 * before committing any hash navigation, including browser back/forward.
 */
export function useUnsavedGuard(dirty: boolean, message = DEFAULT_MESSAGE) {
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

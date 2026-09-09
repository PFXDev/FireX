import { toast } from 'sonner'

/**
 * Surfaces a panel push that failed after the row itself was saved. The save
 * succeeded, so the caller has already shown its own success toast; this adds
 * the part the operator must not miss — a panel now disagrees with FireX.
 */
export function reportSyncError(syncError: string | undefined | null) {
  if (syncError) toast.error(`已保存，但下发到面板时出错：${syncError}`, { duration: 10000 })
}

import { createContext, useContext } from 'react'

/** Mirrors model.AdminRole*; the server decides, the UI only adapts. */
export type AdminRole = 'super_admin' | 'operator'

/** canViewConfig is always true for a super admin. */
export type Admin = { username: string; role: AdminRole; canViewConfig: boolean }

const SessionContext = createContext<Admin>({ username: '', role: 'operator', canViewConfig: false })

export const SessionProvider = SessionContext.Provider

/**
 * isSuperAdmin gates every write; canViewConfig opens the configuration pages
 * read-only to an operator a super admin has granted it to.
 */
export function useSession(): Admin & { isSuperAdmin: boolean; readOnly: boolean } {
  const admin = useContext(SessionContext)
  const isSuperAdmin = admin.role === 'super_admin'
  return { ...admin, isSuperAdmin, readOnly: !isSuperAdmin }
}

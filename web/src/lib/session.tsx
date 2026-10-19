import { createContext, useContext } from 'react'

/** Mirrors model.AdminRole*; the server decides, the UI only adapts. */
export type AdminRole = 'super_admin' | 'operator'

export type Admin = { username: string; role: AdminRole }

const SessionContext = createContext<Admin>({ username: '', role: 'operator' })

export const SessionProvider = SessionContext.Provider

export function useSession(): Admin & { isSuperAdmin: boolean } {
  const admin = useContext(SessionContext)
  return { ...admin, isSuperAdmin: admin.role === 'super_admin' }
}

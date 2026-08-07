import { createContext, useContext } from 'react'
import type { AuthSessionResponse } from '@/api/generated/types.gen'

export const AuthContext = createContext<AuthSessionResponse | undefined>(undefined)

export function useAuthSession(): AuthSessionResponse {
  const session = useContext(AuthContext)
  if (!session) throw new Error('AuthSession is unavailable outside the protected application')
  return session
}

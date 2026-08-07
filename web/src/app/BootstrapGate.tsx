import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Result, Spin } from 'antd'
import { useEffect, type ReactNode } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { ApiClientError, displayApiError } from '@/api/errors'
import { getAuthSessionOptions, getSetupStatusOptions } from '@/api/generated/@tanstack/react-query.gen'
import { onSessionExpired } from '@/api/session'
import { AuthContext } from './auth'

function FullPageLoading() {
  return <div className="full-page-state"><Spin size="large" /></div>
}

export function BootstrapGate({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const setup = useQuery(getSetupStatusOptions())
  const setupRequired = setup.data?.setupRequired === true
  const session = useQuery({ ...getAuthSessionOptions(), enabled: setup.isSuccess && !setupRequired })

  useEffect(() => onSessionExpired(() => {
    if (location.pathname === '/login') return
    void queryClient.cancelQueries().then(() => {
      queryClient.clear()
      navigate('/login', { replace: true })
    })
  }), [location.pathname, navigate, queryClient])

  if (setup.isPending) return <FullPageLoading />
  if (setup.error) {
    return <Result
      status="error"
      title="无法读取实例状态"
      subTitle={displayApiError(setup.error)}
      extra={<Button onClick={() => void setup.refetch()}>重试</Button>}
    />
  }
  if (setupRequired) {
    return location.pathname === '/setup' ? <>{children}</> : <Navigate to="/setup" replace />
  }
  if (location.pathname === '/setup') return <Navigate to="/login" replace />
  if (session.isPending) return <FullPageLoading />

  const unauthenticated = session.error instanceof ApiClientError && session.error.status === 401
  if (!session.data) {
    if (location.pathname !== '/login') return <Navigate to="/login" replace />
    if (session.error && !unauthenticated) {
      return <Result
        status="warning"
        title="暂时无法确认登录状态"
        subTitle={displayApiError(session.error)}
        extra={<Button onClick={() => void session.refetch()}>重试</Button>}
      />
    }
    return <>{children}</>
  }
  if (location.pathname === '/login') return <Navigate to="/dashboard" replace />
  return <AuthContext.Provider value={session.data}>{children}</AuthContext.Provider>
}

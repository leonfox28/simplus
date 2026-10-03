import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Result, Spin } from 'antd'
import { useEffect, useRef, type ReactNode } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router'
import { ApiClientError, displayApiError } from '@/api/errors'
import { getAuthSessionOptions, getSystemHealthOptions } from '@/api/generated/@tanstack/react-query.gen'
import { onSessionExpired } from '@/api/session'
import { AuthContext } from './auth'

function FullPageLoading() {
  return <div className="full-page-state"><Spin size="large" /></div>
}

export function SessionGate({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const expired = useRef(false)
  const health = useQuery(getSystemHealthOptions())
  const ready = health.data?.installationState === 'ready'
  const session = useQuery({ ...getAuthSessionOptions(), enabled: health.isSuccess && ready })

  useEffect(() => { if (session.data) expired.current = false }, [session.data])

  useEffect(() => onSessionExpired(() => {
    if (expired.current) return
    expired.current = true
    void queryClient.cancelQueries()
    queryClient.clear()
    navigate('/login', { replace: true })
  }), [location.pathname, navigate, queryClient])

  if (health.isPending) return <FullPageLoading />
  if (health.error) {
    return <Result
      status="error"
      title="无法读取实例状态"
      subTitle={displayApiError(health.error)}
      extra={<Button onClick={() => void health.refetch()}>重试</Button>}
    />
  }
  if (!ready) return <Result status="info" title="安装尚未完成" subTitle="请在服务器安装阶段初始化管理员，然后刷新页面。" extra={<Button onClick={() => void health.refetch()}>刷新</Button>} />
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

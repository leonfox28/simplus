import { LogoutOutlined, UserOutlined } from '@ant-design/icons'
import { App as AntdApp, Dropdown } from 'antd'
import React from 'react'
import { getAuthSession, getSetupStatus, logout, type AuthSessionResponse } from './api/client'
import './global.css'

export function rootContainer(container: React.ReactNode) {
  return <AntdApp>{container}</AntdApp>
}

export async function getInitialState(): Promise<{ session?: AuthSessionResponse; setupRequired: boolean }> {
  if (window.location.pathname === '/login') return { setupRequired: false }
  try {
    const [session, setup] = await Promise.all([getAuthSession(), getSetupStatus()])
    if (setup.setupRequired && window.location.pathname !== '/setup') window.location.replace('/setup')
    return { session, setupRequired: setup.setupRequired }
  } catch {
    if (window.location.pathname !== '/login') window.location.replace('/login')
    return { setupRequired: false }
  }
}

export const layout = ({ initialState }: { initialState?: { session?: AuthSessionResponse; setupRequired: boolean } }) => ({
  logo: false,
  title: 'Simplus',
  menuHeaderRender: (_logo: React.ReactNode, title: React.ReactNode) => <>{title}<div className="brand-subtitle">LAN Control Center</div></>,
  avatarProps: {
    icon: <UserOutlined />,
    title: initialState?.session?.username ?? 'simplus_admin',
    render: (_props: unknown, avatar: React.ReactNode) => <Dropdown menu={{ items: [{ key: 'logout', icon: <LogoutOutlined />, label: '退出登录', onClick: async () => { await logout(); window.location.replace('/login') } }] }}>{avatar}</Dropdown>,
  },
  menuItemRender: (item: { path?: string; onClick?: () => void; target?: string; isMobile?: boolean }, dom: React.ReactNode) => {
    if (!item.path) return dom
    return <a href={item.path} target={item.target} onClick={(event) => {
      if (item.isMobile) item.onClick?.()
      if (item.target || item.path === window.location.pathname) return
      event.preventDefault()
      window.history.pushState({}, '', item.path)
      window.dispatchEvent(new PopStateEvent('popstate'))
    }}>{dom}</a>
  },
  onPageChange: () => { if (!initialState?.session && window.location.pathname !== '/login') window.location.replace('/login') },
  contentStyle: { minHeight: 'calc(100vh - 64px)' },
})

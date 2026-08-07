import {
  ApiOutlined,
  BellOutlined,
  BranchesOutlined,
  DashboardOutlined,
  GlobalOutlined,
  MessageOutlined,
  PhoneOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import type { ReactNode } from 'react'

export type NavigationItem = {
  path: string
  label: string
  icon: ReactNode
}

export const navigationItems: NavigationItem[] = [
  { path: '/dashboard', label: '概览', icon: <DashboardOutlined /> },
  { path: '/modems', label: '模组配置', icon: <ApiOutlined /> },
  { path: '/lines', label: '线路配置', icon: <BranchesOutlined /> },
  { path: '/messages', label: '短信', icon: <MessageOutlined /> },
  { path: '/calls', label: '语音通话', icon: <PhoneOutlined /> },
  { path: '/mihomo', label: 'Mihomo 配置', icon: <GlobalOutlined /> },
  { path: '/notifications', label: '通知渠道', icon: <BellOutlined /> },
  { path: '/settings', label: '系统设置', icon: <SettingOutlined /> },
]

export default [
  { path: '/login', component: './Login', layout: false },
  { path: '/setup', component: './Setup', layout: false },
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', name: '概览', icon: 'DashboardOutlined', component: './Dashboard' },
  { path: '/modems', name: '模组配置', icon: 'ApiOutlined', component: './Modems' },
  { path: '/lines', name: '线路配置', icon: 'BranchesOutlined', component: './Lines' },
  { path: '/messages', name: '短信', icon: 'MessageOutlined', component: './Messages' },
  { path: '/calls', name: '语音通话', icon: 'PhoneOutlined', component: './Calls' },
  { path: '/mihomo', name: 'Mihomo 配置', icon: 'GlobalOutlined', component: './Mihomo' },
  { path: '/notifications', name: '通知渠道', icon: 'BellOutlined', component: './Notifications' },
  { path: '/settings', name: '系统设置', icon: 'SettingOutlined', component: './Settings' },
  { path: '*', redirect: '/dashboard' },
]

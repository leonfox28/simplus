import { LogoutOutlined, MenuOutlined, UserOutlined } from '@ant-design/icons'
import { App, Avatar, Button, Drawer, Dropdown, Flex, Grid, Layout, Menu, Typography } from 'antd'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router'
import { logoutMutation } from '@/api/generated/@tanstack/react-query.gen'
import { displayApiError } from '@/api/errors'
import { useAuthSession } from './auth'
import { navigationItems } from './navigation'
import { RealtimeBridge } from './RealtimeBridge'

function Brand() {
  return <div className="app-brand"><Typography.Text strong>Simplus</Typography.Text></div>
}

export function AppShell() {
  const compact = !Grid.useBreakpoint().lg
  const [drawerOpen, setDrawerOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const session = useAuthSession()
  const { message } = App.useApp()
  const logout = useMutation({
    ...logoutMutation(),
    onSuccess: async () => {
      await queryClient.cancelQueries()
      queryClient.clear()
      navigate('/login', { replace: true })
    },
    onError: (error) => { void message.error(displayApiError(error)) },
  })
  const menuItems = navigationItems.map((item) => ({ key: item.path, icon: item.icon, label: item.label }))
  const menu = <Menu
    mode="inline"
    selectedKeys={[location.pathname]}
    items={menuItems}
    onClick={({ key }) => {
      setDrawerOpen(false)
      if (key !== location.pathname) navigate(key)
    }}
  />
  const account = <Dropdown menu={{ items: [{
    key: 'logout',
    icon: <LogoutOutlined />,
    label: '退出登录',
    disabled: logout.isPending,
    onClick: () => logout.mutate({}),
  }] }}>
    <Button type="text" className="account-button" aria-label={`管理员菜单：${session.username}`}>
      <Avatar size="small" icon={<UserOutlined />} />
      {!compact && <span className="account-name">{session.username}</span>}
    </Button>
  </Dropdown>

  return <Layout className="app-layout">
    <RealtimeBridge />
    {!compact && <Layout.Sider className="app-sider" theme="light" width={240}>
      <Brand />
      {menu}
    </Layout.Sider>}
    <Layout className="app-main">
      <Layout.Header className="app-header">
        <Flex className="app-header-inner" align="center" gap="middle" wrap={false}>
          {compact && <Flex className="app-header-leading" align="center" gap="small" wrap={false}>
            <Button aria-label="打开导航" type="text" icon={<MenuOutlined />} onClick={() => setDrawerOpen(true)} />
            <Brand />
          </Flex>}
          <Flex className="app-header-actions" role="group" aria-label="页面操作" align="center" justify="flex-end" gap="small" wrap={false}>
            {account}
          </Flex>
        </Flex>
      </Layout.Header>
      <Layout.Content className="app-content"><Outlet /></Layout.Content>
    </Layout>
    <Drawer
      title={<Brand />}
      placement="left"
      size="small"
      open={compact && drawerOpen}
      onClose={() => setDrawerOpen(false)}
      autoFocus={false}
      styles={{ body: { padding: 0 } }}
    >{menu}</Drawer>
  </Layout>
}

import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App, Grid } from 'antd'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router'
import { AuthContext } from './auth'
import { AppShell } from './AppShell'

class FakeEventSource {
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  addEventListener = vi.fn()
  close = vi.fn()
}

function renderShell() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<App><QueryClientProvider client={queryClient}><AuthContext.Provider value={{ username: 'simplus_admin', locale: 'zh-CN', expiresAt: '2099-01-01T00:00:00Z' }}><MemoryRouter initialEntries={['/dashboard']}><Routes><Route element={<AppShell />}><Route path="/dashboard" element={<div>概览内容</div>} /><Route path="/settings" element={<div>设置内容</div>} /></Route></Routes><Outlet /></MemoryRouter></AuthContext.Provider></QueryClientProvider></App>)
}

describe('AppShell responsive navigation', () => {
  beforeEach(() => vi.stubGlobal('EventSource', FakeEventSource))
  afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

  it('keeps the desktop Sider and navigates declaratively', async () => {
    vi.spyOn(Grid, 'useBreakpoint').mockReturnValue({ lg: true })
    const { container } = renderShell()
    expect(screen.queryByRole('button', { name: '打开导航' })).not.toBeInTheDocument()
    const header = container.querySelector<HTMLElement>('.app-header')
    const headerInner = container.querySelector<HTMLElement>('.app-header-inner')
    const headerActions = screen.getByRole('group', { name: '页面操作' })
    expect(screen.getByText('Simplus')).toBeVisible()
    expect(screen.queryByText('LAN Control Center')).not.toBeInTheDocument()
    expect(header).toContainElement(headerInner)
    expect(headerInner).toContainElement(headerActions)
    expect(headerActions).toHaveClass('app-header-actions')
    expect(headerActions).toContainElement(screen.getByRole('button', { name: '管理员菜单：simplus_admin' }))
    expect(headerInner?.children).toHaveLength(1)
    fireEvent.click(screen.getByText('系统设置'))
    expect(await screen.findByText('设置内容')).toBeInTheDocument()
  })

  it('opens the mobile Drawer and closes it after navigation', async () => {
    vi.spyOn(Grid, 'useBreakpoint').mockReturnValue({ lg: false })
    const { container } = renderShell()
    const headerInner = container.querySelector<HTMLElement>('.app-header-inner')
    const headerActions = screen.getByRole('group', { name: '页面操作' })
    expect(screen.getByText('Simplus')).toBeVisible()
    expect(screen.queryByText('LAN Control Center')).not.toBeInTheDocument()
    expect(headerInner).toContainElement(screen.getByRole('button', { name: '打开导航' }))
    expect(headerInner).toContainElement(headerActions)
    expect(headerActions).toContainElement(screen.getByRole('button', { name: '管理员菜单：simplus_admin' }))
    expect(headerActions.previousElementSibling).toHaveClass('app-header-leading')
    expect(screen.queryByText('simplus_admin')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '打开导航' }))
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toBeVisible()
    expect(within(dialog).getByText('Simplus')).toBeVisible()
    expect(within(dialog).queryByText('LAN Control Center')).not.toBeInTheDocument()
    fireEvent.click(within(dialog).getByText('系统设置'))
    expect(await screen.findByText('设置内容')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })
})

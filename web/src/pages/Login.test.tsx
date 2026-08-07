import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { getSetupStatusQueryKey } from '@/api/generated/@tanstack/react-query.gen'
import type { SetupStatusResponse } from '@/api/generated/types.gen'
import { configureApiClient } from '@/api/setupClient'
import { json } from '@/test/render'
import LoginPage from './Login'

configureApiClient()

function CurrentPath() {
  return <output aria-label="当前路径">{useLocation().pathname}</output>
}

describe('LoginPage password-manager semantics', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders a named autocomplete form and native login field attributes', () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
    const { container } = render(<App><QueryClientProvider client={queryClient}><MemoryRouter><LoginPage /></MemoryRouter></QueryClientProvider></App>)
    const form = container.querySelector('form')
    expect(form).not.toBeNull()
    expect(form).toHaveAttribute('autocomplete', 'on')

    const username = screen.getByPlaceholderText('管理员用户名')
    expect(username).toHaveAttribute('id', 'username')
    expect(username).toHaveAttribute('name', 'username')
    expect(username).toHaveAttribute('type', 'text')
    expect(username).toHaveAttribute('autocomplete', 'username')
    expect(username).toHaveValue('')

    const password = screen.getByPlaceholderText('密码')
    expect(password).toHaveAttribute('id', 'password')
    expect(password).toHaveAttribute('name', 'password')
    expect(password).toHaveAttribute('type', 'password')
    expect(password).toHaveAttribute('autocomplete', 'current-password')
  })

  it.each([
    ['setup is incomplete', true, '/setup'],
    ['setup is complete', false, '/dashboard'],
  ] as const)('uses cached setup status after login when %s', async (_label, setupRequired, expectedPath) => {
    const setup: SetupStatusResponse = {
      installationState: setupRequired ? 'uninitialized' : 'ready',
      phase: setupRequired ? 'bootstrap-required' : 'complete',
      setupRequired,
      businessApiAvailable: !setupRequired,
      bootstrapGenerationAvailable: false,
      supportedFlows: ['create-new'],
    }
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      requests.push(path)
      if (path === '/api/v1/auth/login') {
        return json({ username: 'synthetic_admin', locale: 'zh-CN', expiresAt: '2099-01-01T00:00:00Z' })
      }
      throw new Error(`unexpected ${path}`)
    }))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
    queryClient.setQueryData(getSetupStatusQueryKey(), setup)
    render(<App><QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/login']}>
      <CurrentPath />
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/setup" element={<div>初始化页面</div>} />
        <Route path="/dashboard" element={<div>概览页面</div>} />
      </Routes>
    </MemoryRouter></QueryClientProvider></App>)

    fireEvent.change(screen.getByPlaceholderText('管理员用户名'), { target: { value: 'synthetic_admin' } })
    fireEvent.change(screen.getByPlaceholderText('密码'), { target: { value: 'synthetic-password' } })
    fireEvent.click(screen.getByRole('button', { name: /登\s*录/ }))

    await waitFor(() => expect(screen.getByLabelText('当前路径')).toHaveTextContent(expectedPath))
    expect(requests).toEqual(['/api/v1/auth/login'])
  })
})

import { act, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import {
  getAuthSessionQueryKey,
  getSetupStatusQueryKey,
} from '@/api/generated/@tanstack/react-query.gen'
import { notifySessionExpired } from '@/api/session'
import { configureApiClient } from '@/api/setupClient'
import { json } from '@/test/render'
import SetupPage from '@/pages/Setup'
import { BootstrapGate } from './BootstrapGate'

configureApiClient()

const setupRequired = {
  installationState: 'uninitialized' as const,
  phase: 'bootstrap-required' as const,
  setupRequired: true,
  businessApiAvailable: false,
  bootstrapGenerationAvailable: false,
  supportedFlows: ['create-new' as const],
}

function CurrentPath() {
  return <output aria-label="当前路径">{useLocation().pathname}</output>
}

describe('BootstrapGate session recovery', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('clears private snapshots and replace-navigates after a session expires', async () => {
    const setup = {
      installationState: 'ready' as const,
      phase: 'complete' as const,
      setupRequired: false,
      businessApiAvailable: true,
      bootstrapGenerationAvailable: false,
      supportedFlows: ['create-new' as const],
    }
    const session = {
      username: 'synthetic_admin', locale: 'zh-CN', expiresAt: '2099-01-01T00:00:00Z',
    }
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/setup/status') return json(setup)
      if (path === '/api/v1/auth/session') return json({ code: 'AUTH_SESSION_UNAUTHORIZED', retryable: false }, 401)
      throw new Error(`unexpected ${path}`)
    }))

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
    queryClient.setQueryData(getSetupStatusQueryKey(), setup)
    queryClient.setQueryData(getAuthSessionQueryKey(), session)
    const privateKey = [{ _id: 'listMessages', tags: ['messages'] }] as const
    queryClient.setQueryData(privateKey, { messages: [{ id: 'private-snapshot' }] })

    render(<App><QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/dashboard']}>
      <BootstrapGate><Routes>
        <Route path="/dashboard" element={<div>受保护页面</div>} />
        <Route path="/login" element={<div>登录页面</div>} />
      </Routes></BootstrapGate>
    </MemoryRouter></QueryClientProvider></App>)
    expect(await screen.findByText('受保护页面')).toBeInTheDocument()

    await act(async () => notifySessionExpired())

    expect(await screen.findByText('登录页面')).toBeInTheDocument()
    expect(queryClient.getQueryData(privateKey)).toBeUndefined()
  })

  it('settles an anonymous uninitialized visit on setup without a login redirect loop', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      requests.push(path)
      if (path === '/api/v1/setup/status') return json(setupRequired)
      if (path === '/api/v1/setup/session') return json({ code: 'SETUP_SESSION_UNAUTHORIZED', retryable: false }, 401)
      throw new Error(`unexpected ${path}`)
    }))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<App><QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/']}>
      <CurrentPath />
      <BootstrapGate><Routes>
        <Route path="/" element={<div>根页面</div>} />
        <Route path="/setup" element={<SetupPage />} />
        <Route path="/login" element={<div>登录页面</div>} />
      </Routes></BootstrapGate>
    </MemoryRouter></QueryClientProvider></App>)

    expect(await screen.findByText('初始化授权不可用')).toBeInTheDocument()
    expect(screen.getByLabelText('当前路径')).toHaveTextContent('/setup')
    await new Promise((resolve) => window.setTimeout(resolve, 50))
    expect(requests.filter((path) => path === '/api/v1/setup/status')).toHaveLength(1)
    expect(requests.filter((path) => path === '/api/v1/setup/session')).toHaveLength(1)
    expect(requests).not.toContain('/api/v1/auth/session')
  })
})

import { act, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import {
  getAuthSessionQueryKey,
  getSystemHealthQueryKey,
} from '@/api/generated/@tanstack/react-query.gen'
import { notifySessionExpired } from '@/api/session'
import { configureApiClient } from '@/api/configureClient'
import { json } from '@/test/render'
import { SessionGate } from './SessionGate'

configureApiClient()

describe('SessionGate session recovery', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    window.history.replaceState({}, '', '/')
  })

  it('clears private snapshots and replace-navigates after a session expires', async () => {
    const health = { status: 'ok', version: 'test', apiVersion: 'v1', installationState: 'ready', databaseCount: 1, backend: 'simulator' }
    const session = {
      username: 'synthetic_admin', locale: 'zh-CN', expiresAt: '2099-01-01T00:00:00Z',
    }
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/system/health') return json(health)
      if (path === '/api/v1/auth/session') return json({ code: 'AUTH_SESSION_UNAUTHORIZED', retryable: false }, 401)
      throw new Error(`unexpected ${path}`)
    }))

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
    queryClient.setQueryData(getSystemHealthQueryKey(), health)
    queryClient.setQueryData(getAuthSessionQueryKey(), session)
    const privateKey = [{ _id: 'listMessages', tags: ['messages'] }] as const
    queryClient.setQueryData(privateKey, { messages: [{ id: 'private-snapshot' }] })

    render(<App><QueryClientProvider client={queryClient}><MemoryRouter initialEntries={['/dashboard']}>
      <SessionGate><Routes>
        <Route path="/dashboard" element={<div>受保护页面</div>} />
        <Route path="/login" element={<div>登录页面</div>} />
      </Routes></SessionGate>
    </MemoryRouter></QueryClientProvider></App>)
    expect(await screen.findByText('受保护页面')).toBeInTheDocument()

    await act(async () => notifySessionExpired())

    expect(await screen.findByText('登录页面')).toBeInTheDocument()
    expect(queryClient.getQueryData(privateKey)).toBeUndefined()
  })

})

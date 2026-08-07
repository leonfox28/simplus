import { act, render, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiClientError } from '@/api/errors'

const { getAuthSessionMock } = vi.hoisted(() => ({ getAuthSessionMock: vi.fn() }))
vi.mock('@/api/generated/sdk.gen', () => ({ getAuthSession: getAuthSessionMock }))

import { RealtimeBridge } from './RealtimeBridge'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  close = vi.fn()
  listeners = new Map<string, EventListener>()

  constructor(readonly url: string) {
    FakeEventSource.instances.push(this)
  }

  addEventListener(name: string, listener: EventListenerOrEventListenerObject) {
    this.listeners.set(name, listener as EventListener)
  }

  emit(name: string, payload: unknown, lastEventId = '') {
    this.listeners.get(name)?.({ data: JSON.stringify(payload), lastEventId } as MessageEvent<string>)
  }
}

function apiError(status: number) {
  return new ApiClientError({ kind: 'http', code: `HTTP_${status}`, retryable: false, status })
}

describe('RealtimeBridge lifecycle', () => {
  let visibility: DocumentVisibilityState

  beforeEach(() => {
    visibility = 'visible'
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility })
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource)
    getAuthSessionMock.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('uses named events, deduplicates attention IDs, and reconnects after visibility resumes', async () => {
    const info = vi.fn()
    vi.spyOn(App, 'useApp').mockReturnValue({ message: { info } } as never)
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    const view = render(<QueryClientProvider client={queryClient}><RealtimeBridge /></QueryClientProvider>)
    const first = FakeEventSource.instances[0]!
    expect(first.url).toBe('/api/v1/events')
    expect([...first.listeners.keys()].sort()).toEqual(['resync', 'update'])

    await act(async () => {
      first.emit('update', { topics: ['messages'], attention: 'sms.received' }, 'attention-1')
      first.emit('update', { topics: ['messages'], attention: 'sms.received' }, 'attention-1')
      first.emit('resync', { topics: ['inventory'] })
    })
    expect(invalidate).toHaveBeenCalledTimes(3)
    expect(info).toHaveBeenCalledOnce()

    visibility = 'hidden'
    document.dispatchEvent(new Event('visibilitychange'))
    expect(first.close).toHaveBeenCalledOnce()
    visibility = 'visible'
    document.dispatchEvent(new Event('visibilitychange'))
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(2))
    expect(invalidate).toHaveBeenCalledTimes(4)

    view.unmount()
    expect(FakeEventSource.instances[1]!.close).toHaveBeenCalledOnce()
  })

  it('reconnects after transient session probes but stops after a 401', async () => {
    vi.useFakeTimers()
    vi.spyOn(App, 'useApp').mockReturnValue({ message: { info: vi.fn() } } as never)
    const queryClient = new QueryClient()
    render(<QueryClientProvider client={queryClient}><RealtimeBridge /></QueryClientProvider>)

    getAuthSessionMock.mockResolvedValueOnce({ data: { username: 'synthetic' } })
    FakeEventSource.instances[0]!.onerror?.()
    await act(async () => { await Promise.resolve() })
    await act(async () => { await vi.advanceTimersByTimeAsync(3_000) })
    expect(FakeEventSource.instances).toHaveLength(2)

    getAuthSessionMock.mockRejectedValueOnce(apiError(401))
    FakeEventSource.instances[1]!.onerror?.()
    await act(async () => { await Promise.resolve() })
    await act(async () => { await vi.advanceTimersByTimeAsync(30_000) })
    expect(FakeEventSource.instances).toHaveLength(2)
  })
})

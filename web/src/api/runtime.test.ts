import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getAuthSession, sendMessage } from './generated/sdk.gen'
import { configureApiClient } from './setupClient'
import { onSessionExpired } from './session'
import { runtimeFetch } from './runtime'

describe('API runtime', () => {
  beforeEach(() => {
    configureApiClient()
    document.cookie = 'simplus_csrf=test-token'
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('uses same-origin credentials and adds CSRF only to protected mutations', async () => {
    const fetch = vi.fn(async (request: Request) => new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetch)
    await runtimeFetch('/api/v1/messages', { method: 'POST', body: '{}' })
    const request = fetch.mock.calls[0]?.[0] as Request
    expect(request.credentials).toBe('same-origin')
    expect(request.headers.get('X-Simplus-CSRF')).toBe('test-token')
  })

  it('never adds CSRF to login or setup mutations', async () => {
    const fetch = vi.fn(async (_request: Request) => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetch)
    await runtimeFetch('/api/v1/auth/login', { method: 'POST', body: '{}' })
    await runtimeFetch('/api/v1/setup/bootstrap/consume', { method: 'POST', body: '{}' })
    expect(fetch.mock.calls).toHaveLength(2)
    for (const [request] of fetch.mock.calls) expect(request.headers.has('X-Simplus-CSRF')).toBe(false)
  })

  it('normalizes 401 and notifies the in-memory session boundary', async () => {
    const expired = vi.fn()
    const unsubscribe = onSessionExpired(expired)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 'AUTH_SESSION_UNAUTHORIZED', retryable: false }), { status: 401, headers: { 'Content-Type': 'application/json' } })))
    await expect(getAuthSession({ throwOnError: true })).rejects.toMatchObject({
      kind: 'http', code: 'AUTH_SESSION_UNAUTHORIZED', retryable: false, status: 401,
    })
    expect(expired).toHaveBeenCalledOnce()
    unsubscribe()
  })

  it('keeps a 403 as an operation error without expiring the session', async () => {
    const expired = vi.fn()
    const unsubscribe = onSessionExpired(expired)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      code: 'CSRF_INVALID', retryable: false, reference: 'request-reference',
    }), { status: 403, headers: { 'Content-Type': 'application/json' } })))

    await expect(sendMessage({
      body: {
        operationId: 'operation_0123456789abcdef',
        lineId: 'line_AAAAAAAAAAAAAAAAAAAAAA',
        destination: '+12025550123',
        body: 'synthetic message',
      },
      throwOnError: true,
    })).rejects.toMatchObject({
      kind: 'http', code: 'CSRF_INVALID', retryable: false, status: 403, reference: 'request-reference',
    })
    expect(expired).not.toHaveBeenCalled()
    unsubscribe()
  })

  it('normalizes malformed successful JSON as an invalid response', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ username: 'admin' }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await expect(getAuthSession({ throwOnError: true })).rejects.toMatchObject({
      kind: 'invalid-response', code: 'API_RESPONSE_INVALID', retryable: false,
    })
  })

  it('distinguishes an explicit abort from a network failure', async () => {
    const controller = new AbortController()
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => new Promise<Response>((_resolve, reject) => {
      request.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
    })))
    const pending = runtimeFetch('/api/v1/system/health', { signal: controller.signal })
    controller.abort()
    await expect(pending).rejects.toEqual(expect.objectContaining({ kind: 'aborted', retryable: false }))
  })

  it('normalizes timeouts and network failures separately', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => new Promise<Response>((_resolve, reject) => {
      request.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
    })))
    const timedOut = expect(runtimeFetch('/api/v1/system/health')).rejects.toEqual(expect.objectContaining({
      kind: 'timeout', code: 'API_TIMEOUT', retryable: true,
    }))
    await vi.advanceTimersByTimeAsync(20_001)
    await timedOut

    vi.useRealTimers()
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('network unavailable') }))
    await expect(runtimeFetch('/api/v1/system/health')).rejects.toEqual(expect.objectContaining({
      kind: 'transport', code: 'NETWORK_UNAVAILABLE', retryable: true,
    }))
  })

  it('rejects an invalid generated mutation before fetch', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    await expect(sendMessage({
      body: { operationId: 'short', lineId: 'invalid', destination: 'not-a-number', body: '' },
      throwOnError: true,
    })).rejects.toEqual(expect.objectContaining({ kind: 'http', code: 'REQUEST_INVALID', retryable: false }))
    expect(fetch).not.toHaveBeenCalled()
  })
})

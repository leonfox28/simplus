import type { CreateClientConfig } from './generated/client.gen'
import { ApiClientError } from './errors'

const mutatingMethods = new Set(['DELETE', 'PATCH', 'POST', 'PUT'])

function cookieValue(name: string): string {
  if (typeof document === 'undefined') return ''
  const prefix = `${encodeURIComponent(name)}=`
  for (const item of document.cookie.split(';')) {
    const value = item.trim()
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length))
  }
  return ''
}

function requestTimeout(request: Request): number {
  const path = new URL(request.url).pathname
  if (path === '/api/v1/messages' && request.method === 'POST') return 135_000
  if (path === '/api/v1/mihomo/core/install') return 125_000
  if (path === '/api/v1/mihomo/config' && request.method === 'POST') return 35_000
  if (path === '/api/v1/mihomo/subscriptions' && request.method === 'POST') return 50_000
  if (path.startsWith('/api/v1/mihomo/subscriptions/') && request.method === 'PUT') return 50_000
  if (path.startsWith('/api/v1/mihomo/subscriptions/') && path.endsWith('/refresh')) return 50_000
  return 20_000
}

export async function runtimeFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const normalizedInput = typeof input === 'string' && input.startsWith('/')
    ? new URL(input, globalThis.location?.origin ?? 'http://localhost')
    : input
  const source = new Request(normalizedInput, init)
  const headers = new Headers(source.headers)
  headers.set('Accept', 'application/json')
  const path = new URL(source.url).pathname
  if (
    mutatingMethods.has(source.method) &&
    path !== '/api/v1/auth/login' &&
    !path.startsWith('/api/v1/setup/')
  ) {
    const csrf = cookieValue('simplus_csrf')
    if (csrf) headers.set('X-Simplus-CSRF', csrf)
  }

  const controller = new AbortController()
  let timedOut = false
  const abortFromSource = () => controller.abort(source.signal.reason)
  if (source.signal.aborted) abortFromSource()
  else source.signal.addEventListener('abort', abortFromSource, { once: true })
  const timeout = globalThis.setTimeout(() => {
    timedOut = true
    controller.abort()
  }, requestTimeout(source))

  try {
    return await globalThis.fetch(new Request(source, {
      credentials: 'same-origin',
      headers,
      signal: controller.signal,
    }))
  } catch (error) {
    if (timedOut) {
      throw new ApiClientError({ kind: 'timeout', code: 'API_TIMEOUT', retryable: true })
    }
    if (controller.signal.aborted || (error instanceof Error && error.name === 'AbortError')) {
      throw new ApiClientError({ kind: 'aborted', code: 'REQUEST_ABORTED', retryable: false })
    }
    throw new ApiClientError({ kind: 'transport', code: 'NETWORK_UNAVAILABLE', retryable: true })
  } finally {
    globalThis.clearTimeout(timeout)
    source.signal.removeEventListener('abort', abortFromSource)
  }
}

export const createClientConfig: CreateClientConfig = (config) => ({
  ...config,
  baseUrl: globalThis.location?.origin ?? 'http://localhost',
  credentials: 'same-origin',
  fetch: runtimeFetch,
})

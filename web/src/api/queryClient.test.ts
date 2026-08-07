import { describe, expect, it } from 'vitest'
import { ApiClientError } from './errors'
import { createAppQueryClient, shouldRetryQuery } from './queryClient'

describe('QueryClient policy', () => {
  it('retries only bounded retryable query failures', () => {
    const retryable = new ApiClientError({ kind: 'transport', code: 'NETWORK_UNAVAILABLE', retryable: true })
    expect(shouldRetryQuery(0, retryable)).toBe(true)
    expect(shouldRetryQuery(1, retryable)).toBe(true)
    expect(shouldRetryQuery(2, retryable)).toBe(false)
    expect(shouldRetryQuery(0, new ApiClientError({ kind: 'http', code: 'FORBIDDEN', retryable: false, status: 403 }))).toBe(false)
  })

  it('never enables automatic mutation retries', () => {
    expect(createAppQueryClient().getDefaultOptions().mutations?.retry).toBe(false)
  })
})

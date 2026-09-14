import { describe, expect, it } from 'vitest'
import { ApiClientError, displayApiError } from './errors'

describe('API error display', () => {
  it('explains a Mihomo subscription refresh failure without blaming the management service', () => {
    const error = new ApiClientError({
      kind: 'http',
      code: 'MIHOMO_SUBSCRIPTION_REFRESH_FAILED',
      retryable: true,
      status: 502,
    })

    expect(displayApiError(error)).toBe('订阅源拒绝访问或返回了无法使用的内容，请检查订阅是否有效后重试。')
  })
})

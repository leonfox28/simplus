import { ZodError } from 'zod'
import { ApiClientError, isApiError } from './errors'
import { client } from './generated/client.gen'
import { notifySessionExpired } from './session'

let configured = false

export function configureApiClient() {
  if (configured) return
  configured = true
  client.interceptors.error.use((error, response, request) => {
    if (error instanceof ApiClientError) return error
    if (response && !response.ok) {
      const apiError = isApiError(error) ? error : undefined
      const normalized = new ApiClientError({
        kind: 'http',
        code: apiError?.code ?? `HTTP_${response.status}`,
        retryable: apiError?.retryable ?? response.status >= 500,
        status: response.status,
        reference: apiError?.reference,
      })
      const path = request ? new URL(request.url).pathname : ''
      // Setup authorization uses a separate HttpOnly cookie, while a rejected
      // login is not an expired administrator session. Their expected 401s
      // must not clear private state or trigger route recovery.
      const expectedUnauthorized = path.startsWith('/api/v1/setup/') || path === '/api/v1/auth/login'
      if (response.status === 401 && !expectedUnauthorized) notifySessionExpired()
      return normalized
    }
    if (error instanceof ZodError && !response) {
      return new ApiClientError({ kind: 'http', code: 'REQUEST_INVALID', retryable: false })
    }
    if (response?.ok || error instanceof SyntaxError || error instanceof ZodError) {
      return new ApiClientError({
        kind: 'invalid-response',
        code: 'API_RESPONSE_INVALID',
        retryable: false,
        status: response?.status,
      })
    }
    return new ApiClientError({ kind: 'http', code: 'REQUEST_INVALID', retryable: false })
  })
}

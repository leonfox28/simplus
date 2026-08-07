import { ZodError } from 'zod'
import { ApiClientError, isApiError } from './errors'
import { client } from './generated/client.gen'
import { notifySessionExpired } from './session'

let configured = false

export function configureApiClient() {
  if (configured) return
  configured = true
  client.interceptors.error.use((error, response) => {
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
      if (response.status === 401) notifySessionExpired()
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

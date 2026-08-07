import { QueryClient } from '@tanstack/react-query'
import { ApiClientError } from './errors'

export function shouldRetryQuery(failureCount: number, error: unknown): boolean {
  return failureCount < 2 && error instanceof ApiClientError && error.retryable
}

export function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        gcTime: 5 * 60_000,
        staleTime: 10_000,
        refetchOnWindowFocus: true,
        retry: shouldRetryQuery,
        retryDelay: (attempt) => Math.min(500 * 2 ** attempt, 2_000),
      },
      mutations: {
        retry: false,
      },
    },
  })
}

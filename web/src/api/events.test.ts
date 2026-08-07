import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { decodeRealtimeEvent, invalidateRealtimeTopics } from './events'

describe('realtime event boundary', () => {
  it('accepts only the bounded generated shape with unique topics', () => {
    expect(decodeRealtimeEvent('{"topics":["messages"],"attention":"sms.received"}')).toEqual({ topics: ['messages'], attention: 'sms.received' })
    expect(decodeRealtimeEvent('{"topics":["messages","messages"]}')).toBeUndefined()
    expect(decodeRealtimeEvent('{"topics":["messages"],"body":"private"}')).toBeUndefined()
    expect(decodeRealtimeEvent('not-json')).toBeUndefined()
  })

  it('marks matching inactive queries stale without refetching hidden pages', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const fetchMessages = vi.fn(async () => ({ messages: [] }))
    const fetchCalls = vi.fn(async () => ({ calls: [] }))
    await queryClient.fetchQuery({ queryKey: [{ _id: 'listMessages', tags: ['messages'] }], queryFn: fetchMessages })
    await queryClient.fetchQuery({ queryKey: [{ _id: 'listCalls', tags: ['calls'] }], queryFn: fetchCalls })
    fetchMessages.mockClear()
    fetchCalls.mockClear()
    await invalidateRealtimeTopics(queryClient, ['messages'])
    expect(fetchMessages).not.toHaveBeenCalled()
    expect(fetchCalls).not.toHaveBeenCalled()
    expect(queryClient.getQueryState([{ _id: 'listMessages', tags: ['messages'] }])?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState([{ _id: 'listCalls', tags: ['calls'] }])?.isInvalidated).toBe(false)
  })
})

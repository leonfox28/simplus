import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { listCallsInfiniteQueryKey } from '@/api/generated/@tanstack/react-query.gen'
import { json, renderPage, testLine } from '@/test/render'
import Calls from './Calls'

function call(id: string, state: 'incoming' | 'ended', createdAt: string) {
  return { id, operationId: `operation_${id}`, lineId: testLine.id, remoteAddress: '+12025550123', direction: 'inbound', state, endReason: '', createdAt, updatedAt: createdAt }
}

describe('Calls cursor history and controls', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads another page and sends a typed answer action', async () => {
    const requests: Request[] = []
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      requests.push(request)
      const url = new URL(request.url)
      if (url.pathname === '/api/v1/lines') return json({ lines: [testLine] })
      if (url.pathname === '/api/v1/calls' && request.method === 'GET') return url.searchParams.get('cursor')
        ? json({ calls: [call('call_second_12345678901', 'ended', '2026-08-06T00:00:00Z')] })
        : json({ calls: [call('call_first_123456789012', 'incoming', '2026-08-07T00:00:00Z')], nextCursor: 'cursor_next' })
      if (url.pathname.endsWith('/action') && request.method === 'POST') return json({ ...call('call_first_123456789012', 'ended', '2026-08-07T00:00:00Z'), endReason: 'answered' })
      throw new Error(`unexpected ${request.method} ${url.pathname}`)
    }))
    const { queryClient } = renderPage(<Calls />)
    const otherPageSizeKey = listCallsInfiniteQueryKey({ query: { limit: 50 } })
    queryClient.setQueryData(otherPageSizeKey, { pages: [], pageParams: [] })
    fireEvent.click(await screen.findByRole('button', { name: '接听' }))
    await waitFor(() => expect(requests.some((request) => request.method === 'POST' && new URL(request.url).pathname.endsWith('/action'))).toBe(true))
    await waitFor(() => expect(queryClient.getQueryState(otherPageSizeKey)?.isInvalidated).toBe(true))
    const action = requests.find((request) => request.method === 'POST' && new URL(request.url).pathname.endsWith('/action'))
    expect(await action?.clone().json()).toEqual({ action: 'answer' })
    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))
    await waitFor(() => expect(requests.some((request) => new URL(request.url).searchParams.get('cursor') === 'cursor_next')).toBe(true))
  })
})

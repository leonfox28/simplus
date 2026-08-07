import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { listMessagesInfiniteQueryKey } from '@/api/generated/@tanstack/react-query.gen'
import { json, renderPage, testLine } from '@/test/render'
import Messages from './Messages'

function message(id: string, body: string, createdAt: string) {
  return { id, operationId: `operation_${id}`, direction: 'inbound', lineId: testLine.id, remoteAddress: '+12025550123', body, status: 'received', providerMessageId: '', errorCode: '', createdAt, updatedAt: createdAt }
}

describe('Messages cursor history', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads the next cursor and keeps deletion explicit', async () => {
    const requests: Request[] = []
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      requests.push(request)
      const url = new URL(request.url)
      if (url.pathname === '/api/v1/lines') return json({ lines: [testLine] })
      if (url.pathname === '/api/v1/contacts') return json({ contacts: [] })
      if (url.pathname === '/api/v1/messages' && request.method === 'GET') {
        return url.searchParams.get('cursor')
          ? json({ messages: [message('message_second_1234', '第二页', '2026-08-06T00:00:00Z')], totalCount: 2, capacity: 1000, nearCapacity: false })
          : json({ messages: [message('message_first_12345', '第一页', '2026-08-07T00:00:00Z')], totalCount: 2, capacity: 1000, nearCapacity: false, nextCursor: 'cursor_next' })
      }
      if (url.pathname.startsWith('/api/v1/messages/') && request.method === 'DELETE') return json(null, 204)
      throw new Error(`unexpected ${request.method} ${url.pathname}`)
    }))
    const { queryClient } = renderPage(<Messages />)
    const otherHistoryKey = listMessagesInfiniteQueryKey({ query: {
      limit: 20, lineId: testLine.id, remoteAddress: '+12025550999',
    } })
    queryClient.setQueryData(otherHistoryKey, { pages: [], pageParams: [] })
    expect(await screen.findByText('第一页')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '加载更多' }))
    expect(await screen.findByText('第二页')).toBeInTheDocument()
    const deletes = screen.getAllByRole('button', { name: /删除/ })
    fireEvent.click(deletes[0]!)
    fireEvent.click(await screen.findByRole('button', { name: /^(OK|确 定)$/ }))
    await waitFor(() => expect(requests.some((request) => request.method === 'DELETE')).toBe(true))
    await waitFor(() => expect(queryClient.getQueryState(otherHistoryKey)?.isInvalidated).toBe(true))
  })
})

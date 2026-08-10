import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { json, renderPage, testLine } from '@/test/render'
import Messages from './Messages'

const secondLine = { ...testLine, id: 'line_BBBBBBBBBBBBBBBBBBBBBB', displayName: '备用线路' }
const unavailableLine = { ...testLine, state: 'modem-offline' as const }
const remoteAddress = '+12025550123'

function sms(overrides: Record<string, unknown> = {}) {
  return {
    id: 'message_first_12345',
    operationId: 'operation_message_first_12345',
    direction: 'inbound',
    lineId: testLine.id,
    remoteAddress,
    body: '第一条合成短信',
    status: 'received',
    providerMessageId: 'provider-message-first',
    errorCode: '',
    createdAt: '2026-08-10T08:00:00Z',
    updatedAt: '2026-08-10T08:00:00Z',
    ...overrides,
  }
}

function conversation(lastMessage = sms(), unreadCount = 1) {
  return { remoteAddress, lastMessage, unreadCount, lastOutboundLineId: secondLine.id }
}

function installMatchMedia(matches: boolean) {
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })))
}

function installApi(requests: Request[], options?: {
  historyCursor?: boolean
  alpha?: boolean
  deletedLine?: boolean
  allIneligible?: boolean
  unavailableRecent?: boolean
  historyError?: boolean
  orderingRegression?: boolean
}) {
  const address = options?.alpha ? 'Service_Notice' : remoteAddress
  vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
    requests.push(request)
    const url = new URL(request.url)
    if (url.pathname === '/api/v1/lines') {
      return json({ lines: options?.allIneligible ? [unavailableLine] : options?.unavailableRecent ? [unavailableLine, secondLine] : options?.deletedLine ? [testLine] : [testLine, secondLine] })
    }
    const contact = {
      id: 'contact_AAAAAAAAAAAAAAAAAAAAAA', displayName: '测试联系人', phoneNumber: remoteAddress,
      createdAt: '2026-08-10T00:00:00Z', updatedAt: '2026-08-10T00:00:00Z',
    }
    if (url.pathname === '/api/v1/contacts' && request.method === 'GET') return json({ contacts: [contact] })
    if (url.pathname === '/api/v1/contacts' && request.method === 'POST') return json(contact, 201)
    if (url.pathname === `/api/v1/contacts/${contact.id}` && request.method === 'PUT') return json(contact)
    if (url.pathname === `/api/v1/contacts/${contact.id}` && request.method === 'DELETE') return json(null, 204)
    if (url.pathname === '/api/v1/message-conversations') {
      const latest = sms(options?.orderingRegression ? {
        id: 'message_inbound_later_record', direction: 'inbound', body: '后持久化的入站',
        createdAt: '2026-08-10T08:00:00.000Z', updatedAt: '2026-08-10T08:00:01.500Z',
      } : options?.deletedLine ? {
        remoteAddress: address, direction: 'outbound', lineId: secondLine.id, body: '失败后重新编辑',
        status: 'failed', errorCode: 'SMS_TRANSPORT_FAILED', providerMessageId: '',
      } : { remoteAddress: address })
      return json({
        conversations: [{
          ...conversation(latest),
          remoteAddress: address,
          lastOutboundLineId: options?.allIneligible ? undefined : options?.unavailableRecent ? testLine.id : secondLine.id,
        }],
        conversationTotalCount: 1,
        messageTotalCount: options?.historyCursor ? 2 : 1,
        capacity: 10000,
        nearCapacity: false,
      })
    }
    if (url.pathname === '/api/v1/messages' && request.method === 'POST') {
      const body = await request.clone().json() as { operationId: string; lineId: string; destination: string; body: string }
      return json(sms({
        id: 'message_sent_1234567', operationId: body.operationId, direction: 'outbound', lineId: body.lineId,
        remoteAddress: body.destination, body: body.body, status: 'sent', providerMessageId: 'provider-message-sent',
        createdAt: '2026-08-10T09:00:00Z', updatedAt: '2026-08-10T09:00:00Z', sentAt: '2026-08-10T09:00:00Z',
      }), 201)
    }
    if (url.pathname === '/api/v1/messages' && request.method === 'GET') {
      if (options?.historyError) return json({ code: 'MESSAGE_HISTORY_UNAVAILABLE', retryable: true }, 500)
      if (url.searchParams.get('cursor')) {
        return json({ messages: [sms({ id: 'message_older_123456', body: '更早短信', createdAt: '2026-08-09T08:00:00Z', remoteAddress: address })], totalCount: 2, capacity: 10000, nearCapacity: false })
      }
      return json({
        messages: options?.orderingRegression ? [
          sms({
            id: 'message_inbound_later_record', direction: 'inbound', body: '后持久化的入站',
            createdAt: '2026-08-10T08:00:00.000Z', updatedAt: '2026-08-10T08:00:01.500Z',
          }),
          sms({
            id: 'message_outbound_first_record', operationId: 'operation_outbound_first', direction: 'outbound',
            body: '先持久化的出站', status: 'sent', providerMessageId: 'provider-outbound-first',
            createdAt: '2026-08-10T08:00:00.100Z', updatedAt: '2026-08-10T08:00:01.000Z', sentAt: '2026-08-10T08:00:01.000Z',
          }),
        ] : [sms(options?.deletedLine ? {
          remoteAddress: address, direction: 'outbound', lineId: secondLine.id, body: '失败后重新编辑',
          status: 'failed', errorCode: 'SMS_TRANSPORT_FAILED', providerMessageId: '',
        } : { remoteAddress: address })],
        totalCount: options?.historyCursor ? 2 : 1,
        capacity: 10000,
        nearCapacity: false,
        nextCursor: options?.historyCursor ? 'cursor_next' : undefined,
        readThroughToken: 'read_token_1234567890',
      })
    }
    if (url.pathname === '/api/v1/message-conversations/read-state' && request.method === 'PUT') return json(null, 204)
    if (url.pathname.startsWith('/api/v1/messages/') && request.method === 'DELETE') return json(null, 204)
    throw new Error(`unexpected ${request.method} ${url.pathname}`)
  }))
}

describe('Messages conversations', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    Reflect.deleteProperty(document, 'visibilityState')
  })

  it('renders the desktop two-pane recipient conversation and marks only the visible snapshot read', async () => {
    installMatchMedia(true)
    const requests: Request[] = []
    installApi(requests)
    renderPage(<Messages />)

    expect((await screen.findAllByText('测试联系人')).length).toBeGreaterThanOrEqual(1)
    expect(await screen.findByText('第一条合成短信')).toBeInTheDocument()
    expect(screen.getByText('测试线路')).toBeInTheDocument()
    expect(screen.getAllByText(remoteAddress).length).toBeGreaterThanOrEqual(1)
    await waitFor(() => expect(requests.some((request) => new URL(request.url).pathname === '/api/v1/message-conversations/read-state')).toBe(true))

    const readRequest = requests.find((request) => new URL(request.url).pathname === '/api/v1/message-conversations/read-state')
    expect(readRequest?.method).toBe('PUT')
    await expect(readRequest?.clone().json()).resolves.toEqual({ remoteAddress, readThroughToken: 'read_token_1234567890' })
  })

  it('renders server record order when provider business time runs backward', async () => {
    installMatchMedia(true)
    const requests: Request[] = []
    installApi(requests, { orderingRegression: true })
    renderPage(<Messages />)

    expect(await screen.findByText('后持久化的入站', { selector: '.conversation-preview' })).toBeInTheDocument()
    const history = screen.getByLabelText('短信记录')
    await waitFor(() => expect(history.querySelectorAll('.message-row')).toHaveLength(2))
    const bubbles = Array.from(history.querySelectorAll('.message-row')).map((row) => row.textContent)
    expect(bubbles).toHaveLength(2)
    expect(bubbles[0]).toContain('先持久化的出站')
    expect(bubbles[1]).toContain('后持久化的入站')
  })

  it('uses mobile list-detail-back navigation, loads older history, and keeps deletion behind the more menu and confirmation', async () => {
    installMatchMedia(false)
    const requests: Request[] = []
    installApi(requests, { historyCursor: true })
    renderPage(<Messages />)

    const conversationButton = await screen.findByRole('button', { name: /测试联系人/ })
    expect(screen.queryByLabelText('短信记录')).not.toBeInTheDocument()
    expect(requests.some((request) => new URL(request.url).pathname === '/api/v1/message-conversations/read-state')).toBe(false)
    fireEvent.click(conversationButton)
    expect(await screen.findByLabelText('短信记录')).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: '加载更早短信' }))
    expect(await screen.findByText('更早短信')).toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('button', { name: '短信更多操作' })[0]!)
    fireEvent.click(await screen.findByText('删除'))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: /删.*除/ }))
    await waitFor(() => expect(requests.some((request) => request.method === 'DELETE')).toBe(true))

    fireEvent.click(screen.getByRole('button', { name: '返回会话列表' }))
    expect(await screen.findByRole('button', { name: /测试联系人/ })).toBeInTheDocument()
    expect(screen.queryByLabelText('短信记录')).not.toBeInTheDocument()
  })

  it('keeps an alphanumeric service-address conversation readable but disables replies', async () => {
    installMatchMedia(false)
    const requests: Request[] = []
    installApi(requests, { alpha: true })
    renderPage(<Messages />)

    fireEvent.click(await screen.findByRole('button', { name: /Service_Notice/ }))
    expect(await screen.findByText('该服务地址不是数字号码，只能查看历史，无法回复。')).toBeInTheDocument()
    expect(screen.getByText('发送短信').closest('button')).toBeDisabled()
    expect(requests.some((request) => request.method === 'POST' && new URL(request.url).pathname === '/api/v1/messages')).toBe(false)
  })

  it('keeps a deleted historical Line selected and only refills failed messages without resending', async () => {
    installMatchMedia(false)
    const requests: Request[] = []
    installApi(requests, { deletedLine: true })
    renderPage(<Messages />)

    fireEvent.click(await screen.findByRole('button', { name: /测试联系人/ }))
    expect(await screen.findByText('最近使用的历史 Line 已删除，请明确选择其他 Line。')).toBeInTheDocument()
    expect(screen.getAllByText('历史线路（已删除）').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('发送短信').closest('button')).toBeDisabled()
    fireEvent.click(await screen.findByRole('button', { name: '短信更多操作' }))
    fireEvent.click(await screen.findByText('重新编辑'))
    expect(screen.getByLabelText('短信内容')).toHaveValue('失败后重新编辑')
    expect(requests.some((request) => request.method === 'POST' && new URL(request.url).pathname === '/api/v1/messages')).toBe(false)
  })

  it('explains the fail-closed state when Lines exist but none can send', async () => {
    installMatchMedia(false)
    const requests: Request[] = []
    installApi(requests, { allIneligible: true })
    renderPage(<Messages />)

    fireEvent.click(await screen.findByRole('button', { name: /测试联系人/ }))
    expect(await screen.findByText('当前没有可发送的 Line。')).toBeInTheDocument()
    expect(screen.getByText('发送短信').closest('button')).toBeDisabled()
  })

  it('keeps an unavailable recent Line selected until the administrator explicitly switches', async () => {
    installMatchMedia(false)
    const requests: Request[] = []
    installApi(requests, { unavailableRecent: true })
    renderPage(<Messages />)

    fireEvent.click(await screen.findByRole('button', { name: /测试联系人/ }))
    expect(await screen.findByText('所选 Line 当前不可发送：模组离线')).toBeInTheDocument()
    expect(screen.getByText('发送短信').closest('button')).toBeDisabled()
    fireEvent.mouseDown(screen.getByLabelText('发送 Line'))
    fireEvent.click(await screen.findByTitle('备用线路'))
    await waitFor(() => expect(screen.getByText('发送短信').closest('button')).toBeEnabled())
    expect(screen.queryByText('所选 Line 当前不可发送：模组离线')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('短信内容'), { target: { value: '明确切换后的短信' } })
    fireEvent.click(screen.getByText('发送短信'))
    await waitFor(() => expect(requests.some((request) => request.method === 'POST' && new URL(request.url).pathname === '/api/v1/messages')).toBe(true))
    const sendRequest = requests.find((request) => request.method === 'POST' && new URL(request.url).pathname === '/api/v1/messages')
    await expect(sendRequest?.clone().json()).resolves.toEqual(expect.objectContaining({
      lineId: secondLine.id, destination: remoteAddress, body: '明确切换后的短信',
    }))
  })

  it('does not mark a hidden or failed history snapshot read', async () => {
    installMatchMedia(true)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    const hiddenRequests: Request[] = []
    installApi(hiddenRequests)
    const hiddenPage = renderPage(<Messages />)
    expect(await screen.findByText('第一条合成短信')).toBeInTheDocument()
    await waitFor(() => expect(hiddenRequests.some((request) => new URL(request.url).pathname === '/api/v1/messages')).toBe(true))
    expect(hiddenRequests.some((request) => new URL(request.url).pathname === '/api/v1/message-conversations/read-state')).toBe(false)
    hiddenPage.unmount()

    Reflect.deleteProperty(document, 'visibilityState')
    const errorRequests: Request[] = []
    installApi(errorRequests, { historyError: true })
    renderPage(<Messages />)
    expect(await screen.findByText('管理服务暂时无法完成该操作。')).toBeInTheDocument()
    expect(errorRequests.some((request) => new URL(request.url).pathname === '/api/v1/message-conversations/read-state')).toBe(false)
  })

})

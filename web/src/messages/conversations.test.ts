import { describe, expect, it } from 'vitest'
import type { SMSMessage } from '../api/client'
import { buildSMSConversations } from './conversations'

function message(overrides: Partial<SMSMessage>): SMSMessage {
  return {
    id: 'msg_0123456789abcdef012345',
    operationId: 'operation-0123456789abcdef',
    direction: 'outbound',
    lineId: 'line-1',
    remoteAddress: '13800138000',
    body: 'hello',
    status: 'sent',
    providerMessageId: 'provider-1',
    errorCode: '',
    createdAt: '2026-08-03T12:00:00Z',
    updatedAt: '2026-08-03T12:00:01Z',
    sentAt: '2026-08-03T12:00:01Z',
    ...overrides,
  }
}

describe('SMS conversation grouping', () => {
  it('groups by line and remote address while ordering messages oldest first', () => {
    const outbound = message({ body: 'second', createdAt: '2026-08-03T12:02:00Z', updatedAt: '2026-08-03T12:02:01Z' })
    const inbound = message({
      id: 'msg_inbound012345678901234',
      operationId: 'inbound-operation-01234567',
      direction: 'inbound',
      body: 'first',
      status: 'received',
      providerMessageId: 'agent-message-1',
      createdAt: '2026-08-03T12:01:00Z',
      updatedAt: '2026-08-03T12:01:01Z',
      sentAt: undefined,
    })

    const conversations = buildSMSConversations([outbound, inbound])

    expect(conversations).toHaveLength(1)
    expect(conversations[0]?.messages.map((item) => item.body)).toEqual(['first', 'second'])
    expect(conversations[0]?.lastMessage.body).toBe('second')
  })

  it('keeps the same address on different lines in separate conversations', () => {
    const older = message({ lineId: 'line-1', updatedAt: '2026-08-03T12:00:01Z' })
    const newer = message({
      id: 'msg_9876543210abcdef012345',
      operationId: 'operation-fedcba9876543210',
      lineId: 'line-2',
      updatedAt: '2026-08-03T12:05:01Z',
    })

    const conversations = buildSMSConversations([older, newer])

    expect(conversations).toHaveLength(2)
    expect(conversations.map((item) => item.lineId)).toEqual(['line-2', 'line-1'])
  })
})

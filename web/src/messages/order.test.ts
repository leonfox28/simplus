import { describe, expect, it } from 'vitest'
import type { SmsMessage as SMSMessage } from '@/api/generated/types.gen'
import { sortSMSMessagesForDisplay } from './order'

function message(overrides: Partial<SMSMessage>): SMSMessage {
  return {
    id: 'msg_0123456789abcdef012345',
    operationId: 'operation-0123456789abcdef',
    direction: 'outbound',
    lineId: 'simulator-line-1',
    remoteAddress: '+8613800138000',
    body: 'test',
    status: 'sent',
    providerMessageId: 'provider-0123456789',
    errorCode: '',
    createdAt: '2026-08-05T20:16:26.295Z',
    updatedAt: '2026-08-05T20:16:27.795Z',
    sentAt: '2026-08-05T20:16:27.795Z',
    ...overrides,
  }
}

describe('SMS display ordering', () => {
  it('matches the server keyset order by exact created time and stable id', () => {
    const outbound = message({})
    const inbound = message({
      id: 'msg_inbound012345678901234',
      operationId: 'inbound-operation-01234567',
      direction: 'inbound',
      status: 'received',
      createdAt: '2026-08-05T20:16:26.000Z',
      updatedAt: '2026-08-05T20:16:33.102Z',
      sentAt: undefined,
    })
    const older = message({
      id: 'msg_older01234567890123456',
      operationId: 'operation-older0123456789',
      createdAt: '2026-08-05T20:16:25.999Z',
      updatedAt: '2026-08-05T20:17:00.000Z',
    })

    expect(sortSMSMessagesForDisplay([outbound, older, inbound]).map((item) => item.id)).toEqual([
      outbound.id,
      inbound.id,
      older.id,
    ])
  })

  it('does not mutate the API response array', () => {
    const original = [message({}), message({ id: 'msg_second0123456789012345' })]
    const sorted = sortSMSMessagesForDisplay(original)
    expect(sorted).not.toBe(original)
    expect(original[0]?.id).toBe('msg_0123456789abcdef012345')
  })

  it('uses SQLite BINARY order for mixed-case and punctuation IDs at the same timestamp', () => {
    const createdAt = '2026-08-05T20:16:26.295Z'
    const ids = [
      'msg_A0000000000000000000',
      'msg__0000000000000000000',
      'msg_a0000000000000000000',
      'msg_-0000000000000000000',
    ]

    expect(sortSMSMessagesForDisplay(ids.map((id) => message({ id, createdAt }))).map((item) => item.id)).toEqual([
      'msg_a0000000000000000000',
      'msg__0000000000000000000',
      'msg_A0000000000000000000',
      'msg_-0000000000000000000',
    ])
  })
})

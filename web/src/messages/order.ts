import type { SmsMessage as SMSMessage } from '@/api/generated/types.gen'

export function sortSMSMessagesForDisplay(messages: readonly SMSMessage[]): SMSMessage[] {
  return [...messages].sort((left, right) => {
    const createdDifference = Date.parse(right.createdAt) - Date.parse(left.createdAt)
    if (createdDifference !== 0) return createdDifference
    // IDs are contractually ASCII. Relational comparison therefore follows
    // the same byte/code-unit order as SQLite's default BINARY collation.
    if (left.id === right.id) return 0
    return left.id > right.id ? -1 : 1
  })
}

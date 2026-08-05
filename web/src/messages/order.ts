import type { SMSMessage } from '@/api/client'

export function sortSMSMessagesForDisplay(messages: readonly SMSMessage[]): SMSMessage[] {
  return [...messages].sort((left, right) => {
    const createdSecondDifference = Math.floor(Date.parse(right.createdAt) / 1000) - Math.floor(Date.parse(left.createdAt) / 1000)
    if (createdSecondDifference !== 0) return createdSecondDifference

    const observedDifference = Date.parse(right.updatedAt) - Date.parse(left.updatedAt)
    if (observedDifference !== 0) return observedDifference

    return right.id.localeCompare(left.id)
  })
}

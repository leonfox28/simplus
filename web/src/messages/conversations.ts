import type { SmsMessage as SMSMessage } from '../api/generated/types.gen'

export interface SMSConversation {
  key: string
  lineId: string
  remoteAddress: string
  messages: SMSMessage[]
  lastMessage: SMSMessage
}

export function buildSMSConversations(messages: SMSMessage[]): SMSConversation[] {
  const grouped = new Map<string, SMSMessage[]>()
  for (const message of messages) {
    const key = JSON.stringify([message.lineId, message.remoteAddress])
    const conversation = grouped.get(key)
    if (conversation) conversation.push(message)
    else grouped.set(key, [message])
  }

  const conversations: SMSConversation[] = []
  for (const [key, groupedMessages] of grouped) {
    const ordered = [...groupedMessages].sort((left, right) => {
      const byTime = Date.parse(left.createdAt) - Date.parse(right.createdAt)
      return byTime || left.id.localeCompare(right.id)
    })
    const lastMessage = ordered.at(-1)
    if (!lastMessage) continue
    conversations.push({
      key,
      lineId: lastMessage.lineId,
      remoteAddress: lastMessage.remoteAddress,
      messages: ordered,
      lastMessage,
    })
  }
  return conversations.sort((left, right) => {
    const byTime = Date.parse(right.lastMessage.updatedAt) - Date.parse(left.lastMessage.updatedAt)
    return byTime || left.key.localeCompare(right.key)
  })
}

import type { Query, QueryClient } from '@tanstack/react-query'
import { zRealtimeEvent } from './generated/zod.gen'
import type { RealtimeAttention, RealtimeEvent, RealtimeTopic } from './generated/types.gen'

const topicTags: Record<RealtimeTopic, readonly string[]> = {
  system: ['system'],
  inventory: ['inventory', 'modems', 'lines'],
  modems: ['modems', 'inventory'],
  lines: ['lines', 'inventory'],
  vowifi: ['vowifi', 'lines'],
  messages: ['messages'],
  calls: ['calls'],
  contacts: ['contacts'],
  mihomo: ['mihomo', 'lines', 'vowifi'],
  notifications: ['notifications'],
  euicc: ['euicc'],
}

function queryTags(query: Query): readonly string[] {
  const root = query.queryKey[0]
  if (typeof root !== 'object' || root === null || !('tags' in root)) return []
  const tags = (root as { tags?: unknown }).tags
  return Array.isArray(tags) && tags.every((tag) => typeof tag === 'string') ? tags : []
}

export function decodeRealtimeEvent(data: string): RealtimeEvent | undefined {
  try {
    const raw: unknown = JSON.parse(data)
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return undefined
    const keys = Object.keys(raw)
    if (keys.some((key) => key !== 'topics' && key !== 'attention')) return undefined
    const result = zRealtimeEvent.safeParse(raw)
    if (!result.success || new Set(result.data.topics).size !== result.data.topics.length) return undefined
    return result.data
  } catch {
    return undefined
  }
}

export async function invalidateRealtimeTopics(queryClient: QueryClient, topics: readonly RealtimeTopic[]) {
  const tags = new Set(topics.flatMap((topic) => topicTags[topic]))
  await queryClient.invalidateQueries({
    predicate: (query) => queryTags(query).some((tag) => tags.has(tag)),
    refetchType: 'active',
  })
}

export async function resyncActiveQueries(queryClient: QueryClient) {
  await queryClient.invalidateQueries({ refetchType: 'active' })
}

export type RealtimeAttentionHandler = (attention: RealtimeAttention) => void

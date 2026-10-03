type SessionExpiredListener = () => void

let generation = 0
export const sessionGeneration = () => generation
export function advanceSession() { generation++ }

const listeners = new Set<SessionExpiredListener>()

export function notifySessionExpired() {
  advanceSession()
  for (const listener of listeners) listener()
}

export function onSessionExpired(listener: SessionExpiredListener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

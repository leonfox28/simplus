type SessionExpiredListener = () => void

const listeners = new Set<SessionExpiredListener>()

export function notifySessionExpired() {
  for (const listener of listeners) listener()
}

export function onSessionExpired(listener: SessionExpiredListener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

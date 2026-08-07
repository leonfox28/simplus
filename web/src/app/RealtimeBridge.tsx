import { App } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { ApiClientError } from '@/api/errors'
import { decodeRealtimeEvent, invalidateRealtimeTopics, resyncActiveQueries } from '@/api/events'
import { getAuthSession } from '@/api/generated/sdk.gen'

export function RealtimeBridge() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()

  useEffect(() => {
    let source: EventSource | undefined
    let reconnectTimer: number | undefined
    let stopped = false
    let lastAttentionID = ''
    let reconnectAttempt = 0

    const close = () => {
      source?.close()
      source = undefined
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer)
      reconnectTimer = undefined
    }

    const handle = (event: MessageEvent<string>) => {
      const payload = decodeRealtimeEvent(event.data)
      if (!payload) return
      void invalidateRealtimeTopics(queryClient, payload.topics)
      if (payload.attention && (!event.lastEventId || event.lastEventId !== lastAttentionID)) {
        lastAttentionID = event.lastEventId
        void message.info(payload.attention === 'sms.received' ? '收到新短信，列表已更新。' : '有新的来电，请打开语音通话页面。')
      }
    }

    const scheduleReconnect = () => {
      if (stopped || document.visibilityState === 'hidden' || reconnectTimer !== undefined) return
      const delay = Math.min(3_000 * 2 ** reconnectAttempt, 30_000)
      reconnectAttempt += 1
      reconnectTimer = window.setTimeout(() => {
        reconnectTimer = undefined
        connect()
      }, delay)
    }

    const connect = () => {
      if (stopped || document.visibilityState === 'hidden' || source) return
      source = new EventSource('/api/v1/events')
      source.addEventListener('resync', handle as EventListener)
      source.addEventListener('update', handle as EventListener)
      source.onopen = () => { reconnectAttempt = 0 }
      source.onerror = () => {
        close()
        void getAuthSession({ throwOnError: true }).then(() => {
          scheduleReconnect()
        }).catch((error) => {
          if (!(error instanceof ApiClientError) || error.status !== 401) scheduleReconnect()
        })
      }
    }

    const onVisibilityChange = () => {
      if (document.visibilityState === 'hidden') close()
      else {
        void resyncActiveQueries(queryClient)
        connect()
      }
    }

    connect()
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      stopped = true
      document.removeEventListener('visibilitychange', onVisibilityChange)
      close()
    }
  }, [message, queryClient])

  return null
}

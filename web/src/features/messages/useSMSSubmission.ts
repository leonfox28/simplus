import { useMutation } from '@tanstack/react-query'
import { App, type FormInstance } from 'antd'
import { useRef, useState } from 'react'
import { sessionGeneration } from '@/api/session'
import { ApiClientError } from '@/api/errors'
import { sendMessageMutation } from '@/api/generated/@tanstack/react-query.gen'
import type { SendMessageData } from '@/api/generated/types.gen'

type Request = SendMessageData['body']
type Options = { form: FormInstance<{ body: string }>; lineId: string; address: string; temporaryAddress: string; setTemporaryAddress: (value: string) => void; invalidateMessages: () => Promise<unknown>; onError: (error: unknown) => void }

export function useSMSSubmission(options: Options) {
  const { message } = App.useApp()
  const requestSession = useRef(sessionGeneration())
  const [pendingSend, setPendingSend] = useState<Request>()
  const currentSelection = useRef(options)
  currentSelection.current = options
  const send = useMutation({
    ...sendMessageMutation(),
    onSuccess: async (result, variables) => {
      if (requestSession.current !== sessionGeneration()) return
      if (result.status === 'sent' || result.status === 'failed') {
        setPendingSend(undefined)
        if (currentSelection.current.address === variables.body.destination && currentSelection.current.lineId === variables.body.lineId && options.form.getFieldValue('body') === variables.body.body) options.form.resetFields(['body'])
      }
      await options.invalidateMessages()
      if (currentSelection.current.temporaryAddress === result.remoteAddress) options.setTemporaryAddress('')
      if (result.status === 'unconfirmed') {
        void message.warning(result.errorCode === 'IMS_SMS_ACCEPTED_AWAITING_REPORT'
          ? '短信已提交，正在等待运营商确认，请勿重复发送。'
          : '短信可能已经送达，但运营商未返回最终确认，请勿重复发送。')
      } else if (result.status === 'queued') void message.warning('短信正在处理，请查询原请求。')
      else if (result.status === 'failed') void message.error('短信发送失败。')
      else void message.success('短信已发送。')
    },
    onError: (error) => {
      if (requestSession.current !== sessionGeneration()) return
      if (error instanceof ApiClientError && error.kind === 'http' && error.status && error.status >= 400 && error.status < 500 && error.status !== 408) setPendingSend(undefined)
      options.onError(error)
    },
  })

  function submit(body: string) {
    const request = pendingSend ?? { operationId: crypto.randomUUID?.() ?? `operation_${Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('')}`, lineId: options.lineId, destination: options.address, body }
    requestSession.current = sessionGeneration()
    setPendingSend(request)
    send.mutate({ body: request })
  }
  return { send, pendingSend, submit }
}

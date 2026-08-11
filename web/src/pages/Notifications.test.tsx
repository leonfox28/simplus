import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { getFeishuNotificationBindingQueryKey } from '@/api/generated/@tanstack/react-query.gen'
import { json, renderPage } from '@/test/render'
import Notifications from './Notifications'

const idle = { state: 'idle', verificationUrl: '', expiresAt: '', channelId: '', errorCode: '' }
const waiting = {
  state: 'waiting', verificationUrl: 'https://accounts.feishu.cn/synthetic-verification',
  expiresAt: '2099-01-01T00:00:00Z', channelId: '', errorCode: '',
}
const appChannel = {
  id: 'channel_AAAAAAAAAAAAAAAAAAAAAA', provider: 'feishu', deliveryMode: 'feishu_app', targetType: 'authorized_user',
  displayName: '飞书私聊', webhookHint: 'open.feishu.cn', signingSecretConfigured: false, enabled: true,
  eventKinds: ['sms.received', 'sms.failed', 'call.incoming', 'call.missed', 'system.degraded'],
  lastDeliveryAt: '2026-08-11T00:00:00Z', lastDeliveryStatus: 'success', lastErrorCode: '',
}

describe('Feishu notification binding', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.clear()
    window.sessionStorage.clear()
  })

  it('moves from idle to waiting and success, invalidates channels, edits settings, and explains local-only deletion', async () => {
    let binding: typeof idle | typeof waiting | { state: string; verificationUrl: string; expiresAt: string; channelId: string; errorCode: string } = idle
    let channels: typeof appChannel[] = []
    let putBody: unknown
    const fetch = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/notification-channel-bindings/feishu') {
        if (request.method === 'POST') { binding = waiting; return json(binding, 201) }
        if (request.method === 'DELETE') { binding = { ...idle, state: 'cancelled' }; return json(binding) }
        return json(binding)
      }
      if (path === '/api/v1/notification-channels') return json({ channels })
      if (path === `/api/v1/notification-channels/${appChannel.id}` && request.method === 'PUT') {
        putBody = await request.json()
        const values = putBody as { displayName: string }
        channels = [{ ...appChannel, displayName: values.displayName }]
        return json(channels[0])
      }
      if (path === `/api/v1/notification-channels/${appChannel.id}` && request.method === 'DELETE') return json(null, 204)
      throw new Error(`unexpected ${request.method} ${path}`)
    })
    vi.stubGlobal('fetch', fetch)
    const { queryClient } = renderPage(<Notifications />)
    expect(await screen.findByText('尚未发起绑定。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '绑定飞书私聊' }))
    expect(await screen.findByLabelText('飞书验证链接')).toHaveValue(waiting.verificationUrl)
    expect(screen.getByRole('link', { name: /打开飞书授权/ })).toHaveAttribute('href', waiting.verificationUrl)
    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)

    binding = { state: 'testing', verificationUrl: '', expiresAt: '', channelId: '', errorCode: '' }
    await queryClient.invalidateQueries({ queryKey: getFeishuNotificationBindingQueryKey() })
    expect(await screen.findByText('授权完成，正在发送绑定测试消息。')).toBeInTheDocument()

    channels = [appChannel]
    binding = { state: 'succeeded', verificationUrl: '', expiresAt: '', channelId: appChannel.id, errorCode: '' }
    await queryClient.invalidateQueries({ queryKey: getFeishuNotificationBindingQueryKey() })
    expect(await screen.findByText('飞书私聊绑定成功。')).toBeInTheDocument()
    expect(await screen.findByText('授权用户私聊')).toBeInTheDocument()
    expect(screen.queryByDisplayValue(waiting.verificationUrl)).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /编辑/ }))
    const dialog = await screen.findByRole('dialog')
    const nameInput = within(dialog).getByRole('textbox')
    fireEvent.change(nameInput, { target: { value: '飞书值班通知' } })
    fireEvent.click(screen.getByRole('button', { name: /确\s*定|OK/ }))
    await waitFor(() => expect(putBody).toMatchObject({ displayName: '飞书值班通知', webhookUrl: '', signingSecret: '' }))
    expect(await screen.findByText('飞书值班通知')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /删除/ }))
    expect(await screen.findByText('飞书侧自动创建的应用仍会保留，需在飞书开发者后台另行处理。')).toBeInTheDocument()
  })

  it.each([
    ['FEISHU_BINDING_DENIED', '管理员拒绝了飞书授权。'],
    ['FEISHU_BINDING_EXPIRED', '飞书授权已过期。'],
    ['FEISHU_BINDING_LARK_UNSUPPORTED', '当前仅支持飞书中国版租户。'],
    ['FEISHU_BINDING_TEST_FAILED', '授权完成，但测试私聊未能送达。飞书侧应用可能已保留。'],
  ])('renders bounded terminal code %s without provider detail', async (errorCode, message) => {
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/notification-channel-bindings/feishu') return json({ ...idle, state: errorCode.includes('EXPIRED') ? 'expired' : 'failed', errorCode })
      if (path === '/api/v1/notification-channels') return json({ channels: [] })
      throw new Error(`unexpected ${path}`)
    }))
    renderPage(<Notifications />)
    expect(await screen.findByText(message)).toBeInTheDocument()
    expect(screen.queryByText(errorCode)).not.toBeInTheDocument()
  })
})

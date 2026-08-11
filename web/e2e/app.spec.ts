import { expect, test, type Page, type Route } from '@playwright/test'

const capabilities = {
  simAccess: true,
  sms: true,
  cellularVoice: true,
  digitalVoiceMedia: false,
  usbUac: false,
  simApdu: true,
  hostVoWifiAuth: true,
  rfControl: true,
  networkScan: false,
  manualNetworkSelection: false,
  primarySimLockState: false,
  pin1Verify: false,
  puk1Unblock: false,
  euiccProfiles: false,
}

const line = {
  id: 'line_AAAAAAAAAAAAAAAAAAAAAA',
  displayName: 'Synthetic Line',
  managedModemId: 'modem_AAAAAAAAAAAAAAAAAAAAAA',
  managedModemDisplayName: 'Synthetic Modem',
  managedModemModel: 'Simulator',
  managedModemSerialNumber: 'SYNTHETIC-001',
  subscriptionDisplayHint: 'SIM •••• 0001',
  state: 'ready',
  capabilities,
  createdAt: '2026-08-07T00:00:00Z',
}

const session = {
  username: 'synthetic_admin',
  locale: 'zh-CN',
  expiresAt: '2099-01-01T00:00:00Z',
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: 'application/json',
    body: status === 204 ? '' : JSON.stringify(body),
  })
}

async function installEventSource(page: Page) {
  await page.addInitScript(() => {
    type EventCallback = (event: { data: string }) => void
    const instances: FakeEventSource[] = []
    class FakeEventSource {
      static readonly OPEN = 1
      readonly OPEN = 1
      readonly readyState = 1
      onopen: (() => void) | null = null
      onerror: (() => void) | null = null
      private listeners = new Map<string, EventCallback[]>()

      constructor(_url: string) {
        instances.push(this)
        queueMicrotask(() => this.onopen?.())
      }

      addEventListener(name: string, callback: EventListenerOrEventListenerObject) {
        const callbacks = this.listeners.get(name) ?? []
        callbacks.push(callback as unknown as EventCallback)
        this.listeners.set(name, callbacks)
      }

      close() {}

      emit(name: string, payload: unknown) {
        for (const callback of this.listeners.get(name) ?? []) callback({ data: JSON.stringify(payload) })
      }
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: FakeEventSource })
    Object.defineProperty(window, '__emitSimplusEvent', {
      configurable: true,
      value: (name: string, payload: unknown) => instances.at(-1)?.emit(name, payload),
    })
  })
}

async function installApi(page: Page, authenticated = false) {
  let loggedIn = authenticated
  let messageRequests = 0
  let readStateRequests = 0
  let lastSendBody: unknown
  let feishuBindingStarted = false
  await page.exposeFunction('__messageRequestCount', () => messageRequests)
  await page.exposeFunction('__readStateRequestCount', () => readStateRequests)
  await page.exposeFunction('__lastSendBody', () => lastSendBody)
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path === '/api/v1/setup/status') return json(route, {
      installationState: 'ready', phase: 'complete', setupRequired: false,
      businessApiAvailable: true, bootstrapGenerationAvailable: false, supportedFlows: ['create-new'],
    })
    if (path === '/api/v1/auth/session') return loggedIn
      ? json(route, session)
      : json(route, { code: 'AUTH_SESSION_UNAUTHORIZED', retryable: false }, 401)
    if (path === '/api/v1/auth/login') { loggedIn = true; return json(route, session) }
    if (path === '/api/v1/system/health') return json(route, {
      status: 'ok', version: 'e2e', apiVersion: 'v1', installationState: 'ready', backend: 'simulator', databaseCount: 1,
    })
    if (path === '/api/v1/hardware/topology') return json(route, { code: 'TOPOLOGY_UNAVAILABLE', retryable: true }, 503)
    if (path === '/api/v1/modems') return json(route, { modems: [{
      id: line.managedModemId, displayName: 'Synthetic Modem', model: 'Simulator', serialNumber: 'SYNTHETIC-001',
      transport: 'simulated', state: 'online', capabilities, rfState: 'on', simPresence: 'present', addedAt: '2026-08-07T00:00:00Z',
    }] })
    if (path === '/api/v1/modem-candidates') return json(route, { candidates: [{
      candidateId: 'synthetic-candidate', usbAddress: '', vendorId: '', productId: '', usbSerialHint: 'SIMULATED',
      model: 'Unavailable Candidate', transport: 'simulated', supportStatus: 'not-ready', addable: false,
      readinessReason: 'CONTROL_UNAVAILABLE', capabilities: { ...capabilities, rfControl: false }, simPresence: 'unknown',
    }] })
    if (path === '/api/v1/euicc') return json(route, { code: 'EUICC_UNAVAILABLE', retryable: false }, 503)
    if (path === '/api/v1/lines') return json(route, { lines: [line] })
    if (path === '/api/v1/line-candidates') return json(route, { candidates: [] })
    if (path === '/api/v1/line-egress-bindings') return json(route, { bindings: [{
      lineId: line.id, mode: 'direct', countryCode: '', countryName: '', listenerPort: 0, ready: true, readinessReason: 'READY',
    }] })
    if (path === '/api/v1/vowifi-lines') return json(route, { lines: [{
      lineId: line.id, desiredActive: false, eligible: true, readinessCode: 'READY', state: 'stopped', stage: '', online: false,
      egressMode: 'direct', countryCode: '', countryName: '', registeredAt: '', nextRefreshAt: '', phoneNumber: '', attempt: 0, lastErrorCode: '',
    }] })
    if (path === '/api/v1/mihomo/subscriptions') return json(route, { subscriptions: [] })
    if (path === '/api/v1/contacts') return json(route, { contacts: [] })
    if (path === '/api/v1/notification-channel-bindings/feishu') {
      if (request.method() === 'POST') feishuBindingStarted = true
      return json(route, feishuBindingStarted ? {
        state: 'waiting', verificationUrl: 'https://accounts.feishu.cn/synthetic-verification',
        expiresAt: '2099-01-01T00:00:00Z', channelId: '', errorCode: '',
      } : { state: 'idle', verificationUrl: '', expiresAt: '', channelId: '', errorCode: '' }, request.method() === 'POST' ? 201 : 200)
    }
    if (path === '/api/v1/notification-channels') return json(route, { channels: [{
      id: 'channel_AAAAAAAAAAAAAAAAAAAAAA', provider: 'feishu', deliveryMode: 'feishu_app', targetType: 'authorized_user',
      displayName: 'Synthetic Feishu DM', webhookHint: 'open.feishu.cn', signingSecretConfigured: false, enabled: true,
      eventKinds: ['sms.received', 'sms.failed', 'call.incoming', 'call.missed', 'system.degraded'],
      lastDeliveryAt: '2026-08-11T00:00:00Z', lastDeliveryStatus: 'success', lastErrorCode: '',
    }] })
    if (path === '/api/v1/message-conversations') return json(route, {
      conversations: [{
        remoteAddress: '+12025550123',
        lastMessage: {
          id: 'message_page_000000', operationId: 'operation_message_page_000000',
          direction: 'inbound', lineId: line.id, remoteAddress: '+12025550123', body: 'First page',
          status: 'received', providerMessageId: 'synthetic-provider-page-0', errorCode: '',
          createdAt: '2026-08-07T00:18:00Z', updatedAt: '2026-08-07T00:20:00Z',
        },
        unreadCount: 1,
      }],
      conversationTotalCount: 1, messageTotalCount: 21, capacity: 1000, nearCapacity: false,
    })
    if (path === '/api/v1/message-conversations/read-state' && request.method() === 'PUT') {
      readStateRequests += 1
      return json(route, null, 204)
    }
    if (path === '/api/v1/messages') {
      if (request.method() === 'POST') {
        lastSendBody = request.postDataJSON()
        const body = lastSendBody as { operationId: string; lineId: string; destination: string; body: string }
        return json(route, {
          id: 'message_browser_sent_01', operationId: body.operationId, direction: 'outbound',
          lineId: body.lineId, remoteAddress: body.destination, body: body.body, status: 'sent',
          providerMessageId: 'synthetic-provider-browser-sent', errorCode: '',
          createdAt: '2026-08-07T00:30:00Z', updatedAt: '2026-08-07T00:30:00Z', sentAt: '2026-08-07T00:30:00Z',
        }, 201)
      }
      messageRequests += 1
      const second = url.searchParams.get('cursor') === 'cursor_next'
      return json(route, {
        messages: second ? [{
          id: 'message_second_1234', operationId: 'operation_message_second_1234',
          direction: 'inbound', lineId: line.id, remoteAddress: '+12025550123', body: 'Second page',
          status: 'received', providerMessageId: 'synthetic-provider-second', errorCode: '',
          createdAt: '2026-08-06T00:00:00Z', updatedAt: '2026-08-06T00:00:00Z',
        }] : Array.from({ length: 20 }, (_, index) => {
          const inboundLatest = index === 0
          const outboundBeforeInbound = index === 1
          const createdAt = inboundLatest
            ? '2026-08-07T00:18:00Z'
            : outboundBeforeInbound
              ? '2026-08-07T00:19:00Z'
              : `2026-08-07T00:${String(19 - index).padStart(2, '0')}:00Z`
          return {
            id: `message_page_${String(index).padStart(6, '0')}`,
            operationId: `operation_message_page_${String(index).padStart(6, '0')}`,
            direction: outboundBeforeInbound ? 'outbound' : 'inbound',
            lineId: line.id,
            remoteAddress: '+12025550123',
            body: inboundLatest ? 'First page' : outboundBeforeInbound ? 'Outbound before inbound' : `Synthetic history ${index}`,
            status: outboundBeforeInbound ? 'sent' : 'received',
            providerMessageId: `synthetic-provider-page-${index}`,
            errorCode: '',
            createdAt,
            updatedAt: inboundLatest ? '2026-08-07T00:20:00Z' : createdAt,
            ...(outboundBeforeInbound ? { sentAt: createdAt } : {}),
          }
        }),
        totalCount: 21, capacity: 1000, nearCapacity: false,
        ...(second ? {} : { nextCursor: 'cursor_next', readThroughToken: `synthetic_read_token_${messageRequests}` }),
      })
    }
    return json(route, { code: 'E2E_FIXTURE_MISSING', retryable: false }, 503)
  })
}

async function expectNoGlobalOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
}

async function expectHeaderActionsRightAligned(page: Page, expectedPadding: number) {
  const header = page.locator('.app-header')
  const actions = page.getByRole('group', { name: '页面操作' })
  await expect(actions.getByRole('button', { name: `管理员菜单：${session.username}` })).toBeVisible()
  const [headerBox, actionsBox] = await Promise.all([header.boundingBox(), actions.boundingBox()])
  expect(headerBox).not.toBeNull()
  expect(actionsBox).not.toBeNull()
  if (!headerBox || !actionsBox) return
  expect(Math.abs((headerBox.x + headerBox.width) - (actionsBox.x + actionsBox.width) - expectedPadding)).toBeLessThanOrEqual(2)
  expect(actionsBox.x).toBeGreaterThanOrEqual(headerBox.x)
  expect(actionsBox.x + actionsBox.width).toBeLessThanOrEqual(headerBox.x + headerBox.width)
}

test.beforeEach(async ({ page }) => {
  await installEventSource(page)
})

test('@desktop login, core workflows, cursor history, and SSE invalidation', async ({ page }) => {
  await page.setViewportSize({ width: 2400, height: 900 })
  await installApi(page)
  await page.goto('/login')
  await page.getByPlaceholder('管理员用户名').fill('synthetic_admin')
  await page.getByPlaceholder('密码').fill('synthetic-password')
  await page.getByRole('button', { name: /登\s*录/ }).click()
  await expect(page.getByRole('heading', { name: '概览' })).toBeVisible()
  await expect(page.getByText('LAN Control Center')).toHaveCount(0)
  await expectHeaderActionsRightAligned(page, 24)

  await page.getByText('模组配置', { exact: true }).click()
  await expect(page.getByText('Simulator', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '添加模组' }).click()
  await expect(page.getByRole('radio', { name: 'Unavailable Candidate：控制端点不可用' })).toBeDisabled()
  await page.getByRole('button', { name: /^(取\s*消|Cancel)$/ }).click()

  await page.getByText('线路配置', { exact: true }).click()
  await expect(page.getByText('Synthetic Line', { exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toBeVisible()
  await page.getByText('短信', { exact: true }).click()
  await expect(page.getByRole('heading', { name: '短信', exact: true })).toHaveCount(0)
  const appContentBox = await page.locator('.app-content').boundingBox()
  const messagesPageBox = await page.locator('.messages-page').boundingBox()
  const messagesWorkspaceBox = await page.locator('.messages-workspace').boundingBox()
  expect(appContentBox).not.toBeNull()
  expect(messagesPageBox).not.toBeNull()
  expect(messagesWorkspaceBox).not.toBeNull()
  expect(messagesPageBox!.x).toBe(appContentBox!.x)
  expect(messagesPageBox!.width).toBe(appContentBox!.width)
  expect(messagesWorkspaceBox!.y).toBe(messagesPageBox!.y + 24)
  expect(messagesWorkspaceBox!.y + messagesWorkspaceBox!.height).toBe(messagesPageBox!.y + messagesPageBox!.height - 24)
  await expect(page.locator('.conversation-preview').first()).toHaveText('First page')
  await expect(page.getByLabel('短信记录').getByText('First page')).toBeVisible()
  const desktopRecordBodies = await page.getByLabel('短信记录').locator('.message-row').allTextContents()
  expect(desktopRecordBodies.at(-2)).toContain('Outbound before inbound')
  expect(desktopRecordBodies.at(-1)).toContain('First page')
  await expect.poll(() => page.getByLabel('短信记录').evaluate((element) => (
    element.scrollTop + element.clientHeight >= element.scrollHeight - 1
  ))).toBe(true)
  await page.getByLabel('短信内容').fill('Browser synthetic outbound')
  await page.getByRole('button', { name: /发送短信/ }).click()
  await expect.poll(() => page.evaluate(() => (
    window as unknown as { __lastSendBody: () => Promise<unknown> }
  ).__lastSendBody())).toEqual(expect.objectContaining({
    lineId: line.id, destination: '+12025550123', body: 'Browser synthetic outbound',
  }))
  await page.getByRole('button', { name: /新建短信/ }).click()
  const newMessageDialog = page.getByRole('dialog')
  await expect(newMessageDialog.getByRole('combobox', { name: /收件人/ })).toBeVisible()
  await newMessageDialog.getByRole('button', { name: /取\s*消|Cancel/ }).click()
  await page.getByRole('button', { name: /联系人管理/ }).click()
  const contactDrawer = page.getByRole('dialog')
  await expect(contactDrawer.getByLabel('名称')).toBeVisible()
  await contactDrawer.getByRole('button', { name: '关闭' }).click()
  await page.getByRole('button', { name: '加载更早短信' }).click()
  await expect(page.getByText('Second page')).toBeVisible()
  expect(await page.getByLabel('短信记录').evaluate((element) => element.scrollTop)).toBeGreaterThan(0)

  const before = await page.evaluate(() => (window as unknown as { __messageRequestCount: () => Promise<number> }).__messageRequestCount())
  const readsBefore = await page.evaluate(() => (window as unknown as { __readStateRequestCount: () => Promise<number> }).__readStateRequestCount())
  await page.evaluate(() => (window as unknown as { __emitSimplusEvent: (name: string, payload: unknown) => void }).__emitSimplusEvent('update', { topics: ['messages'] }))
  await expect.poll(() => page.evaluate(() => (window as unknown as { __messageRequestCount: () => Promise<number> }).__messageRequestCount())).toBeGreaterThan(before)
  await expect.poll(() => page.evaluate(() => (window as unknown as { __readStateRequestCount: () => Promise<number> }).__readStateRequestCount())).toBeGreaterThan(readsBefore)
  await page.getByText('通知渠道', { exact: true }).click()
  await expect(page.getByText('Synthetic Feishu DM', { exact: true })).toBeVisible()
  await expect(page.getByText('授权用户私聊', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '绑定飞书私聊' }).click()
  await expect(page.getByLabel('飞书验证链接')).toHaveValue('https://accounts.feishu.cn/synthetic-verification')
  await expectNoGlobalOverflow(page)
})

test('@mobile Drawer navigation has no overflow or unintended autofocus', async ({ page }) => {
  await installApi(page, true)
  await page.goto('/dashboard')
  await expect(page.getByText('LAN Control Center')).toHaveCount(0)
  await expectHeaderActionsRightAligned(page, 12)
  await page.getByRole('button', { name: '打开导航' }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toBeVisible()
  expect(await page.evaluate(() => ['INPUT', 'SELECT', 'TEXTAREA'].includes(document.activeElement?.tagName ?? ''))).toBe(false)
  await drawer.getByText('线路配置', { exact: true }).click()
  await expect(drawer).toBeHidden()
  await expect(page.getByRole('heading', { name: '线路配置' })).toBeVisible()
  await expect(page.getByText('Synthetic Line', { exact: true })).toBeVisible()
  await expect(page.locator('.mobile-record-card')).toBeVisible()
  await expect(page.locator('.ant-table')).toHaveCount(0)
  await page.getByRole('button', { name: '打开导航' }).click()
  const messagesDrawer = page.getByRole('dialog')
  await messagesDrawer.getByText('短信', { exact: true }).click()
  await expect(page.getByRole('button', { name: /\+12025550123/ })).toBeVisible()
  await expect(page.locator('.conversation-preview').first()).toHaveText('First page')
  await page.getByRole('button', { name: /\+12025550123/ }).click()
  await expect(page.getByLabel('短信记录')).toBeVisible()
  await expect(page.getByText('First page')).toBeVisible()
  const mobileRecordBodies = await page.getByLabel('短信记录').locator('.message-row').allTextContents()
  expect(mobileRecordBodies.at(-2)).toContain('Outbound before inbound')
  expect(mobileRecordBodies.at(-1)).toContain('First page')
  await page.getByRole('button', { name: '返回会话列表' }).click()
  await expect(page.getByRole('button', { name: /\+12025550123/ })).toBeVisible()
  await page.getByRole('button', { name: '打开导航' }).click()
  const notificationDrawer = page.getByRole('dialog')
  await notificationDrawer.getByText('通知渠道', { exact: true }).click()
  await expect(page.locator('.mobile-record-card').getByText('Synthetic Feishu DM', { exact: true })).toBeVisible()
  await expect(page.locator('.mobile-record-card').getByText('授权用户私聊', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '绑定飞书私聊' }).click()
  await expect(page.getByLabel('飞书验证链接')).toHaveValue('https://accounts.feishu.cn/synthetic-verification')
  await expectNoGlobalOverflow(page)
})

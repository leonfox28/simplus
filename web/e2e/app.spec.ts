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
  await page.exposeFunction('__messageRequestCount', () => messageRequests)
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
    if (path === '/api/v1/messages') {
      messageRequests += 1
      const second = url.searchParams.get('cursor') === 'cursor_next'
      return json(route, {
        messages: [{
          id: second ? 'message_second_1234' : 'message_first_12345',
          operationId: second ? 'operation_message_second_1234' : 'operation_message_first_12345',
          direction: 'inbound', lineId: line.id, remoteAddress: '+12025550123', body: second ? 'Second page' : 'First page',
          status: 'received', providerMessageId: '', errorCode: '',
          createdAt: second ? '2026-08-06T00:00:00Z' : '2026-08-07T00:00:00Z',
          updatedAt: second ? '2026-08-06T00:00:00Z' : '2026-08-07T00:00:00Z',
        }],
        totalCount: 2, capacity: 1000, nearCapacity: false,
        ...(second ? {} : { nextCursor: 'cursor_next' }),
      })
    }
    return json(route, { code: 'E2E_FIXTURE_MISSING', retryable: false }, 503)
  })
}

async function expectNoGlobalOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true)
}

test.beforeEach(async ({ page }) => {
  await installEventSource(page)
})

test('@desktop login, core workflows, cursor history, and SSE invalidation', async ({ page }) => {
  await installApi(page)
  await page.goto('/login')
  await page.getByPlaceholder('管理员用户名').fill('synthetic_admin')
  await page.getByPlaceholder('密码').fill('synthetic-password')
  await page.getByRole('button', { name: /登\s*录/ }).click()
  await expect(page.getByRole('heading', { name: '概览' })).toBeVisible()

  await page.getByText('模组配置', { exact: true }).click()
  await expect(page.getByText('Simulator', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '添加模组' }).click()
  await expect(page.getByRole('radio', { name: 'Unavailable Candidate：控制端点不可用' })).toBeDisabled()
  await page.getByRole('button', { name: /^(取\s*消|Cancel)$/ }).click()

  await page.getByText('线路配置', { exact: true }).click()
  await expect(page.getByText('Synthetic Line', { exact: true })).toBeVisible()
  await expect(page.getByRole('table')).toBeVisible()
  await page.getByText('短信', { exact: true }).click()
  await expect(page.getByText('First page')).toBeVisible()
  await page.getByRole('button', { name: '加载更多' }).click()
  await expect(page.getByText('Second page')).toBeVisible()

  const before = await page.evaluate(() => (window as unknown as { __messageRequestCount: () => Promise<number> }).__messageRequestCount())
  await page.evaluate(() => (window as unknown as { __emitSimplusEvent: (name: string, payload: unknown) => void }).__emitSimplusEvent('update', { topics: ['messages'] }))
  await expect.poll(() => page.evaluate(() => (window as unknown as { __messageRequestCount: () => Promise<number> }).__messageRequestCount())).toBeGreaterThan(before)
  await expectNoGlobalOverflow(page)
})

test('@mobile Drawer navigation has no overflow or unintended autofocus', async ({ page }) => {
  await installApi(page, true)
  await page.goto('/dashboard')
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
  await expectNoGlobalOverflow(page)
})

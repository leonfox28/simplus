import { fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { json, renderPage } from '@/test/render'
import Dashboard from './Dashboard'
import Mihomo from './Mihomo'
import Notifications from './Notifications'
import Settings from './Settings'
import Setup from './Setup'

describe('remaining page boundaries', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('keeps health visible when topology is temporarily unavailable', async () => {
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/system/health') return json({ status: 'ok', version: 'test', apiVersion: 'v1', installationState: 'ready', backend: 'simulator', databaseCount: 1 })
      if (path === '/api/v1/hardware/topology') return json({ code: 'TOPOLOGY_UNAVAILABLE', retryable: true }, 503)
      throw new Error(`unexpected ${path}`)
    }))
    renderPage(<Dashboard />)
    expect(await screen.findByText('ok')).toBeInTheDocument()
    expect(await screen.findByText('simulator')).toBeInTheDocument()
    expect(screen.getByText('管理服务暂时无法完成该操作。')).toBeInTheDocument()
  })

  it('renders independent Mihomo core, runtime, dashboard, and subscription states', async () => {
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/mihomo/core') return json({ installed: false, version: '', architecture: '', sha256: '', installedAt: '' })
      if (path === '/api/v1/mihomo/runtime') return json({ state: 'stopped', pid: 0, selectedSubscriptionId: '', runningSubscriptionId: '', pendingRestart: false, startedAt: '', lastErrorCode: '' })
      if (path === '/api/v1/mihomo/dashboard') return json({ available: false, version: 'v1.0.0', controllerAddress: '', url: '', secret: 'a'.repeat(43) })
      if (path === '/api/v1/mihomo/subscriptions') return json({ subscriptions: [] })
      throw new Error(`unexpected ${path}`)
    }))
    renderPage(<Mihomo />)
    expect(await screen.findByText('尚未配置订阅')).toBeInTheDocument()
    expect(screen.getByText('未安装')).toBeInTheDocument()
    expect(screen.getByText('已停止')).toBeInTheDocument()
  })

  it('shows only the bounded Mihomo URL hint outside the explicit edit flow', async () => {
    const privateUrl = 'https://synthetic.invalid/subscription?token=not-for-display'
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/mihomo/core') return json({ installed: false, version: '', architecture: '', sha256: '', installedAt: '' })
      if (path === '/api/v1/mihomo/runtime') return json({ state: 'stopped', pid: 0, selectedSubscriptionId: '', runningSubscriptionId: '', pendingRestart: false, startedAt: '', lastErrorCode: '' })
      if (path === '/api/v1/mihomo/dashboard') return json({ available: false, version: 'v1.0.0', controllerAddress: '', url: '', secret: 'a'.repeat(43) })
      if (path === '/api/v1/mihomo/subscriptions') return json({ subscriptions: [{
        id: 'subscription_AAAAAAAAAAAAAAAAAAAAAA', displayName: 'Synthetic Subscription',
        url: privateUrl, urlHint: 'synthetic.invalid', enabled: true, selected: false,
        artifactReady: true, lastRefreshAt: '', lastRefreshStatus: 'success', nodeCount: 1, lastErrorCode: '',
      }] })
      throw new Error(`unexpected ${path}`)
    }))
    renderPage(<Mihomo />)
    expect(await screen.findByText('synthetic.invalid')).toBeInTheDocument()
    expect(screen.queryByText(privateUrl)).not.toBeInTheDocument()
  })

  it('does not render server-held notification credentials', async () => {
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/notification-channels') return json({ channels: [{
        id: 'channel_AAAAAAAAAAAAAAAAAAAAAA', provider: 'wecom', displayName: 'Synthetic Channel',
        webhookHint: 'qyapi.weixin.qq.com', signingSecretConfigured: true, enabled: true,
        eventKinds: ['sms.received'], lastDeliveryAt: '', lastDeliveryStatus: 'never', lastErrorCode: '',
      }] })
      throw new Error(`unexpected ${path}`)
    }))
    renderPage(<Notifications />)
    expect(await screen.findByText('Synthetic Channel')).toBeInTheDocument()
    expect(screen.getByText('qyapi.weixin.qq.com')).toBeInTheDocument()
    expect(screen.queryByText('server-held-secret')).not.toBeInTheDocument()
  })

  it('validates password confirmation before issuing a mutation', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    renderPage(<Settings />)
    fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'current-password' } })
    fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'new-password-123' } })
    fireEvent.change(screen.getByLabelText('确认密码'), { target: { value: 'different-password' } })
    fireEvent.click(screen.getByRole('button', { name: /更换密码并重新登录/ }))
    expect(await screen.findByText('两次密码不一致')).toBeInTheDocument()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('resumes a bounded setup session without exposing bootstrap material', async () => {
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/setup/session') return json({
        authorized: true, expiresAt: '2099-01-01T00:00:00Z', selectedFlow: 'create-new', supportedFlows: ['create-new'],
        administratorConfigured: true, administratorUsername: 'synthetic_admin', instanceDefaultLocale: 'zh-CN',
        storageConfigured: false, dataRoot: '/synthetic/data', recordingsRoot: '/synthetic/recordings',
        httpsConfigured: false, httpsConfirmed: false, httpsMode: '', httpsListenUrl: '', httpsRootFingerprint: '', httpsLeafNotAfter: '',
        hardwareReviewed: false, hardwareDeviceCount: 0, hardwareLineCount: 0, hardwareInventoryDigest: '',
      })
      throw new Error(`unexpected ${path}`)
    }))
    renderPage(<Setup />)
    expect(await screen.findByText('当前管理员：synthetic_admin')).toBeInTheDocument()
    expect(screen.getByLabelText('录音目录')).toHaveValue('/synthetic/recordings')
    expect(window.location.hash).not.toContain('bootstrap=')
  })
})

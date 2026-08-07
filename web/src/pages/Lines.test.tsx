import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { json, noCapabilities, renderPage, testLine } from '@/test/render'
import Lines from './Lines'

describe('Lines readiness workflows', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows candidate reasons and keeps VoWiFi activation fail-closed for an unconfigured exit', async () => {
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const url = new URL(request.url)
      if (url.pathname === '/api/v1/lines' && request.method === 'GET') return json({ lines: [testLine] })
      if (url.pathname === '/api/v1/line-egress-bindings') return json({ bindings: [{ lineId: testLine.id, mode: 'unconfigured', countryCode: '', countryName: '', listenerPort: 0, ready: false, readinessReason: 'EGRESS_NOT_CONFIGURED' }] })
      if (url.pathname === '/api/v1/mihomo/subscriptions') return json({ subscriptions: [] })
      if (url.pathname === '/api/v1/vowifi-lines') return json({ lines: [{ lineId: testLine.id, desiredActive: false, eligible: false, readinessCode: 'EGRESS_NOT_CONFIGURED', state: 'stopped', stage: '', online: false, egressMode: 'unconfigured', countryCode: '', countryName: '', registeredAt: '', nextRefreshAt: '', phoneNumber: '', attempt: 0, lastErrorCode: '' }] })
      if (url.pathname === '/api/v1/line-candidates') return json({ candidates: [{
        candidateId: `line-candidate-${'a'.repeat(32)}`, managedModemId: testLine.managedModemId,
        managedModemDisplayName: '测试模组', managedModemModel: 'Simulator', managedModemSerialNumber: 'SYNTHETIC-001',
        subscriptionDisplayHint: 'SIM •••• 0002', homeOperatorName: '', homeOperatorCode: '', simPresence: 'absent',
        capabilities: noCapabilities, addable: false, readinessReason: 'SIM_ABSENT',
      }] })
      throw new Error(`unexpected ${request.method} ${url.pathname}`)
    }))
    renderPage(<Lines />)
    fireEvent.click(await screen.findByRole('button', { name: '配置' }))
    expect(await screen.findByRole('button', { name: '激活 VoWiFi' })).toBeDisabled()
    expect(screen.getByText('请先明确配置出口')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '添加线路' }))
    expect(await screen.findByRole('radio', { name: '未插入 SIM' })).toBeDisabled()
  })

  it('keeps the stable Line list visible when only runtime status fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname
      if (path === '/api/v1/lines') return json({ lines: [testLine] })
      if (path === '/api/v1/line-egress-bindings') return json({ bindings: [] })
      if (path === '/api/v1/mihomo/subscriptions') return json({ subscriptions: [] })
      if (path === '/api/v1/vowifi-lines') return json({ code: 'VOWIFI_UNAVAILABLE', retryable: true }, 503)
      throw new Error(`unexpected ${path}`)
    }))
    renderPage(<Lines />)
    expect(await screen.findByText('测试线路')).toBeInTheDocument()
    expect(await screen.findByText('Host VoWiFi 运行状态暂不可用')).toBeInTheDocument()
  })
})

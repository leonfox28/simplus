import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  getHardwareTopology: vi.fn(),
  listLineEgressBindings: vi.fn(),
  listMihomoSubscriptions: vi.fn(),
  listMihomoSubscriptionNodes: vi.fn(),
  listVoWiFiLines: vi.fn(),
  putSubscriptionProfileAccessMode: vi.fn(),
  putLineEgressBinding: vi.fn(),
  activateVoWiFiLine: vi.fn(),
  deactivateVoWiFiLine: vi.fn(),
}))

vi.mock('@/api/client', () => api)

import Lines from './Lines'

const lineID = 'agent-line-0123456789abcdef0123456789abcdef'
const stopped = {
  lineId: lineID,
  desiredActive: false,
  eligible: true,
  readinessCode: 'READY',
  state: 'stopped',
  stage: '',
  online: false,
  egressMode: 'mihomo-country',
  countryCode: 'GB',
  countryName: '英国',
  registeredAt: '',
  nextRefreshAt: '',
  attempt: 0,
  lastErrorCode: '',
}
const online = {
  ...stopped,
  desiredActive: true,
  state: 'online',
  stage: 'REGISTERED',
  online: true,
  registeredAt: '2026-08-04T21:07:41Z',
  nextRefreshAt: '2026-08-04T21:32:41Z',
  attempt: 1,
}

beforeEach(() => {
  vi.clearAllMocks()
  api.getHardwareTopology.mockResolvedValue({
    subscriptionProfiles: [{ id: 'profile-1', displayName: 'VOXI', displayIdentityHint: '••••', accessMode: 'host-vowifi-only' }],
    resourceGroups: [{ id: 'group-1', displayName: 'ML307A' }],
    lines: [{
      id: lineID,
      displayName: 'VOXI Line',
      subscriptionProfileId: 'profile-1',
      resourceGroupId: 'group-1',
      accessMode: 'host-vowifi-only',
      state: 'ready',
      rfSafety: 'off',
      capabilities: { hostVoWifiAuth: true },
    }],
  })
  api.listLineEgressBindings.mockResolvedValue([{
    lineId: lineID,
    mode: 'mihomo-country',
    countryCode: 'GB',
    countryName: '英国',
    listenerPort: 20157,
    ready: true,
    readinessReason: 'READY',
  }])
  api.listMihomoSubscriptions.mockResolvedValue([{
    id: 'subscription_abcdefghijklmnopqrstuv',
    displayName: '英国订阅',
    selected: true,
  }])
  api.listMihomoSubscriptionNodes.mockResolvedValue([{ countryCode: 'GB', countryName: '英国' }])
  api.activateVoWiFiLine.mockResolvedValue(online)
  api.deactivateVoWiFiLine.mockResolvedValue(stopped)
})

describe('Lines Host VoWiFi controls', () => {
  it('activates from the Line card and renders the observed online state', async () => {
    api.listVoWiFiLines.mockResolvedValueOnce([stopped]).mockResolvedValue([online])
    const view = render(<App><Lines /></App>)

    const activate = await screen.findByRole('button', { name: '激活 VoWiFi' })
    expect(activate).toBeEnabled()
    fireEvent.click(activate)

    await waitFor(() => expect(api.activateVoWiFiLine).toHaveBeenCalledWith(lineID))
    expect(await screen.findByText('在线')).toBeInTheDocument()
    expect(screen.getAllByText('英国 (GB)').length).toBeGreaterThan(0)

    fireEvent.click(screen.getByRole('button', { name: '停用 VoWiFi' }))
    await waitFor(() => expect(api.deactivateVoWiFiLine).toHaveBeenCalledWith(lineID))
    expect(await screen.findByText('已停用')).toBeInTheDocument()
    view.unmount()
  })

  it('polls runtime state without reloading editable Line configuration', async () => {
    vi.useFakeTimers()
    api.listVoWiFiLines.mockResolvedValue([stopped])
    const view = render(<App><Lines /></App>)

    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(api.getHardwareTopology).toHaveBeenCalledTimes(1)
    expect(api.listVoWiFiLines).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(api.listVoWiFiLines).toHaveBeenCalledTimes(2)
    expect(api.getHardwareTopology).toHaveBeenCalledTimes(1)

    view.unmount()
    vi.useRealTimers()
  })
})

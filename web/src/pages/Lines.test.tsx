import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { App } from 'antd'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  listManagedLines: vi.fn(),
  listLineCandidates: vi.fn(),
  addManagedLine: vi.fn(),
  updateManagedLine: vi.fn(),
  listLineEgressBindings: vi.fn(),
  listMihomoSubscriptions: vi.fn(),
  listMihomoSubscriptionNodes: vi.fn(),
  listVoWiFiLines: vi.fn(),
  putLineEgressBinding: vi.fn(),
  activateVoWiFiLine: vi.fn(),
  deactivateVoWiFiLine: vi.fn(),
}))

vi.mock('@/api/client', () => api)

import Lines from './Lines'

const lineID = 'line_AQEBAQEBAQEBAQEBAQEBAQ'
const candidateID = 'line-candidate-0123456789abcdef0123456789abcdef'
const capabilities = {
  simAccess: true, sms: false, cellularVoice: false, digitalVoiceMedia: false, usbUac: false,
  simApdu: true, hostVoWifiAuth: true, rfControl: false, networkScan: false,
  manualNetworkSelection: false, primarySimLockState: false, pin1Verify: false,
  puk1Unblock: false, euiccProfiles: false,
}
const line = {
  id: lineID,
  displayName: 'VOXI Line',
  managedModemId: 'modem_AQEBAQEBAQEBAQEBAQEBAQ',
  managedModemDisplayName: 'ML307A',
  subscriptionDisplayHint: 'ICCID •••• 5553',
  accessMode: 'host-vowifi-only',
  state: 'ready',
  capabilities,
  createdAt: '2026-08-05T12:00:00Z',
}
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
  api.listManagedLines.mockResolvedValue([line])
  api.listLineCandidates.mockResolvedValue([{
    candidateId: candidateID,
    managedModemId: line.managedModemId,
    managedModemDisplayName: 'ML307A',
    subscriptionDisplayHint: 'ICCID •••• 5553',
    capabilities,
    addable: true,
  }])
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
  api.listVoWiFiLines.mockResolvedValue([stopped])
  api.activateVoWiFiLine.mockResolvedValue(online)
  api.deactivateVoWiFiLine.mockResolvedValue(stopped)
  api.addManagedLine.mockResolvedValue(line)
  api.updateManagedLine.mockResolvedValue(line)
})

describe('managed Lines', () => {
  it('activates Host VoWiFi through the stable business Line', async () => {
    const view = render(<App><Lines /></App>)
    const activate = await screen.findByRole('button', { name: '激活 VoWiFi' })
    expect(activate).toBeEnabled()
    fireEvent.click(activate)

    await waitFor(() => expect(api.activateVoWiFiLine).toHaveBeenCalledWith(lineID))
    expect(await screen.findByText('在线')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '停用 VoWiFi' }))
    await waitFor(() => expect(api.deactivateVoWiFiLine).toHaveBeenCalledWith(lineID))
    view.unmount()
  })

  it('creates a Line only from a managed-modem candidate', async () => {
    const view = render(<App><Lines /></App>)
    fireEvent.click(await screen.findByRole('button', { name: '添加线路' }))
    const option = await screen.findByRole('radio')
    fireEvent.click(option)
    expect(screen.getByDisplayValue('ML307A · ICCID •••• 5553')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确 定' }))
    await waitFor(() => expect(api.addManagedLine).toHaveBeenCalledWith({
      candidateId: candidateID,
      displayName: 'ML307A · ICCID •••• 5553',
      accessMode: 'host-vowifi-only',
    }))
    view.unmount()
  })

  it('polls only runtime state without reloading Line configuration', async () => {
    vi.useFakeTimers()
    const view = render(<App><Lines /></App>)
    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(api.listManagedLines).toHaveBeenCalledTimes(1)
    expect(api.listVoWiFiLines).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(api.listVoWiFiLines).toHaveBeenCalledTimes(2)
    expect(api.listManagedLines).toHaveBeenCalledTimes(1)
    view.unmount()
    vi.useRealTimers()
  })
})

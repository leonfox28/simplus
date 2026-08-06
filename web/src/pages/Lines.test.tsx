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
  managedModemModel: 'ML307A',
  managedModemSerialNumber: 'ML307A-DEMO-01',
  subscriptionDisplayHint: 'ICCID •••• 5553',
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
  phoneNumber: '',
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
  phoneNumber: '+447700900123',
  attempt: 1,
}

beforeEach(() => {
  vi.clearAllMocks()
  api.listManagedLines.mockResolvedValue([line])
  api.listLineCandidates.mockResolvedValue([
    {
      candidateId: candidateID,
      managedModemId: line.managedModemId,
      managedModemDisplayName: line.managedModemDisplayName,
      managedModemModel: line.managedModemModel,
      managedModemSerialNumber: line.managedModemSerialNumber,
      subscriptionDisplayHint: line.subscriptionDisplayHint,
      homeOperatorName: 'VOXI',
      homeOperatorCode: '234-15',
      simPresence: 'present',
      capabilities,
      addable: true,
      readinessReason: 'READY',
    },
    {
      candidateId: 'line-candidate-fedcba9876543210fedcba9876543210',
      managedModemId: 'modem_AgICAgICAgICAgICAgICAg',
      managedModemDisplayName: '备用模组',
      managedModemModel: '读取失败',
      managedModemSerialNumber: '读取失败',
      subscriptionDisplayHint: 'SIM 未检测到',
      homeOperatorName: '',
      homeOperatorCode: '',
      simPresence: 'absent',
      capabilities: { ...capabilities, hostVoWifiAuth: false },
      addable: false,
      readinessReason: 'SIM_ABSENT',
    },
  ])
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
  it('uses a stable Line to configure and activate Host VoWiFi', async () => {
    const view = render(<App><Lines /></App>)
    expect(await screen.findByText('VOXI Line')).toBeInTheDocument()
    expect(screen.getByText('ML307A-DEMO-01')).toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: '能力' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '配置' }))

    const activate = await screen.findByRole('button', { name: '激活 VoWiFi' })
    expect(activate).toBeEnabled()
    fireEvent.click(activate)

    await waitFor(() => expect(api.activateVoWiFiLine).toHaveBeenCalledWith(lineID))
    expect(await screen.findAllByText('在线')).not.toHaveLength(0)
    expect(screen.getByText('+447700900123')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '停用 VoWiFi' }))
    await waitFor(() => expect(api.deactivateVoWiFiLine).toHaveBeenCalledWith(lineID))
    view.unmount()
  })

  it('creates a Line from a fresh candidate without configuring RF or a communication path', async () => {
    const view = render(<App><Lines /></App>)
    fireEvent.click(await screen.findByRole('button', { name: '添加线路' }))
    expect(await screen.findByText('未插入 SIM')).toBeInTheDocument()
    expect(screen.getByText('VOXI')).toBeInTheDocument()
    expect(screen.getByText('234-15')).toBeInTheDocument()
    const radios = screen.getAllByRole('radio')
    expect(radios[1]).toBeDisabled()
    fireEvent.click(radios[0])

    const name = screen.getByDisplayValue('ML307A · ICCID •••• 5553')
    expect(name).not.toHaveFocus()
    fireEvent.click(screen.getByRole('button', { name: '确 定' }))
    await waitFor(() => expect(api.addManagedLine).toHaveBeenCalledWith({
      candidateId: candidateID,
      displayName: 'ML307A · ICCID •••• 5553',
    }))
    expect(api.putLineEgressBinding).not.toHaveBeenCalled()
    expect(api.activateVoWiFiLine).not.toHaveBeenCalled()
    expect(await screen.findByText(/配置线路/)).toBeInTheDocument()
    view.unmount()
  })

  it('keeps exactly one Line candidate selected', async () => {
    const secondCandidateID = 'line-candidate-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    api.listLineCandidates.mockResolvedValue([
      {
        candidateId: candidateID,
        managedModemId: line.managedModemId,
        managedModemDisplayName: line.managedModemDisplayName,
        managedModemModel: line.managedModemModel,
        managedModemSerialNumber: line.managedModemSerialNumber,
        subscriptionDisplayHint: line.subscriptionDisplayHint,
        homeOperatorName: 'VOXI',
        homeOperatorCode: '234-15',
        simPresence: 'present',
        capabilities,
        addable: true,
        readinessReason: 'READY',
      },
      {
        candidateId: secondCandidateID,
        managedModemId: 'modem_AgICAgICAgICAgICAgICAg',
        managedModemDisplayName: '第二个模组',
        managedModemModel: 'ML307A',
        managedModemSerialNumber: 'ML307A-DEMO-02',
        subscriptionDisplayHint: 'ICCID •••• 0002',
        homeOperatorName: 'Example Mobile',
        homeOperatorCode: '001-01',
        simPresence: 'present',
        capabilities,
        addable: true,
        readinessReason: 'READY',
      },
    ])

    const view = render(<App><Lines /></App>)
    fireEvent.click(await screen.findByRole('button', { name: '添加线路' }))
    expect(await screen.findByText('每次只能选择一个线路候选；如需添加多条线路，请分别完成添加。')).toBeInTheDocument()
    const radios = screen.getAllByRole('radio')
    fireEvent.click(radios[0])
    expect(radios[0]).toBeChecked()
    expect(radios[1]).not.toBeChecked()
    expect(screen.queryByText(/已选择\s*1\s*项/)).not.toBeInTheDocument()
    fireEvent.click(radios[1])
    expect(radios[0]).not.toBeChecked()
    expect(radios[1]).toBeChecked()
    expect(screen.getByDisplayValue('ML307A · ICCID •••• 0002')).toBeInTheDocument()
    view.unmount()
  })

  it('keeps activation disabled while the egress is unconfigured', async () => {
    api.listLineEgressBindings.mockResolvedValue([{
      lineId: lineID,
      mode: 'unconfigured',
      countryCode: '',
      countryName: '',
      listenerPort: 0,
      ready: false,
      readinessReason: 'EGRESS_NOT_CONFIGURED',
    }])
    api.listVoWiFiLines.mockResolvedValue([{
      ...stopped,
      eligible: false,
      readinessCode: 'EGRESS_NOT_CONFIGURED',
      egressMode: 'unconfigured',
      countryCode: '',
      countryName: '',
    }])
    const view = render(<App><Lines /></App>)
    fireEvent.click(await screen.findByRole('button', { name: '配置' }))
    expect(await screen.findByText('请先明确配置出口')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '激活 VoWiFi' })).toBeDisabled()
    view.unmount()
  })

  it('allows activation to request the backend recovery of a configured Mihomo exit', async () => {
    api.listLineEgressBindings.mockResolvedValue([{
      lineId: lineID,
      mode: 'mihomo-country',
      countryCode: 'GB',
      countryName: '英国',
      listenerPort: 20157,
      ready: false,
      readinessReason: 'MIHOMO_NOT_RUNNING',
    }])
    api.listVoWiFiLines.mockResolvedValue([{
      ...stopped,
      eligible: false,
      readinessCode: 'MIHOMO_NOT_RUNNING',
    }])
    const view = render(<App><Lines /></App>)
    fireEvent.click(await screen.findByRole('button', { name: '配置' }))
    expect(await screen.findAllByText('Mihomo 未运行')).not.toHaveLength(0)
    expect(screen.getByRole('button', { name: '激活 VoWiFi' })).toBeEnabled()
    view.unmount()
  })

  it('keeps the Line catalog usable when the Host VoWiFi runtime is unavailable', async () => {
    api.listVoWiFiLines.mockRejectedValue(new Error('VOWIFI_UNAVAILABLE'))
    const view = render(<App><Lines /></App>)

    expect(await screen.findByText('VOXI Line')).toBeInTheDocument()
    expect(screen.getByText('Host VoWiFi 运行状态暂不可用')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '配置' }))
    expect(await screen.findByRole('button', { name: '激活 VoWiFi' })).toBeDisabled()
    view.unmount()
  })

  it('polls only runtime state without reloading Line configuration', async () => {
    vi.useFakeTimers()
    const view = render(<App><Lines /></App>)
    await act(async () => {
      await Promise.resolve()
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

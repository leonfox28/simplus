import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { App } from 'antd'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  listManagedModems: vi.fn(),
  listModemCandidates: vi.fn(),
  addManagedModem: vi.fn(),
  readManagedModemIMEI: vi.fn(),
  setManagedModemRFState: vi.fn(),
  getEUICCState: vi.fn(),
  activateEUICCProfile: vi.fn(),
}))

vi.mock('@/api/client', () => api)

import Modems from './Modems'

const capabilities = {
  simAccess: true,
  sms: false,
  cellularVoice: false,
  digitalVoiceMedia: false,
  usbUac: false,
  simApdu: true,
  hostVoWifiAuth: true,
  rfControl: true,
  networkScan: false,
  manualNetworkSelection: false,
  primarySimLockState: true,
  pin1Verify: false,
  puk1Unblock: false,
  euiccProfiles: false,
}

const managed = {
  id: 'modem_AQEBAQEBAQEBAQEBAQEBAQ',
  displayName: 'China Mobile IoT ML307A',
  model: 'China Mobile IoT ML307A',
  serialNumber: 'ML307A-SERIAL-0001',
  transport: 'usb',
  state: 'online',
  rfState: 'off',
  simPresence: 'present',
  capabilities,
  addedAt: '2026-08-05T13:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
  api.listManagedModems.mockResolvedValue([managed])
  api.listModemCandidates.mockResolvedValue([{
    candidateId: 'agent-usb-1-1',
    usbAddress: '1-1',
    vendorId: '2c7c',
    productId: '0125',
    usbSerialHint: 'USB •••• 01234567',
    model: 'DJI/Baiwang QDC507',
    transport: 'usb',
    supportStatus: 'supported',
    addable: true,
    readinessReason: 'READY',
    simPresence: 'absent',
    capabilities: { ...capabilities, hostVoWifiAuth: false, simApdu: false },
  }])
  api.addManagedModem.mockResolvedValue({ ...managed, id: 'modem_AgICAgICAgICAgICAgICAg' })
  api.setManagedModemRFState.mockResolvedValue({ ...managed, rfState: 'on' })
  api.readManagedModemIMEI.mockResolvedValue('490154203237518')
  api.getEUICCState.mockRejectedValue(new Error('EUICC_UNAVAILABLE'))
})

afterEach(() => cleanup())

describe('Modems management flow', () => {
  it('shows only managed modems and adds a selected discovered candidate', async () => {
    render(<App><Modems /></App>)

    expect(await screen.findByText('China Mobile IoT ML307A')).toBeInTheDocument()
    expect(screen.getByText('ML307A-SERIAL-0001')).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '序列号' })).toBeInTheDocument()
    expect(screen.getByText('已插入')).toBeInTheDocument()
    expect(screen.queryByText('DJI/Baiwang QDC507')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /添加模组/ }))
    expect(await screen.findByText('DJI/Baiwang QDC507')).toBeInTheDocument()
    const dialog = within(screen.getByRole('dialog', { name: '添加模组' }))
    expect(dialog.getByRole('columnheader', { name: 'USB Device' })).toBeInTheDocument()
    expect(dialog.getByRole('columnheader', { name: 'VID:PID' })).toBeInTheDocument()
    expect(dialog.getByRole('columnheader', { name: '型号' })).toBeInTheDocument()
    expect(dialog.getByRole('columnheader', { name: '序列标识' })).toBeInTheDocument()
    expect(screen.getByText('1-1')).toBeInTheDocument()
    expect(screen.getByText('2c7c:0125')).toBeInTheDocument()
    expect(screen.getByText('USB •••• 01234567')).toBeInTheDocument()
    expect(screen.getByText('系统支持')).toBeInTheDocument()
    expect(screen.getByText('未插入')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio'))
    fireEvent.click(within(screen.getByRole('dialog', { name: '添加模组' })).getByRole('button', { name: /添\s*加/ }))

    await waitFor(() => expect(api.addManagedModem).toHaveBeenCalledWith('agent-usb-1-1'))
    await waitFor(() => expect(api.listManagedModems).toHaveBeenCalledTimes(2))
  })

  it('renders an empty managed list without promoting scan results automatically', async () => {
    api.listManagedModems.mockResolvedValue([])
    render(<App><Modems /></App>)
    expect(await screen.findByText('尚未添加模组')).toBeInTheDocument()
    expect(api.listModemCandidates).not.toHaveBeenCalled()
  })

  it('shows a model read failure without falling back to the adapter name', async () => {
    api.listManagedModems.mockResolvedValue([{ ...managed, model: '' }])
    render(<App><Modems /></App>)

    expect(await screen.findByText('读取失败')).toBeInTheDocument()
    expect(screen.queryByText(managed.displayName)).not.toBeInTheDocument()
  })

  it('explains why a discovered modem cannot be added', async () => {
    api.listModemCandidates.mockResolvedValue([{
      candidateId: 'agent-usb-1-1', usbAddress: '1-1', vendorId: '2c7c', productId: '0125', usbSerialHint: '',
      model: 'DJI/Baiwang QDC507', transport: 'usb',
      supportStatus: 'not-ready', addable: false, readinessReason: 'EQUIPMENT_IDENTITY_UNAVAILABLE',
      simPresence: 'present', capabilities: { ...capabilities, hostVoWifiAuth: false, simApdu: false },
    }])
    render(<App><Modems /></App>)

    fireEvent.click(await screen.findByRole('button', { name: /添加模组/ }))
    expect(await screen.findByText('无法读取模组身份')).toBeInTheDocument()
    expect(screen.getByRole('radio')).toBeDisabled()
  })

  it('reveals the IMEI only on demand and can hide it again', async () => {
    render(<App><Modems /></App>)

    const value = await screen.findByTestId(`imei-value-${managed.id}`)
    expect(value).toHaveTextContent('•••••••••••••••')
    expect(api.readManagedModemIMEI).not.toHaveBeenCalled()
    fireEvent.click(screen.getByTestId(`imei-toggle-${managed.id}`))

    await waitFor(() => expect(api.readManagedModemIMEI).toHaveBeenCalledWith(managed.id))
    await waitFor(() => expect(value).toHaveTextContent('490154203237518'))
    await waitFor(() => expect(screen.getByTestId(`imei-toggle-${managed.id}`)).not.toBeDisabled())
    fireEvent.click(screen.getByTestId(`imei-toggle-${managed.id}`))
    await waitFor(() => expect(value).toHaveTextContent('•••••••••••••••'))
  })

  it('changes RF directly through the state switch and uses the managed modem id', async () => {
    render(<App><Modems /></App>)

    fireEvent.click(await screen.findByTestId(`rf-toggle-${managed.id}`))

    await waitFor(() => expect(api.setManagedModemRFState).toHaveBeenCalledWith(managed.id, true))
    expect(await screen.findByRole('switch', { name: `${managed.model} 射频` })).toBeChecked()
  })

  it('does not offer an RF write while the current state is unknown', async () => {
    api.listManagedModems.mockResolvedValue([{ ...managed, rfState: 'unknown' }])
    render(<App><Modems /></App>)

    expect(await screen.findByText('未知')).toBeInTheDocument()
    expect(screen.getByTestId(`rf-toggle-${managed.id}`)).toBeDisabled()
  })
})

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import type { ReactElement } from 'react'
import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { configureApiClient } from '@/api/setupClient'
import type { HardwareCapabilities, ManagedLine } from '@/api/generated/types.gen'

configureApiClient()

export const noCapabilities: HardwareCapabilities = {
  simAccess: false, sms: false, cellularVoice: false, digitalVoiceMedia: false,
  usbUac: false, simApdu: false, hostVoWifiAuth: false, rfControl: false,
  networkScan: false, manualNetworkSelection: false, primarySimLockState: false,
  pin1Verify: false, puk1Unblock: false, euiccProfiles: false,
}

export const testLine: ManagedLine = {
  id: 'line_AAAAAAAAAAAAAAAAAAAAAA',
  displayName: '测试线路',
  managedModemId: 'modem_AAAAAAAAAAAAAAAAAAAAAA',
  managedModemDisplayName: '测试模组',
  managedModemModel: 'Simulator',
  managedModemSerialNumber: 'SYNTHETIC-001',
  subscriptionDisplayHint: 'SIM •••• 0001',
  state: 'ready',
  capabilities: { ...noCapabilities, simAccess: true, sms: true, cellularVoice: true, hostVoWifiAuth: true, simApdu: true },
  createdAt: '2026-08-07T00:00:00Z',
}

export function json(data: unknown, status = 200): Response {
  return new Response(status === 204 ? null : JSON.stringify(data), {
    status,
    headers: status === 204 ? undefined : { 'Content-Type': 'application/json' },
  })
}

export function renderPage(element: ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 0 }, mutations: { retry: false } } })
  return {
    queryClient,
    ...render(<App><QueryClientProvider client={queryClient}><MemoryRouter>{element}</MemoryRouter></QueryClientProvider></App>),
  }
}

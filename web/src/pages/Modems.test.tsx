import { fireEvent, screen, waitFor } from '@testing-library/react'
import { Grid } from 'antd'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { noCapabilities, json, renderPage } from '@/test/render'
import Modems from './Modems'

describe('Modems safety workflows', () => {
  afterEach(() => vi.restoreAllMocks())

  it('keeps unknown RF disabled, reveals IMEI explicitly, and never promotes a scan result', async () => {
    const requests: Request[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const request = input instanceof Request ? input : new Request(input, init)
      requests.push(request)
      const url = new URL(request.url)
      if (url.pathname === '/api/v1/modems' && request.method === 'GET') return json({ modems: [{
        id: 'modem_AAAAAAAAAAAAAAAAAAAAAA', displayName: '测试模组', model: '', serialNumber: 'SYNTHETIC-001',
        transport: 'usb', state: 'online', capabilities: { ...noCapabilities, simAccess: true, rfControl: true },
        rfState: 'unknown', simPresence: 'present', addedAt: '2026-08-07T00:00:00Z',
      }] })
      if (url.pathname.endsWith('/equipment-identity')) return json({ imei: '123456789012345' })
      if (url.pathname === '/api/v1/modem-candidates') return json({ candidates: [{
        candidateId: 'candidate-1', usbAddress: '1-2', vendorId: '1234', productId: '5678', usbSerialHint: 'USB •••• ABCD1234',
        model: 'Candidate', transport: 'usb', supportStatus: 'not-ready', addable: false,
        readinessReason: 'CONTROL_UNAVAILABLE', capabilities: noCapabilities, simPresence: 'unknown',
      }] })
      throw new Error(`unexpected ${request.method} ${url.pathname}`)
    })
    const { queryClient } = renderPage(<Modems />)
    const modemID = 'modem_AAAAAAAAAAAAAAAAAAAAAA'
    const rf = await screen.findByTestId(`rf-toggle-${modemID}`)
    expect(screen.getAllByText('读取失败').length).toBeGreaterThan(0)
    expect(rf).toBeDisabled()
    fireEvent.click(screen.getByTestId(`imei-toggle-${modemID}`))
    await waitFor(() => expect(screen.getByTestId(`imei-value-${modemID}`)).toHaveTextContent('123456789012345'))
    await waitFor(() => expect(queryClient.getMutationCache().getAll().some((mutation) => (
      mutation.state.data as { imei?: string } | undefined
    )?.imei === '123456789012345')).toBe(false))
    fireEvent.click(screen.getByRole('button', { name: '添加模组' }))
    expect(await screen.findByRole('radio', { name: 'Candidate：控制端点不可用' })).toBeDisabled()
    expect(screen.queryByText(/eUICC/i)).not.toBeInTheDocument()
    expect(requests.filter((request) => new URL(request.url).pathname === '/api/v1/euicc')).toHaveLength(0)
    await waitFor(() => expect(requests.filter((request) => request.method === 'POST' && new URL(request.url).pathname === '/api/v1/modems')).toHaveLength(0))
  })

  it.each([
    ['desktop', { md: true }],
    ['compact', { md: false }],
  ])('omits unavailable eUICC presentation and requests in the %s view', async (_, breakpoints) => {
    vi.spyOn(Grid, 'useBreakpoint').mockReturnValue(breakpoints)
    const requests: Request[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const request = input instanceof Request ? input : new Request(input, init)
      requests.push(request)
      const url = new URL(request.url)
      if (url.pathname === '/api/v1/modems' && request.method === 'GET') return json({ modems: [] })
      throw new Error(`unexpected ${request.method} ${url.pathname}`)
    })

    renderPage(<Modems />)

    expect(await screen.findByText('尚未添加模组')).toBeInTheDocument()
    expect(screen.queryByText(/eUICC/i)).not.toBeInTheDocument()
    expect(requests.filter((request) => new URL(request.url).pathname === '/api/v1/euicc')).toHaveLength(0)
  })
})

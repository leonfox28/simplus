import { afterEach, describe, expect, it, vi } from 'vitest'
import { SimulatorMediaSession } from './simulatorMedia'

describe('SimulatorMediaSession', () => {
  afterEach(() => vi.unstubAllGlobals())
  it('fails closed outside a secure context', async () => {
    vi.stubGlobal('isSecureContext', false)
    const session = new SimulatorMediaSession(); await session.start()
    expect(session.state).toBe('failed'); expect(session.errorCode).toBe('CALL_MEDIA_SECURE_CONTEXT_REQUIRED')
  })
  it('captures microphone and produces a bounded remote test tone', async () => {
    const stop = vi.fn(); const oscillator = { frequency:{value:0}, connect:vi.fn(), start:vi.fn(), stop:vi.fn() }
    const gain = { gain:{value:0}, connect:vi.fn().mockReturnThis() }; oscillator.connect.mockReturnValue(gain)
    const context = { state:'running', destination:{}, createMediaStreamSource:vi.fn(()=>({connect:vi.fn()})), createAnalyser:vi.fn(()=>({})), createOscillator:vi.fn(()=>oscillator), createGain:vi.fn(()=>gain), close:vi.fn() }
    class AudioContextMock { constructor() { return context } }
    vi.stubGlobal('isSecureContext', true); vi.stubGlobal('navigator', { mediaDevices:{ getUserMedia:vi.fn(async()=>({getTracks:()=>[{stop}]})) } }); vi.stubGlobal('AudioContext', AudioContextMock)
    const session = new SimulatorMediaSession(); await session.start(); expect(session.state).toBe('active'); expect(oscillator.start).toHaveBeenCalled(); await session.stop(); expect(stop).toHaveBeenCalled(); expect(session.state).toBe('idle')
  })
})

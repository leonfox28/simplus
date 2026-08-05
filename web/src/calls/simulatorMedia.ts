export type SimulatorMediaState = 'idle' | 'starting' | 'active' | 'failed'

export class SimulatorMediaSession {
  state: SimulatorMediaState = 'idle'
  errorCode = ''
  private stream?: MediaStream
  private context?: AudioContext
  private oscillator?: OscillatorNode

  async start(): Promise<void> {
    if (this.state === 'active' || this.state === 'starting') return
    if (!globalThis.isSecureContext || !navigator.mediaDevices?.getUserMedia) {
      this.state = 'failed'; this.errorCode = 'CALL_MEDIA_SECURE_CONTEXT_REQUIRED'; return
    }
    this.state = 'starting'; this.errorCode = ''
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true }, video: false })
      const context = new AudioContext()
      const source = context.createMediaStreamSource(stream)
      const analyser = context.createAnalyser()
      source.connect(analyser)
      const oscillator = context.createOscillator()
      const gain = context.createGain()
      oscillator.frequency.value = 440
      gain.gain.value = 0.025
      oscillator.connect(gain).connect(context.destination)
      oscillator.start()
      this.stream = stream; this.context = context; this.oscillator = oscillator; this.state = 'active'
    } catch {
      this.state = 'failed'; this.errorCode = 'CALL_MEDIA_DEVICE_UNAVAILABLE'; await this.stop(false)
    }
  }

  async stop(reset = true): Promise<void> {
    try { this.oscillator?.stop() } catch { /* already stopped */ }
    this.stream?.getTracks().forEach((track) => track.stop())
    if (this.context && this.context.state !== 'closed') await this.context.close()
    this.stream = undefined; this.context = undefined; this.oscillator = undefined
    if (reset) { this.state = 'idle'; this.errorCode = '' }
  }
}

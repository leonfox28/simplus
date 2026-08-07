import type { MihomoDashboardStatus } from '@/api/generated/types.gen'

export function zashboardLaunchURL(
  status: MihomoDashboardStatus | undefined,
  browserHostname = globalThis.location?.hostname,
): string | undefined {
  if (!status?.available) return undefined
  try {
    const controller = new URL(`http://${status.controllerAddress}`)
    const target = new URL(status.url)
    if (controller.hostname === '0.0.0.0' || target.hostname === '0.0.0.0') {
      if (!browserHostname) return undefined
      controller.hostname = browserHostname
      target.hostname = browserHostname
    }
    const parameters = new URLSearchParams({
      hostname: controller.hostname,
      port: controller.port,
      secret: status.secret,
      disableUpgradeCore: '1',
    })
    target.hash = `/setup?${parameters.toString()}`
    return target.toString()
  } catch {
    return undefined
  }
}

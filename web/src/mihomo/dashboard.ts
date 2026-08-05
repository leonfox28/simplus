import type { MihomoDashboardStatus } from '@/api/client'

export function zashboardLaunchURL(status: MihomoDashboardStatus | undefined): string | undefined {
  if (!status?.available) return undefined
  try {
    const controller = new URL(`http://${status.controllerAddress}`)
    const target = new URL(status.url)
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

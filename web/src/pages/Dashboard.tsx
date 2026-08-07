import { useQuery } from '@tanstack/react-query'
import { Alert, Card, Statistic } from 'antd'
import { ApiClientError, displayApiError } from '@/api/errors'
import { getHardwareTopologyOptions, getSystemHealthOptions } from '@/api/generated/@tanstack/react-query.gen'
import { isHardwareTopologyResponse } from '@/api/hardwareSchema'
import { PageHeader } from '@/components/Page'

export default function Dashboard() {
  const health = useQuery(getSystemHealthOptions())
  const topology = useQuery({
    ...getHardwareTopologyOptions(),
    select: (value) => {
      if (!isHardwareTopologyResponse(value)) throw new ApiClientError({ kind: 'invalid-response', code: 'TOPOLOGY_RESPONSE_INVALID', retryable: false })
      return value
    },
  })
  const error = health.error ?? topology.error
  return <main className="page-content">
    <PageHeader title="概览" subtitle="查看系统、硬件和线路的当前运行概况" />
    {error && <Alert className="page-alert" type="error" showIcon title={displayApiError(error)} />}
    <div className="stat-grid">
      <Card loading={health.isPending}><Statistic title="系统状态" value={health.data?.status ?? '—'} /></Card>
      <Card loading={health.isPending}><Statistic title="后端" value={health.data?.backend ?? '—'} /></Card>
      <Card loading={topology.isPending}><Statistic title="模组" value={topology.data?.devices.length ?? 0} /></Card>
      <Card loading={topology.isPending}><Statistic title="线路" value={topology.data?.lines.length ?? 0} /></Card>
    </div>
  </main>
}

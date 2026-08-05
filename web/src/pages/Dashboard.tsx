import { PageContainer, StatisticCard } from '@ant-design/pro-components'
import { Alert, Grid, Spin } from 'antd'
import React, { useEffect, useState } from 'react'
import { getHardwareTopology, getSystemHealth, type HardwareTopologyResponse, type HealthResponse } from '@/api/client'

export default function Dashboard() {
  const screens = Grid.useBreakpoint()
  const compact = !screens.md
  const [health, setHealth] = useState<HealthResponse>()
  const [topology, setTopology] = useState<HardwareTopologyResponse>()
  const [error, setError] = useState('')
  useEffect(() => { Promise.all([getSystemHealth(), getHardwareTopology()]).then(([h,t]) => { setHealth(h); setTopology(t) }).catch(e => setError(String(e))) }, [])
  return <PageContainer title="概览" subTitle="查看系统、硬件和线路的当前运行概况">
    {error && <Alert type="error" message={error} />}
    {!health ? <Spin /> : <StatisticCard.Group direction={compact ? 'column' : 'row'}>
      <StatisticCard statistic={{ title: '系统状态', value: health.status }} />
      <StatisticCard statistic={{ title: '射频安全', value: health.rfSafety }} />
      <StatisticCard statistic={{ title: '后端', value: health.backend }} />
      <StatisticCard statistic={{ title: '模组', value: topology?.devices.length ?? 0 }} />
      <StatisticCard statistic={{ title: '线路', value: topology?.lines.length ?? 0 }} />
    </StatisticCard.Group>}
  </PageContainer>
}

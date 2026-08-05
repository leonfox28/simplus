import { PageContainer, ProCard, ProDescriptions } from '@ant-design/pro-components'
import { Alert, Button, Divider, Empty, Flex, Grid, Select, Space, Tag, Typography } from 'antd'
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  getHardwareTopology,
  activateVoWiFiLine,
  deactivateVoWiFiLine,
  listLineEgressBindings,
  listMihomoSubscriptionNodes,
  listMihomoSubscriptions,
  listVoWiFiLines,
  putLineEgressBinding,
  putSubscriptionProfileAccessMode,
  type AccessMode,
  type HardwareTopologyResponse,
  type LineEgressBinding,
  type VoWiFiLineState,
} from '@/api/client'

type CountryOption = { code: string; name: string }

const accessModeOptions = [
  { value: 'cellular-native', label: '原生蜂窝' },
  { value: 'host-vowifi-only', label: 'Host VoWiFi' },
  { value: 'hold-rf-off', label: '保持 RF Off' },
]

const readinessLabels: Record<LineEgressBinding['readinessReason'], string> = {
  READY: '出口可用',
  LINE_NOT_HOST_VOWIFI: '等待选择 Host VoWiFi',
  SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅',
  COUNTRY_NOT_FOUND: '当前订阅没有该国家',
  MIHOMO_NOT_RUNNING: 'Mihomo 未运行',
  MIHOMO_RESTART_REQUIRED: '等待 Mihomo 重启应用订阅',
}

const voWiFiStateLabels: Record<VoWiFiLineState['state'], string> = {
  stopped: '已停用', starting: '正在启动', connecting: '连接 ePDG', registering: '注册 IMS',
  online: '在线', reconnecting: '正在重连', stopping: '正在停用', failed: '运行失败',
}

const voWiFiReadinessLabels: Record<VoWiFiLineState['readinessCode'], string> = {
  READY: '可以激活', LINE_NOT_HOST_VOWIFI: '接入方式不是 Host VoWiFi', LINE_HARDWARE_NOT_READY: '模组、SIM 或 RF 状态不满足条件',
  SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅', COUNTRY_NOT_FOUND: '当前订阅没有该国家', MIHOMO_NOT_RUNNING: 'Mihomo 未运行',
  MIHOMO_RESTART_REQUIRED: '需要先重启 Mihomo 应用订阅',
}

export default function Lines() {
  const screens = Grid.useBreakpoint()
  const compact = !screens.md
  const [data, setData] = useState<HardwareTopologyResponse>()
  const [bindings, setBindings] = useState<LineEgressBinding[]>([])
  const [voWiFiStates, setVoWiFiStates] = useState<VoWiFiLineState[]>([])
  const [countries, setCountries] = useState<CountryOption[]>([])
  const [selectedSubscriptionName, setSelectedSubscriptionName] = useState('')
  const [accessDrafts, setAccessDrafts] = useState<Record<string, AccessMode>>({})
  const [egressDrafts, setEgressDrafts] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const hasAgentLines = data?.lines.some((line) => line.id.startsWith('agent-line-')) ?? false

  const load = useCallback(async () => {
    setError('')
    try {
      const [topology, bindingItems, subscriptions] = await Promise.all([
        getHardwareTopology(),
        listLineEgressBindings(),
        listMihomoSubscriptions(),
      ])
      const selected = subscriptions.find((subscription) => subscription.selected)
      const nodes = selected ? await listMihomoSubscriptionNodes(selected.id) : []
      const countryMap = new Map<string, string>()
      for (const node of nodes) {
        if (node.countryCode && node.countryName) countryMap.set(node.countryCode, node.countryName)
      }
      const countryItems = [...countryMap.entries()]
        .map(([code, name]) => ({ code, name }))
        .sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'))

      let observedVoWiFi: VoWiFiLineState[] = []
      if (topology.lines.some((line) => line.id.startsWith('agent-line-'))) {
        observedVoWiFi = await listVoWiFiLines()
      }

      setData(topology)
      setBindings(bindingItems)
      setVoWiFiStates(observedVoWiFi)
      setCountries(countryItems)
      setSelectedSubscriptionName(selected?.displayName ?? '')
      setAccessDrafts(Object.fromEntries(topology.subscriptionProfiles.map((profile) => [profile.id, profile.accessMode])))
      setEgressDrafts(Object.fromEntries(topology.lines.map((line) => {
        const binding = bindingItems.find((item) => item.lineId === line.id)
        return [line.id, binding?.mode === 'mihomo-country' ? `country:${binding.countryCode}` : 'direct']
      })))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const refreshVoWiFi = useCallback(async () => {
    if (!hasAgentLines) return
    try {
      setVoWiFiStates(await listVoWiFiLines())
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }, [hasAgentLines])

  useEffect(() => {
    if (!hasAgentLines) return undefined
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') void refreshVoWiFi()
    }, 5000)
    return () => window.clearInterval(timer)
  }, [hasAgentLines, refreshVoWiFi])

  const countryOptions = useMemo(() => countries.map((country) => ({
    value: `country:${country.code}`,
    label: `${country.name} (${country.code})`,
  })), [countries])

  const save = async (lineID: string, profileID: string) => {
    const accessMode = accessDrafts[profileID]
    if (!accessMode) return
    setBusy(lineID)
    setError('')
    try {
      await putSubscriptionProfileAccessMode(profileID, accessMode)
      if (accessMode === 'host-vowifi-only') {
        const egress = egressDrafts[lineID] ?? 'direct'
        if (egress === 'direct') {
          await putLineEgressBinding(lineID, { mode: 'direct', countryCode: '' })
        } else {
          await putLineEgressBinding(lineID, { mode: 'mihomo-country', countryCode: egress.slice('country:'.length) })
        }
      }
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  const setVoWiFiActive = async (lineID: string, active: boolean) => {
    setBusy(`vowifi:${lineID}`)
    setError('')
    try {
      const observed = active ? await activateVoWiFiLine(lineID) : await deactivateVoWiFiLine(lineID)
      setVoWiFiStates((current) => [...current.filter((item) => item.lineId !== lineID), observed])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  return (
    <PageContainer
      title="线路配置"
      subTitle="为每张 SIM 配置接入方式；Mihomo 国家出口与订阅配置相互独立"
      extra={<Button onClick={() => void load()}>刷新</Button>}
    >
      {error && <Alert type="error" showIcon message="线路配置失败" description={error} style={{ marginBottom: 16 }} />}
      {!data?.lines.length && <ProCard><Empty description="当前没有可配置的 Line" /></ProCard>}
      <div className="page-grid two">
        {data?.lines.map((line) => {
          const profile = data.subscriptionProfiles.find((item) => item.id === line.subscriptionProfileId)
          const group = data.resourceGroups.find((item) => item.id === line.resourceGroupId)
          const binding = bindings.find((item) => item.lineId === line.id)
          const voWiFi = voWiFiStates.find((item) => item.lineId === line.id)
          const accessMode = accessDrafts[line.subscriptionProfileId] ?? line.accessMode
          const egressValue = egressDrafts[line.id] ?? 'direct'
          const missingCountry = binding?.mode === 'mihomo-country' && !countries.some((country) => country.code === binding.countryCode)
          const options = [
            { value: 'direct', label: '直连' },
            ...countryOptions,
            ...(missingCountry ? [{ value: `country:${binding.countryCode}`, label: `${binding.countryCode}（当前订阅不可用）`, disabled: true }] : []),
          ]

          return (
            <ProCard
              key={line.id}
              title={line.displayName}
              extra={(
                <Space wrap>
                  <Tag color={line.state === 'ready' ? 'green' : 'orange'}>{line.state}</Tag>
                  <Tag>RF {line.rfSafety}</Tag>
                </Space>
              )}
            >
              <ProDescriptions
                column={compact ? 1 : 2}
                dataSource={{
                  profile: `${profile?.displayName ?? ''} · ${profile?.displayIdentityHint ?? ''}`,
                  resource: group?.displayName ?? line.resourceGroupId,
                  id: line.id,
                }}
                columns={[
                  { title: 'SIM / Profile', dataIndex: 'profile' },
                  { title: '资源组', dataIndex: 'resource' },
                  { title: 'Line ID', dataIndex: 'id', span: compact ? 1 : 2 },
                ]}
              />
              <Space wrap>
                {Object.entries(line.capabilities).filter(([, enabled]) => enabled).map(([name]) => <Tag key={name}>{name}</Tag>)}
              </Space>

              <Flex vertical gap="small" style={{ marginTop: 16 }}>
                <Typography.Text strong>接入方式</Typography.Text>
                <Select
                  value={accessMode}
                  options={accessModeOptions}
                  onChange={(value) => setAccessDrafts((current) => ({ ...current, [line.subscriptionProfileId]: value }))}
                />

                <Flex justify="space-between" align="center" wrap gap="small" style={{ marginTop: 8 }}>
                  <Typography.Text strong>Host VoWiFi 出口</Typography.Text>
                  {binding && (
                    <Tag color={binding.ready ? 'green' : 'orange'}>
                      {readinessLabels[binding.readinessReason]}
                    </Tag>
                  )}
                </Flex>
                <Select
                  value={egressValue}
                  disabled={accessMode !== 'host-vowifi-only'}
                  options={options}
                  onChange={(value) => setEgressDrafts((current) => ({ ...current, [line.id]: value }))}
                  placeholder="选择直连或国家出口"
                />
                <Typography.Text type="secondary">
                  {selectedSubscriptionName
                    ? `国家列表来自当前选择的订阅“${selectedSubscriptionName}”；修改 Line 不会重启 Mihomo。`
                    : '请先在 Mihomo 页面选择一个已转换的订阅。'}
                </Typography.Text>
                <Button type="primary" loading={busy === line.id} onClick={() => void save(line.id, line.subscriptionProfileId)}>
                  保存线路配置
                </Button>

                {line.id.startsWith('agent-line-') && (
                  <>
                    <Divider />
                    <Flex justify="space-between" align="center" wrap gap="small">
                      <div>
                        <Typography.Title level={5} style={{ margin: 0 }}>Host VoWiFi</Typography.Title>
                        <Typography.Text type="secondary">
                          {voWiFi ? voWiFiReadinessLabels[voWiFi.readinessCode] : '正在读取运行状态'}
                        </Typography.Text>
                      </div>
                      <Tag color={voWiFi?.online ? 'green' : voWiFi?.desiredActive ? 'orange' : 'default'}>
                        {voWiFi ? voWiFiStateLabels[voWiFi.state] : '未知'}
                      </Tag>
                    </Flex>
                    {voWiFi && (
                      <ProDescriptions
                        column={compact ? 1 : 2}
                        dataSource={{
                          egress: voWiFi.egressMode === 'direct' ? '直连' : `${voWiFi.countryName || voWiFi.countryCode} (${voWiFi.countryCode})`,
                          registered: voWiFi.registeredAt ? new Date(voWiFi.registeredAt).toLocaleString() : '—',
                          refresh: voWiFi.nextRefreshAt ? new Date(voWiFi.nextRefreshAt).toLocaleString() : '—',
                          error: voWiFi.lastErrorCode || '—',
                        }}
                        columns={[
                          { title: '当前出口', dataIndex: 'egress' },
                          { title: '最近注册', dataIndex: 'registered' },
                          { title: '下次刷新', dataIndex: 'refresh' },
                          { title: '运行错误', dataIndex: 'error' },
                        ]}
                      />
                    )}
                    <Space wrap>
                      <Button
                        type="primary"
                        disabled={!voWiFi?.eligible || voWiFi.desiredActive}
                        loading={busy === `vowifi:${line.id}` && !voWiFi?.desiredActive}
                        onClick={() => void setVoWiFiActive(line.id, true)}
                      >激活 VoWiFi</Button>
                      <Button
                        danger
                        disabled={!voWiFi?.desiredActive}
                        loading={busy === `vowifi:${line.id}` && !!voWiFi?.desiredActive}
                        onClick={() => void setVoWiFiActive(line.id, false)}
                      >停用 VoWiFi</Button>
                    </Space>
                    <Typography.Text type="secondary">
                      激活后系统会持续维护 ePDG、IPsec 与 IMS 注册；网络短时故障会自动重连，停用会清理该 Line 的全部临时网络状态。
                    </Typography.Text>
                  </>
                )}
              </Flex>
            </ProCard>
          )
        })}
      </div>
    </PageContainer>
  )
}

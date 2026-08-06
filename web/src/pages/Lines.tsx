import { PageContainer, ProCard, ProDescriptions } from '@ant-design/pro-components'
import { Alert, Button, Divider, Empty, Flex, Grid, Input, Modal, Radio, Select, Space, Tag, Typography } from 'antd'
import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  activateVoWiFiLine,
  addManagedLine,
  deactivateVoWiFiLine,
  listLineCandidates,
  listLineEgressBindings,
  listManagedLines,
  listMihomoSubscriptionNodes,
  listMihomoSubscriptions,
  listVoWiFiLines,
  putLineEgressBinding,
  updateManagedLine,
  type AccessMode,
  type LineCandidate,
  type LineEgressBinding,
  type ManagedLine,
  type VoWiFiLineState,
} from '@/api/client'

type CountryOption = { code: string; name: string }

const accessModeOptions: { value: AccessMode; label: string }[] = [
  { value: 'cellular-native', label: '原生蜂窝' },
  { value: 'host-vowifi-only', label: 'Host VoWiFi' },
  { value: 'hold-rf-off', label: '保持 RF Off' },
]

const lineStateLabels: Record<ManagedLine['state'], string> = {
  ready: '就绪', 'modem-offline': '模组离线', 'sim-unavailable': 'SIM / Profile 不可用',
}

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
  READY: '可以激活',
  LINE_NOT_HOST_VOWIFI: '接入方式不是 Host VoWiFi',
  LINE_HARDWARE_NOT_READY: '模组或 SIM / Profile 尚未就绪',
  SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅',
  COUNTRY_NOT_FOUND: '当前订阅没有该国家',
  MIHOMO_NOT_RUNNING: 'Mihomo 未运行',
  MIHOMO_RESTART_REQUIRED: '需要先重启 Mihomo 应用订阅',
}

function capabilityTags(capabilities: ManagedLine['capabilities']) {
  return Object.entries(capabilities)
    .filter(([, enabled]) => enabled)
    .map(([name]) => <Tag key={name}>{name}</Tag>)
}

export default function Lines() {
  const screens = Grid.useBreakpoint()
  const compact = !screens.md
  const [lines, setLines] = useState<ManagedLine[]>([])
  const [bindings, setBindings] = useState<LineEgressBinding[]>([])
  const [voWiFiStates, setVoWiFiStates] = useState<VoWiFiLineState[]>([])
  const [countries, setCountries] = useState<CountryOption[]>([])
  const [selectedSubscriptionName, setSelectedSubscriptionName] = useState('')
  const [accessDrafts, setAccessDrafts] = useState<Record<string, AccessMode>>({})
  const [egressDrafts, setEgressDrafts] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const [addOpen, setAddOpen] = useState(false)
  const [candidates, setCandidates] = useState<LineCandidate[]>([])
  const [candidateID, setCandidateID] = useState('')
  const [newName, setNewName] = useState('')
  const [newMode, setNewMode] = useState<AccessMode>('host-vowifi-only')
  const [candidateLoading, setCandidateLoading] = useState(false)

  const [editLine, setEditLine] = useState<ManagedLine>()
  const [editName, setEditName] = useState('')
  const [editMode, setEditMode] = useState<AccessMode>('host-vowifi-only')

  const hasHostVoWiFiLines = lines.some((line) => line.accessMode === 'host-vowifi-only')

  const load = useCallback(async () => {
    setError('')
    try {
      const [lineItems, bindingItems, subscriptions] = await Promise.all([
        listManagedLines(), listLineEgressBindings(), listMihomoSubscriptions(),
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
      const observedVoWiFi = lineItems.some((line) => line.accessMode === 'host-vowifi-only')
        ? await listVoWiFiLines()
        : []

      setLines(lineItems)
      setBindings(bindingItems)
      setVoWiFiStates(observedVoWiFi)
      setCountries(countryItems)
      setSelectedSubscriptionName(selected?.displayName ?? '')
      setAccessDrafts(Object.fromEntries(lineItems.map((line) => [line.id, line.accessMode])))
      setEgressDrafts(Object.fromEntries(lineItems.map((line) => {
        const binding = bindingItems.find((item) => item.lineId === line.id)
        return [line.id, binding?.mode === 'mihomo-country' ? `country:${binding.countryCode}` : 'direct']
      })))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const refreshVoWiFi = useCallback(async () => {
    if (!hasHostVoWiFiLines) return
    try {
      setVoWiFiStates(await listVoWiFiLines())
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }, [hasHostVoWiFiLines])

  useEffect(() => {
    if (!hasHostVoWiFiLines) return undefined
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') void refreshVoWiFi()
    }, 5000)
    return () => window.clearInterval(timer)
  }, [hasHostVoWiFiLines, refreshVoWiFi])

  const countryOptions = useMemo(() => countries.map((country) => ({
    value: `country:${country.code}`, label: `${country.name} (${country.code})`,
  })), [countries])

  const openAdd = async () => {
    setAddOpen(true)
    setCandidateLoading(true)
    setCandidateID('')
    setNewName('')
    setNewMode('host-vowifi-only')
    setError('')
    try {
      setCandidates(await listLineCandidates())
    } catch (cause) {
      setCandidates([])
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setCandidateLoading(false)
    }
  }

  const chooseCandidate = (value: string) => {
    setCandidateID(value)
    const selected = candidates.find((candidate) => candidate.candidateId === value)
    if (selected && !newName) setNewName(`${selected.managedModemDisplayName} · ${selected.subscriptionDisplayHint}`)
  }

  const createLine = async () => {
    if (!candidateID || !newName.trim()) return
    setBusy('add')
    setError('')
    try {
      await addManagedLine({ candidateId: candidateID, displayName: newName.trim(), accessMode: newMode })
      setAddOpen(false)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  const openEdit = (line: ManagedLine) => {
    setEditLine(line)
    setEditName(line.displayName)
    setEditMode(line.accessMode)
  }

  const saveEdit = async () => {
    if (!editLine || !editName.trim()) return
    setBusy(`edit:${editLine.id}`)
    setError('')
    try {
      await updateManagedLine(editLine.id, { displayName: editName.trim(), accessMode: editMode })
      setEditLine(undefined)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  const saveRuntimeConfig = async (line: ManagedLine) => {
    const accessMode = accessDrafts[line.id] ?? line.accessMode
    setBusy(line.id)
    setError('')
    try {
      if (accessMode !== line.accessMode) {
        await updateManagedLine(line.id, { displayName: line.displayName, accessMode })
      }
      if (accessMode === 'host-vowifi-only') {
        const egress = egressDrafts[line.id] ?? 'direct'
        if (egress === 'direct') {
          await putLineEgressBinding(line.id, { mode: 'direct', countryCode: '' })
        } else {
          await putLineEgressBinding(line.id, { mode: 'mihomo-country', countryCode: egress.slice('country:'.length) })
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
      subTitle="线路独立绑定已添加模组与 SIM / Profile；硬件路径和型号细节不会进入线路配置"
      extra={<Space><Button onClick={() => void load()}>刷新</Button><Button type="primary" onClick={() => void openAdd()}>添加线路</Button></Space>}
    >
      {error && <Alert type="error" showIcon message="线路配置失败" description={error} style={{ marginBottom: 16 }} />}
      {!lines.length && <ProCard><Empty description="尚未添加线路；请先添加模组，再添加线路" /></ProCard>}
      <div className="page-grid two">
        {lines.map((line) => {
          const binding = bindings.find((item) => item.lineId === line.id)
          const voWiFi = voWiFiStates.find((item) => item.lineId === line.id)
          const accessMode = accessDrafts[line.id] ?? line.accessMode
          const egressValue = egressDrafts[line.id] ?? 'direct'
          const missingCountry = binding?.mode === 'mihomo-country' && !countries.some((country) => country.code === binding.countryCode)
          const options = [
            { value: 'direct', label: '直连' }, ...countryOptions,
            ...(missingCountry ? [{ value: `country:${binding.countryCode}`, label: `${binding.countryCode}（当前订阅不可用）`, disabled: true }] : []),
          ]
          return (
            <ProCard
              key={line.id}
              title={line.displayName}
              extra={<Space><Tag color={line.state === 'ready' ? 'green' : 'orange'}>{lineStateLabels[line.state]}</Tag><Button size="small" onClick={() => openEdit(line)}>编辑</Button></Space>}
            >
              <ProDescriptions
                column={compact ? 1 : 2}
                dataSource={{ modem: line.managedModemDisplayName, sim: line.subscriptionDisplayHint, id: line.id }}
                columns={[
                  { title: '模组', dataIndex: 'modem' }, { title: 'SIM / Profile', dataIndex: 'sim' },
                  { title: 'Line ID', dataIndex: 'id', span: compact ? 1 : 2 },
                ]}
              />
              <Space wrap>{capabilityTags(line.capabilities)}</Space>
              <Flex vertical gap="small" style={{ marginTop: 16 }}>
                <Typography.Text strong>接入方式</Typography.Text>
                <Select value={accessMode} options={accessModeOptions} onChange={(value) => setAccessDrafts((current) => ({ ...current, [line.id]: value }))} />
                <Flex justify="space-between" align="center" wrap gap="small" style={{ marginTop: 8 }}>
                  <Typography.Text strong>Host VoWiFi 出口</Typography.Text>
                  {binding && <Tag color={binding.ready ? 'green' : 'orange'}>{readinessLabels[binding.readinessReason]}</Tag>}
                </Flex>
                <Select
                  value={egressValue} disabled={accessMode !== 'host-vowifi-only'} options={options}
                  onChange={(value) => setEgressDrafts((current) => ({ ...current, [line.id]: value }))}
                  placeholder="选择直连或国家出口"
                />
                <Typography.Text type="secondary">
                  {selectedSubscriptionName ? `国家列表来自当前订阅“${selectedSubscriptionName}”；修改线路不会重写 Mihomo 配置。` : '请先在 Mihomo 页面选择一个已转换的订阅。'}
                </Typography.Text>
                <Button type="primary" loading={busy === line.id} onClick={() => void saveRuntimeConfig(line)}>保存线路配置</Button>

                {line.accessMode === 'host-vowifi-only' && (
                  <>
                    <Divider />
                    <Flex justify="space-between" align="center" wrap gap="small">
                      <div><Typography.Title level={5} style={{ margin: 0 }}>Host VoWiFi</Typography.Title><Typography.Text type="secondary">{voWiFi ? voWiFiReadinessLabels[voWiFi.readinessCode] : '正在读取运行状态'}</Typography.Text></div>
                      <Tag color={voWiFi?.online ? 'green' : voWiFi?.desiredActive ? 'orange' : 'default'}>{voWiFi ? voWiFiStateLabels[voWiFi.state] : '未知'}</Tag>
                    </Flex>
                    {voWiFi && <ProDescriptions
                      column={compact ? 1 : 2}
                      dataSource={{
                        egress: voWiFi.egressMode === 'direct' ? '直连' : `${voWiFi.countryName || voWiFi.countryCode} (${voWiFi.countryCode})`,
                        registered: voWiFi.registeredAt ? new Date(voWiFi.registeredAt).toLocaleString() : '—',
                        refresh: voWiFi.nextRefreshAt ? new Date(voWiFi.nextRefreshAt).toLocaleString() : '—', error: voWiFi.lastErrorCode || '—',
                      }}
                      columns={[
                        { title: '当前出口', dataIndex: 'egress' }, { title: '最近注册', dataIndex: 'registered' },
                        { title: '下次刷新', dataIndex: 'refresh' }, { title: '运行错误', dataIndex: 'error' },
                      ]}
                    />}
                    <Space wrap>
                      <Button type="primary" disabled={!voWiFi?.eligible || voWiFi.desiredActive} loading={busy === `vowifi:${line.id}` && !voWiFi?.desiredActive} onClick={() => void setVoWiFiActive(line.id, true)}>激活 VoWiFi</Button>
                      <Button danger disabled={!voWiFi?.desiredActive} loading={busy === `vowifi:${line.id}` && !!voWiFi?.desiredActive} onClick={() => void setVoWiFiActive(line.id, false)}>停用 VoWiFi</Button>
                    </Space>
                  </>
                )}
              </Flex>
            </ProCard>
          )
        })}
      </div>

      <Modal title="添加线路" open={addOpen} onCancel={() => setAddOpen(false)} onOk={() => void createLine()} confirmLoading={busy === 'add'} okButtonProps={{ disabled: !candidateID || !newName.trim() }} destroyOnHidden>
        {candidateLoading ? <Typography.Text type="secondary">正在扫描可用 SIM / Profile…</Typography.Text> : candidates.length === 0 ? <Empty description="没有可添加的线路候选；请确认模组已添加且 SIM 已就绪" /> : (
          <Flex vertical gap="middle">
            <Radio.Group value={candidateID} onChange={(event) => chooseCandidate(event.target.value)} style={{ width: '100%' }}>
              <Flex vertical gap="small">
                {candidates.map((candidate) => <Radio key={candidate.candidateId} value={candidate.candidateId} disabled={!candidate.addable}>
                  <Space wrap><Typography.Text strong>{candidate.managedModemDisplayName}</Typography.Text><Typography.Text>{candidate.subscriptionDisplayHint}</Typography.Text></Space>
                </Radio>)}
              </Flex>
            </Radio.Group>
            <div><Typography.Text strong>线路名称</Typography.Text><Input value={newName} maxLength={120} onChange={(event) => setNewName(event.target.value)} placeholder="例如 VOXI 主线路" style={{ marginTop: 8 }} /></div>
            <div><Typography.Text strong>接入方式</Typography.Text><Select value={newMode} options={accessModeOptions} onChange={setNewMode} style={{ width: '100%', marginTop: 8 }} /></div>
          </Flex>
        )}
      </Modal>

      <Modal title="编辑线路" open={!!editLine} onCancel={() => setEditLine(undefined)} onOk={() => void saveEdit()} confirmLoading={busy.startsWith('edit:')} okButtonProps={{ disabled: !editName.trim() }} destroyOnHidden>
        <Flex vertical gap="middle">
          <div><Typography.Text strong>线路名称</Typography.Text><Input value={editName} maxLength={120} onChange={(event) => setEditName(event.target.value)} style={{ marginTop: 8 }} /></div>
          <div><Typography.Text strong>接入方式</Typography.Text><Select value={editMode} options={accessModeOptions} onChange={setEditMode} style={{ width: '100%', marginTop: 8 }} /></div>
          <Typography.Text type="secondary">编辑不会更改模组或 SIM / Profile 绑定。</Typography.Text>
        </Flex>
      </Modal>
    </PageContainer>
  )
}

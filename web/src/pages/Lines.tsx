import { PageContainer, ProCard, ProDescriptions, ProTable, type ProColumns } from '@ant-design/pro-components'
import { Alert, Button, Divider, Drawer, Empty, Flex, Grid, Input, Modal, Select, Space, Tag, Typography } from 'antd'
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
  type LineCandidate,
  type LineEgressBinding,
  type ManagedLine,
  type VoWiFiLineState,
} from '@/api/client'

type CountryOption = { code: string; name: string }
type LineRow = ManagedLine & { binding?: LineEgressBinding; voWiFi?: VoWiFiLineState }

const lineStateLabels: Record<ManagedLine['state'], string> = {
  ready: '就绪',
  'modem-offline': '模组离线',
  'sim-unavailable': 'SIM / Profile 不可用',
}

const candidateReasonLabels: Record<LineCandidate['readinessReason'], string> = {
  READY: '可添加',
  MODEM_OFFLINE: '模组离线',
  SIM_ABSENT: '未插入 SIM',
  SIM_UNAVAILABLE: 'SIM / Profile 不可用',
  ALREADY_ADDED: '已添加',
  BINDING_CONFLICT: '绑定身份冲突',
}

const readinessLabels: Record<LineEgressBinding['readinessReason'], string> = {
  READY: '出口可用',
  EGRESS_NOT_CONFIGURED: '尚未配置出口',
  LINE_VOWIFI_UNSUPPORTED: '线路不支持 Host VoWiFi',
  SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅',
  COUNTRY_NOT_FOUND: '当前订阅没有该国家',
  MIHOMO_NOT_RUNNING: 'Mihomo 未运行',
  MIHOMO_RESTART_REQUIRED: '等待 Mihomo 重启应用订阅',
}

const voWiFiStateLabels: Record<VoWiFiLineState['state'], string> = {
  stopped: '已停用',
  starting: '正在启动',
  connecting: '连接 ePDG',
  registering: '注册 IMS',
  online: '在线',
  reconnecting: '正在重连',
  stopping: '正在停用',
  failed: '运行失败',
}

const voWiFiReadinessLabels: Record<VoWiFiLineState['readinessCode'], string> = {
  READY: '可以激活',
  EGRESS_NOT_CONFIGURED: '请先明确配置出口',
  LINE_VOWIFI_UNSUPPORTED: '线路不支持 Host VoWiFi',
  LINE_HARDWARE_NOT_READY: '模组或 SIM / Profile 尚未就绪',
  SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅',
  COUNTRY_NOT_FOUND: '当前订阅没有该国家',
  MIHOMO_NOT_RUNNING: 'Mihomo 未运行',
  MIHOMO_RESTART_REQUIRED: '需要先重启 Mihomo 应用订阅',
}

function egressLabel(binding?: LineEgressBinding) {
  if (!binding || binding.mode === 'unconfigured') return '未配置'
  if (binding.mode === 'direct') return '直连'
  return `${binding.countryName || binding.countryCode} (${binding.countryCode})`
}

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

function canRequestVoWiFiActivation(state?: VoWiFiLineState) {
  return Boolean(state && !state.desiredActive && (
    state.eligible || state.readinessCode === 'MIHOMO_NOT_RUNNING' || state.readinessCode === 'MIHOMO_RESTART_REQUIRED'
  ))
}

export default function Lines() {
  const screens = Grid.useBreakpoint()
  const compact = !screens.md
  const [lines, setLines] = useState<ManagedLine[]>([])
  const [bindings, setBindings] = useState<LineEgressBinding[]>([])
  const [voWiFiStates, setVoWiFiStates] = useState<VoWiFiLineState[]>([])
  const [countries, setCountries] = useState<CountryOption[]>([])
  const [selectedSubscriptionName, setSelectedSubscriptionName] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [voWiFiError, setVoWiFiError] = useState('')

  const [addOpen, setAddOpen] = useState(false)
  const [candidates, setCandidates] = useState<LineCandidate[]>([])
  const [candidateID, setCandidateID] = useState('')
  const [newName, setNewName] = useState('')
  const [candidateLoading, setCandidateLoading] = useState(false)
  const [addError, setAddError] = useState('')

  const [drawerLineID, setDrawerLineID] = useState('')
  const [nameDraft, setNameDraft] = useState('')
  const [egressDraft, setEgressDraft] = useState<string>()

  const load = useCallback(async () => {
    setError('')
    setVoWiFiError('')
    setLoading(true)
    try {
      const [lineItems, bindingItems, subscriptions] = await Promise.all([
        listManagedLines(),
        listLineEgressBindings(),
        listMihomoSubscriptions(),
      ])
      let runtimeStates: VoWiFiLineState[] = []
      try {
        runtimeStates = await listVoWiFiLines()
      } catch (cause) {
        setVoWiFiError(errorText(cause))
      }
      const selected = subscriptions.find((subscription) => subscription.selected)
      const nodes = selected ? await listMihomoSubscriptionNodes(selected.id) : []
      const countryMap = new Map<string, string>()
      for (const node of nodes) {
        if (node.countryCode && node.countryName) countryMap.set(node.countryCode, node.countryName)
      }
      setLines(lineItems)
      setBindings(bindingItems)
      setVoWiFiStates(runtimeStates)
      setCountries([...countryMap.entries()]
        .map(([code, name]) => ({ code, name }))
        .sort((left, right) => left.name.localeCompare(right.name, 'zh-CN')))
      setSelectedSubscriptionName(selected?.displayName ?? '')
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  useEffect(() => {
    if (!lines.some((line) => line.capabilities.hostVoWifiAuth) || voWiFiError === 'VOWIFI_UNAVAILABLE') return undefined
    const timer = window.setInterval(() => {
      if (document.visibilityState !== 'visible') return
      void listVoWiFiLines()
        .then((states) => { setVoWiFiStates(states); setVoWiFiError('') })
        .catch((cause) => setVoWiFiError(errorText(cause)))
    }, 5000)
    return () => window.clearInterval(timer)
  }, [lines, voWiFiError])

  const rows = useMemo<LineRow[]>(() => lines.map((line) => ({
    ...line,
    binding: bindings.find((binding) => binding.lineId === line.id),
    voWiFi: voWiFiStates.find((state) => state.lineId === line.id),
  })), [bindings, lines, voWiFiStates])

  const activeLine = rows.find((line) => line.id === drawerLineID)

  const countryOptions = useMemo(() => countries.map((country) => ({
    value: `country:${country.code}`,
    label: `${country.name} (${country.code})`,
  })), [countries])
  const activeCountryMissing = activeLine?.binding?.mode === 'mihomo-country' &&
    !countries.some((country) => country.code === activeLine.binding?.countryCode)
  const drawerEgressOptions = [
    { value: 'direct', label: '直连' },
    ...countryOptions,
    ...(activeCountryMissing && activeLine?.binding?.countryCode
      ? [{ value: `country:${activeLine.binding.countryCode}`, label: `${activeLine.binding.countryCode}（当前订阅不可用）`, disabled: true }]
      : []),
  ]
  const egressDraftValid = egressDraft === 'direct' || Boolean(
    egressDraft?.startsWith('country:') && countries.some((country) => country.code === egressDraft.slice('country:'.length))
  )

  const openDrawer = (line: LineRow) => {
    setDrawerLineID(line.id)
    setNameDraft(line.displayName)
    setEgressDraft(line.binding?.mode === 'direct'
      ? 'direct'
      : line.binding?.mode === 'mihomo-country' ? `country:${line.binding.countryCode}` : undefined)
    setError('')
  }

  const openAdd = async () => {
    setAddOpen(true)
    setCandidateLoading(true)
    setCandidateID('')
    setNewName('')
    setAddError('')
    try {
      setCandidates(await listLineCandidates())
    } catch (cause) {
      setCandidates([])
      setAddError(errorText(cause))
    } finally {
      setCandidateLoading(false)
    }
  }

  const chooseCandidate = (selectedID: string) => {
    setCandidateID(selectedID)
    const selected = candidates.find((candidate) => candidate.candidateId === selectedID)
    if (selected) setNewName(`${selected.managedModemModel || selected.managedModemDisplayName} · ${selected.subscriptionDisplayHint}`)
  }

  const createLine = async () => {
    if (!candidateID || !newName.trim()) return
    setBusy('add')
    setAddError('')
    try {
      const created = await addManagedLine({ candidateId: candidateID, displayName: newName.trim() })
      setAddOpen(false)
      await load()
      setDrawerLineID(created.id)
      setNameDraft(created.displayName)
      setEgressDraft(undefined)
    } catch (cause) {
      setAddError(errorText(cause))
    } finally {
      setBusy('')
    }
  }

  const saveName = async () => {
    if (!activeLine || !nameDraft.trim()) return
    setBusy(`name:${activeLine.id}`)
    setError('')
    try {
      await updateManagedLine(activeLine.id, { displayName: nameDraft.trim() })
      await load()
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setBusy('')
    }
  }

  const saveEgress = async () => {
    if (!activeLine || !egressDraft) return
    setBusy(`egress:${activeLine.id}`)
    setError('')
    try {
      if (egressDraft === 'direct') {
        await putLineEgressBinding(activeLine.id, { mode: 'direct', countryCode: '' })
      } else {
        await putLineEgressBinding(activeLine.id, {
          mode: 'mihomo-country',
          countryCode: egressDraft.slice('country:'.length),
        })
      }
      await load()
    } catch (cause) {
      setError(errorText(cause))
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
      setError(errorText(cause))
    } finally {
      setBusy('')
    }
  }

  const columns: ProColumns<LineRow>[] = [
    { title: '名称', dataIndex: 'displayName', ellipsis: true },
    {
      title: '模组',
      render: (_, line) => <Flex vertical>
        <Typography.Text>{line.managedModemModel || '读取失败'}</Typography.Text>
        <Typography.Text type="secondary">{line.managedModemSerialNumber || '读取失败'}</Typography.Text>
      </Flex>,
    },
    { title: 'SIM / Profile', dataIndex: 'subscriptionDisplayHint', ellipsis: true },
    {
      title: '手机号',
      render: (_, line) => line.voWiFi?.phoneNumber || <Typography.Text type="secondary">未获取</Typography.Text>,
    },
    {
      title: '线路状态',
      render: (_, line) => <Tag color={line.state === 'ready' ? 'green' : 'orange'}>{lineStateLabels[line.state]}</Tag>,
    },
    {
      title: 'VoWiFi 状态',
      render: (_, line) => line.capabilities.hostVoWifiAuth
        ? <Tag color={line.voWiFi?.online ? 'green' : line.voWiFi?.desiredActive ? 'orange' : 'default'}>
            {line.voWiFi ? voWiFiStateLabels[line.voWiFi.state] : '读取中'}
          </Tag>
        : <Typography.Text type="secondary">不支持</Typography.Text>,
    },
    {
      title: '出口',
      render: (_, line) => <Flex vertical>
        <Typography.Text>{egressLabel(line.binding)}</Typography.Text>
        {line.binding && line.binding.readinessReason !== 'READY' &&
          <Typography.Text type="secondary">{readinessLabels[line.binding.readinessReason]}</Typography.Text>}
      </Flex>,
    },
    { title: '操作', valueType: 'option', render: (_, line) => <Button type="link" onClick={() => openDrawer(line)}>配置</Button> },
  ]

  const candidateColumns: ProColumns<LineCandidate>[] = [
    {
      title: 'ICCID',
      dataIndex: 'subscriptionDisplayHint',
      ellipsis: true,
      render: (_, candidate) => candidate.subscriptionDisplayHint || '—',
    },
    {
      title: '归属运营商',
      render: (_, candidate) => <Flex vertical>
        <Typography.Text>{candidate.homeOperatorName || candidate.homeOperatorCode || '未知'}</Typography.Text>
        {candidate.homeOperatorName && candidate.homeOperatorCode
          ? <Typography.Text type="secondary">{candidate.homeOperatorCode}</Typography.Text>
          : null}
      </Flex>,
    },
    {
      title: '模组',
      render: (_, candidate) => candidate.managedModemModel || '读取失败',
    },
    { title: '序列号', dataIndex: 'managedModemSerialNumber', ellipsis: true, render: (_, candidate) => candidate.managedModemSerialNumber || '读取失败' },
    {
      title: '状态',
      render: (_, candidate) => <Tag color={candidate.addable ? 'green' : 'default'}>
        {candidateReasonLabels[candidate.readinessReason]}
      </Tag>,
    },
  ]

  return <PageContainer
    title="线路配置"
    subTitle="线路只绑定已添加模组与当前 SIM / Profile；通信路径在配置中独立管理"
    extra={<Space><Button onClick={() => void load()}>刷新</Button><Button type="primary" onClick={() => void openAdd()}>添加线路</Button></Space>}
  >
    {error && <Alert type="error" showIcon message="线路配置失败" description={error} style={{ marginBottom: 16 }} />}
    {voWiFiError && <Alert type="warning" showIcon message="Host VoWiFi 运行状态暂不可用" description={voWiFiError} style={{ marginBottom: 16 }} />}
    <ProTable<LineRow>
      rowKey="id"
      search={false}
      options={false}
      pagination={false}
      loading={loading}
      dataSource={rows}
      columns={columns}
      scroll={{ x: 'max-content' }}
      locale={{ emptyText: <Empty description="尚未添加线路；请先添加模组，再添加线路" /> }}
    />

    <Modal
      title="添加线路"
      open={addOpen}
      onCancel={() => setAddOpen(false)}
      onOk={() => void createLine()}
      confirmLoading={busy === 'add'}
      okButtonProps={{ disabled: !candidateID || !newName.trim() }}
      destroyOnHidden
      width={compact ? '96%' : 'min(92vw, 72rem)'}
      styles={{ body: { maxHeight: '72dvh', overflowY: 'auto' } }}
    >
      {addError && <Alert type="error" showIcon message="无法添加线路" description={addError} style={{ marginBottom: 16 }} />}
      <Typography.Paragraph type="secondary">
        每次只能选择一个线路候选；如需添加多条线路，请分别完成添加。
      </Typography.Paragraph>
      <ProTable<LineCandidate>
        rowKey="candidateId"
        search={false}
        options={false}
        pagination={false}
        loading={candidateLoading}
        dataSource={candidates}
        columns={candidateColumns}
        tableAlertRender={false}
        scroll={{ x: 'max-content' }}
        rowSelection={{
          type: 'radio',
          selectedRowKeys: candidateID ? [candidateID] : [],
          onChange: (keys) => chooseCandidate(String(keys[0] ?? '')),
          getCheckboxProps: (candidate) => ({ disabled: !candidate.addable, 'aria-label': candidateReasonLabels[candidate.readinessReason] }),
        }}
        onRow={(candidate) => ({
          onClick: () => { if (candidate.addable) chooseCandidate(candidate.candidateId) },
          style: { cursor: candidate.addable ? 'pointer' : 'not-allowed' },
        })}
        locale={{ emptyText: <Empty description="没有可观察到的线路候选；请确认已添加模组" /> }}
      />
      <Divider />
      <Flex vertical gap="small">
        <Typography.Text strong>线路名称</Typography.Text>
        <Input
          value={newName}
          maxLength={120}
          onChange={(event) => setNewName(event.target.value)}
          placeholder="选择候选后可修改名称"
          disabled={!candidateID}
        />
        <Typography.Text type="secondary">名称允许重复；系统使用随机 Line ID 识别线路。</Typography.Text>
      </Flex>
    </Modal>

    <Drawer
      title={activeLine ? `配置线路 · ${activeLine.displayName}` : '配置线路'}
      open={Boolean(activeLine)}
      onClose={() => setDrawerLineID('')}
      placement={compact ? 'bottom' : 'right'}
      width={compact ? undefined : 'min(92vw, 44rem)'}
      height={compact ? '88dvh' : undefined}
      autoFocus={false}
      destroyOnHidden
    >
      {!activeLine ? null : <Flex vertical gap="middle">
        <ProDescriptions
          column={1}
          dataSource={{
            modem: `${activeLine.managedModemModel || '读取失败'} · ${activeLine.managedModemSerialNumber || '读取失败'}`,
            sim: activeLine.subscriptionDisplayHint,
            lineID: activeLine.id,
          }}
          columns={[
            { title: '模组', dataIndex: 'modem' },
            { title: 'SIM / Profile', dataIndex: 'sim' },
            { title: 'Line ID', dataIndex: 'lineID', copyable: true },
          ]}
        />

        <ProCard title="名称">
          <Flex vertical gap="small">
            <Input value={nameDraft} maxLength={120} onChange={(event) => setNameDraft(event.target.value)} />
            <Flex justify="flex-end"><Button
              type="primary"
              loading={busy === `name:${activeLine.id}`}
              disabled={!nameDraft.trim() || nameDraft.trim() === activeLine.displayName}
              onClick={() => void saveName()}
            >保存名称</Button></Flex>
          </Flex>
        </ProCard>

        <ProCard title="Host VoWiFi 出口" extra={
          <Tag color={activeLine.binding?.ready ? 'green' : 'orange'}>
            {activeLine.binding ? readinessLabels[activeLine.binding.readinessReason] : '尚未配置出口'}
          </Tag>
        }>
          <Flex vertical gap="small">
            <Select
              value={egressDraft}
              placeholder="请选择明确出口"
              options={drawerEgressOptions}
              onChange={setEgressDraft}
              disabled={!activeLine.capabilities.hostVoWifiAuth}
            />
            <Typography.Text type="secondary">
              {!activeLine.capabilities.hostVoWifiAuth
                ? '该线路不具备 Host VoWiFi 鉴权能力，不能配置专用出口。'
                : selectedSubscriptionName
                ? `国家列表来自当前订阅“${selectedSubscriptionName}”；保存线路出口不会重写 Mihomo 配置。`
                : '国家出口需要先在 Mihomo 页面选择一个已转换的订阅；也可明确选择直连。'}
            </Typography.Text>
            <Flex justify="flex-end"><Button
              type="primary"
              loading={busy === `egress:${activeLine.id}`}
              disabled={!activeLine.capabilities.hostVoWifiAuth || !egressDraftValid}
              onClick={() => void saveEgress()}
            >保存出口</Button></Flex>
          </Flex>
        </ProCard>

        <ProCard title="Host VoWiFi">
          {!activeLine.capabilities.hostVoWifiAuth
            ? <Alert type="info" showIcon message="该线路不具备 Host VoWiFi 鉴权能力" />
            : <Flex vertical gap="middle">
                <Flex justify="space-between" align="center" wrap gap="small">
                  <div>
                    <Typography.Text strong>{activeLine.voWiFi ? voWiFiStateLabels[activeLine.voWiFi.state] : '正在读取运行状态'}</Typography.Text><br />
                    <Typography.Text type="secondary">
                      {activeLine.voWiFi ? voWiFiReadinessLabels[activeLine.voWiFi.readinessCode] : '请稍候'}
                    </Typography.Text>
                  </div>
                  <Tag color={activeLine.voWiFi?.online ? 'green' : activeLine.voWiFi?.desiredActive ? 'orange' : 'default'}>
                    {activeLine.voWiFi?.online ? '在线' : activeLine.voWiFi?.desiredActive ? '已启用' : '未启用'}
                  </Tag>
                </Flex>
                <ProDescriptions
                  column={1}
                  dataSource={{
                    egress: egressLabel(activeLine.binding),
                    registered: activeLine.voWiFi?.registeredAt ? new Date(activeLine.voWiFi.registeredAt).toLocaleString() : '—',
                    refresh: activeLine.voWiFi?.nextRefreshAt ? new Date(activeLine.voWiFi.nextRefreshAt).toLocaleString() : '—',
                    error: activeLine.voWiFi?.lastErrorCode || '—',
                  }}
                  columns={[
                    { title: '当前出口', dataIndex: 'egress' },
                    { title: '最近注册', dataIndex: 'registered' },
                    { title: '下次刷新', dataIndex: 'refresh' },
                    { title: '运行错误', dataIndex: 'error' },
                  ]}
                />
                <Space wrap>
                  <Button
                    type="primary"
                    disabled={!canRequestVoWiFiActivation(activeLine.voWiFi)}
                    loading={busy === `vowifi:${activeLine.id}` && !activeLine.voWiFi?.desiredActive}
                    onClick={() => void setVoWiFiActive(activeLine.id, true)}
                  >激活 VoWiFi</Button>
                  <Button
                    danger
                    disabled={!activeLine.voWiFi?.desiredActive}
                    loading={busy === `vowifi:${activeLine.id}` && Boolean(activeLine.voWiFi?.desiredActive)}
                    onClick={() => void setVoWiFiActive(activeLine.id, false)}
                  >停用 VoWiFi</Button>
                </Space>
              </Flex>}
        </ProCard>
      </Flex>}
    </Drawer>
  </PageContainer>
}

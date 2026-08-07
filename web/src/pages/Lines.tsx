import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Divider, Drawer, Empty, Flex, Grid, Input, Modal, Radio, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useMemo, useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  activateVoWiFiLineMutation,
  addManagedLineMutation,
  deactivateVoWiFiLineMutation,
  listLineCandidatesOptions,
  listLineCandidatesQueryKey,
  listLineEgressBindingsOptions,
  listLineEgressBindingsQueryKey,
  listManagedLinesOptions,
  listManagedLinesQueryKey,
  listMihomoSubscriptionNodesOptions,
  listMihomoSubscriptionsOptions,
  listVoWiFiLinesOptions,
  listVoWiFiLinesQueryKey,
  putLineEgressBindingMutation,
  updateManagedLineMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { LineCandidate, LineEgressBinding, ManagedLine, VoWiFiLineState } from '@/api/generated/types.gen'
import { PageHeader, PageSection, ResponsiveDataView } from '@/components/Page'

type CountryOption = { code: string; name: string }
type LineRow = ManagedLine & { binding?: LineEgressBinding; voWiFi?: VoWiFiLineState }

const lineStateLabels: Record<ManagedLine['state'], string> = {
  ready: '就绪', 'modem-offline': '模组离线', 'sim-unavailable': 'SIM / Profile 不可用',
}
const candidateReasonLabels: Record<LineCandidate['readinessReason'], string> = {
  READY: '可添加', MODEM_OFFLINE: '模组离线', SIM_ABSENT: '未插入 SIM',
  SIM_UNAVAILABLE: 'SIM / Profile 不可用', ALREADY_ADDED: '已添加', BINDING_CONFLICT: '绑定身份冲突',
}
const readinessLabels: Record<LineEgressBinding['readinessReason'], string> = {
  READY: '出口可用', EGRESS_NOT_CONFIGURED: '尚未配置出口', LINE_VOWIFI_UNSUPPORTED: '线路不支持 Host VoWiFi',
  SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅', COUNTRY_NOT_FOUND: '当前订阅没有该国家',
  MIHOMO_NOT_RUNNING: 'Mihomo 未运行', MIHOMO_RESTART_REQUIRED: '等待 Mihomo 重启应用订阅',
}
const voWiFiStateLabels: Record<VoWiFiLineState['state'], string> = {
  stopped: '已停用', starting: '正在启动', connecting: '连接 ePDG', registering: '注册 IMS',
  online: '在线', reconnecting: '正在重连', stopping: '正在停用', failed: '运行失败',
}
const voWiFiReadinessLabels: Record<VoWiFiLineState['readinessCode'], string> = {
  READY: '可以激活', EGRESS_NOT_CONFIGURED: '请先明确配置出口', LINE_VOWIFI_UNSUPPORTED: '线路不支持 Host VoWiFi',
  LINE_HARDWARE_NOT_READY: '模组或 SIM / Profile 尚未就绪', SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅',
  COUNTRY_NOT_FOUND: '当前订阅没有该国家', MIHOMO_NOT_RUNNING: 'Mihomo 未运行',
  MIHOMO_RESTART_REQUIRED: '需要先重启 Mihomo 应用订阅',
}

function egressLabel(binding?: LineEgressBinding) {
  if (!binding || binding.mode === 'unconfigured') return '未配置'
  if (binding.mode === 'direct') return '直连'
  return `${binding.countryName || binding.countryCode} (${binding.countryCode})`
}

function canRequestVoWiFiActivation(state?: VoWiFiLineState) {
  return Boolean(state && !state.desiredActive && (
    state.eligible || state.readinessCode === 'MIHOMO_NOT_RUNNING' || state.readinessCode === 'MIHOMO_RESTART_REQUIRED'
  ))
}

export default function Lines() {
  const compact = !Grid.useBreakpoint().md
  const queryClient = useQueryClient()
  const linesQuery = useQuery(listManagedLinesOptions())
  const bindingsQuery = useQuery(listLineEgressBindingsOptions())
  const subscriptionsQuery = useQuery(listMihomoSubscriptionsOptions())
  const voWiFiQuery = useQuery({
    ...listVoWiFiLinesOptions(),
    refetchInterval: 10_000,
    refetchIntervalInBackground: false,
    retry: false,
  })
  const selectedSubscription = subscriptionsQuery.data?.subscriptions.find((item) => item.selected)
  const nodesQuery = useQuery({
    ...listMihomoSubscriptionNodesOptions({ path: { subscriptionId: selectedSubscription?.id ?? 'subscription_missing' } }),
    enabled: Boolean(selectedSubscription),
  })
  const [addOpen, setAddOpen] = useState(false)
  const candidatesQuery = useQuery({ ...listLineCandidatesOptions(), enabled: addOpen })
  const [candidateID, setCandidateID] = useState('')
  const [newName, setNewName] = useState('')
  const [drawerLineID, setDrawerLineID] = useState('')
  const [nameDraft, setNameDraft] = useState('')
  const [egressDraft, setEgressDraft] = useState<string>()
  const [operationError, setOperationError] = useState<unknown>()
  const [busy, setBusy] = useState('')

  const lines = linesQuery.data?.lines ?? []
  const bindings = bindingsQuery.data?.bindings ?? []
  const voWiFiStates = voWiFiQuery.data?.lines ?? []
  const candidates = candidatesQuery.data?.candidates ?? []
  const rows = useMemo<LineRow[]>(() => lines.map((line) => ({
    ...line,
    binding: bindings.find((binding) => binding.lineId === line.id),
    voWiFi: voWiFiStates.find((state) => state.lineId === line.id),
  })), [bindings, lines, voWiFiStates])
  const activeLine = rows.find((line) => line.id === drawerLineID)
  const countries = useMemo<CountryOption[]>(() => {
    const values = new Map<string, string>()
    for (const node of nodesQuery.data?.nodes ?? []) if (node.countryCode && node.countryName) values.set(node.countryCode, node.countryName)
    return [...values].map(([code, name]) => ({ code, name })).sort((left, right) => left.name.localeCompare(right.name, 'zh-CN'))
  }, [nodesQuery.data])
  const activeCountryMissing = activeLine?.binding?.mode === 'mihomo-country' && !countries.some((country) => country.code === activeLine.binding?.countryCode)
  const drawerEgressOptions = [
    { value: 'direct', label: '直连' },
    ...countries.map((country) => ({ value: `country:${country.code}`, label: `${country.name} (${country.code})` })),
    ...(activeCountryMissing && activeLine?.binding?.countryCode
      ? [{ value: `country:${activeLine.binding.countryCode}`, label: `${activeLine.binding.countryCode}（当前订阅不可用）`, disabled: true }]
      : []),
  ]
  const egressDraftValid = egressDraft === 'direct' || Boolean(egressDraft?.startsWith('country:') && countries.some((country) => country.code === egressDraft.slice(8)))

  const invalidateLines = async () => Promise.all([
    queryClient.invalidateQueries({ queryKey: listManagedLinesQueryKey() }),
    queryClient.invalidateQueries({ queryKey: listLineEgressBindingsQueryKey() }),
    queryClient.invalidateQueries({ queryKey: listVoWiFiLinesQueryKey() }),
  ])
  const addLine = useMutation({
    ...addManagedLineMutation(),
    onSuccess: async (created) => {
      setAddOpen(false)
      setDrawerLineID(created.id)
      setNameDraft(created.displayName)
      setEgressDraft(undefined)
      await Promise.all([invalidateLines(), queryClient.invalidateQueries({ queryKey: listLineCandidatesQueryKey() })])
    },
    onError: setOperationError,
    onSettled: () => setBusy(''),
  })
  const updateName = useMutation({
    ...updateManagedLineMutation(), onSuccess: invalidateLines, onError: setOperationError, onSettled: () => setBusy(''),
  })
  const updateEgress = useMutation({
    ...putLineEgressBindingMutation(), onSuccess: invalidateLines, onError: setOperationError, onSettled: () => setBusy(''),
  })
  const activateVoWiFi = useMutation({
    ...activateVoWiFiLineMutation(),
    onSuccess: (state) => queryClient.setQueryData(listVoWiFiLinesQueryKey(), (current: typeof voWiFiQuery.data) => current && ({ ...current, lines: [...current.lines.filter((item) => item.lineId !== state.lineId), state] })),
    onError: setOperationError, onSettled: () => setBusy(''),
  })
  const deactivateVoWiFi = useMutation({
    ...deactivateVoWiFiLineMutation(),
    onSuccess: (state) => queryClient.setQueryData(listVoWiFiLinesQueryKey(), (current: typeof voWiFiQuery.data) => current && ({ ...current, lines: [...current.lines.filter((item) => item.lineId !== state.lineId), state] })),
    onError: setOperationError, onSettled: () => setBusy(''),
  })

  const openDrawer = (line: LineRow) => {
    setDrawerLineID(line.id)
    setNameDraft(line.displayName)
    setEgressDraft(line.binding?.mode === 'direct' ? 'direct' : line.binding?.mode === 'mihomo-country' ? `country:${line.binding.countryCode}` : undefined)
    setOperationError(undefined)
  }
  const chooseCandidate = (id: string) => {
    setCandidateID(id)
    const selected = candidates.find((candidate) => candidate.candidateId === id)
    if (selected) setNewName(`${selected.managedModemModel || selected.managedModemDisplayName} · ${selected.subscriptionDisplayHint}`)
  }
  const lineDetails = (line: LineRow) => <Descriptions column={1} size="small" items={[
    { key: 'modem', label: '模组', children: <><div>{line.managedModemModel || '读取失败'}</div><Typography.Text type="secondary">{line.managedModemSerialNumber || '读取失败'}</Typography.Text></> },
    { key: 'sim', label: 'SIM / Profile', children: line.subscriptionDisplayHint },
    { key: 'phone', label: '手机号', children: line.voWiFi?.phoneNumber || '未获取' },
    { key: 'state', label: '线路状态', children: <Tag color={line.state === 'ready' ? 'green' : 'orange'}>{lineStateLabels[line.state]}</Tag> },
    { key: 'vowifi', label: 'VoWiFi', children: line.capabilities.hostVoWifiAuth ? <Tag color={line.voWiFi?.online ? 'green' : line.voWiFi?.desiredActive ? 'orange' : 'default'}>{line.voWiFi ? voWiFiStateLabels[line.voWiFi.state] : '读取中'}</Tag> : '不支持' },
    { key: 'egress', label: '出口', children: egressLabel(line.binding) },
  ]} />
  const columns: TableColumnsType<LineRow> = [
    { title: '名称', dataIndex: 'displayName' },
    { title: '模组', render: (_, line) => <Flex vertical><Typography.Text>{line.managedModemModel || '读取失败'}</Typography.Text><Typography.Text type="secondary">{line.managedModemSerialNumber || '读取失败'}</Typography.Text></Flex> },
    { title: 'SIM / Profile', dataIndex: 'subscriptionDisplayHint' },
    { title: '手机号', render: (_, line) => line.voWiFi?.phoneNumber || <Typography.Text type="secondary">未获取</Typography.Text> },
    { title: '线路状态', render: (_, line) => <Tag color={line.state === 'ready' ? 'green' : 'orange'}>{lineStateLabels[line.state]}</Tag> },
    { title: 'VoWiFi 状态', render: (_, line) => line.capabilities.hostVoWifiAuth ? <Tag color={line.voWiFi?.online ? 'green' : line.voWiFi?.desiredActive ? 'orange' : 'default'}>{line.voWiFi ? voWiFiStateLabels[line.voWiFi.state] : '读取中'}</Tag> : <Typography.Text type="secondary">不支持</Typography.Text> },
    { title: '出口', render: (_, line) => <Flex vertical><Typography.Text>{egressLabel(line.binding)}</Typography.Text>{line.binding && line.binding.readinessReason !== 'READY' && <Typography.Text type="secondary">{readinessLabels[line.binding.readinessReason]}</Typography.Text>}</Flex> },
    { title: '操作', render: (_, line) => <Button type="link" onClick={() => openDrawer(line)}>配置</Button> },
  ]

  const queryError = linesQuery.error ?? bindingsQuery.error ?? subscriptionsQuery.error
  return <main className="page-content">
    <PageHeader title="线路配置" subtitle="线路只绑定已添加模组与当前 SIM / Profile；通信路径在配置中独立管理" extra={<>
      <Button icon={<ReloadOutlined />} onClick={() => void invalidateLines()}>刷新</Button>
      <Button aria-label="添加线路" type="primary" icon={<PlusOutlined />} onClick={() => { setAddOpen(true); setCandidateID(''); setNewName(''); setOperationError(undefined); void candidatesQuery.refetch() }}>添加线路</Button>
    </>} />
    {Boolean(operationError || queryError) && <Alert className="page-alert" type="error" showIcon title="线路配置失败" description={displayApiError(operationError ?? queryError)} />}
    {voWiFiQuery.error && <Alert className="page-alert" type="warning" showIcon title="Host VoWiFi 运行状态暂不可用" description={displayApiError(voWiFiQuery.error)} />}
    <ResponsiveDataView
      data={rows}
      columns={columns}
      rowKey="id"
      loading={linesQuery.isPending || bindingsQuery.isPending}
      emptyText="尚未添加线路；请先添加模组，再添加线路"
      renderCard={(line) => <Card className="mobile-record-card" title={line.displayName} extra={<Button type="link" onClick={() => openDrawer(line)}>配置</Button>}>{lineDetails(line)}</Card>}
    />

    <Modal
      title="添加线路"
      open={addOpen}
      onCancel={() => setAddOpen(false)}
      onOk={() => {
        if (!candidateID || !newName.trim()) return
        setBusy('add')
        setOperationError(undefined)
        addLine.mutate({ body: { candidateId: candidateID, displayName: newName.trim() } })
      }}
      confirmLoading={busy === 'add'}
      okButtonProps={{ disabled: !candidateID || !newName.trim() }}
      destroyOnHidden
      width={compact ? '96%' : 'min(92vw, 72rem)'}
      styles={{ body: { maxHeight: '72dvh', overflowY: 'auto' } }}
    >
      {Boolean(candidatesQuery.error || operationError) && <Alert type="error" showIcon title="无法添加线路" description={displayApiError(candidatesQuery.error ?? operationError)} />}
      <Typography.Paragraph type="secondary">每次只能选择一个线路候选；如需添加多条线路，请分别完成添加。</Typography.Paragraph>
      {!candidatesQuery.isFetching && !candidates.length ? <Empty description="没有可观察到的线路候选；请确认已添加模组" /> : compact
        ? <Radio.Group value={candidateID} onChange={(event) => chooseCandidate(event.target.value)} style={{ width: '100%' }}><div className="responsive-card-list">{candidates.map((candidate) => <Card key={candidate.candidateId} size="small" style={{ width: '100%' }}><Radio value={candidate.candidateId} disabled={!candidate.addable} aria-label={candidateReasonLabels[candidate.readinessReason]}>{candidate.subscriptionDisplayHint || '未知 SIM / Profile'}</Radio><Descriptions column={1} size="small" items={[
            { key: 'operator', label: '归属运营商', children: candidate.homeOperatorName || candidate.homeOperatorCode || '未知' },
            { key: 'modem', label: '模组', children: candidate.managedModemModel || '读取失败' },
            { key: 'serial', label: '序列号', children: candidate.managedModemSerialNumber || '读取失败' },
            { key: 'state', label: '状态', children: candidateReasonLabels[candidate.readinessReason] },
          ]} /></Card>)}</div></Radio.Group>
        : <div className="table-scroll"><Table<LineCandidate>
            rowKey="candidateId" loading={candidatesQuery.isFetching} dataSource={candidates} pagination={false} scroll={{ x: 'max-content' }}
            rowSelection={{ type: 'radio', selectedRowKeys: candidateID ? [candidateID] : [], onChange: (keys) => chooseCandidate(String(keys[0] ?? '')), getCheckboxProps: (candidate) => ({ disabled: !candidate.addable, 'aria-label': candidateReasonLabels[candidate.readinessReason] }) }}
            onRow={(candidate) => ({ onClick: () => candidate.addable && chooseCandidate(candidate.candidateId) })}
            columns={[
              { title: 'ICCID', dataIndex: 'subscriptionDisplayHint' },
              { title: '归属运营商', render: (_, candidate) => <Flex vertical><Typography.Text>{candidate.homeOperatorName || candidate.homeOperatorCode || '未知'}</Typography.Text>{candidate.homeOperatorName && candidate.homeOperatorCode && <Typography.Text type="secondary">{candidate.homeOperatorCode}</Typography.Text>}</Flex> },
              { title: '模组', dataIndex: 'managedModemModel', render: (value) => value || '读取失败' },
              { title: '序列号', dataIndex: 'managedModemSerialNumber', render: (value) => value || '读取失败' },
              { title: '状态', render: (_, candidate) => <Tag color={candidate.addable ? 'green' : 'default'}>{candidateReasonLabels[candidate.readinessReason]}</Tag> },
            ]}
          /></div>}
      <Divider />
      <Flex vertical gap="small"><Typography.Text strong>线路名称</Typography.Text><Input value={newName} maxLength={120} onChange={(event) => setNewName(event.target.value)} placeholder="选择候选后可修改名称" disabled={!candidateID} /><Typography.Text type="secondary">名称允许重复；系统使用随机 Line ID 识别线路。</Typography.Text></Flex>
    </Modal>

    <Drawer title={activeLine ? `配置线路 · ${activeLine.displayName}` : '配置线路'} open={Boolean(activeLine)} onClose={() => setDrawerLineID('')} placement={compact ? 'bottom' : 'right'} size={compact ? '88dvh' : 'min(92vw, 44rem)'} autoFocus={false} destroyOnHidden>
      {activeLine && <Flex vertical gap="middle">
        {Boolean(operationError) && <Alert type="error" showIcon title="线路操作失败" description={displayApiError(operationError)} />}
        {(voWiFiQuery.error || nodesQuery.error) && <Alert type="warning" showIcon title="部分运行状态暂不可用" description={displayApiError(voWiFiQuery.error ?? nodesQuery.error)} />}
        <Descriptions column={1} bordered size="small" items={[
          { key: 'modem', label: '模组', children: `${activeLine.managedModemModel || '读取失败'} · ${activeLine.managedModemSerialNumber || '读取失败'}` },
          { key: 'sim', label: 'SIM / Profile', children: activeLine.subscriptionDisplayHint },
          { key: 'id', label: 'Line ID', children: <Typography.Text copyable>{activeLine.id}</Typography.Text> },
        ]} />
        <PageSection title="名称"><Flex vertical gap="small"><Input value={nameDraft} maxLength={120} onChange={(event) => setNameDraft(event.target.value)} /><Flex justify="flex-end"><Button type="primary" loading={busy === `name:${activeLine.id}`} disabled={!nameDraft.trim() || nameDraft.trim() === activeLine.displayName} onClick={() => { setBusy(`name:${activeLine.id}`); setOperationError(undefined); updateName.mutate({ path: { lineId: activeLine.id }, body: { displayName: nameDraft.trim() } }) }}>保存名称</Button></Flex></Flex></PageSection>
        <PageSection title="Host VoWiFi 出口" extra={<Tag color={activeLine.binding?.ready ? 'green' : 'orange'}>{activeLine.binding ? readinessLabels[activeLine.binding.readinessReason] : '尚未配置出口'}</Tag>}>
          <Flex vertical gap="small"><Select value={egressDraft} placeholder="请选择明确出口" options={drawerEgressOptions} onChange={setEgressDraft} disabled={!activeLine.capabilities.hostVoWifiAuth} /><Typography.Text type="secondary">{!activeLine.capabilities.hostVoWifiAuth ? '该线路不具备 Host VoWiFi 鉴权能力，不能配置专用出口。' : selectedSubscription ? `国家列表来自当前订阅“${selectedSubscription.displayName}”；保存线路出口不会重写 Mihomo 配置。` : '国家出口需要先在 Mihomo 页面选择一个已转换的订阅；也可明确选择直连。'}</Typography.Text><Flex justify="flex-end"><Button type="primary" loading={busy === `egress:${activeLine.id}`} disabled={!activeLine.capabilities.hostVoWifiAuth || !egressDraftValid} onClick={() => {
            if (!egressDraft) return
            setBusy(`egress:${activeLine.id}`); setOperationError(undefined)
            updateEgress.mutate({ path: { lineId: activeLine.id }, body: egressDraft === 'direct' ? { mode: 'direct', countryCode: '' } : { mode: 'mihomo-country', countryCode: egressDraft.slice(8) } })
          }}>保存出口</Button></Flex></Flex>
        </PageSection>
        <PageSection title="Host VoWiFi">{!activeLine.capabilities.hostVoWifiAuth ? <Alert type="info" showIcon title="该线路不具备 Host VoWiFi 鉴权能力" /> : <Flex vertical gap="middle">
          <Flex justify="space-between" align="center" wrap gap="small"><div><Typography.Text strong>{activeLine.voWiFi ? voWiFiStateLabels[activeLine.voWiFi.state] : '正在读取运行状态'}</Typography.Text><br /><Typography.Text type="secondary">{activeLine.voWiFi ? voWiFiReadinessLabels[activeLine.voWiFi.readinessCode] : '请稍候'}</Typography.Text></div><Tag color={activeLine.voWiFi?.online ? 'green' : activeLine.voWiFi?.desiredActive ? 'orange' : 'default'}>{activeLine.voWiFi?.online ? '在线' : activeLine.voWiFi?.desiredActive ? '已启用' : '未启用'}</Tag></Flex>
          <Descriptions column={1} size="small" items={[
            { key: 'egress', label: '当前出口', children: egressLabel(activeLine.binding) },
            { key: 'registered', label: '最近注册', children: activeLine.voWiFi?.registeredAt ? new Date(activeLine.voWiFi.registeredAt).toLocaleString() : '—' },
            { key: 'refresh', label: '下次刷新', children: activeLine.voWiFi?.nextRefreshAt ? new Date(activeLine.voWiFi.nextRefreshAt).toLocaleString() : '—' },
            { key: 'error', label: '运行错误', children: activeLine.voWiFi?.lastErrorCode || '—' },
          ]} />
          <Space wrap><Button type="primary" disabled={!canRequestVoWiFiActivation(activeLine.voWiFi)} loading={busy === `vowifi:${activeLine.id}` && !activeLine.voWiFi?.desiredActive} onClick={() => { setBusy(`vowifi:${activeLine.id}`); setOperationError(undefined); activateVoWiFi.mutate({ path: { lineId: activeLine.id } }) }}>激活 VoWiFi</Button><Button danger disabled={!activeLine.voWiFi?.desiredActive} loading={busy === `vowifi:${activeLine.id}` && Boolean(activeLine.voWiFi?.desiredActive)} onClick={() => { setBusy(`vowifi:${activeLine.id}`); setOperationError(undefined); deactivateVoWiFi.mutate({ path: { lineId: activeLine.id } }) }}>停用 VoWiFi</Button></Space>
        </Flex>}</PageSection>
      </Flex>}
    </Drawer>
  </main>
}

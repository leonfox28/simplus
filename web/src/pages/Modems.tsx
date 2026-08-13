import { EyeInvisibleOutlined, EyeOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Card, Descriptions, Empty, Grid, Modal, Radio, Space, Switch, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useEffect, useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  addManagedModemMutation,
  listManagedModemsOptions,
  listManagedModemsQueryKey,
  listModemCandidatesOptions,
  listModemCandidatesQueryKey,
  setManagedModemRfStateMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import { readManagedModemEquipmentIdentity } from '@/api/generated/sdk.gen'
import type { ManagedModem, ModemCandidate } from '@/api/generated/types.gen'
import { PageHeader, ResponsiveDataView } from '@/components/Page'

type CapabilityKey = keyof ManagedModem['capabilities']

const capabilityLabels: Array<[CapabilityKey, string]> = [
  ['simAccess', 'SIM 卡'], ['sms', '短信'], ['cellularVoice', '语音通话'],
  ['digitalVoiceMedia', '数字音频'], ['hostVoWifiAuth', 'Host VoWiFi'],
  ['rfControl', '射频控制'], ['networkScan', '网络扫描'],
  ['manualNetworkSelection', '手动选网'], ['primarySimLockState', 'SIM 锁状态'],
]

const readinessLabels: Record<ModemCandidate['readinessReason'], string> = {
  READY: '可以添加',
  CONTROL_UNAVAILABLE: '控制端点不可用',
  SIM_ACCESS_UNAVAILABLE: 'SIM 访问能力不可用',
  EQUIPMENT_IDENTITY_UNAVAILABLE: '无法读取模组身份',
  IDENTITY_CONFLICT: '模组身份冲突',
}

function CapabilityTags({ capabilities }: { capabilities: ManagedModem['capabilities'] }) {
  const enabled = capabilityLabels.filter(([key]) => capabilities[key])
  if (!enabled.length) return <Typography.Text type="secondary">暂无可用能力</Typography.Text>
  return <Space size={[0, 4]} wrap>{enabled.map(([key, label]) => <Tag key={key}>{label}</Tag>)}</Space>
}

function SIMPresenceTag({ value }: { value: ManagedModem['simPresence'] }) {
  if (value === 'present') return <Tag color="green">已插入</Tag>
  if (value === 'absent') return <Tag>未插入</Tag>
  return <Tag color="orange">未知</Tag>
}

const cellularLabels: Record<ManagedModem['cellular']['state'], { label: string; color?: string }> = {
  'registered-home': { label: '已注册（本地）', color: 'green' },
  'registered-roaming': { label: '已注册（漫游）', color: 'cyan' },
  searching: { label: '正在搜网', color: 'processing' },
  denied: { label: '注册被拒绝', color: 'red' },
  'not-registered': { label: '未注册', color: 'orange' },
  'rf-off': { label: '射频已关闭' },
  'sim-not-ready': { label: 'SIM 未就绪', color: 'orange' },
  unavailable: { label: '状态不可用', color: 'red' },
  unknown: { label: '状态未知', color: 'orange' },
}

function CellularStatusView({ item }: { item: ManagedModem }) {
  const presentation = cellularLabels[item.cellular.state]
  const network = [item.cellular.operatorName || item.cellular.operatorCode, item.cellular.rat.toUpperCase()].filter(Boolean).join(' · ')
  const signal = item.cellular.signalState === 'measured' ? `${item.cellular.signalRssiDbm} dBm` : '信号不可用'
  const observed = item.cellular.observedAt ? new Date(item.cellular.observedAt).toLocaleString('zh-CN') : ''
  return <Space orientation="vertical" size={0}>
    <Tag color={presentation.color}>{presentation.label}</Tag>
    <Typography.Text type="secondary">{network || '网络未知'} · {signal}</Typography.Text>
    <Typography.Text type="secondary">{observed ? `观测于 ${observed}` : item.cellular.errorCode || '无新鲜观测'}</Typography.Text>
  </Space>
}

function ModemModel({ value, strong = false }: { value: string; strong?: boolean }) {
  return value
    ? <Typography.Text strong={strong}>{value}</Typography.Text>
    : <Typography.Text type="danger">读取失败</Typography.Text>
}

export default function Modems() {
  const compact = !Grid.useBreakpoint().md
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const modemsQuery = useQuery(listManagedModemsOptions())
  const [addOpen, setAddOpen] = useState(false)
  const candidatesQuery = useQuery({ ...listModemCandidatesOptions(), enabled: addOpen })
  const [selectedCandidate, setSelectedCandidate] = useState('')
  const [revealedIMEIs, setRevealedIMEIs] = useState<Record<string, string>>({})
  const [operationError, setOperationError] = useState<unknown>()
  const [rfBusyModemId, setRFBusyModemId] = useState('')
  const [imeiBusyModemId, setIMEIBusyModemId] = useState('')
  const modems = modemsQuery.data?.modems ?? []
  const candidates = candidatesQuery.data?.candidates ?? []

  useEffect(() => {
    setRevealedIMEIs({})
  }, [modemsQuery.dataUpdatedAt])

  const addModem = useMutation({
    ...addManagedModemMutation(),
    onSuccess: async () => {
      setAddOpen(false)
      setSelectedCandidate('')
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: listManagedModemsQueryKey() }),
        queryClient.invalidateQueries({ queryKey: listModemCandidatesQueryKey() }),
      ])
      void message.success('模组已添加。')
    },
    onError: setOperationError,
  })
  const setRF = useMutation({
    ...setManagedModemRfStateMutation(),
    onSuccess: (updated) => {
      queryClient.setQueryData(listManagedModemsQueryKey(), (current: typeof modemsQuery.data) => current && ({
        ...current,
        modems: current.modems.map((item) => item.id === updated.id ? updated : item),
      }))
    },
    onError: async (error) => {
      setOperationError(error)
      await modemsQuery.refetch()
    },
    onSettled: () => setRFBusyModemId(''),
  })
  const reload = async () => {
    setOperationError(undefined)
    setRevealedIMEIs({})
    await modemsQuery.refetch()
  }
  const toggleIMEI = (item: ManagedModem) => {
    if (revealedIMEIs[item.id]) {
      setRevealedIMEIs((current) => {
        const next = { ...current }
        delete next[item.id]
        return next
      })
      return
    }
    setOperationError(undefined)
    setIMEIBusyModemId(item.id)
    // This sensitive read deliberately bypasses the mutation cache. The
    // generated SDK still owns validation/transport while the value exists
    // only in the explicitly controlled reveal state below.
    void readManagedModemEquipmentIdentity({
      path: { modemId: item.id },
      throwOnError: true,
    }).then(({ data }) => {
      setRevealedIMEIs((current) => ({ ...current, [item.id]: data.imei }))
    }).catch(setOperationError).finally(() => setIMEIBusyModemId(''))
  }

  const renderIMEI = (item: ManagedModem) => {
    const imei = revealedIMEIs[item.id]
    const revealed = imei !== undefined
    return <Space size="small">
      <Typography.Text code data-testid={`imei-value-${item.id}`}>{revealed ? imei : '•••••••••••••••'}</Typography.Text>
      <Button
        type="text"
        size="small"
        icon={revealed ? <EyeInvisibleOutlined /> : <EyeOutlined />}
        aria-label={revealed ? '隐藏 IMEI' : '显示 IMEI'}
        data-testid={`imei-toggle-${item.id}`}
        loading={imeiBusyModemId === item.id}
        disabled={item.state !== 'online' || (imeiBusyModemId !== '' && imeiBusyModemId !== item.id)}
        onClick={() => toggleIMEI(item)}
      />
    </Space>
  }
  const renderRF = (item: ManagedModem) => {
    if (!item.capabilities.rfControl) return <Typography.Text type="secondary">不支持</Typography.Text>
    const controllable = item.state === 'online' && item.rfState !== 'unknown'
    return <Space size="small">
      <Switch
        checkedChildren="开"
        unCheckedChildren="关"
        checked={item.rfState === 'on'}
        loading={rfBusyModemId === item.id}
        disabled={!controllable || (rfBusyModemId !== '' && rfBusyModemId !== item.id)}
        aria-label={`${item.model || '模组'} 射频`}
        data-testid={`rf-toggle-${item.id}`}
        onChange={(enabled) => {
          setOperationError(undefined)
          setRFBusyModemId(item.id)
          setRF.mutate({ path: { modemId: item.id }, body: { enabled } })
        }}
      />
      {!controllable && <Typography.Text type="secondary">未知</Typography.Text>}
    </Space>
  }

  const columns: TableColumnsType<ManagedModem> = [
    { title: '型号', dataIndex: 'model', render: (value) => <ModemModel value={String(value)} /> },
    { title: '序列号', dataIndex: 'serialNumber', render: (value) => value ? <Typography.Text code>{String(value)}</Typography.Text> : <Typography.Text type="secondary">未提供</Typography.Text> },
    { title: 'IMEI', render: (_, item) => renderIMEI(item) },
    { title: '在线状态', dataIndex: 'state', render: (value) => <Tag color={value === 'online' ? 'green' : 'default'}>{value === 'online' ? '在线' : '离线'}</Tag> },
    { title: 'SIM 卡', dataIndex: 'simPresence', render: (_, item) => <SIMPresenceTag value={item.simPresence} /> },
    { title: '蜂窝网络', dataIndex: 'cellular', render: (_, item) => <CellularStatusView item={item} /> },
    { title: '射频', dataIndex: 'rfState', render: (_, item) => renderRF(item) },
  ]

  return <main className="page-content">
    <PageHeader
      title="模组配置"
      subtitle="管理已经添加的模组；扫描硬件不会自动创建模组或线路"
      extra={<>
        <Button icon={<ReloadOutlined />} onClick={() => void reload()}>刷新</Button>
        <Button aria-label="添加模组" type="primary" icon={<PlusOutlined />} onClick={() => { setAddOpen(true); setSelectedCandidate(''); setOperationError(undefined); void candidatesQuery.refetch() }}>添加模组</Button>
      </>}
    />
    {Boolean(operationError || modemsQuery.error) && <Alert className="page-alert" type="error" showIcon title={displayApiError(operationError ?? modemsQuery.error)} />}
    <ResponsiveDataView
      data={modems}
      columns={columns}
      rowKey="id"
      loading={modemsQuery.isPending}
      emptyText="尚未添加模组"
      renderCard={(item) => <Card className="mobile-record-card" title={<ModemModel value={item.model} strong />}>
        <Descriptions column={1} size="small" items={[
          { key: 'serial', label: '序列号', children: item.serialNumber || '未提供' },
          { key: 'imei', label: 'IMEI', children: renderIMEI(item) },
          { key: 'state', label: '在线状态', children: <Tag color={item.state === 'online' ? 'green' : 'default'}>{item.state === 'online' ? '在线' : '离线'}</Tag> },
          { key: 'sim', label: 'SIM 卡', children: <SIMPresenceTag value={item.simPresence} /> },
          { key: 'cellular', label: '蜂窝网络', children: <CellularStatusView item={item} /> },
          { key: 'rf', label: '射频', children: renderRF(item) },
        ]} />
      </Card>}
    />

    <Modal
      title="添加模组"
      open={addOpen}
      width="min(96vw, 76rem)"
      destroyOnHidden
      onCancel={() => { if (!addModem.isPending) setAddOpen(false) }}
      onOk={() => selectedCandidate && addModem.mutate({ body: { candidateId: selectedCandidate } })}
      confirmLoading={addModem.isPending}
      okButtonProps={{ disabled: !selectedCandidate }}
    >
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Typography.Text type="secondary">这里只显示当前检测到且尚未添加的模组；请选择一项后添加。</Typography.Text>
          <Button icon={<ReloadOutlined />} loading={candidatesQuery.isFetching} onClick={() => { setSelectedCandidate(''); void candidatesQuery.refetch() }}>重新扫描</Button>
        </Space>
        {Boolean(candidatesQuery.error || (addOpen && operationError)) && <Alert type="error" showIcon title={displayApiError(candidatesQuery.error ?? operationError)} />}
        {!candidatesQuery.isFetching && !candidates.length ? <Empty description="没有发现未添加的模组" /> : compact
          ? <Radio.Group value={selectedCandidate} onChange={(event) => setSelectedCandidate(event.target.value)} style={{ width: '100%' }}>
              <div className="responsive-card-list">{candidates.map((candidate) => <Card key={candidate.candidateId} size="small" style={{ width: '100%' }}>
                <Radio value={candidate.candidateId} disabled={!candidate.addable} aria-label={`${candidate.model || '读取失败'}：${readinessLabels[candidate.readinessReason]}`}>{candidate.model || '读取失败'}</Radio>
                <Descriptions column={1} size="small" items={[
                  { key: 'usb', label: 'USB Device', children: candidate.usbAddress || '—' },
                  { key: 'vidpid', label: 'VID:PID', children: `${candidate.vendorId}:${candidate.productId}` },
                  { key: 'serial', label: '序列标识', children: candidate.usbSerialHint || '未提供' },
                  { key: 'state', label: '支持状态', children: readinessLabels[candidate.readinessReason] },
                  { key: 'capabilities', label: '能力', children: <CapabilityTags capabilities={candidate.capabilities} /> },
                ]} />
              </Card>)}</div>
            </Radio.Group>
          : <div className="table-scroll"><Table<ModemCandidate>
              rowKey="candidateId"
              loading={candidatesQuery.isFetching}
              dataSource={candidates}
              pagination={false}
              scroll={{ x: 'max-content' }}
              rowSelection={{
                type: 'radio',
                selectedRowKeys: selectedCandidate ? [selectedCandidate] : [],
                onChange: (keys) => setSelectedCandidate(String(keys[0] ?? '')),
                getCheckboxProps: (candidate) => ({ disabled: !candidate.addable, 'aria-label': `${candidate.model || '读取失败'}：${readinessLabels[candidate.readinessReason]}` }),
              }}
              onRow={(candidate) => ({ onClick: () => candidate.addable && setSelectedCandidate(candidate.candidateId) })}
              columns={[
                { title: 'USB Device', dataIndex: 'usbAddress' },
                { title: 'VID:PID', render: (_, item) => <Typography.Text code>{item.vendorId}:{item.productId}</Typography.Text> },
                { title: '型号', dataIndex: 'model', render: (value) => <ModemModel value={String(value)} strong /> },
                { title: '序列标识', dataIndex: 'usbSerialHint' },
                { title: '支持状态', render: (_, item) => <Space orientation="vertical" size={0}><Tag color={item.addable ? 'green' : 'orange'}>{item.supportStatus === 'supported' ? '系统支持' : '暂不可添加'}</Tag>{!item.addable && <Typography.Text type="secondary">{readinessLabels[item.readinessReason]}</Typography.Text>}</Space> },
                { title: 'SIM', render: (_, item) => <SIMPresenceTag value={item.simPresence} /> },
                { title: '能力', render: (_, item) => <CapabilityTags capabilities={item.capabilities} /> },
              ]}
            /></div>}
      </Space>
    </Modal>
  </main>
}

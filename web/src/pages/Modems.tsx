import { EyeInvisibleOutlined, EyeOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { PageContainer, ProCard, ProDescriptions, ProTable } from '@ant-design/pro-components'
import { Alert, Button, Empty, Modal, Space, Switch, Tag, Typography } from 'antd'
import React, { useCallback, useEffect, useState } from 'react'
import {
  activateEUICCProfile,
  addManagedModem,
  getEUICCState,
  listManagedModems,
  listModemCandidates,
  readManagedModemIMEI,
  setManagedModemRFState,
  type EUICCState,
  type ManagedModem,
  type ModemCandidate,
} from '@/api/client'

type CapabilityKey = keyof ManagedModem['capabilities']

const capabilityLabels: Array<[CapabilityKey, string]> = [
  ['simAccess', 'SIM 卡'],
  ['sms', '短信'],
  ['cellularVoice', '语音通话'],
  ['digitalVoiceMedia', '数字音频'],
  ['hostVoWifiAuth', 'Host VoWiFi'],
  ['rfControl', '射频控制'],
  ['networkScan', '网络扫描'],
  ['manualNetworkSelection', '手动选网'],
  ['primarySimLockState', 'SIM 锁状态'],
  ['euiccProfiles', 'eUICC'],
]

function CapabilityTags({ capabilities }: { capabilities: ManagedModem['capabilities'] }) {
  const enabled = capabilityLabels.filter(([key]) => capabilities[key])
  if (enabled.length === 0) return <Typography.Text type="secondary">暂无可用能力</Typography.Text>
  return <Space size={[0, 4]} wrap>{enabled.map(([key, label]) => <Tag key={key}>{label}</Tag>)}</Space>
}

function SIMPresenceTag({ value }: { value: ManagedModem['simPresence'] }) {
  if (value === 'present') return <Tag color="green">已插入</Tag>
  if (value === 'absent') return <Tag>未插入</Tag>
  return <Tag color="orange">未知</Tag>
}

function ModemModel({ value, strong = false }: { value: string, strong?: boolean }) {
  if (!value) return <Typography.Text type="danger">读取失败</Typography.Text>
  return <Typography.Text strong={strong}>{value}</Typography.Text>
}

const readinessLabels: Record<ModemCandidate['readinessReason'], string> = {
  READY: '可以添加',
  CONTROL_UNAVAILABLE: '控制端点不可用',
  SIM_ACCESS_UNAVAILABLE: 'SIM 访问能力不可用',
  EQUIPMENT_IDENTITY_UNAVAILABLE: '无法读取模组身份',
  IDENTITY_CONFLICT: '模组身份冲突',
}

const errorLabels: Record<string, string> = {
  MODEM_SCAN_FAILED: '扫描模组失败，请检查 Agent 和设备连接后重试。',
  MODEM_CANDIDATE_NOT_FOUND: '该模组已经离线，请重新扫描。',
  MODEM_CANDIDATE_NOT_READY: '该模组目前不满足添加条件，请检查控制端点。',
  MODEM_ALREADY_ADDED: '该模组已经添加。',
  MODEM_IDENTITY_CONFLICT: '模组身份发生冲突，系统已拒绝自动绑定或显示 IMEI。',
  MODEM_NOT_FOUND: '该模组记录不存在，请刷新后重试。',
  MODEM_RF_UNAVAILABLE: '该模组当前不支持射频控制。',
  MODEM_RF_CHANGE_FAILED: '射频状态未能确认，请刷新状态后再决定是否重试。',
  MODEM_RF_NETWORK_UNAVAILABLE: '无法连接管理服务，请检查服务状态后重试。',
  MODEM_IDENTITY_UNAVAILABLE: '当前无法读取 IMEI，请确认模组在线后重试。',
  MODEM_IDENTITY_NETWORK_UNAVAILABLE: '无法连接管理服务，请检查服务状态后重试。',
  MODEM_IDENTITY_RESPONSE_INVALID: '管理服务返回了无效的 IMEI。',
}

function displayError(error: unknown): string {
  const code = error instanceof Error ? error.message : String(error)
  return errorLabels[code] ?? code
}

export default function Modems() {
  const [modems, setModems] = useState<ManagedModem[]>([])
  const [euicc, setEUICC] = useState<EUICCState>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [addOpen, setAddOpen] = useState(false)
  const [candidates, setCandidates] = useState<ModemCandidate[]>([])
  const [selectedCandidate, setSelectedCandidate] = useState('')
  const [scanning, setScanning] = useState(false)
  const [adding, setAdding] = useState(false)
  const [rfBusyModemId, setRFBusyModemId] = useState('')
  const [imeiBusyModemId, setIMEIBusyModemId] = useState('')
  const [revealedIMEIs, setRevealedIMEIs] = useState<Record<string, string>>({})
  const [modalError, setModalError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    setRevealedIMEIs({})
    try {
      setModems(await listManagedModems())
      try {
        setEUICC(await getEUICCState())
      } catch {
        setEUICC(undefined)
      }
    } catch (loadError) {
      setError(displayError(loadError))
    } finally {
      setLoading(false)
    }
  }, [])

  const scan = useCallback(async () => {
    setScanning(true)
    setModalError('')
    setSelectedCandidate('')
    try {
      setCandidates(await listModemCandidates())
    } catch (scanError) {
      setCandidates([])
      setModalError(displayError(scanError))
    } finally {
      setScanning(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const openAdd = () => {
    setAddOpen(true)
    void scan()
  }

  const addSelected = async () => {
    if (!selectedCandidate) return
    setAdding(true)
    setModalError('')
    try {
      await addManagedModem(selectedCandidate)
      setAddOpen(false)
      setSelectedCandidate('')
      await load()
    } catch (addError) {
      setModalError(displayError(addError))
    } finally {
      setAdding(false)
    }
  }

  const changeRFState = async (item: ManagedModem, enabled: boolean) => {
    setRFBusyModemId(item.id)
    setError('')
    try {
      const updated = await setManagedModemRFState(item.id, enabled)
      setModems((current) => current.map((modem) => modem.id === updated.id ? updated : modem))
    } catch (rfError) {
      const message = displayError(rfError)
      await load()
      setError(message)
    } finally {
      setRFBusyModemId('')
    }
  }

  const toggleIMEI = async (item: ManagedModem) => {
    if (revealedIMEIs[item.id]) {
      setRevealedIMEIs((current) => {
        const next = { ...current }
        delete next[item.id]
        return next
      })
      return
    }
    setIMEIBusyModemId(item.id)
    setError('')
    try {
      const imei = await readManagedModemIMEI(item.id)
      setRevealedIMEIs((current) => ({ ...current, [item.id]: imei }))
    } catch (identityError) {
      setError(displayError(identityError))
    } finally {
      setIMEIBusyModemId('')
    }
  }

  return <PageContainer
    title="模组配置"
    subTitle="管理已经添加的模组；扫描硬件不会自动创建模组或线路"
    extra={<Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>添加模组</Button>}
  >
    {error && <Alert type="error" message={error} showIcon style={{ marginBottom: '1rem' }} />}

    <ProTable<ManagedModem>
      rowKey="id"
      search={false}
      options={{ reload: () => { void load() } }}
      loading={loading}
      dataSource={modems}
      pagination={false}
      scroll={{ x: 'max-content' }}
      locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未添加模组" /> }}
      columns={[
        {
          title: '型号', dataIndex: 'model', ellipsis: true,
          render: (_, item) => <ModemModel value={item.model} />,
        },
        {
          title: '序列号', dataIndex: 'serialNumber', ellipsis: true,
          render: (_, item) => item.serialNumber
            ? <Typography.Text code>{item.serialNumber}</Typography.Text>
            : <Typography.Text type="secondary">未提供</Typography.Text>,
        },
        {
          title: 'IMEI', key: 'imei',
          render: (_, item) => {
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
                onClick={() => void toggleIMEI(item)}
              />
            </Space>
          },
        },
        {
          title: '在线状态', dataIndex: 'state',
          render: (_, item) => <Tag color={item.state === 'online' ? 'green' : 'default'}>{item.state === 'online' ? '在线' : '离线'}</Tag>,
        },
        {
          title: 'SIM 卡', dataIndex: 'simPresence',
          render: (_, item) => <SIMPresenceTag value={item.simPresence} />,
        },
        {
          title: '射频', dataIndex: 'rfState',
          render: (_, item) => {
            if (!item.capabilities.rfControl) return <Typography.Text type="secondary">不支持</Typography.Text>
            const controllable = item.state === 'online' && item.rfState !== 'unknown'
            return <Space size="small">
              <Switch
                checkedChildren="开"
                unCheckedChildren="关"
                checked={item.rfState === 'on'}
                loading={rfBusyModemId === item.id}
                disabled={!controllable || (rfBusyModemId !== '' && rfBusyModemId !== item.id)}
                aria-label={`${item.model} 射频`}
                data-testid={`rf-toggle-${item.id}`}
                onChange={(enabled) => void changeRFState(item, enabled)}
              />
              {!controllable && <Typography.Text type="secondary">未知</Typography.Text>}
            </Space>
          },
        },
      ]}
    />

    {euicc && <ProCard title={`可拔插 eUICC · ${euicc.eidHint}`} style={{ marginTop: '1rem' }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 18rem), 1fr))', gap: '1rem' }}>
        {euicc.profiles.map((profile) => <ProCard key={profile.id} variant="outlined">
          <ProDescriptions column={1} dataSource={profile} columns={[
            { title: 'Profile', dataIndex: 'displayName' },
            { title: 'Identity', dataIndex: 'displayIdentityHint' },
          ]} />
          <Button
            type={profile.active ? 'primary' : 'default'}
            disabled={profile.active}
            onClick={async () => { setEUICC(await activateEUICCProfile(profile.id)) }}
          >{profile.active ? '当前 Profile' : '激活'}</Button>
        </ProCard>)}
      </div>
    </ProCard>}

    <Modal
      title="添加模组"
      open={addOpen}
      width="min(96vw, 76rem)"
      destroyOnHidden
      onCancel={() => { if (!adding) setAddOpen(false) }}
      footer={[
        <Button key="cancel" disabled={adding} onClick={() => setAddOpen(false)}>取消</Button>,
        <Button key="add" type="primary" loading={adding} disabled={!selectedCandidate} onClick={() => void addSelected()}>添加</Button>,
      ]}
    >
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Typography.Text type="secondary">这里只显示当前检测到且尚未添加的模组；点击一行进行选择。</Typography.Text>
          <Button icon={<ReloadOutlined />} loading={scanning} onClick={() => void scan()}>重新扫描</Button>
        </Space>
        {modalError && <Alert type="error" message={modalError} showIcon />}
        {!scanning && candidates.length === 0
          ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有发现未添加的模组" />
          : <ProTable<ModemCandidate>
            rowKey="candidateId"
            search={false}
            options={false}
            loading={scanning}
            dataSource={candidates}
            pagination={false}
            tableAlertRender={false}
            scroll={{ x: 'max-content' }}
            rowSelection={{
              type: 'radio',
              selectedRowKeys: selectedCandidate ? [selectedCandidate] : [],
              onChange: (keys) => setSelectedCandidate(String(keys[0] ?? '')),
              getCheckboxProps: (candidate) => ({ disabled: !candidate.addable }),
            }}
            onRow={(candidate) => ({
              onClick: () => { if (candidate.addable) setSelectedCandidate(candidate.candidateId) },
              style: { cursor: candidate.addable ? 'pointer' : 'not-allowed' },
            })}
            columns={[
              {
                title: 'USB Device', dataIndex: 'usbAddress',
                render: (_, candidate) => candidate.usbAddress || <Typography.Text type="secondary">—</Typography.Text>,
              },
              {
                title: 'VID:PID', key: 'vidpid',
                render: (_, candidate) => candidate.vendorId && candidate.productId
                  ? <Typography.Text code>{candidate.vendorId}:{candidate.productId}</Typography.Text>
                  : <Typography.Text type="secondary">—</Typography.Text>,
              },
              {
                title: '型号', dataIndex: 'model', ellipsis: true,
                render: (_, candidate) => <ModemModel value={candidate.model} strong />,
              },
              {
                title: '序列标识', dataIndex: 'usbSerialHint',
                render: (_, candidate) => candidate.usbSerialHint
                  ? <Typography.Text title="由 USB Serial 生成的本机脱敏标识">{candidate.usbSerialHint}</Typography.Text>
                  : <Typography.Text type="secondary">未提供</Typography.Text>,
              },
              {
                title: '支持状态', key: 'support',
                render: (_, candidate) => <Space orientation="vertical" size={0}>
                  <Tag color={candidate.supportStatus === 'supported' ? 'green' : 'orange'}>
                    {candidate.supportStatus === 'supported' ? '系统支持' : '暂不可添加'}
                  </Tag>
                  {!candidate.addable && <Typography.Text type="secondary">{readinessLabels[candidate.readinessReason]}</Typography.Text>}
                </Space>,
              },
              {
                title: 'SIM', dataIndex: 'simPresence',
                render: (_, candidate) => <SIMPresenceTag value={candidate.simPresence} />,
              },
              {
                title: '能力', dataIndex: 'capabilities',
                render: (_, candidate) => <CapabilityTags capabilities={candidate.capabilities} />,
              },
            ]}
          />}
      </Space>
    </Modal>

  </PageContainer>
}

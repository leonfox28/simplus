import {
  DashboardOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import {
  ModalForm,
  PageContainer,
  ProCard,
  ProDescriptions,
  ProFormText,
  ProTable,
} from '@ant-design/pro-components'
import { Alert, Badge, Button, Flex, Grid, Popconfirm, Space, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import React, { useCallback, useEffect, useState } from 'react'
import {
  createMihomoSubscription,
  deleteMihomoSubscription,
  getLatestMihomoCore,
  getMihomoCoreStatus,
  getMihomoDashboardStatus,
  getMihomoRuntimeStatus,
  installLatestMihomoCore,
  listMihomoSubscriptions,
  refreshMihomoSubscription,
  restartMihomo,
  selectMihomoSubscription,
  startMihomo,
  stopMihomo,
  updateMihomoSubscription,
  type MihomoCoreCandidate,
  type MihomoCoreStatus,
  type MihomoDashboardStatus,
  type MihomoRuntimeStatus,
  type MihomoSubscription,
} from '@/api/client'
import { zashboardLaunchURL } from '@/mihomo/dashboard'

type CreateSubscriptionValues = { url: string }
type EditSubscriptionValues = { displayName: string; url: string }

const runtimeLabels = {
  running: { text: '运行中', status: 'success' as const },
  stopped: { text: '已停止', status: 'default' as const },
  fault: { text: '故障', status: 'error' as const },
}

function subscriptionStatus(record: MihomoSubscription, runningSubscriptionID: string | undefined) {
  const running = record.id === runningSubscriptionID
  return (
    <Space size={[0, 4]} wrap>
      {running && <Tag color="green">运行中</Tag>}
      {!running && record.selected && <Tag color="blue">已选择</Tag>}
      {!record.enabled && <Tag>已停用</Tag>}
      {record.lastRefreshStatus === 'failed' && <Tag color="red">更新失败</Tag>}
      {record.enabled && record.lastRefreshStatus !== 'failed' && !record.artifactReady && <Tag color="orange">未就绪</Tag>}
      {record.enabled && record.lastRefreshStatus !== 'failed' && record.artifactReady && !running && !record.selected && <Tag color="green">可用</Tag>}
    </Space>
  )
}

function subscriptionUpdatedAt(value: string) {
  const timestamp = dayjs(value)
  return value && timestamp.isValid() ? timestamp.format('YYYY-MM-DD HH:mm:ss') : '—'
}

export default function Mihomo() {
  const screens = Grid.useBreakpoint()
  const compact = !screens.md
  const [core, setCore] = useState<MihomoCoreStatus>()
  const [candidate, setCandidate] = useState<MihomoCoreCandidate>()
  const [runtime, setRuntime] = useState<MihomoRuntimeStatus>()
  const [dashboard, setDashboard] = useState<MihomoDashboardStatus>()
  const [subscriptions, setSubscriptions] = useState<MihomoSubscription[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')

  const load = useCallback(async () => {
    try {
      const [coreStatus, runtimeStatus, dashboardStatus, subscriptionItems] = await Promise.all([
        getMihomoCoreStatus(),
        getMihomoRuntimeStatus(),
        getMihomoDashboardStatus(),
        listMihomoSubscriptions(),
      ])
      setCore(coreStatus)
      setRuntime(runtimeStatus)
      setDashboard(dashboardStatus)
      setSubscriptions(subscriptionItems)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const run = async (key: string, operation: () => Promise<unknown>): Promise<boolean> => {
    setBusy(key)
    setError('')
    try {
      await operation()
      await load()
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      return false
    } finally {
      setBusy('')
    }
  }

  const subscriptionName = (id: string) => subscriptions.find((item) => item.id === id)?.displayName ?? '—'
  const runtimeState = runtimeLabels[runtime?.state ?? 'stopped']

  return (
    <PageContainer title="Mihomo 配置" subTitle="管理 Host VoWiFi 专用核心、订阅工件和运行生命周期">
      {error && (
        <Alert
          type="error"
          showIcon
          message="Mihomo 操作失败"
          description={error}
          closable
          onClose={() => setError('')}
          style={{ marginBottom: 16 }}
        />
      )}

      <ProCard title="运行状态" extra={<Badge status={runtimeState.status} text={runtimeState.text} />}>
        <Flex vertical={compact} justify="space-between" align={compact ? 'flex-start' : 'center'} gap="middle">
          <div>
            <Typography.Text type="secondary">当前运行配置</Typography.Text>
            <Flex align="center" gap="small" wrap style={{ marginTop: 4 }}>
              <Typography.Text strong>{runtime?.runningSubscriptionId ? subscriptionName(runtime.runningSubscriptionId) : '无'}</Typography.Text>
              {runtime?.pendingRestart && <Tag color="orange">等待重启应用</Tag>}
            </Flex>
          </div>
          <Space wrap>
            <Button
              type="primary"
              loading={busy === 'start'}
              disabled={runtime?.state === 'running' || !runtime?.selectedSubscriptionId}
              onClick={() => void run('start', startMihomo)}
            >
              启动
            </Button>
            <Button
              loading={busy === 'restart'}
              disabled={!runtime?.selectedSubscriptionId}
              onClick={() => void run('restart', restartMihomo)}
            >
              重启
            </Button>
            <Button
              danger
              loading={busy === 'stop'}
              disabled={runtime?.state !== 'running'}
              onClick={() => void run('stop', stopMihomo)}
            >
              停止
            </Button>
          </Space>
        </Flex>
      </ProCard>

      <ProCard title="External UI" style={{ marginTop: 16 }}>
        <Button
          icon={<DashboardOutlined />}
          disabled={!dashboard?.available || runtime?.state !== 'running'}
          href={zashboardLaunchURL(dashboard)}
          target="_blank"
          rel="noreferrer"
        >
          Zashboard
        </Button>
      </ProCard>

      <ProCard
        title="Mihomo Core"
        style={{ marginTop: 16 }}
        extra={<Tag color={core?.installed ? 'green' : 'orange'}>{core?.installed ? '已安装' : '未安装'}</Tag>}
      >
        <ProDescriptions
          column={compact ? 1 : 3}
          dataSource={core}
          columns={[
            { title: '版本', dataIndex: 'version' },
            { title: '架构', dataIndex: 'architecture' },
            { title: 'SHA-256', dataIndex: 'sha256', render: (value) => <span className="mono">{String(value || '—')}</span> },
          ]}
        />
        <Space wrap>
          <Button loading={busy === 'latest'} onClick={() => void run('latest', async () => setCandidate(await getLatestMihomoCore()))}>
            检查最新版
          </Button>
          {candidate && candidate.version !== core?.version && (
            <Button
              type="primary"
              loading={busy === 'install'}
              onClick={() => void run('install', async () => setCore(await installLatestMihomoCore()))}
            >
              安装 {candidate.version}
            </Button>
          )}
        </Space>
      </ProCard>

      <ProCard
        title="订阅管理"
        style={{ marginTop: 16 }}
        extra={(
          <ModalForm<CreateSubscriptionValues>
            title="新建订阅"
            trigger={<Button type="primary" icon={<PlusOutlined />}>新建</Button>}
            modalProps={{ destroyOnHidden: true }}
            submitter={{ searchConfig: { submitText: '保存', resetText: '取消' } }}
            onFinish={(values) => run('create', () => createMihomoSubscription({ url: values.url }))}
          >
            <ProFormText
              name="url"
              label="订阅 URL"
              placeholder="https://example.com/subscription"
              rules={[{ required: true, message: '请输入订阅 URL' }, { type: 'url', message: '请输入有效 URL' }]}
              fieldProps={{ autoComplete: 'url' }}
            />
            <Typography.Text type="secondary">保存后会立即下载、转换并校验订阅。</Typography.Text>
          </ModalForm>
        )}
      >
        <ProTable<MihomoSubscription>
          search={false}
          options={false}
          rowKey="id"
          dataSource={subscriptions}
          pagination={subscriptions.length > 10 ? { pageSize: 10 } : false}
          scroll={{ x: 'max-content' }}
          columns={[
            { title: '名称', dataIndex: 'displayName' },
            { title: 'URL', dataIndex: 'url', ellipsis: true, copyable: true },
            { title: '更新时间', dataIndex: 'lastRefreshAt', render: (value) => subscriptionUpdatedAt(String(value ?? '')) },
            {
              title: '状态',
              render: (_, record) => subscriptionStatus(record, runtime?.runningSubscriptionId),
            },
            {
              title: '操作',
              render: (_, record) => (
                <Space wrap>
                  <ModalForm<EditSubscriptionValues>
                    title="编辑订阅"
                    trigger={<Button icon={<EditOutlined />}>编辑</Button>}
                    initialValues={{ displayName: record.displayName, url: record.url }}
                    modalProps={{ destroyOnHidden: true }}
                    submitter={{ searchConfig: { submitText: '保存', resetText: '取消' } }}
                    onFinish={(values) => run(`edit:${record.id}`, () => updateMihomoSubscription(record.id, {
                      displayName: values.displayName,
                      url: values.url === record.url ? '' : values.url,
                      enabled: record.enabled,
                    }))}
                  >
                    <ProFormText name="displayName" label="名称" rules={[{ required: true }, { max: 80 }]} />
                    <ProFormText
                      name="url"
                      label="订阅 URL"
                      rules={[{ required: true }, { type: 'url', message: '请输入有效 URL' }]}
                      fieldProps={{ autoComplete: 'url' }}
                    />
                  </ModalForm>
                  <Button
                    icon={<ReloadOutlined />}
                    loading={busy === `refresh:${record.id}`}
                    onClick={() => void run(`refresh:${record.id}`, () => refreshMihomoSubscription(record.id))}
                  >
                    更新
                  </Button>
                  <Button
                    disabled={record.selected || !record.artifactReady}
                    onClick={() => void run(`select:${record.id}`, () => selectMihomoSubscription(record.id))}
                  >
                    切换
                  </Button>
                  <Popconfirm
                    title="删除订阅？"
                    onConfirm={() => run(`delete:${record.id}`, () => deleteMihomoSubscription(record.id))}
                  >
                    <Button
                      danger
                      icon={<DeleteOutlined />}
                      disabled={record.selected || record.id === runtime?.runningSubscriptionId}
                    >
                      删除
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      </ProCard>
    </PageContainer>
  )
}

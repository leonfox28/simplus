import { DashboardOutlined, DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Badge, Button, Card, Descriptions, Flex, Form, Grid, Input, Modal, Popconfirm, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import { useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  createMihomoSubscriptionMutation,
  deleteMihomoSubscriptionMutation,
  getLatestMihomoCoreOptions,
  getMihomoCoreStatusOptions,
  getMihomoCoreStatusQueryKey,
  getMihomoDashboardStatusOptions,
  getMihomoDashboardStatusQueryKey,
  getMihomoRuntimeStatusOptions,
  getMihomoRuntimeStatusQueryKey,
  installLatestMihomoCoreMutation,
  listMihomoSubscriptionsOptions,
  listMihomoSubscriptionsQueryKey,
  refreshMihomoSubscriptionMutation,
  restartMihomoMutation,
  selectMihomoSubscriptionMutation,
  startMihomoMutation,
  stopMihomoMutation,
  updateMihomoSubscriptionMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { MihomoSubscription } from '@/api/generated/types.gen'
import { PageHeader, PageSection, ResponsiveDataView } from '@/components/Page'
import { zashboardLaunchURL } from '@/mihomo/dashboard'

type SubscriptionValues = { displayName?: string; url: string }

const runtimeLabels = {
  running: { text: '运行中', status: 'success' as const },
  stopped: { text: '已停止', status: 'default' as const },
  fault: { text: '故障', status: 'error' as const },
}

function subscriptionStatus(record: MihomoSubscription, runningSubscriptionID?: string) {
  return <Space size={[0, 4]} wrap>
    {record.id === runningSubscriptionID && <Tag color="green">运行中</Tag>}
    {record.id !== runningSubscriptionID && record.selected && <Tag color="blue">已选择</Tag>}
    {!record.enabled && <Tag>已停用</Tag>}
    {record.lastRefreshStatus === 'failed' && <Tag color="red">更新失败</Tag>}
    {record.enabled && record.lastRefreshStatus !== 'failed' && !record.artifactReady && <Tag color="orange">未就绪</Tag>}
    {record.enabled && record.lastRefreshStatus !== 'failed' && record.artifactReady && record.id !== runningSubscriptionID && !record.selected && <Tag color="green">可用</Tag>}
  </Space>
}

export default function Mihomo() {
  const compact = !Grid.useBreakpoint().md
  const queryClient = useQueryClient()
  const [operationError, setOperationError] = useState<unknown>()
  const [busy, setBusy] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [editing, setEditing] = useState<MihomoSubscription>()
  const [createForm] = Form.useForm<SubscriptionValues>()
  const [editForm] = Form.useForm<SubscriptionValues>()
  const core = useQuery(getMihomoCoreStatusOptions())
  const latest = useQuery({ ...getLatestMihomoCoreOptions(), enabled: false })
  const runtime = useQuery({ ...getMihomoRuntimeStatusOptions(), refetchInterval: 10_000, refetchIntervalInBackground: false })
  const dashboard = useQuery(getMihomoDashboardStatusOptions())
  const subscriptions = useQuery(listMihomoSubscriptionsOptions())
  const items = subscriptions.data?.subscriptions ?? []
  const refresh = async () => Promise.all([
    queryClient.invalidateQueries({ queryKey: getMihomoCoreStatusQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getMihomoRuntimeStatusQueryKey() }),
    queryClient.invalidateQueries({ queryKey: getMihomoDashboardStatusQueryKey() }),
    queryClient.invalidateQueries({ queryKey: listMihomoSubscriptionsQueryKey() }),
  ])
  const mutationOptions = {
    onSuccess: refresh,
    onError: setOperationError,
    onSettled: () => setBusy(''),
  }
  const start = useMutation({ ...startMihomoMutation(), ...mutationOptions })
  const restart = useMutation({ ...restartMihomoMutation(), ...mutationOptions })
  const stop = useMutation({ ...stopMihomoMutation(), ...mutationOptions })
  const install = useMutation({ ...installLatestMihomoCoreMutation(), ...mutationOptions })
  const create = useMutation({ ...createMihomoSubscriptionMutation(), ...mutationOptions, onSuccess: async () => { setCreateOpen(false); createForm.resetFields(); await refresh() } })
  const update = useMutation({ ...updateMihomoSubscriptionMutation(), ...mutationOptions, onSuccess: async () => { setEditing(undefined); await refresh() } })
  const refreshSubscription = useMutation({ ...refreshMihomoSubscriptionMutation(), ...mutationOptions })
  const selectSubscription = useMutation({ ...selectMihomoSubscriptionMutation(), ...mutationOptions })
  const removeSubscription = useMutation({ ...deleteMihomoSubscriptionMutation(), ...mutationOptions })
  const runtimeState = runtimeLabels[runtime.data?.state ?? 'stopped']
  const subscriptionName = (id: string) => items.find((item) => item.id === id)?.displayName ?? '—'
  const fail = (key: string, run: () => void) => { setBusy(key); setOperationError(undefined); run() }
  const actions = (record: MihomoSubscription) => <Space wrap>
    <Button icon={<EditOutlined />} onClick={() => { update.reset(); setOperationError(undefined); setEditing(record); editForm.setFieldsValue({ displayName: record.displayName, url: record.url }) }}>编辑</Button>
    <Button icon={<ReloadOutlined />} loading={busy === `refresh:${record.id}`} onClick={() => fail(`refresh:${record.id}`, () => refreshSubscription.mutate({ path: { subscriptionId: record.id } }))}>更新</Button>
    <Button disabled={record.selected || !record.artifactReady} onClick={() => fail(`select:${record.id}`, () => selectSubscription.mutate({ path: { subscriptionId: record.id } }))}>切换</Button>
    <Popconfirm title="删除订阅？" onConfirm={() => fail(`delete:${record.id}`, () => removeSubscription.mutate({ path: { subscriptionId: record.id } }))}><Button danger icon={<DeleteOutlined />} disabled={record.selected || record.id === runtime.data?.runningSubscriptionId}>删除</Button></Popconfirm>
  </Space>
  const columns: TableColumnsType<MihomoSubscription> = [
    { title: '名称', dataIndex: 'displayName' },
    { title: 'URL', render: (_, record) => record.urlHint || '已配置' },
    { title: '更新时间', dataIndex: 'lastRefreshAt', render: (value) => value && dayjs(String(value)).isValid() ? dayjs(String(value)).format('YYYY-MM-DD HH:mm:ss') : '—' },
    { title: '状态', render: (_, record) => subscriptionStatus(record, runtime.data?.runningSubscriptionId) },
    { title: '操作', render: (_, record) => actions(record) },
  ]
  const queryError = core.error ?? runtime.error ?? dashboard.error ?? subscriptions.error

  return <main className="page-content">
    <PageHeader title="Mihomo 配置" subtitle="管理 Host VoWiFi 专用核心、订阅工件和运行生命周期" />
    {Boolean(operationError || queryError) && <Alert className="page-alert" type="error" showIcon title="Mihomo 操作失败" description={displayApiError(operationError ?? queryError)} closable onClose={() => setOperationError(undefined)} />}
    <PageSection title="运行状态" extra={<Badge status={runtimeState.status} text={runtimeState.text} />}>
      <Flex vertical={compact} justify="space-between" align={compact ? 'flex-start' : 'center'} gap="middle">
        <div><Typography.Text type="secondary">当前运行配置</Typography.Text><Flex align="center" gap="small" wrap><Typography.Text strong>{runtime.data?.runningSubscriptionId ? subscriptionName(runtime.data.runningSubscriptionId) : '无'}</Typography.Text>{runtime.data?.pendingRestart && <Tag color="orange">等待重启应用</Tag>}</Flex></div>
        <Space wrap>
          <Button type="primary" loading={busy === 'start'} disabled={runtime.data?.state === 'running' || !runtime.data?.selectedSubscriptionId} onClick={() => fail('start', () => start.mutate({}))}>启动</Button>
          <Button loading={busy === 'restart'} disabled={!runtime.data?.selectedSubscriptionId} onClick={() => fail('restart', () => restart.mutate({}))}>重启</Button>
          <Button danger loading={busy === 'stop'} disabled={runtime.data?.state !== 'running'} onClick={() => fail('stop', () => stop.mutate({}))}>停止</Button>
        </Space>
      </Flex>
    </PageSection>
    <PageSection title="External UI" className="page-section"><Button icon={<DashboardOutlined />} disabled={!dashboard.data?.available || runtime.data?.state !== 'running'} href={zashboardLaunchURL(dashboard.data)} target="_blank" rel="noreferrer">Zashboard</Button></PageSection>
    <PageSection title="Mihomo Core" className="page-section" extra={<Tag color={core.data?.installed ? 'green' : 'orange'}>{core.data?.installed ? '已安装' : '未安装'}</Tag>}>
      <Descriptions column={compact ? 1 : 3} items={[
        { key: 'version', label: '版本', children: core.data?.version || '—' },
        { key: 'architecture', label: '架构', children: core.data?.architecture || '—' },
        { key: 'sha', label: 'SHA-256', children: <span className="mono">{core.data?.sha256 || '—'}</span> },
      ]} />
      <Space wrap><Button loading={latest.isFetching} onClick={() => void latest.refetch()}>检查最新版</Button>{latest.data && latest.data.version !== core.data?.version && <Button type="primary" loading={busy === 'install'} onClick={() => fail('install', () => install.mutate({}))}>安装 {latest.data.version}</Button>}</Space>
      {latest.error && <Alert className="page-alert" type="error" showIcon title="无法检查最新版" description={displayApiError(latest.error)} />}
    </PageSection>
    <PageSection title="订阅管理" className="page-section" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { create.reset(); setOperationError(undefined); setCreateOpen(true) }}>新建</Button>}>
      <ResponsiveDataView data={items} columns={columns} rowKey="id" loading={subscriptions.isPending} emptyText="尚未配置订阅" renderCard={(record) => <Card className="mobile-record-card" title={record.displayName} extra={subscriptionStatus(record, runtime.data?.runningSubscriptionId)}><Descriptions column={1} size="small" items={[
        { key: 'url', label: 'URL', children: record.urlHint || '已配置' },
        { key: 'updated', label: '更新时间', children: record.lastRefreshAt && dayjs(record.lastRefreshAt).isValid() ? dayjs(record.lastRefreshAt).format('YYYY-MM-DD HH:mm:ss') : '—' },
        { key: 'nodes', label: '节点', children: record.nodeCount },
      ]} />{actions(record)}</Card>} />
    </PageSection>
    <Modal title="新建订阅" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={() => createForm.submit()} confirmLoading={create.isPending} destroyOnHidden>
      {create.error && <Alert className="page-alert" type="error" showIcon title="无法新建订阅" description={displayApiError(create.error)} />}
      <Form<SubscriptionValues> form={createForm} layout="vertical" onFinish={(values) => fail('create', () => create.mutate({ body: { url: values.url } }))}><Form.Item name="url" label="订阅 URL" rules={[{ required: true }, { type: 'url' }]}><Input autoComplete="url" /></Form.Item><Typography.Text type="secondary">保存后会立即下载、转换并校验订阅。</Typography.Text></Form>
    </Modal>
    <Modal title="编辑订阅" open={Boolean(editing)} onCancel={() => setEditing(undefined)} onOk={() => editForm.submit()} confirmLoading={update.isPending} destroyOnHidden>
      {update.error && <Alert className="page-alert" type="error" showIcon title="无法编辑订阅" description={displayApiError(update.error)} />}
      <Form<SubscriptionValues> form={editForm} layout="vertical" onFinish={(values) => editing && fail(`edit:${editing.id}`, () => update.mutate({ path: { subscriptionId: editing.id }, body: { displayName: values.displayName ?? editing.displayName, url: values.url === editing.url ? '' : values.url, enabled: editing.enabled } }))}><Form.Item name="displayName" label="名称" rules={[{ required: true }, { max: 80 }]}><Input /></Form.Item><Form.Item name="url" label="订阅 URL" rules={[{ required: true }, { type: 'url' }]}><Input autoComplete="url" /></Form.Item></Form>
    </Modal>
  </main>
}

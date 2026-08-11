import { DeleteOutlined, EditOutlined, ExportOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Checkbox, Descriptions, Form, Grid, Input, Modal, Popconfirm, Select, Space, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  cancelFeishuNotificationBindingMutation,
  createNotificationChannelMutation,
  deleteNotificationChannelMutation,
  getFeishuNotificationBindingOptions,
  getFeishuNotificationBindingQueryKey,
  listNotificationChannelsOptions,
  listNotificationChannelsQueryKey,
  startFeishuNotificationBindingMutation,
  testNotificationChannelMutation,
  updateNotificationChannelMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { FeishuNotificationBinding, NotificationChannel, NotificationEventKind } from '@/api/generated/types.gen'
import { PageHeader, PageSection, ResponsiveDataView } from '@/components/Page'

type ChannelValues = {
  provider: 'wecom' | 'feishu'
  displayName: string
  webhookUrl: string
  signingSecret?: string
  eventKinds: NotificationEventKind[]
}
type EditValues = { displayName: string; eventKinds: NotificationEventKind[] }

const events: NotificationEventKind[] = ['sms.received', 'sms.failed', 'call.incoming', 'call.missed', 'system.degraded']
const bindingMessages: Record<FeishuNotificationBinding['state'], string> = {
  idle: '尚未发起绑定。',
  waiting: '等待管理员在飞书完成授权。',
  testing: '授权完成，正在发送绑定测试消息。',
  succeeded: '飞书私聊绑定成功。',
  failed: '绑定失败，可重新生成验证链接。',
  expired: '验证链接已过期，请重新发起绑定。',
  cancelled: '绑定已取消。',
}
const bindingErrors: Record<string, string> = {
  FEISHU_BINDING_DENIED: '管理员拒绝了飞书授权。',
  FEISHU_BINDING_EXPIRED: '飞书授权已过期。',
  FEISHU_BINDING_LARK_UNSUPPORTED: '当前仅支持飞书中国版租户。',
  FEISHU_BINDING_RESULT_INVALID: '飞书返回了无法使用的授权结果。',
  FEISHU_BINDING_PROVIDER_FAILED: '暂时无法连接飞书授权服务。',
  FEISHU_BINDING_TEST_FAILED: '授权完成，但测试私聊未能送达。飞书侧应用可能已保留。',
  FEISHU_BINDING_PERSIST_FAILED: '测试消息已发送，但本地保存失败。飞书侧应用可能已保留。',
}

export default function Notifications() {
  const compact = !Grid.useBreakpoint().md
  const queryClient = useQueryClient()
  const [form] = Form.useForm<ChannelValues>()
  const [editForm] = Form.useForm<EditValues>()
  const [operationError, setOperationError] = useState<unknown>()
  const [busyKey, setBusyKey] = useState('')
  const [editing, setEditing] = useState<NotificationChannel>()
  const lastSucceededChannel = useRef('')
  const channelsQuery = useQuery(listNotificationChannelsOptions())
  const bindingQuery = useQuery({
    ...getFeishuNotificationBindingOptions(),
    refetchInterval: (query) => {
      const state = query.state.data?.state
      return state === 'waiting' || state === 'testing' ? 1500 : false
    },
    refetchIntervalInBackground: false,
  })
  const refreshChannels = () => queryClient.invalidateQueries({ queryKey: listNotificationChannelsQueryKey() })
  const refreshBinding = () => queryClient.invalidateQueries({ queryKey: getFeishuNotificationBindingQueryKey() })
  const create = useMutation({ ...createNotificationChannelMutation(), onSuccess: async () => { form.resetFields(); await refreshChannels() }, onError: setOperationError })
  const update = useMutation({
    ...updateNotificationChannelMutation(),
    onSuccess: async () => { setEditing(undefined); setBusyKey(''); await refreshChannels() },
    onError: (error) => { setBusyKey(''); setOperationError(error) },
  })
  const remove = useMutation({ ...deleteNotificationChannelMutation(), onSuccess: async () => { setBusyKey(''); await refreshChannels() }, onError: (error) => { setBusyKey(''); setOperationError(error) } })
  const test = useMutation({ ...testNotificationChannelMutation(), onSuccess: async () => { setBusyKey(''); await refreshChannels() }, onError: (error) => { setBusyKey(''); setOperationError(error) } })
  const startBinding = useMutation({ ...startFeishuNotificationBindingMutation(), onSuccess: refreshBinding, onError: setOperationError })
  const cancelBinding = useMutation({ ...cancelFeishuNotificationBindingMutation(), onSuccess: refreshBinding, onError: setOperationError })
  const channels = channelsQuery.data?.channels ?? []
  const binding = bindingQuery.data

  useEffect(() => {
    if (binding?.state !== 'succeeded' || !binding.channelId || lastSucceededChannel.current === binding.channelId) return
    lastSucceededChannel.current = binding.channelId
    void refreshChannels()
  }, [binding?.channelId, binding?.state])

  const mutateSettings = (record: NotificationChannel, values: EditValues, enabled = record.enabled) => {
    setOperationError(undefined)
    setBusyKey(`update:${record.id}`)
    update.mutate({ path: { channelId: record.id }, body: {
      provider: record.provider, displayName: values.displayName,
      webhookUrl: '', signingSecret: '', enabled, eventKinds: values.eventKinds,
    } })
  }
  const openEdit = (record: NotificationChannel) => {
    setEditing(record)
    editForm.setFieldsValue({ displayName: record.displayName, eventKinds: record.eventKinds })
  }
  const actions = (record: NotificationChannel) => <Space wrap>
    <Button icon={<EditOutlined />} onClick={() => openEdit(record)}>编辑</Button>
    <Button loading={busyKey === `test:${record.id}`} onClick={() => { setBusyKey(`test:${record.id}`); setOperationError(undefined); test.mutate({ path: { channelId: record.id } }) }}>测试</Button>
    <Button loading={busyKey === `update:${record.id}`} onClick={() => mutateSettings(record, { displayName: record.displayName, eventKinds: record.eventKinds }, !record.enabled)}>{record.enabled ? '停用' : '启用'}</Button>
    <Popconfirm
      title={record.deliveryMode === 'feishu_app' ? '只删除 Simplus 本地绑定？' : '删除渠道？'}
      description={record.deliveryMode === 'feishu_app' ? '飞书侧自动创建的应用仍会保留，需在飞书开发者后台另行处理。' : undefined}
      onConfirm={() => { setBusyKey(`delete:${record.id}`); setOperationError(undefined); remove.mutate({ path: { channelId: record.id } }) }}
    ><Button danger loading={busyKey === `delete:${record.id}`} icon={<DeleteOutlined />}>删除</Button></Popconfirm>
  </Space>
  const columns: TableColumnsType<NotificationChannel> = [
    { title: '名称', dataIndex: 'displayName' },
    { title: '平台', dataIndex: 'provider' },
    { title: '目标', render: (_, record) => record.targetType === 'authorized_user' ? '授权用户私聊' : record.webhookHint },
    { title: '事件', dataIndex: 'eventKinds', render: (value: NotificationEventKind[]) => <Space wrap>{value.map((event) => <Tag key={event}>{event}</Tag>)}</Space> },
    { title: '最近投递', dataIndex: 'lastDeliveryStatus' },
    { title: '状态', dataIndex: 'enabled', render: (value: boolean) => <Tag color={value ? 'green' : 'default'}>{value ? '启用' : '停用'}</Tag> },
    { title: '操作', render: (_, record) => actions(record) },
  ]
  const bindingMessage = binding?.errorCode ? (bindingErrors[binding.errorCode] ?? bindingMessages[binding.state]) : binding ? bindingMessages[binding.state] : ''
  const bindingActive = binding?.state === 'waiting' || binding?.state === 'testing'

  return <main className="page-content">
    <PageHeader title="通知渠道" subtitle="企业微信与飞书单向出站通知，不接受远程命令" />
    {Boolean(operationError || channelsQuery.error || bindingQuery.error) && <Alert className="page-alert" type="error" showIcon title={displayApiError(operationError ?? channelsQuery.error ?? bindingQuery.error)} />}
    <PageSection title="绑定飞书私聊">
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Typography.Text>{bindingMessage || '正在读取绑定状态…'}</Typography.Text>
        {binding?.state === 'waiting' && binding.verificationUrl && <>
          <Input aria-label="飞书验证链接" value={binding.verificationUrl} readOnly />
          <Space wrap>
            <Typography.Text copyable={{ text: binding.verificationUrl }}>复制验证链接</Typography.Text>
            <Button type="link" icon={<ExportOutlined />} href={binding.verificationUrl} target="_blank" rel="noreferrer">打开飞书授权</Button>
            {binding.expiresAt && <Typography.Text type="secondary">有效期至 {new Date(binding.expiresAt).toLocaleString()}</Typography.Text>}
          </Space>
        </>}
        <Space wrap>
          <Button type="primary" loading={startBinding.isPending} disabled={bindingActive} onClick={() => { setOperationError(undefined); startBinding.mutate({}) }}>
            {binding?.state === 'waiting' ? '等待授权' : '绑定飞书私聊'}
          </Button>
          {binding?.state === 'waiting' && <Button loading={cancelBinding.isPending} onClick={() => cancelBinding.mutate({})}>取消绑定</Button>}
        </Space>
      </Space>
    </PageSection>
    <PageSection title="手工 Webhook（高级入口）">
      <Form<ChannelValues> form={form} layout={compact ? 'vertical' : 'inline'} initialValues={{ eventKinds: [] }} onFinish={(values) => {
        setOperationError(undefined)
        create.mutate({ body: { ...values, signingSecret: values.signingSecret ?? '', enabled: true } })
      }}>
        <Form.Item name="provider" label="平台" rules={[{ required: true }]}><Select style={{ minWidth: 120 }} options={[{ value: 'wecom', label: '企业微信' }, { value: 'feishu', label: '飞书' }]} /></Form.Item>
        <Form.Item name="displayName" label="名称" rules={[{ required: true }]}><Input maxLength={80} /></Form.Item>
        <Form.Item name="webhookUrl" label="Webhook" rules={[{ required: true }, { type: 'url' }]}><Input autoComplete="url" /></Form.Item>
        <Form.Item name="signingSecret" label="签名密钥"><Input.Password autoComplete="new-password" /></Form.Item>
        <Form.Item name="eventKinds" label="事件" rules={[{ required: true }]}><Checkbox.Group options={events.map((event) => ({ value: event, label: event }))} /></Form.Item>
        <Form.Item><Button htmlType="submit" loading={create.isPending}>添加 Webhook</Button></Form.Item>
      </Form>
    </PageSection>
    <PageSection title="已配置渠道" className="page-section">
      <ResponsiveDataView data={channels} columns={columns} rowKey="id" loading={channelsQuery.isPending} emptyText="尚未配置通知渠道" renderCard={(record) => <Card className="mobile-record-card" title={record.displayName} extra={<Tag color={record.enabled ? 'green' : 'default'}>{record.enabled ? '启用' : '停用'}</Tag>}><Descriptions column={1} size="small" items={[
        { key: 'provider', label: '平台', children: record.provider },
        { key: 'target', label: '目标', children: record.targetType === 'authorized_user' ? '授权用户私聊' : record.webhookHint },
        { key: 'events', label: '事件', children: <Space wrap>{record.eventKinds.map((event) => <Tag key={event}>{event}</Tag>)}</Space> },
        { key: 'delivery', label: '最近投递', children: record.lastDeliveryStatus || '—' },
      ]} />{actions(record)}</Card>} />
    </PageSection>
    <Modal title="编辑通知渠道" open={Boolean(editing)} onCancel={() => setEditing(undefined)} onOk={() => editForm.submit()} confirmLoading={Boolean(editing && busyKey === `update:${editing.id}`)}>
      <Form<EditValues> form={editForm} layout="vertical" onFinish={(values) => { if (editing) mutateSettings(editing, values) }}>
        <Form.Item name="displayName" label="名称" rules={[{ required: true }]}><Input maxLength={80} /></Form.Item>
        <Form.Item name="eventKinds" label="事件" rules={[{ required: true }]}><Checkbox.Group options={events.map((event) => ({ value: event, label: event }))} /></Form.Item>
      </Form>
    </Modal>
  </main>
}

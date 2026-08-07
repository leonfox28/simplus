import { DeleteOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Checkbox, Descriptions, Form, Grid, Input, Popconfirm, Select, Space, Tag } from 'antd'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  createNotificationChannelMutation,
  deleteNotificationChannelMutation,
  listNotificationChannelsOptions,
  listNotificationChannelsQueryKey,
  testNotificationChannelMutation,
  updateNotificationChannelMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { NotificationChannel, NotificationEventKind } from '@/api/generated/types.gen'
import { PageHeader, PageSection, ResponsiveDataView } from '@/components/Page'

type ChannelValues = {
  provider: 'wecom' | 'feishu'
  displayName: string
  webhookUrl: string
  signingSecret?: string
  eventKinds: NotificationEventKind[]
}
const events: NotificationEventKind[] = ['sms.received', 'sms.failed', 'call.incoming', 'call.missed', 'system.degraded']

export default function Notifications() {
  const compact = !Grid.useBreakpoint().md
  const queryClient = useQueryClient()
  const [form] = Form.useForm<ChannelValues>()
  const [operationError, setOperationError] = useState<unknown>()
  const channelsQuery = useQuery(listNotificationChannelsOptions())
  const refresh = () => queryClient.invalidateQueries({ queryKey: listNotificationChannelsQueryKey() })
  const create = useMutation({ ...createNotificationChannelMutation(), onSuccess: async () => { form.resetFields(); await refresh() }, onError: setOperationError })
  const update = useMutation({ ...updateNotificationChannelMutation(), onSuccess: refresh, onError: setOperationError })
  const remove = useMutation({ ...deleteNotificationChannelMutation(), onSuccess: refresh, onError: setOperationError })
  const test = useMutation({ ...testNotificationChannelMutation(), onSuccess: refresh, onError: setOperationError })
  const channels = channelsQuery.data?.channels ?? []
  const actions = (record: NotificationChannel) => <Space wrap>
    <Button loading={test.isPending} onClick={() => test.mutate({ path: { channelId: record.id } })}>测试</Button>
    <Button loading={update.isPending} onClick={() => update.mutate({ path: { channelId: record.id }, body: { provider: record.provider, displayName: record.displayName, webhookUrl: '', signingSecret: '', enabled: !record.enabled, eventKinds: record.eventKinds } })}>{record.enabled ? '停用' : '启用'}</Button>
    <Popconfirm title="删除渠道？" onConfirm={() => remove.mutate({ path: { channelId: record.id } })}><Button danger icon={<DeleteOutlined />}>删除</Button></Popconfirm>
  </Space>
  const columns: TableColumnsType<NotificationChannel> = [
    { title: '名称', dataIndex: 'displayName' }, { title: '平台', dataIndex: 'provider' },
    { title: 'Webhook', dataIndex: 'webhookHint' },
    { title: '事件', dataIndex: 'eventKinds', render: (value) => <Space wrap>{(value as string[]).map((event) => <Tag key={event}>{event}</Tag>)}</Space> },
    { title: '最近投递', dataIndex: 'lastDeliveryStatus' },
    { title: '状态', dataIndex: 'enabled', render: (value) => <Tag color={value ? 'green' : 'default'}>{value ? '启用' : '停用'}</Tag> },
    { title: '操作', render: (_, record) => actions(record) },
  ]
  return <main className="page-content">
    <PageHeader title="通知渠道" subtitle="企业微信与飞书单向出站通知，不接受远程命令" />
    {Boolean(operationError || channelsQuery.error) && <Alert className="page-alert" type="error" showIcon title={displayApiError(operationError ?? channelsQuery.error)} />}
    <PageSection title="添加渠道">
      <Form<ChannelValues> form={form} layout={compact ? 'vertical' : 'inline'} initialValues={{ eventKinds: [] }} onFinish={(values) => {
        setOperationError(undefined)
        create.mutate({ body: { ...values, signingSecret: values.signingSecret ?? '', enabled: true } })
      }}>
        <Form.Item name="provider" label="平台" rules={[{ required: true }]}><Select style={{ minWidth: 120 }} options={[{ value: 'wecom', label: '企业微信' }, { value: 'feishu', label: '飞书' }]} /></Form.Item>
        <Form.Item name="displayName" label="名称" rules={[{ required: true }]}><Input maxLength={80} /></Form.Item>
        <Form.Item name="webhookUrl" label="Webhook" rules={[{ required: true }, { type: 'url' }]}><Input autoComplete="url" /></Form.Item>
        <Form.Item name="signingSecret" label="签名密钥"><Input.Password autoComplete="new-password" /></Form.Item>
        <Form.Item name="eventKinds" label="事件" rules={[{ required: true }]}><Checkbox.Group options={events.map((event) => ({ value: event, label: event }))} /></Form.Item>
        <Form.Item><Button type="primary" htmlType="submit" loading={create.isPending}>添加渠道</Button></Form.Item>
      </Form>
    </PageSection>
    <PageSection title="已配置渠道" className="page-section">
      <ResponsiveDataView data={channels} columns={columns} rowKey="id" loading={channelsQuery.isPending} emptyText="尚未配置通知渠道" renderCard={(record) => <Card className="mobile-record-card" title={record.displayName} extra={<Tag color={record.enabled ? 'green' : 'default'}>{record.enabled ? '启用' : '停用'}</Tag>}><Descriptions column={1} size="small" items={[
        { key: 'provider', label: '平台', children: record.provider },
        { key: 'webhook', label: 'Webhook', children: record.webhookHint },
        { key: 'events', label: '事件', children: <Space wrap>{record.eventKinds.map((event) => <Tag key={event}>{event}</Tag>)}</Space> },
        { key: 'delivery', label: '最近投递', children: record.lastDeliveryStatus || '—' },
      ]} />{actions(record)}</Card>} />
    </PageSection>
  </main>
}

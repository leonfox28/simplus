import { DeleteOutlined, SendOutlined, UserAddOutlined } from '@ant-design/icons'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Card, Descriptions, Form, Input, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useMemo, useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  createContactMutation,
  deleteContactMutation,
  deleteMessageMutation,
  listContactsOptions,
  listContactsQueryKey,
  listManagedLinesOptions,
  listMessagesInfiniteOptions,
  listMessagesInfiniteQueryKey,
  sendMessageMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { SmsMessage } from '@/api/generated/types.gen'
import { PageHeader, PageSection, ResponsiveDataView } from '@/components/Page'
import { sortSMSMessagesForDisplay } from '@/messages/order'
import { smsStatusPresentation } from '@/messages/status'

type SendValues = { lineId: string; destination: string; body: string }
type ContactValues = { displayName: string; phoneNumber: string }

function operationId() {
  return crypto.randomUUID?.() ?? `operation_${Date.now()}_abcdefghijkl`
}

export default function Messages() {
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [sendForm] = Form.useForm<SendValues>()
  const [contactForm] = Form.useForm<ContactValues>()
  const [conversationLine, setConversationLine] = useState('')
  const [conversationAddress, setConversationAddress] = useState('')
  const [filter, setFilter] = useState<{ lineId: string; remoteAddress: string }>()
  const [operationError, setOperationError] = useState<unknown>()
  const linesQuery = useQuery(listManagedLinesOptions())
  const contactsQuery = useQuery(listContactsOptions())
  const messagesOptions = listMessagesInfiniteOptions({ query: { limit: 20, ...filter } })
  const messagesQuery = useInfiniteQuery({
    ...messagesOptions,
    initialPageParam: { query: {} },
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  })
  const lines = linesQuery.data?.lines ?? []
  const contacts = contactsQuery.data?.contacts ?? []
  const items = useMemo(() => sortSMSMessagesForDisplay(messagesQuery.data?.pages.flatMap((page) => page.messages) ?? []), [messagesQuery.data])
  const historyKey = listMessagesInfiniteQueryKey()

  const refreshMessages = () => queryClient.invalidateQueries({ queryKey: historyKey })
  const send = useMutation({
    ...sendMessageMutation(),
    onSuccess: async (result) => {
      await refreshMessages()
      sendForm.setFieldValue('body', '')
      if (result.status === 'unconfirmed') {
        void message.warning(result.errorCode === 'IMS_SMS_ACCEPTED_AWAITING_REPORT'
          ? '短信已提交，正在等待运营商确认，请勿重复发送。'
          : '短信可能已经送达，但运营商未返回最终确认，请勿重复发送。')
      } else if (result.status === 'failed') void message.error('短信发送失败。')
      else void message.success('短信已发送。')
    },
    onError: setOperationError,
  })
  const removeMessage = useMutation({
    ...deleteMessageMutation(), onSuccess: refreshMessages, onError: setOperationError,
  })
  const createContact = useMutation({
    ...createContactMutation(),
    onSuccess: async () => { contactForm.resetFields(); await queryClient.invalidateQueries({ queryKey: listContactsQueryKey() }) },
    onError: setOperationError,
  })
  const removeContact = useMutation({
    ...deleteContactMutation(), onSuccess: () => queryClient.invalidateQueries({ queryKey: listContactsQueryKey() }), onError: setOperationError,
  })

  const columns: TableColumnsType<SmsMessage> = [
    { title: '时间', dataIndex: 'createdAt', render: (value) => new Date(String(value)).toLocaleString() },
    { title: '线路', dataIndex: 'lineId' },
    { title: '方向', dataIndex: 'direction', render: (value) => <Tag>{value === 'inbound' ? '收到' : '发出'}</Tag> },
    { title: '号码', dataIndex: 'remoteAddress' },
    { title: '内容', dataIndex: 'body', ellipsis: true },
    { title: '状态', render: (_, record) => { const status = smsStatusPresentation(record); return <Tag color={status.color}>{status.label}</Tag> } },
    { title: '操作', render: (_, record) => <Popconfirm title="删除这条记录？" onConfirm={() => removeMessage.mutate({ path: { messageId: record.id } })}><Button aria-label="删除短信" danger type="text" icon={<DeleteOutlined />} /></Popconfirm> },
  ]
  const queryError = messagesQuery.error ?? linesQuery.error ?? contactsQuery.error

  return <main className="page-content">
    <PageHeader title="短信" subtitle="发送、接收和管理本地短信历史" />
    {Boolean(operationError || queryError) && <Alert className="page-alert" type="error" showIcon title={displayApiError(operationError ?? queryError)} />}
    <div className="page-grid two">
      <PageSection title="发送短信">
        <Form<SendValues> form={sendForm} layout="vertical" autoComplete="off" onFinish={(values) => {
          setOperationError(undefined)
          send.mutate({ body: { operationId: operationId(), lineId: values.lineId, destination: values.destination, body: values.body } })
        }}>
          <Form.Item name="lineId" label="线路" rules={[{ required: true, message: '请选择线路' }]}><Select options={lines.filter((line) => line.state === 'ready' && (line.capabilities.sms || line.capabilities.hostVoWifiAuth)).map((line) => ({ value: line.id, label: line.displayName }))} /></Form.Item>
          <Form.Item name="destination" label="号码" rules={[{ required: true }, { pattern: /^\+?[0-9]{3,20}$/, message: '请输入有效号码' }]}><Input inputMode="tel" autoComplete="tel" /></Form.Item>
          <Form.Item name="body" label="内容" rules={[{ required: true }]}><Input.TextArea maxLength={1600} showCount autoSize={{ minRows: 3, maxRows: 8 }} /></Form.Item>
          <Button type="primary" htmlType="submit" icon={<SendOutlined />} loading={send.isPending}>发送</Button>
        </Form>
      </PageSection>
      <PageSection title="联系人">
        <Form<ContactValues> form={contactForm} layout="vertical" onFinish={(values) => {
          setOperationError(undefined)
          createContact.mutate({ body: values })
        }}>
          <Form.Item name="displayName" label="名称" rules={[{ required: true }]}><Input maxLength={80} /></Form.Item>
          <Form.Item name="phoneNumber" label="号码" rules={[{ required: true }]}><Input inputMode="tel" autoComplete="tel" /></Form.Item>
          <Button htmlType="submit" icon={<UserAddOutlined />} loading={createContact.isPending}>添加</Button>
        </Form>
        <Space wrap className="contact-tags">{contacts.map((contact) => <Tag key={contact.id} closable onClose={(event) => { event.preventDefault(); removeContact.mutate({ path: { contactId: contact.id } }) }}>{contact.displayName} · {contact.phoneNumber}</Tag>)}</Space>
      </PageSection>
    </div>

    <PageSection title="短信历史" className="page-section" extra={<Space wrap>
      <Button onClick={() => void messagesQuery.refetch()}>刷新历史</Button>
      <Select allowClear placeholder="会话线路" value={conversationLine || undefined} onChange={(value) => setConversationLine(value ?? '')} options={lines.map((line) => ({ value: line.id, label: line.displayName }))} />
      <Input aria-label="会话号码" placeholder="会话号码" value={conversationAddress} onChange={(event) => setConversationAddress(event.target.value)} />
      <Button disabled={!conversationLine || !conversationAddress.trim()} onClick={() => setFilter({ lineId: conversationLine, remoteAddress: conversationAddress.trim() })}>筛选会话</Button>
      {filter && <Button onClick={() => { setFilter(undefined); setConversationLine(''); setConversationAddress('') }}>清除筛选</Button>}
    </Space>}>
      {messagesQuery.data?.pages[0]?.nearCapacity && <Alert className="page-alert" type="warning" showIcon title="短信历史接近容量上限，请按需清理旧记录。" />}
      <ResponsiveDataView
        data={items}
        columns={columns}
        rowKey="id"
        loading={messagesQuery.isPending}
        emptyText={filter ? '该会话暂无短信' : '暂无短信记录'}
        renderCard={(record) => {
          const status = smsStatusPresentation(record)
          return <Card className="mobile-record-card" title={record.remoteAddress} extra={<Tag color={status.color}>{status.label}</Tag>}>
            <Typography.Paragraph>{record.body}</Typography.Paragraph>
            <Descriptions column={1} size="small" items={[
              { key: 'time', label: '时间', children: new Date(record.createdAt).toLocaleString() },
              { key: 'line', label: '线路', children: record.lineId },
              { key: 'direction', label: '方向', children: record.direction === 'inbound' ? '收到' : '发出' },
            ]} />
            <Popconfirm title="删除这条记录？" onConfirm={() => removeMessage.mutate({ path: { messageId: record.id } })}><Button danger icon={<DeleteOutlined />}>删除</Button></Popconfirm>
          </Card>
        }}
      />
      {messagesQuery.hasNextPage && <div className="load-more"><Button loading={messagesQuery.isFetchingNextPage} onClick={() => void messagesQuery.fetchNextPage()}>加载更多</Button></div>}
    </PageSection>
  </main>
}

import {
  ArrowLeftOutlined,
  ContactsOutlined,
  DeleteOutlined,
  EditOutlined,
  MoreOutlined,
  PlusOutlined,
  SendOutlined,
} from '@ant-design/icons'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  App,
  AutoComplete,
  Badge,
  Button,
  Card,
  Drawer,
  Dropdown,
  Empty,
  Flex,
  Form,
  Grid,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  createContactMutation,
  deleteContactMutation,
  deleteMessageMutation,
  listContactsOptions,
  listContactsQueryKey,
  listManagedLinesOptions,
  listMessageConversationsInfiniteOptions,
  listMessageConversationsInfiniteQueryKey,
  listMessagesInfiniteOptions,
  listMessagesInfiniteQueryKey,
  markMessageConversationReadMutation,
  sendMessageMutation,
  updateContactMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { Contact, ManagedLine } from '@/api/generated/types.gen'
import { PageHeader } from '@/components/Page'
import { smsMessagesForDisplay } from '@/messages/order'
import { smsStatusPresentation } from '@/messages/status'

type ComposerValues = { body: string }
type ContactValues = { displayName: string; phoneNumber: string }
type NewConversationValues = { recipient: string }

const numericAddress = /^\+?[0-9]{3,20}$/

function operationId() {
  return crypto.randomUUID?.() ?? `operation_${Date.now()}_abcdefghijkl`
}

function canSendOnLine(line: ManagedLine) {
  return line.state === 'ready' && (line.capabilities.sms || line.capabilities.hostVoWifiAuth)
}

function unavailableLineReason(line: ManagedLine) {
  if (line.state === 'modem-offline') return '模组离线'
  if (line.state === 'sim-unavailable') return 'SIM 不可用'
  if (!line.capabilities.sms && !line.capabilities.hostVoWifiAuth) return '不支持短信'
  return ''
}

function formatTime(value: string) {
  return new Date(value).toLocaleString()
}

export default function Messages() {
  const queryClient = useQueryClient()
  const { message, modal } = App.useApp()
  const screens = Grid.useBreakpoint()
  const compact = !screens.md
  const [composerForm] = Form.useForm<ComposerValues>()
  const [contactForm] = Form.useForm<ContactValues>()
  const [newConversationForm] = Form.useForm<NewConversationValues>()
  const [selectedAddress, setSelectedAddress] = useState('')
  const [temporaryAddress, setTemporaryAddress] = useState('')
  const [mobileDetail, setMobileDetail] = useState(false)
  const [selectedLineID, setSelectedLineID] = useState('')
  const [operationError, setOperationError] = useState<unknown>()
  const [newConversationOpen, setNewConversationOpen] = useState(false)
  const [contactsOpen, setContactsOpen] = useState(false)
  const [editingContactID, setEditingContactID] = useState('')
  const [documentVisible, setDocumentVisible] = useState(() => document.visibilityState === 'visible')
  const selectionOwner = useRef('')
  const attemptedReadToken = useRef('')
  const historyRef = useRef<HTMLDivElement>(null)
  const historyAtBottom = useRef(true)
  const pendingOlderAnchor = useRef<{ height: number; top: number } | undefined>(undefined)
  const renderedHistory = useRef<{ address: string; lastID: string }>({ address: '', lastID: '' })

  const linesQuery = useQuery(listManagedLinesOptions())
  const contactsQuery = useQuery(listContactsOptions())
  const conversationOptions = listMessageConversationsInfiniteOptions({ query: { limit: 20 } })
  const conversationsQuery = useInfiniteQuery({
    ...conversationOptions,
    initialPageParam: { query: {} },
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  })
  const historyOptions = listMessagesInfiniteOptions({
    query: { limit: 20, remoteAddress: selectedAddress || undefined },
  })
  const messagesQuery = useInfiniteQuery({
    ...historyOptions,
    enabled: selectedAddress !== '',
    initialPageParam: { query: {} },
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  })

  const lines = linesQuery.data?.lines ?? []
  const contacts = contactsQuery.data?.contacts ?? []
  const conversations = useMemo(
    () => conversationsQuery.data?.pages.flatMap((page) => page.conversations) ?? [],
    [conversationsQuery.data],
  )
  const messages = useMemo(
    () => smsMessagesForDisplay(messagesQuery.data?.pages.map((page) => page.messages) ?? []),
    [messagesQuery.data],
  )
  const contactByNumber = useMemo(() => new Map(contacts.map((contact) => [contact.phoneNumber, contact])), [contacts])
  const lineByID = useMemo(() => new Map(lines.map((line) => [line.id, line])), [lines])
  const eligibleLines = lines.filter(canSendOnLine)
  const selectedConversation = conversations.find((conversation) => conversation.remoteAddress === selectedAddress)
  const selectedContact = contactByNumber.get(selectedAddress)
  const detailVisible = selectedAddress !== '' && (!compact || mobileDetail)
  const selectedLine = lineByID.get(selectedLineID)
  const selectedLineAvailable = Boolean(selectedLine && canSendOnLine(selectedLine))
  const addressReplyable = numericAddress.test(selectedAddress)

  const invalidateMessages = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: listMessageConversationsInfiniteQueryKey() }),
      queryClient.invalidateQueries({ queryKey: listMessagesInfiniteQueryKey() }),
    ])
  }

  const send = useMutation({
    ...sendMessageMutation(),
    onSuccess: async (result) => {
      await invalidateMessages()
      composerForm.resetFields(['body'])
      if (temporaryAddress === result.remoteAddress) setTemporaryAddress('')
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
    ...deleteMessageMutation(),
    onSuccess: invalidateMessages,
    onError: setOperationError,
  })
  const markRead = useMutation({
    ...markMessageConversationReadMutation(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: listMessageConversationsInfiniteQueryKey() }),
    onError: setOperationError,
  })
  const refreshContacts = () => queryClient.invalidateQueries({ queryKey: listContactsQueryKey() })
  const createContact = useMutation({
    ...createContactMutation(),
    onSuccess: async () => {
      contactForm.resetFields()
      await refreshContacts()
      void message.success('联系人已添加。')
    },
    onError: setOperationError,
  })
  const updateContact = useMutation({
    ...updateContactMutation(),
    onSuccess: async () => {
      setEditingContactID('')
      contactForm.resetFields()
      await refreshContacts()
      void message.success('联系人已更新。')
    },
    onError: setOperationError,
  })
  const removeContact = useMutation({
    ...deleteContactMutation(),
    onSuccess: async (_result, variables) => {
      if (editingContactID === variables.path.contactId) {
        setEditingContactID('')
        contactForm.resetFields()
      }
      await refreshContacts()
      void message.success('联系人已删除。')
    },
    onError: setOperationError,
  })

  useEffect(() => {
    const onVisibilityChange = () => setDocumentVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => document.removeEventListener('visibilitychange', onVisibilityChange)
  }, [])

  useEffect(() => {
    if (!compact && selectedAddress === '' && conversations[0]) setSelectedAddress(conversations[0].remoteAddress)
  }, [compact, conversations, selectedAddress])

  useEffect(() => {
    if (selectedAddress === '' || temporaryAddress === selectedAddress) return
    if (!conversationsQuery.isFetching && conversationsQuery.data && !selectedConversation) {
      setSelectedAddress('')
      setMobileDetail(false)
    }
  }, [conversationsQuery.data, conversationsQuery.isFetching, selectedAddress, selectedConversation, temporaryAddress])

  const defaultLineID = selectedConversation?.lastOutboundLineId ?? eligibleLines[0]?.id ?? ''
  useEffect(() => {
    if (selectionOwner.current !== selectedAddress) {
      selectionOwner.current = selectedAddress
      setSelectedLineID(defaultLineID)
      return
    }
    if (selectedAddress && !selectedLineID && defaultLineID) setSelectedLineID(defaultLineID)
  }, [defaultLineID, selectedAddress, selectedLineID])

  const readToken = messagesQuery.data?.pages[0]?.readThroughToken ?? ''

  useLayoutEffect(() => {
    const history = historyRef.current
    if (!history) return
    const pendingAnchor = pendingOlderAnchor.current
    if (pendingAnchor) {
      history.scrollTop = pendingAnchor.top + Math.max(0, history.scrollHeight - pendingAnchor.height)
      pendingOlderAnchor.current = undefined
      return
    }
    const previous = renderedHistory.current
    const lastID = messages.at(-1)?.id ?? ''
    if (previous.address !== selectedAddress || (previous.lastID !== lastID && historyAtBottom.current)) {
      history.scrollTop = history.scrollHeight
      historyAtBottom.current = true
    }
    renderedHistory.current = { address: selectedAddress, lastID }
  }, [messages, selectedAddress])

  useEffect(() => {
    if (!detailVisible || !documentVisible || messagesQuery.isPending || messagesQuery.isError ||
      !selectedConversation?.unreadCount || !readToken || attemptedReadToken.current === readToken) return
    attemptedReadToken.current = readToken
    markRead.mutate({ body: { remoteAddress: selectedAddress, readThroughToken: readToken } })
  }, [detailVisible, documentVisible, messagesQuery.isError, messagesQuery.isPending, readToken, selectedAddress, selectedConversation?.unreadCount])

  const selectConversation = (remoteAddress: string, temporary = false) => {
    setSelectedAddress(remoteAddress)
    setTemporaryAddress(temporary ? remoteAddress : '')
    setMobileDetail(true)
    setOperationError(undefined)
    attemptedReadToken.current = ''
  }

  const openNewConversation = (values: NewConversationValues) => {
    const recipient = values.recipient.trim()
    if (!numericAddress.test(recipient)) return
    selectConversation(recipient, !conversations.some((item) => item.remoteAddress === recipient))
    newConversationForm.resetFields()
    setNewConversationOpen(false)
  }

  const loadOlderMessages = async () => {
    const history = historyRef.current
    if (history) pendingOlderAnchor.current = { height: history.scrollHeight, top: history.scrollTop }
    try {
      const result = await messagesQuery.fetchNextPage()
      if (result.isError) pendingOlderAnchor.current = undefined
    } catch {
      pendingOlderAnchor.current = undefined
    }
  }

  const startContactEdit = (contact: Contact) => {
    setEditingContactID(contact.id)
    contactForm.setFieldsValue({ displayName: contact.displayName, phoneNumber: contact.phoneNumber })
  }

  const lineOptions = lines.map((line) => ({
    value: line.id,
    disabled: !canSendOnLine(line),
    label: canSendOnLine(line) ? line.displayName : `${line.displayName}（${unavailableLineReason(line)}）`,
  }))
  if (selectedLineID && !lineByID.has(selectedLineID)) {
    lineOptions.unshift({ value: selectedLineID, disabled: true, label: '历史线路（已删除）' })
  }

  const firstConversationPage = conversationsQuery.data?.pages[0]

  return <main className="page-content messages-page">
    <PageHeader title="短信" subtitle="按收件人集中查看跨线路短信会话" />
    {Boolean(operationError) && <Alert className="page-alert" type="error" showIcon title={displayApiError(operationError)} />}
    {firstConversationPage?.nearCapacity && <Alert className="page-alert" type="warning" showIcon title="短信历史接近容量上限，请按需清理旧记录。" />}

    <div className="messages-workspace">
      {(!compact || !mobileDetail) && <Card className="conversation-pane" styles={{ body: { padding: 0 } }}>
        <Flex className="conversation-toolbar" gap="small" wrap="wrap">
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setNewConversationOpen(true)}>新建短信</Button>
          <Button icon={<ContactsOutlined />} onClick={() => setContactsOpen(true)}>联系人管理</Button>
        </Flex>
        <div className="conversation-list" aria-busy={conversationsQuery.isPending}>
          {conversationsQuery.isError && <Alert type="error" showIcon title={displayApiError(conversationsQuery.error)} />}
          {!conversationsQuery.isPending && conversations.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无短信会话" />}
          {conversations.map((conversation) => {
            const contact = contactByNumber.get(conversation.remoteAddress)
            return <div key={conversation.remoteAddress} className="conversation-list-item">
              <button
                type="button"
                className={`conversation-button${selectedAddress === conversation.remoteAddress ? ' selected' : ''}`}
                aria-current={selectedAddress === conversation.remoteAddress ? 'true' : undefined}
                onClick={() => selectConversation(conversation.remoteAddress)}
              >
                <Flex justify="space-between" align="start" gap="small">
                  <span className="conversation-identity">
                    <Typography.Text strong ellipsis>{contact?.displayName ?? conversation.remoteAddress}</Typography.Text>
                    {contact && <Typography.Text type="secondary" ellipsis>{conversation.remoteAddress}</Typography.Text>}
                  </span>
                  <Typography.Text type="secondary" className="conversation-time">{formatTime(conversation.lastMessage.createdAt)}</Typography.Text>
                </Flex>
                <Flex justify="space-between" align="center" gap="small">
                  <Typography.Text type="secondary" ellipsis className="conversation-preview">
                    {conversation.lastMessage.direction === 'outbound' ? '我：' : ''}{conversation.lastMessage.body}
                  </Typography.Text>
                  <Badge count={conversation.unreadCount} overflowCount={99} />
                </Flex>
              </button>
            </div>
          })}
        </div>
        {conversationsQuery.hasNextPage && <div className="load-more conversation-load-more">
          <Button loading={conversationsQuery.isFetchingNextPage} onClick={() => void conversationsQuery.fetchNextPage()}>加载更多会话</Button>
        </div>}
      </Card>}

      {(!compact || mobileDetail) && <Card className="message-pane" styles={{ body: { padding: 0 } }}>
        {selectedAddress ? <>
          <Flex className="message-pane-header" align="center" gap="small">
            {compact && <Button aria-label="返回会话列表" type="text" icon={<ArrowLeftOutlined />} onClick={() => setMobileDetail(false)} />}
            <span className="message-pane-identity">
              <Typography.Title level={4}>{selectedContact?.displayName ?? selectedAddress}</Typography.Title>
              {selectedContact && <Typography.Text type="secondary">{selectedAddress}</Typography.Text>}
            </span>
          </Flex>
          <div
            ref={historyRef}
            className="message-history"
            aria-label="短信记录"
            onScroll={(event) => {
              const history = event.currentTarget
              historyAtBottom.current = history.scrollHeight - history.scrollTop - history.clientHeight <= 80
            }}
          >
            {messagesQuery.hasNextPage && <div className="load-more message-load-older">
              <Button loading={messagesQuery.isFetchingNextPage} onClick={() => void loadOlderMessages()}>加载更早短信</Button>
            </div>}
            {messagesQuery.isPending && <Flex justify="center"><Typography.Text type="secondary">正在加载短信…</Typography.Text></Flex>}
            {messagesQuery.isError && <Alert type="error" showIcon title={displayApiError(messagesQuery.error)} />}
            {!messagesQuery.isPending && messages.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={temporaryAddress === selectedAddress ? '新会话，发送第一条短信后会保存' : '该会话暂无短信'} />}
            {messages.map((record) => {
              const status = smsStatusPresentation(record)
              const line = lineByID.get(record.lineId)
              const menuItems = [
                ...(record.direction === 'outbound' && record.status === 'failed'
                  ? [{ key: 'edit', label: '重新编辑', icon: <EditOutlined /> }]
                  : []),
                { key: 'delete', label: '删除', danger: true, icon: <DeleteOutlined /> },
              ]
              return <div key={record.id} className={`message-row ${record.direction}`}>
                <div className="message-bubble">
                  <Flex justify="space-between" align="start" gap="small">
                    <Typography.Paragraph>{record.body}</Typography.Paragraph>
                    <Dropdown trigger={['click']} menu={{
                      items: menuItems,
                      onClick: ({ key }) => {
                        if (key === 'edit') {
                          composerForm.setFieldValue('body', record.body)
                          setSelectedLineID(record.lineId)
                          return
                        }
                        modal.confirm({
                          title: '删除这条短信？',
                          content: '删除后无法恢复。',
                          okText: '删除',
                          okButtonProps: { danger: true },
                          cancelText: '取消',
                          onOk: () => removeMessage.mutateAsync({ path: { messageId: record.id } }),
                        })
                      },
                    }}>
                      <Button aria-label="短信更多操作" type="text" size="small" icon={<MoreOutlined />} />
                    </Dropdown>
                  </Flex>
                  <Flex className="message-meta" gap="small" wrap="wrap" align="center">
                    <Typography.Text type="secondary">{formatTime(record.createdAt)}</Typography.Text>
                    <Typography.Text type="secondary">{line?.displayName ?? '历史线路（已删除）'}</Typography.Text>
                    {record.direction === 'outbound' && <Tag color={status.color}>{status.label}</Tag>}
                  </Flex>
                </div>
              </div>
            })}
          </div>
          <div className="message-composer">
            {!addressReplyable && <Alert type="info" showIcon title="该服务地址不是数字号码，只能查看历史，无法回复。" />}
            {addressReplyable && !selectedLineID && eligibleLines.length === 0 && <Alert type="warning" showIcon title="当前没有可发送的 Line。" />}
            {addressReplyable && selectedLineID && !selectedLineAvailable && <Alert type="warning" showIcon title={selectedLine ? `所选 Line 当前不可发送：${unavailableLineReason(selectedLine)}` : '最近使用的历史 Line 已删除，请明确选择其他 Line。'} />}
            {linesQuery.isError && <Alert type="error" showIcon title={displayApiError(linesQuery.error)} />}
            <Form<ComposerValues> form={composerForm} layout="vertical" onFinish={(values) => {
              setOperationError(undefined)
              send.mutate({ body: { operationId: operationId(), lineId: selectedLineID, destination: selectedAddress, body: values.body } })
            }}>
              <Form.Item label="发送 Line">
                <Select aria-label="发送 Line" value={selectedLineID || undefined} onChange={setSelectedLineID} options={lineOptions} placeholder="请选择 Line" disabled={!addressReplyable} />
              </Form.Item>
              <Form.Item name="body" rules={[{ required: true, whitespace: true, message: '请输入短信内容' }] }>
                <Input.TextArea aria-label="短信内容" maxLength={1600} showCount autoSize={{ minRows: 2, maxRows: 6 }} disabled={!addressReplyable} />
              </Form.Item>
              <Button type="primary" htmlType="submit" icon={<SendOutlined />} loading={send.isPending} disabled={!addressReplyable || !selectedLineAvailable}>发送短信</Button>
            </Form>
          </div>
        </> : <div className="message-pane-empty"><Empty description="选择一个会话或新建短信" /></div>}
      </Card>}
    </div>

    <Modal title="新建短信" open={newConversationOpen} onCancel={() => setNewConversationOpen(false)} onOk={() => newConversationForm.submit()} destroyOnHidden>
      {contactsQuery.isError && <Alert className="page-alert" type="error" showIcon title={displayApiError(contactsQuery.error)} />}
      <Form<NewConversationValues> form={newConversationForm} layout="vertical" onFinish={openNewConversation}>
        <Form.Item name="recipient" label="收件人" rules={[
          { required: true, message: '请选择联系人或输入号码' },
          { pattern: numericAddress, message: '请输入 3–20 位数字号码，可带 + 前缀' },
        ]}>
          <AutoComplete
            options={contacts.map((contact) => ({ value: contact.phoneNumber, label: `${contact.displayName} · ${contact.phoneNumber}` }))}
            placeholder="搜索联系人或输入号码"
            filterOption={(input, option) => String(option?.label ?? '').toLowerCase().includes(input.toLowerCase()) || String(option?.value ?? '').includes(input)}
          />
        </Form.Item>
      </Form>
    </Modal>

    <Drawer title="联系人管理" open={contactsOpen} onClose={() => setContactsOpen(false)} size={480}>
      {contactsQuery.isError && <Alert className="page-alert" type="error" showIcon title={displayApiError(contactsQuery.error)} />}
      <Form<ContactValues> form={contactForm} layout="vertical" onFinish={(values) => {
        setOperationError(undefined)
        if (editingContactID) updateContact.mutate({ path: { contactId: editingContactID }, body: values })
        else createContact.mutate({ body: values })
      }}>
        <Form.Item name="displayName" label="名称" rules={[{ required: true, whitespace: true }]}><Input maxLength={80} /></Form.Item>
        <Form.Item name="phoneNumber" label="号码" rules={[{ required: true }, { pattern: numericAddress, message: '请输入有效号码' }]}><Input inputMode="tel" autoComplete="tel" /></Form.Item>
        <Space>
          <Button type="primary" htmlType="submit" loading={createContact.isPending || updateContact.isPending}>{editingContactID ? '保存联系人' : '添加联系人'}</Button>
          {editingContactID && <Button onClick={() => { setEditingContactID(''); contactForm.resetFields() }}>取消编辑</Button>}
        </Space>
      </Form>
      <Flex className="contact-list" vertical gap="small">
        {contacts.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无联系人" />}
        {contacts.map((contact) => <Card key={contact.id} size="small">
          <Flex justify="space-between" align="center" gap="small" wrap="wrap">
            <span><Typography.Text strong>{contact.displayName}</Typography.Text><br /><Typography.Text type="secondary">{contact.phoneNumber}</Typography.Text></span>
            <Space>
              <Button type="text" icon={<EditOutlined />} onClick={() => startContactEdit(contact)}>编辑</Button>
              <Popconfirm title="删除这个联系人？" onConfirm={() => removeContact.mutateAsync({ path: { contactId: contact.id } })}>
                <Button
                  type="text"
                  danger
                  icon={<DeleteOutlined />}
                  loading={removeContact.isPending && removeContact.variables?.path.contactId === contact.id}
                >删除</Button>
              </Popconfirm>
            </Space>
          </Flex>
        </Card>)}
      </Flex>
    </Drawer>
  </main>
}

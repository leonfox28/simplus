import { PhoneOutlined } from '@ant-design/icons'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Card, Descriptions, Form, Grid, Input, Modal, Select, Space, Tag } from 'antd'
import type { TableColumnsType } from 'antd'
import { useMemo, useState } from 'react'
import { displayApiError } from '@/api/errors'
import {
  controlCallMutation,
  dialCallMutation,
  listCallsInfiniteOptions,
  listCallsInfiniteQueryKey,
  listManagedLinesOptions,
  simulateIncomingCallMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { Call } from '@/api/generated/types.gen'
import { PageHeader, PageSection, ResponsiveDataView } from '@/components/Page'

type DialValues = { lineId: string; remoteAddress: string }

function operationId() {
  return crypto.randomUUID?.() ?? `call_${Date.now()}_abcdefghijkl`
}

const stateLabels: Record<Call['state'], string> = {
  incoming: '来电', dialing: '拨号中', active: '通话中', ended: '已结束', failed: '失败',
}

export default function Calls() {
  const compact = !Grid.useBreakpoint().md
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [form] = Form.useForm<DialValues>()
  const [operationError, setOperationError] = useState<unknown>()
  const [dtmfCallID, setDTMFCallID] = useState('')
  const [dtmfDigits, setDTMFDigits] = useState('')
  const linesQuery = useQuery(listManagedLinesOptions())
  const callOptions = listCallsInfiniteOptions({ query: { limit: 20 } })
  const callsQuery = useInfiniteQuery({
    ...callOptions,
    initialPageParam: { query: {} },
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  })
  const calls = useMemo(() => callsQuery.data?.pages.flatMap((page) => page.calls) ?? [], [callsQuery.data])
  const lines = linesQuery.data?.lines ?? []
  const callsKey = listCallsInfiniteQueryKey()
  const refreshCalls = () => queryClient.invalidateQueries({ queryKey: callsKey })
  const dial = useMutation({
    ...dialCallMutation(),
    onSuccess: async () => { form.setFieldValue('remoteAddress', ''); await refreshCalls(); void message.success('拨号请求已提交。') },
    onError: setOperationError,
  })
  const incoming = useMutation({ ...simulateIncomingCallMutation(), onSuccess: refreshCalls, onError: setOperationError })
  const control = useMutation({
    ...controlCallMutation(),
    onSuccess: async () => { setDTMFCallID(''); setDTMFDigits(''); await refreshCalls() },
    onError: setOperationError,
  })
  const controls = (call: Call) => <Space wrap>
    {call.state === 'incoming' && <><Button aria-label="接听" onClick={() => control.mutate({ path: { callId: call.id }, body: { action: 'answer' } })}>接听</Button><Button aria-label="拒接" danger onClick={() => control.mutate({ path: { callId: call.id }, body: { action: 'reject' } })}>拒接</Button></>}
    {(call.state === 'active' || call.state === 'dialing') && <Button aria-label="挂断" danger onClick={() => control.mutate({ path: { callId: call.id }, body: { action: 'hangup' } })}>挂断</Button>}
    {call.state === 'active' && <Button onClick={() => setDTMFCallID(call.id)}>DTMF</Button>}
  </Space>
  const columns: TableColumnsType<Call> = [
    { title: '时间', dataIndex: 'createdAt', render: (value) => new Date(String(value)).toLocaleString() },
    { title: '线路', dataIndex: 'lineId' }, { title: '号码', dataIndex: 'remoteAddress' },
    { title: '方向', dataIndex: 'direction', render: (value) => value === 'inbound' ? '呼入' : '呼出' },
    { title: '状态', dataIndex: 'state', render: (value) => <Tag>{stateLabels[value as Call['state']]}</Tag> },
    { title: '控制', render: (_, call) => controls(call) },
  ]
  const callableLines = lines.filter((line) => line.state === 'ready' && line.capabilities.cellularVoice)

  return <main className="page-content">
    <PageHeader title="语音通话" subtitle="拨号、来电模拟和通话控制" />
    {Boolean(operationError || callsQuery.error || linesQuery.error) && <Alert className="page-alert" type="error" showIcon title={displayApiError(operationError ?? callsQuery.error ?? linesQuery.error)} />}
    <PageSection title="拨号">
      <Form<DialValues> form={form} layout={compact ? 'vertical' : 'inline'} onFinish={(values) => {
        setOperationError(undefined)
        dial.mutate({ body: { operationId: operationId(), lineId: values.lineId, remoteAddress: values.remoteAddress } })
      }}>
        <Form.Item name="lineId" label="线路" rules={[{ required: true, message: '请选择线路' }]}><Select style={{ minWidth: 180 }} options={callableLines.map((line) => ({ value: line.id, label: line.displayName }))} /></Form.Item>
        <Form.Item name="remoteAddress" label="号码" rules={[{ required: true }]}><Input inputMode="tel" autoComplete="tel" /></Form.Item>
        <Form.Item><Button type="primary" htmlType="submit" icon={<PhoneOutlined />} loading={dial.isPending}>拨号</Button></Form.Item>
      </Form>
    </PageSection>
    <PageSection title="通话历史" className="page-section" extra={<Button disabled={!callableLines.length} loading={incoming.isPending} onClick={() => {
      const line = callableLines[0]
      if (line) incoming.mutate({ body: { operationId: operationId(), lineId: line.id, remoteAddress: '+447700900123' } })
    }}>模拟来电</Button>}>
      <ResponsiveDataView
        data={calls}
        columns={columns}
        rowKey="id"
        loading={callsQuery.isPending}
        emptyText="暂无通话记录"
        renderCard={(call) => <Card className="mobile-record-card" title={call.remoteAddress} extra={<Tag>{stateLabels[call.state]}</Tag>}><Descriptions column={1} size="small" items={[
          { key: 'time', label: '时间', children: new Date(call.createdAt).toLocaleString() },
          { key: 'line', label: '线路', children: call.lineId },
          { key: 'direction', label: '方向', children: call.direction === 'inbound' ? '呼入' : '呼出' },
        ]} />{controls(call)}</Card>}
      />
      {callsQuery.hasNextPage && <div className="load-more"><Button loading={callsQuery.isFetchingNextPage} onClick={() => void callsQuery.fetchNextPage()}>加载更多</Button></div>}
    </PageSection>
    <Modal title="发送 DTMF" open={Boolean(dtmfCallID)} onCancel={() => setDTMFCallID('')} onOk={() => control.mutate({ path: { callId: dtmfCallID }, body: { action: 'dtmf', digits: dtmfDigits } })} confirmLoading={control.isPending} okButtonProps={{ disabled: !/^[0-9*#]{1,32}$/.test(dtmfDigits) }}>
      <Input aria-label="DTMF 按键" value={dtmfDigits} onChange={(event) => setDTMFDigits(event.target.value)} maxLength={32} placeholder="0-9、* 或 #" />
    </Modal>
  </main>
}

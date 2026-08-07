import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Form, Input, Result, Spin } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { displayApiError } from '@/api/errors'
import {
  completeSetupMutation,
  consumeSetupBootstrapMutation,
  getSetupSessionOptions,
  getSetupSessionQueryKey,
  getSetupStatusQueryKey,
  putSetupStorageMutation,
} from '@/api/generated/@tanstack/react-query.gen'
import type { SetupSessionResponse } from '@/api/generated/types.gen'
import { PageHeader } from '@/components/Page'

type StorageValues = { recordingsRoot: string }

export default function SetupPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [error, setError] = useState<unknown>()
  const bootstrapCode = useMemo(() => new URLSearchParams(window.location.hash.slice(1)).get('bootstrap') ?? '', [])
  const session = useQuery({ ...getSetupSessionOptions(), enabled: !bootstrapCode })
  const consume = useMutation({
    ...consumeSetupBootstrapMutation(),
    onSuccess: (value) => queryClient.setQueryData(getSetupSessionQueryKey(), value),
    onError: setError,
  })
  useEffect(() => {
    if (!bootstrapCode || consume.isPending || consume.isSuccess || consume.isError) return
    window.history.replaceState({}, '', `${window.location.pathname}${window.location.search}`)
    consume.mutate({ body: { bootstrapCode } })
  }, [bootstrapCode, consume])
  const setupSession = (consume.data ?? session.data) as SetupSessionResponse | undefined
  const storage = useMutation({ ...putSetupStorageMutation(), onError: setError })
  const complete = useMutation({
    ...completeSetupMutation(),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: getSetupStatusQueryKey() })
      const target = new URL(result.managementUrl, window.location.href)
      if (target.origin !== window.location.origin) window.location.replace(target.href)
      else navigate('/login', { replace: true })
    },
    onError: setError,
  })
  const loading = session.isPending || consume.isPending
  if (loading) return <div className="full-page-state"><Spin size="large" /></div>
  if (!setupSession) return <Result status="error" title="初始化授权不可用" subTitle={displayApiError(error ?? session.error)} />
  return <main className="public-page">
    <PageHeader title="完成基础初始化" subtitle="管理员已由安装器创建，确认本机录音目录后进入后台" />
    <Card className="setup-card">
      <Alert type="info" showIcon title={`当前管理员：${setupSession.administratorUsername}`} description="可信 HTTPS、模组和线路配置已移动到管理后台。" />
      {Boolean(error) && <Alert className="page-alert" type="error" showIcon title={displayApiError(error)} />}
      <Form<StorageValues>
        layout="vertical"
        initialValues={{ recordingsRoot: setupSession.recordingsRoot }}
        onFinish={async (values) => {
          setError(undefined)
          if (!setupSession.storageConfigured) {
            try {
              await storage.mutateAsync({ body: { recordingsRoot: values.recordingsRoot } })
            } catch {
              return
            }
          }
          complete.mutate({})
        }}
      >
        <Form.Item name="recordingsRoot" label="录音目录" rules={[{ required: true }, { pattern: /^\//, message: '必须是绝对路径' }]}><Input autoComplete="off" /></Form.Item>
        <Button type="primary" htmlType="submit" loading={storage.isPending || complete.isPending}>完成初始化并进入后台</Button>
      </Form>
    </Card>
  </main>
}

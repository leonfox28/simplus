import { PageContainer, ProForm, ProFormText } from '@ant-design/pro-components'
import { Alert, Card, Result, Spin } from 'antd'
import React, { useEffect, useState } from 'react'
import { completeSetup, getSetupSession, putSetupStorage, type SetupSessionResponse } from '@/api/client'

export default function SetupPage() {
  const [session, setSession] = useState<SetupSessionResponse | null>(null)
  const [error, setError] = useState('')
  useEffect(() => { getSetupSession().then(setSession).catch(e => setError(String(e))) }, [])
  if (error) return <Result status="error" title="初始化信息读取失败" subTitle={error} />
  if (!session) return <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center' }}><Spin size="large" /></div>
  return <PageContainer title="完成基础初始化" subTitle="管理员已由安装器创建，确认本机录音目录后进入后台">
    <Card style={{ maxWidth: 720, margin: '48px auto' }}>
      <Alert type="info" showIcon message={`当前管理员：${session.administratorUsername}`} description="可信 HTTPS、模组和线路配置已移动到管理后台。" style={{ marginBottom: 24 }} />
      <ProForm initialValues={{ recordingsRoot: session.recordingsRoot }} submitter={{ searchConfig: { submitText: '完成初始化并进入后台' } }} onFinish={async values => {
        try {
          if (!session.storageConfigured) await putSetupStorage({ recordingsRoot: values.recordingsRoot })
          await completeSetup()
          window.location.replace('/dashboard')
          return true
        } catch (e) { setError(e instanceof Error ? e.message : 'SETUP_FAILED'); return false }
      }}>
        {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} />}
        <ProFormText name="recordingsRoot" label="录音目录" rules={[{ required: true }, { pattern: /^\//, message: '必须是绝对路径' }]} />
      </ProForm>
    </Card>
  </PageContainer>
}

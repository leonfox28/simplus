import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Input } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { displayApiError } from '@/api/errors'
import { changeAdministratorPasswordMutation } from '@/api/generated/@tanstack/react-query.gen'
import { PageHeader, PageSection } from '@/components/Page'

type PasswordValues = { currentPassword: string; newPassword: string; confirmPassword: string }

export default function Settings() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [error, setError] = useState<unknown>()
  const changePassword = useMutation({
    ...changeAdministratorPasswordMutation(),
    onSuccess: async () => {
      await queryClient.cancelQueries()
      queryClient.clear()
      navigate('/login?password=changed', { replace: true })
    },
    onError: setError,
  })
  return <main className="page-content">
    <PageHeader title="系统设置" subtitle="管理当前实例的访问入口和管理员安全设置" />
    {Boolean(error) && <Alert className="page-alert" type="error" showIcon title={displayApiError(error)} />}
    <PageSection title="可信 HTTPS"><Alert type="info" showIcon title="HTTPS 设置将在后续管理纵切提供" description="当前只在可信局域网使用 HTTP。" /></PageSection>
    <PageSection title="管理员密码" className="page-section">
      <Form<PasswordValues> layout="vertical" onFinish={(values) => {
        setError(undefined)
        changePassword.mutate({ body: { currentPassword: values.currentPassword, newPassword: values.newPassword, newPasswordConfirmation: values.confirmPassword } })
      }}>
        <Form.Item name="currentPassword" label="当前密码" rules={[{ required: true }]}><Input.Password autoComplete="current-password" /></Form.Item>
        <Form.Item name="newPassword" label="新密码" rules={[{ required: true }, { min: 12 }]}><Input.Password autoComplete="new-password" /></Form.Item>
        <Form.Item name="confirmPassword" label="确认密码" dependencies={['newPassword']} rules={[{ required: true }, ({ getFieldValue }) => ({ validator: (_, value) => !value || value === getFieldValue('newPassword') ? Promise.resolve() : Promise.reject(new Error('两次密码不一致')) })]}><Input.Password autoComplete="new-password" /></Form.Item>
        <Button type="primary" htmlType="submit" loading={changePassword.isPending}>更换密码并重新登录</Button>
      </Form>
    </PageSection>
  </main>
}

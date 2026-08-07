import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Card, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { displayApiError } from '@/api/errors'
import { getAuthSessionQueryKey, loginMutation } from '@/api/generated/@tanstack/react-query.gen'

type LoginValues = { username: string; password: string }

export default function LoginPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [error, setError] = useState<unknown>()
  const login = useMutation({
    ...loginMutation(),
    onSuccess: (session) => {
      queryClient.setQueryData(getAuthSessionQueryKey(), session)
      void message.success('登录成功')
      navigate('/dashboard', { replace: true })
    },
    onError: setError,
  })
  return <main className="login-page">
    <div className="login-content">
      <Card className="login-card">
        <Typography.Title level={1}>Simplus</Typography.Title>
        <Typography.Paragraph type="secondary">可信局域网通信控制后台</Typography.Paragraph>
        {Boolean(error) && <Alert className="page-alert" type="error" showIcon title="登录失败" description={displayApiError(error)} />}
        <Form<LoginValues> name="login" layout="vertical" autoComplete="on" onFinish={(values) => {
          setError(undefined)
          login.mutate({ body: { username: values.username.trim(), password: values.password } })
        }}>
          <Form.Item name="username" rules={[{ required: true, message: '请输入管理员用户名' }]}>
            <Input id="username" name="username" type="text" prefix={<UserOutlined />} autoComplete="username" placeholder="管理员用户名" size="large" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password id="password" name="password" prefix={<LockOutlined />} autoComplete="current-password" placeholder="密码" size="large" />
          </Form.Item>
          <Button block type="primary" htmlType="submit" size="large" loading={login.isPending}>登录</Button>
        </Form>
      </Card>
    </div>
  </main>
}

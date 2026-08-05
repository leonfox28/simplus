import { LockOutlined, UserOutlined } from '@ant-design/icons'
import { LoginForm, ProFormText } from '@ant-design/pro-components'
import { Alert, App } from 'antd'
import React, { useState } from 'react'
import { getSetupStatus, login } from '@/api/client'

export default function LoginPage() {
  const { message } = App.useApp()
  const [error, setError] = useState('')
  return <main className="login-page">
    <div className="login-content">
      <LoginForm
        title="Simplus"
        subTitle="可信局域网通信控制后台"
        name="login"
        autoComplete="on"
        containerStyle={{ width: '100%', boxSizing: 'border-box' }}
        contentStyle={{ width: '100%', minWidth: 0, maxWidth: 328 }}
        submitter={{ searchConfig: { submitText: '登录' }, submitButtonProps: { size: 'large' } }}
        onFinish={async values => {
        setError('')
        try {
          const session = await login({ username: String(values.username).trim(), password: String(values.password) })
          const setup = await getSetupStatus()
          message.success('登录成功')
          window.location.replace(setup.setupRequired ? '/setup' : '/dashboard')
          return true
        } catch (e) { setError(e instanceof Error ? e.message : 'LOGIN_FAILED'); return false }
      }}>
        {error && <Alert type="error" showIcon message="登录失败" description={error} style={{ marginBottom: 16 }} />}
        <ProFormText name="username" fieldProps={{ id: 'username', name: 'username', type: 'text', prefix: <UserOutlined />, autoComplete: 'username' }} placeholder="管理员用户名" rules={[{ required: true }]} />
        <ProFormText.Password name="password" fieldProps={{ id: 'password', name: 'password', prefix: <LockOutlined />, autoComplete: 'current-password' }} placeholder="密码" rules={[{ required: true }]} />
      </LoginForm>
    </div>
  </main>
}

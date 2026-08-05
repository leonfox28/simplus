import { render, screen } from '@testing-library/react'
import { App } from 'antd'
import { describe, expect, it } from 'vitest'
import LoginPage from './Login'

describe('LoginPage password-manager semantics', () => {
  it('renders a named autocomplete form and native login field attributes', () => {
    const { container } = render(<App><LoginPage /></App>)
    const form = container.querySelector('form')
    expect(form).not.toBeNull()
    expect(form).toHaveAttribute('autocomplete', 'on')

    const username = screen.getByPlaceholderText('管理员用户名')
    expect(username).toHaveAttribute('id', 'username')
    expect(username).toHaveAttribute('name', 'username')
    expect(username).toHaveAttribute('type', 'text')
    expect(username).toHaveAttribute('autocomplete', 'username')
    expect(username).toHaveValue('')

    const password = screen.getByPlaceholderText('密码')
    expect(password).toHaveAttribute('id', 'password')
    expect(password).toHaveAttribute('name', 'password')
    expect(password).toHaveAttribute('type', 'password')
    expect(password).toHaveAttribute('autocomplete', 'current-password')
  })
})

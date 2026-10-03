import { expect, test } from '@playwright/test'

test('installed Simulator manages devices, SMS, calls and durable connection notices', async ({ page }) => {
  await page.goto('/login')
  await page.getByPlaceholder('管理员用户名').fill('browser_admin')
  await page.getByPlaceholder('密码').fill('integration-test-password')
  await page.getByRole('button', { name: /登\s*录/ }).click()
  await expect(page.getByRole('heading', { name: '概览' })).toBeVisible()
  await expect(page).toHaveURL(/\/dashboard$/)
  const api = page.request
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === 'simplus_csrf')?.value ?? ''
  const headers = { Origin: 'http://127.0.0.1:4183', 'X-Simplus-CSRF': csrf }
  const post = async (path: string, data: unknown) => {
    const response = await api.post(`/api/v1/${path}`, { data, headers })
    expect(response.ok(), await response.text()).toBeTruthy()
    return response.json()
  }
  const put = async (path: string, data: unknown) => {
    const response = await api.put(`/api/v1/${path}`, { data, headers })
    expect(response.ok(), await response.text()).toBeTruthy()
    return response.json()
  }
  const get = async (path: string) => {
    const response = await api.get(`/api/v1/${path}`)
    expect(response.ok(), await response.text()).toBeTruthy()
    return response.json()
  }

  await page.goto('/notifications')
  await page.getByRole('combobox', { name: /平台/ }).click()
  await page.getByText('企业微信', { exact: true }).click()
  await page.getByLabel('名称', { exact: true }).fill('Browser notifications')
  await page.getByLabel('Webhook', { exact: true }).fill('https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-browser')
  await expect(page.getByRole('checkbox', { checked: true })).toHaveCount(9)
  await page.getByRole('button', { name: '添加 Webhook' }).click()
  await expect(page.getByText('Browser notifications', { exact: true })).toBeVisible()
  const channel = (await get('notification-channels')).channels[0]
  await put(`notification-channels/${channel.id}`, { provider: 'wecom', displayName: channel.displayName, webhookUrl: '', signingSecret: '', enabled: true, eventKinds: channel.eventKinds.filter((kind: string) => !kind.startsWith('sms.')) })
  const pending = async () => (await get('notification-channels')).channels[0].pendingCount as number

  const candidates = (await get('modem-candidates')).candidates
  expect(candidates).toHaveLength(2)
  const modems = []
  for (const candidate of candidates) modems.push(await post('modems', { candidateId: candidate.candidateId }))
  await expect.poll(pending, { timeout: 15_000 }).toBe(2)
  const lineCandidates = (await get('line-candidates')).candidates
  const lines = []
  for (const [index, candidate] of lineCandidates.entries()) lines.push(await post('lines', { candidateId: candidate.candidateId, displayName: `Browser Line ${index + 1}` }))
  expect(lines).toHaveLength(2)
  await page.goto('/lines')
  await expect(page.getByText('Browser Line 1', { exact: true })).toBeVisible()
  await expect(page.getByText('Browser Line 2', { exact: true })).toBeVisible()
  await put(`lines/${lines[0].id}/egress`, { mode: 'direct', countryCode: '' })
  await post(`vowifi-lines/${lines[0].id}/activate`, {})
  await expect.poll(pending).toBe(3)
  await post(`vowifi-lines/${lines[0].id}/deactivate`, {})
  await post(`vowifi-lines/${lines[0].id}/activate`, {})
  await expect.poll(pending).toBe(5)
  await put(`modems/${modems[0].id}/rf-state`, { enabled: false })
  await expect.poll(pending, { timeout: 15_000 }).toBe(6)
  await put(`modems/${modems[0].id}/rf-state`, { enabled: true })
  await expect.poll(pending, { timeout: 15_000 }).toBe(7)
  // Stop IMS before native-SMS simulation; transport selection remains explicit.
  await post(`vowifi-lines/${lines[0].id}/deactivate`, {})

  await page.goto('/messages')
  await page.getByRole('button', { name: /新建短信/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('combobox', { name: /收件人/ }).fill('+12025550123')
  await dialog.getByRole('button', { name: /确\s*定|OK/ }).click()
  await page.getByLabel('短信内容').fill('Browser Simulator outbound')
  await page.getByRole('button', { name: /发送短信/ }).click()
  await expect(page.getByLabel('短信记录').getByText('Browser Simulator outbound', { exact: true })).toBeVisible()
  await expect(page.getByText('已发送', { exact: true }).first()).toBeVisible()

  await page.goto('/calls')
  await page.getByRole('button', { name: '模拟来电' }).click()
  await page.getByRole('button', { name: '接听', exact: true }).click()
  await expect(page.getByText('通话中', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '挂断', exact: true }).click()
  await expect(page.getByText('已结束', { exact: true })).toBeVisible()
  await expect.poll(pending).toBeGreaterThanOrEqual(9)
  // The local rejecting proxy keeps delivery pending; disabling cancels it.
  await put(`notification-channels/${channel.id}`, { provider: 'wecom', displayName: channel.displayName, webhookUrl: '', signingSecret: '', enabled: false, eventKinds: channel.eventKinds })
  await expect.poll(pending).toBe(0)
  await page.goto('/notifications')
  await expect(page.getByRole('cell', { name: '0 / 0', exact: true })).toBeVisible()
})

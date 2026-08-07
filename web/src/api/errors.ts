import type { ApiError } from './generated/types.gen'

export type ApiClientErrorKind = 'transport' | 'http' | 'invalid-response' | 'timeout' | 'aborted'

export class ApiClientError extends Error {
  readonly kind: ApiClientErrorKind
  readonly code: string
  readonly retryable: boolean
  readonly status?: number
  readonly reference?: string

  constructor(options: {
    kind: ApiClientErrorKind
    code: string
    retryable: boolean
    status?: number
    reference?: string
  }) {
    super(options.code)
    this.name = 'ApiClientError'
    this.kind = options.kind
    this.code = options.code
    this.retryable = options.retryable
    this.status = options.status
    this.reference = options.reference
  }
}

const codeMessages: Record<string, string> = {
  API_TIMEOUT: '操作超时，请刷新状态后再决定是否重试。',
  AUTH_CREDENTIALS_INVALID: '用户名或密码不正确。',
  AUTH_SESSION_UNAUTHORIZED: '登录已失效，请重新登录。',
  CSRF_INVALID: '页面安全令牌已失效，请刷新页面后重试。',
  EVENTS_UNAVAILABLE: '实时状态通道暂不可用，页面仍可手动刷新。',
  LINE_NOT_FOUND: '线路不存在，请刷新后重试。',
  LINE_NOT_READY: '线路当前不可用，请检查模组和 SIM / Profile 状态。',
  MIHOMO_NOT_RUNNING: 'Mihomo 当前未运行。',
  MODEM_ALREADY_ADDED: '该模组已经添加。',
  MODEM_CANDIDATE_NOT_FOUND: '该模组已经离线，请重新扫描。',
  MODEM_CANDIDATE_NOT_READY: '该模组目前不满足添加条件。',
  MODEM_IDENTITY_CONFLICT: '模组身份发生冲突，系统已拒绝自动绑定。',
  MODEM_IDENTITY_UNAVAILABLE: '当前无法读取 IMEI，请确认模组在线后重试。',
  MODEM_NOT_FOUND: '该模组记录不存在，请刷新后重试。',
  MODEM_RF_CHANGE_FAILED: '射频状态未能确认，请刷新状态后再决定是否重试。',
  MODEM_RF_UNAVAILABLE: '该模组当前不支持射频控制。',
  NOTIFICATION_DELIVERY_FAILED: '测试通知未能送达，请检查渠道配置。',
  PAGE_CURSOR_INVALID: '分页位置已失效，请重新加载列表。',
  REQUEST_INVALID: '提交内容不符合要求，请检查后重试。',
  SMS_SEND_OUTCOME_UNKNOWN: '发送结果暂不确定，请刷新历史，切勿立即重复发送。',
  VOWIFI_UNAVAILABLE: 'Host VoWiFi 运行状态暂不可用。',
}

export function isApiError(value: unknown): value is ApiError {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<ApiError>
  return (
    typeof candidate.code === 'string' &&
    /^[A-Z0-9_]{1,128}$/.test(candidate.code) &&
    typeof candidate.retryable === 'boolean' &&
    (candidate.reference === undefined || (
      typeof candidate.reference === 'string' && candidate.reference.length <= 128
    ))
  )
}

export function displayApiError(error: unknown): string {
  if (!(error instanceof ApiClientError)) return '操作失败，请稍后重试。'
  const base = codeMessages[error.code] ?? (() => {
    switch (error.kind) {
      case 'aborted': return '操作已取消。'
      case 'invalid-response': return '管理服务返回了无法识别的数据，请刷新后重试。'
      case 'timeout': return '操作超时，请刷新状态后再决定是否重试。'
      case 'transport': return '无法连接管理服务，请检查网络和服务状态。'
      case 'http': return error.status === 403 ? '当前操作未通过安全校验。' : '管理服务暂时无法完成该操作。'
    }
  })()
  return error.reference ? `${base} 参考编号：${error.reference}` : base
}

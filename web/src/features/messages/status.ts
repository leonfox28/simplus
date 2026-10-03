import type { SmsMessage as SMSMessage } from '@/api/generated/types.gen'

export interface SMSStatusPresentation {
  color: string
  label: string
}

export function smsStatusPresentation(message: Pick<SMSMessage, 'status' | 'errorCode'>): SMSStatusPresentation {
  switch (message.status) {
    case 'queued':
      return { color: 'blue', label: '正在发送' }
    case 'unconfirmed':
      if (message.errorCode === 'SEND_OUTCOME_UNKNOWN_AFTER_RESTART') {
        return { color: 'orange', label: '发送状态未知（服务重启）' }
      }
      if (message.errorCode === 'IMS_SMS_ACCEPTED_AWAITING_REPORT') {
        return { color: 'gold', label: '已提交，等待运营商确认' }
      }
      return { color: 'orange', label: '已提交，结果未知' }
    case 'sent':
      return { color: 'green', label: '已发送' }
    case 'failed':
      switch (message.errorCode) {
        case 'SMS_SIM_NOT_READY': return { color: 'red', label: 'SIM 未就绪' }
        case 'SMS_SIM_IDENTITY_CHANGED': return { color: 'red', label: 'SIM 已更换' }
        case 'SMS_EQUIPMENT_IDENTITY_CHANGED': return { color: 'red', label: '模组身份已变化' }
        case 'SMS_RF_OFF': return { color: 'red', label: '射频已关闭' }
        case 'SMS_REGISTRATION_DENIED': return { color: 'red', label: '蜂窝注册被拒绝' }
        case 'SMS_NOT_REGISTERED': return { color: 'red', label: '蜂窝网络未注册' }
        case 'SMS_STATUS_UNAVAILABLE': return { color: 'red', label: '蜂窝状态不可用' }
        case 'SMS_DEVICE_STALE': return { color: 'red', label: '模组连接已变化' }
      }
      return { color: 'red', label: '发送失败' }
    case 'received':
      return { color: 'green', label: '已接收' }
  }
}

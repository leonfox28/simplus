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
      return { color: 'red', label: '发送失败' }
    case 'received':
      return { color: 'green', label: '已接收' }
  }
}

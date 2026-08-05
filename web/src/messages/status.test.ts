import { describe, expect, it } from 'vitest'
import { smsStatusPresentation } from './status'

describe('SMS status presentation', () => {
  it('distinguishes unconfirmed submission from a definitive failure', () => {
    expect(smsStatusPresentation({ status: 'unconfirmed', errorCode: 'SMS_SEND_OUTCOME_UNKNOWN' })).toEqual({
      color: 'orange',
      label: '已提交，结果未知',
    })
    expect(smsStatusPresentation({ status: 'failed', errorCode: 'IMS_SMS_REJECTED' })).toEqual({
      color: 'red',
      label: '发送失败',
    })
  })

  it('explains an interrupted operation without implying a retry', () => {
    expect(smsStatusPresentation({ status: 'unconfirmed', errorCode: 'SEND_OUTCOME_UNKNOWN_AFTER_RESTART' })).toEqual({
      color: 'orange',
      label: '发送状态未知（服务重启）',
    })
  })

  it('shows SIP acceptance as pending instead of final success', () => {
    expect(smsStatusPresentation({ status: 'unconfirmed', errorCode: 'IMS_SMS_ACCEPTED_AWAITING_REPORT' })).toEqual({
      color: 'gold',
      label: '已提交，等待运营商确认',
    })
  })
})

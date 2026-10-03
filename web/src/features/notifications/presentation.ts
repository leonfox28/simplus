import type { FeishuNotificationBinding, NotificationEventKind } from '@/api/generated/types.gen'
export const eventLabels: Record<NotificationEventKind, string> = {
  'sms.received': '收到短信', 'sms.failed': '短信失败', 'call.incoming': '来电', 'call.missed': '未接来电', 'system.degraded': '系统异常',
  'vowifi.connected': 'VoWiFi 连接', 'vowifi.disconnected': 'VoWiFi 断开', 'cellular.connected': '蜂窝网络连接', 'cellular.disconnected': '蜂窝网络丢失',
}
export const events = Object.keys(eventLabels) as NotificationEventKind[]
export const bindingMessages: Record<FeishuNotificationBinding['state'], string> = {
  idle: '尚未发起绑定。',
  waiting: '等待管理员在飞书完成授权。',
  testing: '授权完成，正在发送绑定测试消息。',
  succeeded: '飞书私聊绑定成功。',
  failed: '绑定失败，可重新生成验证链接。',
  expired: '验证链接已过期，请重新发起绑定。',
  cancelled: '绑定已取消。',
}
export const bindingErrors: Record<string, string> = {
  FEISHU_BINDING_DENIED: '管理员拒绝了飞书授权。',
  FEISHU_BINDING_EXPIRED: '飞书授权已过期。',
  FEISHU_BINDING_LARK_UNSUPPORTED: '当前仅支持飞书中国版租户。',
  FEISHU_BINDING_RESULT_INVALID: '飞书返回了无法使用的授权结果。',
  FEISHU_BINDING_PROVIDER_FAILED: '暂时无法连接飞书授权服务。',
  FEISHU_BINDING_TEST_FAILED: '授权完成，但测试私聊未能送达。飞书侧应用可能已保留。',
  FEISHU_BINDING_PERSIST_FAILED: '测试消息已发送，但本地保存失败。飞书侧应用可能已保留。',
}

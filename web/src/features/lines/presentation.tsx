import { Flex, Tag, Typography } from 'antd'
import type { LineCandidate, LineEgressBinding, ManagedLine, PhoneNumberObservation, PhoneNumberSource, VoWiFiLineState } from '@/api/generated/types.gen'
export type CountryOption = { code: string; name: string }
export type LineRow = ManagedLine & { binding?: LineEgressBinding; voWiFi?: VoWiFiLineState }

export const lineStateLabels: Record<ManagedLine['state'], string> = {
  ready: '就绪', 'modem-offline': '模组离线', 'sim-unavailable': 'SIM / Profile 不可用',
}
export const candidateReasonLabels: Record<LineCandidate['readinessReason'], string> = {
  READY: '可添加', MODEM_OFFLINE: '模组离线', SIM_ABSENT: '未插入 SIM',
  SIM_UNAVAILABLE: 'SIM / Profile 不可用', ALREADY_ADDED: '已添加', BINDING_CONFLICT: '绑定身份冲突',
}
export const readinessLabels: Record<LineEgressBinding['readinessReason'], string> = {
  READY: '出口可用', EGRESS_NOT_CONFIGURED: '尚未配置出口', LINE_VOWIFI_UNSUPPORTED: '线路不支持 Host VoWiFi',
  SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅', COUNTRY_NOT_FOUND: '当前订阅没有该国家',
  MIHOMO_NOT_RUNNING: 'Mihomo 未运行', MIHOMO_RESTART_REQUIRED: '等待 Mihomo 重启应用订阅',
}
export const voWiFiStateLabels: Record<VoWiFiLineState['state'], string> = {
  stopped: '已停用', starting: '正在启动', connecting: '连接 ePDG', registering: '注册 IMS',
  online: '在线', reconnecting: '正在重连', stopping: '正在停用', failed: '运行失败',
}
export const voWiFiReadinessLabels: Record<VoWiFiLineState['readinessCode'], string> = {
  READY: '可以激活', EGRESS_NOT_CONFIGURED: '请先明确配置出口', LINE_VOWIFI_UNSUPPORTED: '线路不支持 Host VoWiFi',
  LINE_HARDWARE_NOT_READY: '模组或 SIM / Profile 尚未就绪', SUBSCRIPTION_NOT_SELECTED: '尚未选择订阅',
  COUNTRY_NOT_FOUND: '当前订阅没有该国家', MIHOMO_NOT_RUNNING: 'Mihomo 未运行',
  MIHOMO_RESTART_REQUIRED: '需要先重启 Mihomo 应用订阅',
}
export const phoneNumberSourceLabels: Record<PhoneNumberSource, string> = {
  'cellular-sim': '蜂窝 SIM', ims: 'IMS',
}

export function phoneNumbers(observations: PhoneNumberObservation[]) {
  if (!observations.length) return <Typography.Text type="secondary">未获取</Typography.Text>
  return <Flex vertical gap={4}>{observations.map((observation) => <Flex key={observation.number} align="center" gap={4} wrap>
    <Typography.Text>{observation.number}</Typography.Text>
    {observation.sources.map((source) => <Tag key={source}>{phoneNumberSourceLabels[source]}</Tag>)}
  </Flex>)}</Flex>
}

export function egressLabel(binding?: LineEgressBinding) {
  if (!binding || binding.mode === 'unconfigured') return '未配置'
  if (binding.mode === 'direct') return '直连'
  return `${binding.countryName || binding.countryCode} (${binding.countryCode})`
}

export function canRequestVoWiFiActivation(state?: VoWiFiLineState) {
  return Boolean(state && !state.desiredActive && (
    state.eligible || state.readinessCode === 'MIHOMO_NOT_RUNNING' || state.readinessCode === 'MIHOMO_RESTART_REQUIRED'
  ))
}

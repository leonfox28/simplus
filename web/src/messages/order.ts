import type { SmsMessage as SMSMessage } from '@/api/generated/types.gen'

// Infinite-query pages and every page's messages are both newest-first. Keep
// that authoritative server sequence intact while flattening, then reverse a
// fresh array for oldest-first chat rendering.
export function smsMessagesForDisplay(pages: readonly (readonly SMSMessage[])[]): SMSMessage[] {
  return pages.flatMap((page) => page).reverse()
}

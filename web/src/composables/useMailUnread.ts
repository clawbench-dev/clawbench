import { readonly, ref } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import type { MailUnreadSummary } from '@/utils/mailTypes'

const TAG = 'MailUnread'
/** One minute is plenty: a badge that lags a mail by 60s is still useful. */
const POLL_MS = 60_000

const total = ref(0)
const accounts = ref<MailUnreadSummary[]>([])
let timer: ReturnType<typeof setInterval> | null = null
let inFlight = false

/** Refreshes the badge immediately (after a sync, a read, a delete…). */
export async function refreshMailUnread(): Promise<void> {
  if (inFlight) return
  inFlight = true
  try {
    const data = await apiGet<{ accounts?: MailUnreadSummary[]; total?: number }>('/api/mail/unread')
    accounts.value = Array.isArray(data.accounts) ? data.accounts : []
    total.value = typeof data.total === 'number' ? data.total : 0
  } catch (e) {
    // A badge is not worth surfacing an error for; keep the last value.
    appLog.d(TAG, 'refresh failed', e)
  } finally {
    inFlight = false
  }
}

/**
 * Starts the shared poller. Idempotent, so every mount site can call it — the
 * dock badge lives in App.vue and the panel refreshes on its own actions.
 */
export function startMailUnreadPolling(): void {
  if (timer !== null) return
  void refreshMailUnread()
  timer = setInterval(() => void refreshMailUnread(), POLL_MS)
}

export function stopMailUnreadPolling(): void {
  if (timer !== null) {
    clearInterval(timer)
    timer = null
  }
}

export function useMailUnread() {
  return { total: readonly(total), accounts: readonly(accounts), refresh: refreshMailUnread }
}

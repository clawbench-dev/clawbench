import { ref } from 'vue'
import i18n from '@/i18n'
import { apiDelete, apiGet, apiPatch, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { mailErrorMessage, type MailAccount, type MailAccountInput, type MailPreset } from '@/utils/mailTypes'

const TAG = 'MailAccounts'

/**
 * Account state for the mail panel.
 *
 * The list is small (a handful of mailboxes) so it is loaded whole and kept in
 * memory; every mutation refetches rather than patching the array, because the
 * server also updates lastSyncAt / lastError and a hand-patched row drifts.
 */
export function useMailAccounts() {
  const accounts = ref<MailAccount[]>([])
  const presets = ref<MailPreset[]>([])
  const loading = ref(false)
  const error = ref('')
  const syncingId = ref<number | null>(null)

  function t(key: string): string {
    return i18n.global.t(key)
  }

  async function load(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const data = await apiGet<{ accounts?: MailAccount[] }>('/api/mail/accounts')
      accounts.value = Array.isArray(data.accounts) ? data.accounts : []
    } catch (e) {
      appLog.w(TAG, 'load accounts failed', e)
      error.value = mailErrorMessage(e, t)
    } finally {
      loading.value = false
    }
  }

  async function loadPresets(): Promise<void> {
    if (presets.value.length > 0) return
    try {
      const data = await apiGet<{ presets?: MailPreset[] }>('/api/mail/presets')
      presets.value = Array.isArray(data.presets) ? data.presets : []
    } catch (e) {
      appLog.w(TAG, 'load presets failed', e)
    }
  }

  async function create(input: MailAccountInput): Promise<MailAccount> {
    const data = await apiPost<{ account: MailAccount }>('/api/mail/accounts', input)
    await load()
    return data.account
  }

  async function update(id: number, input: MailAccountInput): Promise<MailAccount> {
    const data = await apiPatch<{ account: MailAccount }>(`/api/mail/accounts/${id}`, input)
    await load()
    return data.account
  }

  async function remove(id: number): Promise<void> {
    await apiDelete(`/api/mail/accounts/${id}`)
    await load()
  }

  async function test(id: number): Promise<void> {
    await apiPost(`/api/mail/accounts/${id}/test`, {})
  }

  /**
   * Manual sync. Runs on the server for up to a minute, so the request gets a
   * longer budget than the default 10s.
   */
  async function sync(id: number): Promise<void> {
    syncingId.value = id
    try {
      await apiPost(`/api/mail/accounts/${id}/sync`, {}, { timeoutMs: 90_000 })
      await load()
    } finally {
      syncingId.value = null
    }
  }

  return { accounts, presets, loading, error, syncingId, load, loadPresets, create, update, remove, test, sync }
}

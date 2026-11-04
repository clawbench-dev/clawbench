import { computed, ref } from 'vue'
import i18n from '@/i18n'
import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { mailErrorMessage, type MailFolder, type MailMessage } from '@/utils/mailTypes'

const TAG = 'MailMessages'
const PAGE_SIZE = 50

/**
 * Folder + message-list state for one account.
 *
 * Paging is keyset (`sentAt`,`id`) over the LOCAL cache. When the cache runs out
 * but the server says the folder has more, `loadMore` asks the server for an
 * older page (one IMAP round trip) and appends it. That keeps the first render
 * instant even for a 50k-message archive.
 */
export function useMailMessages() {
  const folders = ref<MailFolder[]>([])
  const messages = ref<MailMessage[]>([])
  const activeFolderId = ref(0)
  const loading = ref(false)
  const loadingMore = ref(false)
  const error = ref('')
  const hasMore = ref(false)
  const query = ref('')

  /** Bumped on every folder switch so a slow response cannot land on the new one. */
  let generation = 0

  function t(key: string): string {
    return i18n.global.t(key)
  }

  const activeFolder = computed(() => folders.value.find((f) => f.id === activeFolderId.value) ?? null)

  async function loadFolders(accountId: number): Promise<void> {
    try {
      const data = await apiGet<{ folders?: MailFolder[] }>(`/api/mail/folders?accountId=${accountId}`)
      folders.value = Array.isArray(data.folders) ? data.folders : []
    } catch (e) {
      appLog.w(TAG, 'load folders failed', e)
      folders.value = []
      error.value = mailErrorMessage(e, t)
    }
  }

  async function selectFolder(folderId: number): Promise<void> {
    activeFolderId.value = folderId
    await reload()
  }

  async function reload(): Promise<void> {
    if (!activeFolderId.value) {
      messages.value = []
      return
    }
    const gen = ++generation
    loading.value = true
    error.value = ''
    try {
      const data = await apiGet<{ messages?: MailMessage[]; hasMore?: boolean }>(
        `/api/mail/messages?folderId=${activeFolderId.value}&limit=${PAGE_SIZE}${query.value ? `&q=${encodeURIComponent(query.value)}` : ''}`,
      )
      if (gen !== generation) return
      messages.value = Array.isArray(data.messages) ? data.messages : []
      hasMore.value = data.hasMore === true
    } catch (e) {
      if (gen !== generation) return
      appLog.w(TAG, 'load messages failed', e)
      error.value = mailErrorMessage(e, t)
    } finally {
      if (gen === generation) loading.value = false
    }
  }

  async function loadMore(): Promise<void> {
    if (loadingMore.value || loading.value || !hasMore.value) return
    const last = messages.value[messages.value.length - 1]
    if (!last) return
    const gen = generation
    loadingMore.value = true
    try {
      const page = await apiGet<{ messages?: MailMessage[]; hasMore?: boolean }>(
        `/api/mail/messages?folderId=${activeFolderId.value}&limit=${PAGE_SIZE}` +
          `&beforeAt=${last.sentAt}&beforeId=${last.id}${query.value ? `&q=${encodeURIComponent(query.value)}` : ''}`,
      )
      if (gen !== generation) return
      const fromCache = Array.isArray(page.messages) ? page.messages : []
      appendUnique(fromCache)

      // The local cache is exhausted but the server has more: pull the next
      // older page from IMAP. Only worth doing when the cache returned a short
      // page — otherwise we are still walking what we already have.
      if (fromCache.length < PAGE_SIZE && page.hasMore) {
        const older = await apiPost<{ messages?: MailMessage[]; hasMore?: boolean }>(
          `/api/mail/folders/${activeFolderId.value}/load-older`,
          {},
          { timeoutMs: 60_000 },
        )
        if (gen !== generation) return
        appendUnique(Array.isArray(older.messages) ? older.messages : [])
        hasMore.value = older.hasMore === true
      } else {
        hasMore.value = page.hasMore === true
      }
    } catch (e) {
      if (gen !== generation) return
      appLog.w(TAG, 'load more failed', e)
      error.value = mailErrorMessage(e, t)
    } finally {
      if (gen === generation) loadingMore.value = false
    }
  }

  function appendUnique(rows: MailMessage[]): void {
    if (rows.length === 0) return
    const seen = new Set(messages.value.map((m) => m.id))
    for (const m of rows) {
      if (!seen.has(m.id)) {
        messages.value.push(m)
        seen.add(m.id)
      }
    }
  }

  async function setQuery(q: string): Promise<void> {
    query.value = q
    await reload()
  }

  /** Replaces one row in place after a flag change, without refetching the page. */
  function patchMessage(updated: MailMessage): void {
    const i = messages.value.findIndex((m) => m.id === updated.id)
    if (i >= 0) messages.value[i] = { ...messages.value[i], ...updated }
  }

  function removeMessage(id: number): void {
    const i = messages.value.findIndex((m) => m.id === id)
    if (i >= 0) messages.value.splice(i, 1)
  }

  return {
    folders,
    messages,
    activeFolderId,
    activeFolder,
    loading,
    loadingMore,
    error,
    hasMore,
    query,
    loadFolders,
    selectFolder,
    reload,
    loadMore,
    setQuery,
    patchMessage,
    removeMessage,
  }
}

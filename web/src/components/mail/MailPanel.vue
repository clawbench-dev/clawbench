<template>
  <div class="mail-panel">
    <div v-if="initialLoading" class="mail-panel__loading">
      <PanelSkeleton variant="list" />
    </div>

    <!-- No mailbox configured: the panel does not grow its own account form —
         configuration lives in Settings → 邮箱账号, one place for the whole app. -->
    <MailEmpty
      v-else-if="accounts.length === 0"
      :icon="Mail"
      :title="t('mail.empty.title')"
      :desc="t('mail.empty.desc')"
    >
      <template #action>
        <button class="mail-empty-btn" @click="emit('open-settings')">
          {{ t('mail.empty.action') }}
        </button>
      </template>
    </MailEmpty>

    <template v-else>
      <MailFolderBar
        v-model:account-id="activeAccountId"
        :accounts="accounts"
        :folders="list.folders.value"
        :folder-id="list.activeFolderId.value"
        :query="list.query.value"
        :refreshing="refreshing"
        @select-folder="onSelectFolder"
        @refresh="refreshAll"
        @compose="openCompose"
        @setup="emit('open-settings')"
        @update:query="onQuery"
      />

      <MailList
        v-if="view === 'list'"
        :messages="list.messages.value"
        :loading="list.loading.value"
        :loading-more="list.loadingMore.value"
        :error="list.error.value"
        :has-more="list.hasMore.value"
        :query="list.query.value"
        @open="openMessage"
        @load-more="list.loadMore"
        @retry="list.reload"
      />

      <MailReader
        v-else
        :key="readerMessageId"
        :message-id="readerMessageId"
        @back="backToList"
        @reply="startReply"
        @deleted="onMessageDeleted"
        @updated="onMessageUpdated"
      />
    </template>

    <MailCompose
      :open="composeOpen"
      :account-id="activeAccountId"
      :initial="composeInitial"
      @close="composeOpen = false"
      @sent="onSent"
    />
  </div>
</template>

<script setup lang="ts">
/**
 * The 邮件 dock tab.
 *
 * Two views and one overlay:
 *
 *   list ──点一行──▶ reader ──返回──▶ list
 *     └──写邮件──▶ compose (modal)
 *
 * A three-pane layout is deliberately NOT used: the dock pane is 40% of the
 * window (320px at its narrowest), where a folder rail + list + reading pane
 * would each be too narrow to read. Drilling down gives the message the full
 * width and matches every other panel in the app.
 */
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Mail } from 'lucide-vue-next'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import MailEmpty from './MailEmpty.vue'
import MailFolderBar from './MailFolderBar.vue'
import MailList from './MailList.vue'
import MailReader from './MailReader.vue'
import MailCompose from './MailCompose.vue'
import { useMailAccounts } from '@/composables/useMailAccounts'
import { useMailMessages } from '@/composables/useMailMessages'
import { refreshMailUnread } from '@/composables/useMailUnread'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import type { MailMessage, MailReplyContext } from '@/utils/mailTypes'

const props = defineProps<{ active?: boolean }>()
const emit = defineEmits<{ 'open-settings': [] }>()

const { t } = useI18n()
const accountApi = useMailAccounts()
const list = useMailMessages()

const accounts = computed(() => accountApi.accounts.value)
const activeAccountId = ref(0)
const view = ref<'list' | 'reader'>('list')
const readerMessageId = ref(0)
const initialLoading = ref(true)
const refreshing = ref(false)
const composeOpen = ref(false)
const composeInitial = ref<Partial<MailReplyContext> & { body?: string; draftId?: number }>({})

watch(activeAccountId, async (id) => {
  if (!id) return
  await list.loadFolders(id)
  const preferred = list.folders.value.find((f) => f.specialUse === 'inbox') ?? list.folders.value[0]
  if (preferred) await list.selectFolder(preferred.id)
  else list.activeFolderId.value = 0
})

onMounted(async () => {
  try {
    await accountApi.load()
    const first = accounts.value[0]
    // A freshly added account has never been synced, and the background worker
    // only runs every three minutes — opening the tab used to show an empty
    // inbox for that whole window with no hint that anything was happening.
    // Sync once here so the first visit actually shows the mailbox.
    if (first && !first.lastSyncAt) {
      refreshing.value = true
      try {
        await accountApi.sync(first.id)
        await accountApi.load()
      } catch (e) {
        appLog.w('MailPanel', 'initial sync failed', e)
      } finally {
        refreshing.value = false
      }
    }
    if (accounts.value.length > 0) activeAccountId.value = accounts.value[0].id
  } finally {
    initialLoading.value = false
  }
})

// Re-opening the tab refreshes the list: mail may have arrived while the user
// was in another panel, and the badge alone does not tell which rows are new.
watch(
  () => props.active,
  (active) => {
    if (!active || accounts.value.length === 0) return
    // Only the LOCAL list is reloaded here. The background worker syncs every
    // three minutes; running a full IMAP sync on every tab switch would make
    // opening the tab feel slow and fight the worker for the same account.
    void list.reload()
    void list.loadFolders(activeAccountId.value)
  },
)

async function refreshAll(): Promise<void> {
  if (!activeAccountId.value) return
  refreshing.value = true
  try {
    await accountApi.sync(activeAccountId.value)
    await list.loadFolders(activeAccountId.value)
    await list.reload()
    void refreshMailUnread()
  } catch (e) {
    appLog.w('MailPanel', 'refresh failed', e)
  } finally {
    refreshing.value = false
  }
}

async function onSelectFolder(id: number): Promise<void> {
  view.value = 'list'
  await list.selectFolder(id)
}

async function onQuery(q: string): Promise<void> {
  view.value = 'list'
  await list.setQuery(q)
}

function openMessage(id: number): void {
  readerMessageId.value = id
  view.value = 'reader'
}

async function backToList(): Promise<void> {
  view.value = 'list'
  await list.reload()
  await list.loadFolders(activeAccountId.value)
}

function onMessageDeleted(): void {
  list.removeMessage(readerMessageId.value)
  view.value = 'list'
  void list.loadFolders(activeAccountId.value)
}

function onMessageUpdated(msg: MailMessage): void {
  list.patchMessage(msg)
}

function openCompose(): void {
  composeInitial.value = {}
  composeOpen.value = true
}

async function startReply(mode: 'reply' | 'replyAll' | 'forward'): Promise<void> {
  try {
    const data = await apiGet<{ context: MailReplyContext }>(
      `/api/mail/messages/${readerMessageId.value}/reply-context?mode=${mode}`,
    )
    composeInitial.value = data.context
    composeOpen.value = true
  } catch (e) {
    appLog.w('MailPanel', 'reply context failed', e)
  }
}

async function onSent(): Promise<void> {
  composeOpen.value = false
  await list.reload()
  await list.loadFolders(activeAccountId.value)
  void refreshMailUnread()
}
</script>

<style scoped>
.mail-panel__loading {
  padding: var(--space-2, 4px);
}
.mail-empty-btn {
  padding: calc(5 * var(--dp)) calc(14 * var(--dp));
  font-size: var(--font-size-sm, 12px);
  color: #fff;
  background: var(--accent-color);
  border: none;
  border-radius: var(--radius-sm, 4px);
  cursor: pointer;
  font-family: inherit;
}
.mail-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  height: 100%;
  overflow: hidden;
}
</style>

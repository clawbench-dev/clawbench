<template>
  <div class="mail-bar">
    <div class="mail-bar__top">
      <MenuSelect
        class="mail-bar__account"
        :model-value="accountId"
        :options="accountOptions"
        :title="t('mail.bar.account')"
        @update:model-value="(v) => (accountId = Number(v))"
      />
      <span class="mail-bar__spacer" />
      <button class="mail-icon-btn" :title="t('mail.bar.refresh')" :disabled="refreshing" @click="emit('refresh')">
        <RefreshCw :class="{ 'refresh-spin': refreshing }" />
      </button>
      <button class="mail-icon-btn" :title="t('mail.bar.compose')" @click="emit('compose')">
        <PenLine />
      </button>
      <button class="mail-icon-btn" :title="t('mail.bar.manage')" @click="emit('setup')">
        <Settings />
      </button>
    </div>

    <div class="mail-bar__folders">
      <button
        v-for="f in folders"
        :key="f.id"
        class="mail-folder"
        :class="{ 'is-active': f.id === folderId }"
        @click="emit('select-folder', f.id)"
      >
        <span class="mail-folder__name">{{ folderLabel(f) }}</span>
        <span v-if="f.unread > 0" class="count-badge mail-folder__badge">{{ f.unread > 99 ? '99+' : f.unread }}</span>
      </button>
    </div>

    <div class="mail-bar__search">
      <Search class="mail-bar__search-icon" />
      <input
        :value="query"
        class="mail-bar__search-input"
        type="search"
        :placeholder="t('mail.bar.search')"
        @input="onSearchInput"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Account switcher + folder bar + search.
 *
 * Folders are chips rather than a vertical tree: the dock pane is narrow, and a
 * horizontal strip keeps the whole width for the message list. Servers that
 * expose a deep hierarchy are flattened — the folder's own name is what the
 * user recognizes, not its path.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { PenLine, RefreshCw, Search, Settings } from 'lucide-vue-next'
import MenuSelect from '@/components/common/MenuSelect.vue'
import type { MailAccount, MailFolder } from '@/utils/mailTypes'

const props = defineProps<{
  accounts: MailAccount[]
  folders: MailFolder[]
  accountId: number
  folderId: number
  query: string
  refreshing?: boolean
}>()

const emit = defineEmits<{
  'update:accountId': [id: number]
  'select-folder': [id: number]
  refresh: []
  compose: []
  setup: []
  'update:query': [q: string]
}>()

const { t } = useI18n()

const accountId = computed({
  get: () => props.accountId,
  set: (v: number) => emit('update:accountId', v),
})

const accountOptions = computed(() =>
  props.accounts.map((a) => ({ value: a.id, label: a.name || a.email })),
)

const SPECIAL_LABEL: Record<string, string> = {
  inbox: 'mail.folder.inbox',
  sent: 'mail.folder.sent',
  drafts: 'mail.folder.drafts',
  trash: 'mail.folder.trash',
  junk: 'mail.folder.junk',
  archive: 'mail.folder.archive',
}

function folderLabel(f: MailFolder): string {
  const key = SPECIAL_LABEL[f.specialUse]
  return key ? t(key) : f.name || f.path
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
function onSearchInput(e: Event): void {
  const value = (e.target as HTMLInputElement).value
  // Debounce: the list request hits SQLite, but typing at speed would still
  // queue one query per keystroke.
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => emit('update:query', value), 250)
}
</script>

<style scoped>
.mail-bar {
  display: flex;
  flex-direction: column;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
.mail-bar__top {
  display: flex;
  align-items: center;
  gap: var(--space-1, 2px);
}
/* The trigger is MenuSelect's; only the width is ours. */
.mail-bar__account {
  max-width: calc(180 * var(--dp));
  min-width: 0;
  padding: calc(3 * var(--dp)) calc(7 * var(--dp));
  font-size: var(--font-size-sm, 12px);
}
.mail-bar__spacer {
  flex: 1;
}
.mail-bar__folders {
  display: flex;
  gap: var(--space-2, 4px);
  overflow-x: auto;
  scrollbar-width: none;
}
.mail-bar__folders::-webkit-scrollbar {
  display: none;
}
.mail-folder {
  display: inline-flex;
  align-items: center;
  gap: calc(4 * var(--dp));
  flex: none;
  padding: calc(3 * var(--dp)) calc(9 * var(--dp));
  font-size: var(--font-size-sm, 12px);
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  border: 1px solid transparent;
  border-radius: var(--radius-full, 999px);
  cursor: pointer;
  white-space: nowrap;
}
.mail-folder:hover {
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
}
.mail-folder.is-active {
  color: var(--text-primary);
  border-color: var(--accent-color);
}
.mail-folder__badge {
  font-size: var(--font-size-xs, 11px);
}
.mail-bar__search {
  position: relative;
  display: flex;
  align-items: center;
}
.mail-bar__search-icon {
  position: absolute;
  left: calc(8 * var(--dp));
  width: calc(13 * var(--dp));
  height: calc(13 * var(--dp));
  color: var(--text-tertiary);
  pointer-events: none;
}
.mail-bar__search-input {
  width: 100%;
  box-sizing: border-box;
  padding: calc(4 * var(--dp)) calc(8 * var(--dp)) calc(4 * var(--dp)) calc(26 * var(--dp));
  font-size: var(--font-size-sm, 12px);
  color: var(--text-primary);
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm, 4px);
  outline: none;
}
.mail-bar__search-input:focus {
  border-color: var(--accent-color);
}
.mail-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  padding: 0;
  border: none;
  border-radius: var(--radius-sm, 4px);
  background: none;
  color: var(--text-secondary);
  cursor: pointer;
  flex: none;
}
.mail-icon-btn:hover:not(:disabled) {
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
}
.mail-icon-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.mail-icon-btn :deep(svg) {
  width: calc(15 * var(--dp));
  height: calc(15 * var(--dp));
}
</style>

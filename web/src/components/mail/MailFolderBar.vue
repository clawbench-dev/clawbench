<template>
  <div class="mail-bar">
    <div ref="barRef" class="mail-bar__top" :class="{ 'show-labels': showLabels }">
      <!-- The pickers are wrapped: MenuSelect renders a fragment (trigger +
           teleported popup), so a class on the component itself is dropped and
           the width would never reach the trigger. -->
      <div class="mail-bar__account" :title="t('mail.bar.account')">
        <MenuSelect
          :model-value="accountId"
          :options="accountOptions"
          @update:model-value="(v) => (accountId = Number(v))"
        />
      </div>
      <div class="mail-bar__folder" :title="t('mail.bar.folder')">
        <MenuSelect
          :model-value="folderId"
          :options="folderOptions"
          :placeholder="t('mail.bar.folder')"
          @update:model-value="(v) => emit('select-folder', Number(v))"
        />
      </div>
      <span class="mail-bar__spacer" />
      <RefreshButton class="mail-icon-btn" :loading="refreshing" :title="t('mail.bar.refresh')" @click="emit('refresh')">
        <span class="action-label">{{ t('mail.bar.refresh') }}</span>
      </RefreshButton>
      <button class="mail-icon-btn" :title="t('mail.bar.compose')" @click="emit('compose')">
        <PenLine />
        <span class="action-label">{{ t('mail.bar.compose') }}</span>
      </button>
      <button class="mail-icon-btn" :title="t('mail.bar.manage')" @click="emit('setup')">
        <Settings />
        <span class="action-label">{{ t('mail.bar.manage') }}</span>
      </button>
    </div>

    <div class="mail-bar__search-row">
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
      <button
        class="mail-read-all"
        :disabled="marking || currentUnread === 0"
        :title="t('mail.bar.markAllRead')"
        @click="emit('mark-all-read')"
      >
        {{ t('mail.bar.markAllRead') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Account switcher + folder picker + search.
 *
 * Folders are a dropdown next to the account picker rather than a chip strip:
 * the dock pane is narrow, and the strip cost a whole row of vertical space
 * above the list for what is one selection. Unread counts ride in the option
 * labels so they stay visible without the chips. Servers that expose a deep
 * hierarchy are flattened — the folder's own name is what the user recognizes,
 * not its path.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useActionBarLabels } from '@/composables/useActionBarLabels'
import { PenLine, Search, Settings } from 'lucide-vue-next'
import MenuSelect from '@/components/common/MenuSelect.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import type { MailAccount, MailFolder } from '@/utils/mailTypes'

const props = defineProps<{
  accounts: MailAccount[]
  folders: MailFolder[]
  accountId: number
  folderId: number
  query: string
  refreshing?: boolean
  /** A folder-wide "mark all read" is in flight. */
  marking?: boolean
}>()

const emit = defineEmits<{
  'update:accountId': [id: number]
  'select-folder': [id: number]
  'mark-all-read': []
  refresh: []
  compose: []
  setup: []
  'update:query': [q: string]
}>()

const { t } = useI18n()

/** Labels ride next to the icons only when the row can hold them (shared rule). */
const barRef = ref<HTMLElement | null>(null)
const { showLabels } = useActionBarLabels(() => barRef.value)

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

const folderOptions = computed(() =>
  props.folders.map((f) => ({
    value: f.id,
    // The unread count lives in the label so the picker carries the same
    // information the old chip strip did.
    label: f.unread > 0 ? `${folderLabel(f)} (${f.unread > 99 ? '99+' : f.unread})` : folderLabel(f),
  })),
)

/** Unread in the folder being shown; nothing to mark when it is zero. */
const currentUnread = computed(() => props.folders.find((f) => f.id === props.folderId)?.unread ?? 0)

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
/* The trigger is MenuSelect's; the wrapper only owns the width. */
.mail-bar__account {
  display: flex;
  max-width: calc(150 * var(--dp));
  min-width: 0;
  flex: 0 1 auto;
}
/* The folder picker sits right of the account picker and gives up width first. */
.mail-bar__folder {
  display: flex;
  max-width: calc(120 * var(--dp));
  min-width: 0;
  flex: 0 1 auto;
}
.mail-bar__account :deep(.menu-select),
.mail-bar__folder :deep(.menu-select) {
  min-width: 0;
  max-width: 100%;
  padding: calc(3 * var(--dp)) calc(7 * var(--dp));
  font-size: var(--font-size-sm, 12px);
}
.mail-bar__spacer {
  flex: 1;
}
/* While measuring, the two pickers keep their natural width: otherwise they
   would shrink to absorb the labels and the row would never report an
   overflow. */
.mail-bar__top.measure-labels .mail-bar__account,
.mail-bar__top.measure-labels .mail-bar__folder {
  flex: 0 0 auto;
}
/* Labelled buttons grow with their text instead of staying 26px squares. */
.mail-bar__top.show-labels .mail-icon-btn {
  width: auto;
  padding: 0 calc(8 * var(--dp));
  gap: calc(4 * var(--dp));
}
.mail-bar__search-row {
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
}
.mail-bar__search {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
}
.mail-read-all {
  flex: none;
  padding: calc(4 * var(--dp)) calc(9 * var(--dp));
  font-family: inherit;
  font-size: var(--font-size-sm, 12px);
  color: var(--text-secondary);
  background: var(--bg-tertiary);
  border: 1px solid transparent;
  border-radius: var(--radius-sm, 4px);
  cursor: pointer;
  white-space: nowrap;
}
.mail-read-all:hover:not(:disabled) {
  color: var(--text-primary);
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
}
.mail-read-all:disabled {
  opacity: 0.5;
  cursor: default;
}
.mail-bar__search-icon {
  position: absolute;
  left: calc(8 * var(--dp));
  width: calc(13 * var(--dp));
  height: calc(13 * var(--dp));
  color: var(--text-hint);
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

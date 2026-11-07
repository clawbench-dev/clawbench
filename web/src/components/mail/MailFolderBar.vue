<template>
  <div class="mail-bar">
    <ActionBar class="mail-bar__top" mode="menu" align="end">
      <!-- Two pickers, deliberately not drawn as two equal boxes. The account
           is switched rarely, so it collapses to a small avatar; the folder is
           where the user is, so it gets the name, its unread count and the
           room. A hairline between them keeps "who I am" and "where I am"
           from reading as one form field. -->
      <div class="mail-bar__account" :title="t('mail.bar.account')">
        <MenuSelect
          :model-value="accountId"
          :options="accountOptions"
          :menu-width="280"
          @update:model-value="(v) => (accountId = Number(v))"
        >
          <template #trigger>
            <span class="mail-acct">
              <span class="mail-acct__avatar">{{ currentAccountInitial }}</span>
              <ChevronDown v-if="accounts.length > 1" class="mail-acct__caret" />
            </span>
          </template>
          <template #options>
            <div class="mail-menu">
              <div class="mail-menu__title">{{ t('mail.bar.account') }}</div>
              <button
                v-for="a in accounts"
                :key="a.id"
                type="button"
                class="mail-menu__row"
                :class="{ 'is-current': a.id === accountId }"
                :title="a.email"
                @click="chooseAccount(a.id)"
              >
                <span class="mail-acct__avatar mail-acct__avatar--sm">{{ accountInitial(a) }}</span>
                <span class="mail-menu__who">
                  <span class="mail-menu__name">{{ a.email }}</span>
                  <span v-if="a.name && a.name !== a.email" class="mail-menu__meta">{{ a.name }}</span>
                </span>
                <Check v-if="a.id === accountId" class="mail-menu__check" />
              </button>
            </div>
          </template>
        </MenuSelect>
      </div>

      <span class="mail-bar__divider" />

      <div class="mail-bar__folder" :title="t('mail.bar.folder')">
        <MenuSelect
          :model-value="folderId"
          :options="folderOptions"
          :placeholder="t('mail.bar.folder')"
          :menu-width="280"
          @update:model-value="(v) => emit('select-folder', Number(v))"
        >
          <template #trigger>
            <span class="mail-loc">
              <component :is="currentFolderIcon" class="mail-loc__icon" />
              <span class="mail-loc__name">{{ currentFolderName }}</span>
              <span v-if="currentUnread > 0" class="mail-loc__count">{{ unreadLabel(currentUnread) }}</span>
              <ChevronDown class="mail-loc__caret" />
            </span>
          </template>
          <template #options>
            <div class="mail-menu">
              <div class="mail-menu__title">{{ currentAccountEmail }}</div>
              <button
                v-for="f in folders"
                :key="f.id"
                type="button"
                class="mail-menu__row"
                :class="{ 'is-current': f.id === folderId }"
                :title="folderLabel(f)"
                @click="emit('select-folder', f.id)"
              >
                <component :is="folderIcon(f)" class="mail-menu__fic" />
                <span class="mail-menu__name">{{ folderLabel(f) }}</span>
                <span v-if="f.unread > 0" class="mail-menu__count">{{ unreadLabel(f.unread) }}</span>
                <Check v-if="f.id === folderId" class="mail-menu__check" />
              </button>
            </div>
          </template>
        </MenuSelect>
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
    </ActionBar>

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
 * Two different questions, two different weights: the account ("who am I") is
 * changed rarely and collapses to a 22dp avatar; the folder ("where am I") is
 * changed constantly, so it carries the name, the unread badge and the room.
 * Both keep their dropdown — the folder list can be a dozen entries on QQ mail,
 * and a chip strip above the list cost a whole row of vertical space.
 *
 * Unread counts live on the trigger (badge) and at the right edge of every menu
 * row, instead of being folded into the label as "收件箱 (12)": the count is
 * what the eye is looking for, so it should not sit inside a sentence.
 */
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import ActionBar from '@/components/common/ActionBar.vue'
import {
  Archive,
  Check,
  ChevronDown,
  FileText,
  Folder,
  Inbox,
  MailX,
  PenLine,
  Search,
  Send,
  Settings,
  Trash2,
} from 'lucide-vue-next'
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

const accountId = computed({
  get: () => props.accountId,
  set: (v: number) => emit('update:accountId', v),
})

/**
 * Menu labels are the full address: the trigger shows only the avatar, so the
 * tooltip and the menu are the only places the address is legible.
 */
const accountOptions = computed(() =>
  props.accounts.map((a) => ({ value: a.id, label: a.email || a.name })),
)

/** First character of the address (or the account name) as the avatar glyph. */
function accountInitial(a: MailAccount): string {
  const base = (a.name || a.email || '?').trim()
  return base.charAt(0).toUpperCase()
}

const currentAccountInitial = computed(() => {
  const current = props.accounts.find((a) => a.id === props.accountId) ?? props.accounts[0]
  return current ? accountInitial(current) : '?'
})

const currentAccountEmail = computed(() => {
  const current = props.accounts.find((a) => a.id === props.accountId) ?? props.accounts[0]
  return current?.email ?? ''
})

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

/** Menu rows carry the count separately, so their label stays a plain name. */
const folderOptions = computed(() =>
  props.folders.map((f) => ({ value: f.id, label: folderLabel(f) })),
)

function unreadLabel(n: number): string {
  return n > 99 ? '99+' : String(n)
}

const FOLDER_ICON: Record<string, Component> = {
  inbox: Inbox,
  sent: Send,
  drafts: FileText,
  trash: Trash2,
  junk: MailX,
  archive: Archive,
}

/** Servers expose arbitrary hierarchy; an unmapped folder still needs a face. */
function folderIcon(f: MailFolder): Component {
  return FOLDER_ICON[f.specialUse] ?? Folder
}

const currentFolder = computed(() => props.folders.find((f) => f.id === props.folderId) ?? null)
const currentFolderName = computed(() =>
  currentFolder.value ? folderLabel(currentFolder.value) : t('mail.bar.folder'),
)
const currentFolderIcon = computed(() => (currentFolder.value ? folderIcon(currentFolder.value) : Folder))

/** Unread in the folder being shown; nothing to mark when it is zero. */
const currentUnread = computed(() => currentFolder.value?.unread ?? 0)

/** The menu's own selection is driven by click, not by MenuSelect's default
 *  row handler — the rows are custom (icon + count), so they emit directly. */
function chooseAccount(id: number): void {
  if (id !== props.accountId) emit('update:accountId', id)
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

/* ── Account: a 22dp avatar, not a field ─────────────────────────────── */
.mail-bar__account {
  display: flex;
  flex: none;
}
.mail-acct {
  display: inline-flex;
  align-items: center;
  gap: calc(2 * var(--dp));
}
.mail-acct__avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(22 * var(--dp));
  height: calc(22 * var(--dp));
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 16%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
  flex: none;
}
.mail-acct__avatar--sm {
  width: calc(20 * var(--dp));
  height: calc(20 * var(--dp));
}
.mail-acct__caret {
  width: calc(11 * var(--dp));
  height: calc(11 * var(--dp));
  color: var(--text-muted);
  flex: none;
}

/* ── Divider: "who I am" | "where I am" ──────────────────────────────── */
.mail-bar__divider {
  width: 1px;
  height: calc(14 * var(--dp));
  margin: 0 calc(5 * var(--dp));
  background: var(--border-color);
  flex: none;
}

/* ── Folder: the current location ─────────────────────────────────────── */
.mail-bar__folder {
  display: flex;
  min-width: 0;
  flex: 0 1 auto;
  max-width: calc(180 * var(--dp));
}
.mail-loc {
  display: inline-flex;
  align-items: center;
  gap: calc(5 * var(--dp));
  min-width: 0;
}
.mail-loc__icon {
  width: calc(15 * var(--dp));
  height: calc(15 * var(--dp));
  color: var(--text-secondary);
  flex: none;
}
.mail-loc__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}
.mail-loc__count {
  display: inline-flex;
  align-items: center;
  height: calc(16 * var(--dp));
  padding: 0 calc(5 * var(--dp));
  border-radius: var(--radius-full);
  background: color-mix(in srgb, var(--accent-color) 18%, transparent);
  color: var(--accent-color);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
  flex: none;
}
.mail-loc__caret {
  width: calc(12 * var(--dp));
  height: calc(12 * var(--dp));
  color: var(--text-muted);
  flex: none;
}

/* Both pickers are navigation, not form fields: no border, no box. They keep
   MenuSelect's button (anchoring, exclusive-open, focus handling) and only
   replace its chrome. */
.mail-bar__account :deep(.menu-select),
.mail-bar__folder :deep(.menu-select) {
  min-width: 0;
  max-width: 100%;
  height: calc(26 * var(--dp));
  padding: 0 calc(4 * var(--dp));
  border: none;
  background: none;
  border-radius: var(--radius-sm);
  color: var(--text-primary);
  gap: 0;
}
.mail-bar__account :deep(.menu-select) {
  padding: 0 calc(3 * var(--dp));
}
.mail-bar__account :deep(.menu-select:hover),
.mail-bar__folder :deep(.menu-select:hover),
.mail-bar__account :deep(.menu-select.is-open),
.mail-bar__folder :deep(.menu-select.is-open) {
  background: var(--bg-hover, rgba(128, 128, 128, 0.12));
}
/* The shared focus ring is a hardcoded blue glow; a borderless trigger needs
   the theme's own ring instead. */
.mail-bar__account :deep(.menu-select:focus-visible),
.mail-bar__folder :deep(.menu-select:focus-visible) {
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}

/* ── Menus ───────────────────────────────────────────────────────────── */
.mail-menu {
  display: flex;
  flex-direction: column;
  min-width: calc(236 * var(--dp));
  max-width: 100%;
  padding: var(--space-1, 2px) 0;
}
.mail-menu__title {
  padding: var(--space-1, 2px) calc(10 * var(--dp)) var(--space-2, 4px);
  border-bottom: 1px solid var(--border-color);
  margin-bottom: var(--space-1, 2px);
  font-size: var(--font-size-xs);
  color: var(--text-hint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-menu__row {
  display: flex;
  align-items: center;
  gap: calc(8 * var(--dp));
  width: 100%;
  padding: calc(6 * var(--dp)) calc(10 * var(--dp));
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
}
.mail-menu__row:hover {
  background: var(--bg-tertiary);
}
.mail-menu__row.is-current {
  color: var(--accent-color);
}
.mail-menu__fic {
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  color: var(--text-secondary);
  flex: none;
}
.mail-menu__row.is-current .mail-menu__fic {
  color: var(--accent-color);
}
.mail-menu__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-menu__who {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
  line-height: var(--line-height-tight);
}
.mail-menu__who .mail-menu__name {
  flex: none;
}
.mail-menu__meta {
  font-size: var(--font-size-2xs);
  color: var(--text-hint);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mail-menu__count {
  margin-left: auto;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--accent-color);
  flex: none;
}
.mail-menu__check {
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  color: var(--accent-color);
  flex: none;
}
/* Folder rows: the name takes the slack, the count/check sit at the right. */
.mail-menu__row > .mail-menu__name {
  flex: 1;
}

.mail-bar__spacer {
  flex: 1;
}
/* Labelled buttons grow with their text instead of staying 26px squares.
   ActionBar decides when the labels are shown. */
.mail-bar__top.ab--labels .mail-icon-btn {
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

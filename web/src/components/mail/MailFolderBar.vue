<template>
  <div class="mail-bar">
    <!-- The row is NOT the ActionBar: the account and folder pickers are text
         controls (context switchers), and ActionBar only manages icon buttons —
         it would measure them as fixed items and then squash them with the
         icon-button box, and their length would decide how many buttons fold.
         Same split as the git history toolbar's branch picker. -->
    <div v-if="!searchOpen" class="mail-bar__top">
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

      <!-- Order is the fold order: ActionBar keeps what fits from the left and
           moves the rest into "more", so the primary action comes first and the
           rare one (account settings) is the first to go. -->
      <ActionBar class="mail-bar__actions" mode="menu" align="end" size="bar">
        <!-- `is-primary`: composing is this row's one stand-out action, the same
             marking the task / session / proxy headers give their create entry.
             Everything else here stays neutral. -->
        <button class="mail-icon-btn is-primary" :title="t('mail.bar.compose')" @click="emit('compose')">
          <PenLine />
          <span class="action-label">{{ t('mail.bar.compose') }}</span>
        </button>
        <button
          class="mail-icon-btn"
          data-action="search"
          :title="t('mail.bar.search')"
          :aria-label="t('mail.bar.search')"
          @click="openSearch"
        >
          <Search />
          <span class="action-label">{{ t('mail.bar.search') }}</span>
        </button>
        <RefreshButton class="mail-icon-btn" :loading="refreshing" :title="t('mail.bar.refresh')" @click="emit('refresh')">
          <span class="action-label">{{ t('mail.bar.refresh') }}</span>
        </RefreshButton>
        <button
          class="mail-icon-btn"
          data-action="mark-all-read"
          :disabled="marking || currentUnread === 0"
          :title="t('mail.bar.markAllRead')"
          :aria-label="t('mail.bar.markAllRead')"
          @click="emit('mark-all-read')"
        >
          <MailCheck />
          <span class="action-label">{{ t('mail.bar.markAllRead') }}</span>
        </button>
        <button class="mail-icon-btn" :title="t('mail.bar.manage')" @click="emit('setup')">
          <Settings />
          <span class="action-label">{{ t('mail.bar.manage') }}</span>
        </button>
      </ActionBar>
    </div>

    <!-- Search REPLACES the toolbar rather than floating over it: same row, same
         height, so the list below never shifts. Closing clears the query — a
         hidden filter is a lie (the list would stay filtered with nothing on
         screen saying why). Same rule as the git history search. -->
    <div v-else class="mail-bar__search-row" @keydown.esc="closeSearch">
      <SearchInput
        ref="searchRef"
        class="mail-bar__search"
        :model-value="searchText"
        :placeholder="t('mail.bar.searchPlaceholder')"
        @update:model-value="onSearchInput"
      />
      <button class="mail-bar__search-cancel" @click="closeSearch">{{ t('common.cancel') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * One toolbar: account switcher + folder picker + the actions.
 *
 * Two different questions, two different weights: the account ("who am I") is
 * changed rarely and collapses to a 22dp avatar; the folder ("where am I") is
 * changed constantly, so it carries the name, the unread badge and the room.
 * Both keep their dropdown — the folder list can be a dozen entries on QQ mail,
 * and a chip strip above the list cost a whole row of vertical space.
 *
 * Search and "mark all read" are ordinary buttons in the row, not a second row
 * of their own: search expands in place over the whole toolbar.
 *
 * Unread counts live on the trigger (badge) and at the right edge of every menu
 * row, instead of being folded into the label as "收件箱 (12)": the count is
 * what the eye is looking for, so it should not sit inside a sentence.
 */
import { computed, nextTick, ref, watch, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import ActionBar from '@/components/common/ActionBar.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import {
  Archive,
  Check,
  ChevronDown,
  FileText,
  Folder,
  Inbox,
  MailCheck,
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

/**
 * The search field is not a permanent resident of the toolbar.
 *
 * It used to take a row of its own, which is why the bar was two rows tall.
 * Collapsing it to a button buys that row back; opening it replaces the whole
 * toolbar with the field, so the bar height never changes and the list below
 * does not jump. Same arrangement as the git history toolbar.
 */
const searchOpen = ref(!!props.query)
const searchText = ref(props.query)
const searchRef = ref<InstanceType<typeof SearchInput> | null>(null)
let searchTimer: ReturnType<typeof setTimeout> | null = null

async function openSearch(): Promise<void> {
  searchOpen.value = true
  await nextTick()
  searchRef.value?.focus?.()
}

function closeSearch(): void {
  searchOpen.value = false
  searchText.value = ''
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = null
  // A hidden filter is a lie: the list would stay filtered with nothing on
  // screen saying why. Closing the field therefore clears the query.
  emit('update:query', '')
}

// The query can also change from outside (the panel resets it when a folder is
// switched). Keep the field in step, and re-open it if a query arrives while
// the toolbar is showing — otherwise the list would be filtered invisibly.
watch(
  () => props.query,
  (q) => {
    searchText.value = q
    if (q) searchOpen.value = true
  },
)

function onSearchInput(value: string): void {
  searchText.value = value
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
  padding: var(--space-2, 4px) var(--space-3, 6px);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}
/* Both states are one row of the same height, so swapping the toolbar for the
   search field does not move the list below. */
.mail-bar__top,
.mail-bar__search-row {
  display: flex;
  align-items: center;
  min-height: calc(26 * var(--dp));
  gap: var(--space-4);
}
.mail-bar__search-row {
  gap: var(--space-3);
}

.mail-bar__actions {
  flex: 1 1 auto;
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


/* The field takes the width left over between the row's edges and the cancel
   button. No max-width: filling the remaining space is the point. */
.mail-bar__search {
  flex: 1;
  min-width: 0;
}
/* Match the toolbar row's height exactly (26dp, the icon-button size): the pill
   is a couple of pixels taller on its own, and that difference would show up as
   the list below jumping when the field opens. */
.mail-bar__search :deep(.search-pill) {
  height: calc(26 * var(--dp));
  padding-top: 0;
  padding-bottom: 0;
}
/* Closing the field is a text action, not another square icon: it belongs to
   the field it dismisses (same as the git history search). */
.mail-bar__search-cancel {
  flex: none;
  height: calc(26 * var(--dp));
  padding: 0 var(--space-4);
  border: none;
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--accent-color);
  font-family: inherit;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
}
.mail-bar__search-cancel:hover {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}
/* Size, radius, colour, hover and icon size come from the shared ActionBar row
   rules (size="bar"); this block keeps only what is specific to the bar. */
.mail-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  background: none;
  cursor: pointer;
  flex: none;
}
.mail-icon-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
</style>

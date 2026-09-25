<template>
  <ModalDialog :open="open" :title="t('sessionShare.title')" :z-index="2500" :max-width="560" @close="$emit('close')">
    <div class="session-share-dialog-body">
      <template v-if="loading">
        <div class="session-share-dialog-hint">{{ t('common.loading') }}</div>
      </template>

      <template v-else-if="loadError">
        <div class="session-share-dialog-error">{{ loadError }}</div>
      </template>

      <template v-else-if="messages.length === 0">
        <div class="session-share-dialog-hint">{{ t('sessionShare.empty') }}</div>
      </template>

      <template v-else>
        <!-- Info notice: same visual language as the file share dialog -->
        <div class="share-notice share-notice-info">
          <Info :size="15" class="share-notice-icon" />
          <div class="share-notice-content">
            <span class="share-notice-text">{{ linkUrl ? t('sessionShare.active') : t('sessionShare.explain') }}</span>
            <div class="share-notice-divider" />
            <span class="share-notice-warning">{{ t('sessionShare.securityHint') }}</span>
          </div>
        </div>

        <!-- Link bar (only once a link exists) -->
        <div v-if="linkUrl" class="share-dialog-link-bar">
          <div class="share-dialog-link-input-wrap">
            <input
              ref="linkInputRef"
              class="share-dialog-link-input"
              type="text"
              :value="linkUrl"
              readonly
              spellcheck="false"
              @focus="$event.target.select()"
            />
            <button class="share-dialog-link-btn" :title="t('sessionShare.copyTip')" :aria-label="t('sessionShare.copyTip')" @click="copyLink">
              <Copy :size="14" />
            </button>
            <button
              class="share-dialog-link-btn"
              :title="t('sessionShare.regenerateTip')"
              :aria-label="t('sessionShare.regenerateTip')"
              :disabled="creating"
              @click="regenerateLink"
            >
              <RefreshCw v-if="creating" :size="14" class="share-dialog-spin" />
              <RefreshCw v-else :size="14" />
            </button>
          </div>
        </div>

        <!-- Creation error: shown regardless of whether a link already exists,
             since a failed FIRST creation has no link bar to attach it to. -->
        <div v-if="errorMsg" class="share-dialog-error">{{ errorMsg }}</div>

        <!-- Selection toolbar -->
        <div class="session-share-dialog-toolbar">
          <button class="fbtn session-share-dialog-toggle" @click="toggleAll">
            {{ allSelected ? t('sessionShare.deselectAll') : t('sessionShare.selectAll') }}
          </button>
          <span class="session-share-dialog-count">
            {{ t('sessionShare.selectedCount', { selected: selectedIds.size, total: selectableCount }) }}
          </span>
        </div>

        <!-- Message list: every message is listed, in-flight ones disabled -->
        <div class="session-share-dialog-list" ref="listRef">
          <label
            v-for="m in messages"
            :key="m.id"
            class="session-share-dialog-row"
            :class="{ 'is-unselectable': !isSelectable(m), 'is-checked': selectedIds.has(m.id) }"
          >
            <input
              type="checkbox"
              class="session-share-dialog-check"
              :checked="selectedIds.has(m.id)"
              :disabled="!isSelectable(m)"
              @change="toggleOne(m)"
            />
            <span class="session-share-dialog-role" :class="'role-' + m.role">
              {{ m.role === 'user' ? t('sessionShare.roleUser') : t('sessionShare.roleAssistant') }}
            </span>
            <span class="session-share-dialog-preview" :title="m.preview">{{ m.preview || '—' }}</span>
            <span v-if="!isSelectable(m)" class="session-share-dialog-flag">
              {{ t('sessionShare.generatingCannotShare') }}
            </span>
            <span v-else class="session-share-dialog-time">{{ relativeTime(m.createdAt) }}</span>
          </label>
        </div>

        <!-- Truncation hint: the dialog always lists everything, but the share
             link is what carries the full conversation. -->
        <div v-if="messages.length > previewLimit" class="session-share-dialog-more">
          {{ t('sessionShare.viewFullInLink') }}
        </div>
      </template>
    </div>

    <template #footer>
      <template v-if="loading || loadError || messages.length === 0">
        <span />
      </template>
      <template v-else-if="!linkUrl">
        <button class="fbtn fbtn-primary" :disabled="creating || selectedIds.size === 0" @click="createLink">
          <Link2 :size="14" />
          {{ creating ? t('common.loading') : t('sessionShare.generate') }}
        </button>
      </template>
      <template v-else>
        <a class="fbtn" :href="linkUrl" target="_blank" rel="noopener noreferrer" :title="t('sessionShare.openPage')">
          <ExternalLink :size="14" />
          {{ t('sessionShare.openPage') }}
        </a>
        <button class="fbtn fbtn-danger" @click="revokeLink">
          <Trash2 :size="14" />
          {{ t('sessionShare.revoke') }}
        </button>
      </template>
    </template>
  </ModalDialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, ExternalLink, Info, Link2, RefreshCw, Trash2 } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast.ts'
import { copyText } from '@/utils/clipboard.ts'
import { formatRelativeTime } from '@/utils/format.ts'
import { appLog } from '@/utils/appLog'
import { useSessionShare } from '@/composables/useSessionShare'
// Shared notice + link-bar chrome. Imported (not global) so only the two share
// dialogs load it; see the file header for why it cannot be scoped.
import '@/assets/share-dialog.css'

const TAG = 'SessionShareDialog'

/** How many trailing messages the preview hint mentions (Qwen-style). */
const previewLimit = 10

const props = defineProps({
  open: Boolean,
  sessionId: { type: String, default: '' },
})

defineEmits(['close'])

const { t, locale } = useI18n()
const dialog = useDialog()
const toast = useToast()
const { markShared, markUnshared } = useSessionShare()

const loading = ref(false)
const creating = ref(false)
const loadError = ref('')
const errorMsg = ref('')
const linkUrl = ref('')
const messages = ref([])
const selectedIds = ref(new Set())
const linkInputRef = ref(null)

/** A message is shareable only once finalized (streaming/queued excluded). */
function isSelectable(m) {
  return !m.streaming && !m.queued
}

const selectableCount = computed(() => messages.value.filter(isSelectable).length)
const allSelected = computed(() => {
  const selectable = messages.value.filter(isSelectable)
  return selectable.length > 0 && selectable.every((m) => selectedIds.value.has(m.id))
})

function relativeTime(iso) {
  return iso ? formatRelativeTime(iso, locale.value) : ''
}

function toggleOne(m) {
  if (!isSelectable(m)) return
  const next = new Set(selectedIds.value)
  if (next.has(m.id)) next.delete(m.id)
  else next.add(m.id)
  selectedIds.value = next
}

function toggleAll() {
  if (allSelected.value) {
    selectedIds.value = new Set()
    return
  }
  selectedIds.value = new Set(messages.value.filter(isSelectable).map((m) => m.id))
}

function toAbsoluteUrl(path) {
  return window.location.origin + path
}

async function loadStatus() {
  if (!props.sessionId) return
  loading.value = true
  loadError.value = ''
  errorMsg.value = ''
  linkUrl.value = ''
  try {
    const resp = await fetch(`/api/share/session?session_id=${encodeURIComponent(props.sessionId)}`)
    if (!resp.ok) throw new Error(resp.statusText)
    const data = await resp.json()
    messages.value = Array.isArray(data.messages) ? data.messages : []
    // Default: everything selectable is checked. In-flight messages can never be
    // selected, so they are excluded from the default rather than silently
    // dropped at submit time.
    selectedIds.value = new Set(messages.value.filter(isSelectable).map((m) => m.id))
    if (data.path) linkUrl.value = toAbsoluteUrl(data.path)
    // Reconcile this session's badge with the server, which is authoritative.
    if (data.path) markShared(props.sessionId)
    else markUnshared(props.sessionId)
  } catch (err) {
    appLog.w(TAG, 'failed to load share status', err)
    loadError.value = t('sessionShare.loadFailed')
  } finally {
    loading.value = false
  }
}

async function createLink() {
  creating.value = true
  errorMsg.value = ''
  try {
    const resp = await fetch('/api/share/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        sessionId: props.sessionId,
        messageIds: [...selectedIds.value],
      }),
    })
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}))
      throw new Error(err.error || resp.statusText)
    }
    const data = await resp.json()
    linkUrl.value = toAbsoluteUrl(data.path)
    markShared(props.sessionId)
    copyLink()
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : String(err)
  } finally {
    creating.value = false
  }
}

function copyLink() {
  if (!linkUrl.value) return
  copyText(linkUrl.value, () => {
    toast.show(t('sessionShare.copied'), { icon: '✅', type: 'success', duration: 2000 })
  })
}

async function regenerateLink() {
  // Rotating the token invalidates the old link — confirm before acting.
  const confirmed = await dialog.confirm(t('sessionShare.confirmRegenerate'), { dangerous: true })
  if (!confirmed) return
  await createLink()
}

async function revokeLink() {
  const confirmed = await dialog.confirm(t('sessionShare.confirmRevoke'), { dangerous: true })
  if (!confirmed) return
  errorMsg.value = ''
  try {
    const resp = await fetch(`/api/share/session?session_id=${encodeURIComponent(props.sessionId)}`, { method: 'DELETE' })
    if (!resp.ok) throw new Error(resp.statusText)
    linkUrl.value = ''
    markUnshared(props.sessionId)
    toast.show(t('sessionShare.revoked'), { icon: '🔗', type: 'success', duration: 2000 })
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : String(err)
  }
}

watch(() => props.open, (isOpen) => {
  if (isOpen) void loadStatus()
}, { immediate: true })
</script>

<style scoped>
.session-share-dialog-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  /* Same body padding as SessionTagDialog — .modal-body itself has none. */
  padding: var(--space-6) var(--space-7);
  min-height: 200px;
}

.session-share-dialog-hint,
.session-share-dialog-error {
  padding: 24px 0;
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}

.session-share-dialog-error {
  color: var(--color-red);
}

.session-share-dialog-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}

.session-share-dialog-count {
  font-size: var(--font-size-xs, 12px);
  color: var(--text-muted, #656d76);
}

/* Sits on the shared .fbtn pill; only the size is dial-specific, since the
   toolbar is a compact inline row rather than a dialog footer. */
.session-share-dialog-toggle {
  height: 26px;
  padding: 0 var(--space-5);
  font-size: var(--font-size-xs, 12px);
}

/* ── Message list ──
   The scroll region, so the dialog height stays bounded on a long conversation
   and the footer actions remain reachable. A bordered card on the dialog
   surface, matching how every other list in the app is framed. */
.session-share-dialog-list {
  max-height: 320px;
  overflow-y: auto;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
}

/* ── Row ──
   Dense by design: ~7px vertical padding keeps roughly nine rows visible in the
   320px window, the same density as before the restyle. */
.session-share-dialog-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: 7px var(--space-5);
  cursor: pointer;
  /* Separator derived from the theme border rather than a second, undefined
     token: the previous `--border-color-subtle` was declared nowhere, so every
     theme silently fell through to its light-only #eaeef2 fallback. */
  border-bottom: 1px solid color-mix(in srgb, var(--border-color) 45%, transparent);
  transition: background var(--duration-fast);
}

.session-share-dialog-row:last-child {
  border-bottom: none;
}

/* Selected rows carry the accent wash the session list uses for its active
   row, dialled down to 7% because many rows are selected at once — the point
   is to show what is included, not to shout. */
.session-share-dialog-row.is-checked {
  background: color-mix(in srgb, var(--accent-color) 7%, transparent);
}

@media (hover: hover) {
  .session-share-dialog-row:not(.is-unselectable):hover {
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
  /* A checked row keeps its accent identity while hovered (more specific than
     the plain hover rule above, so it wins). */
  .session-share-dialog-row.is-checked:not(.is-unselectable):hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  }
}

.session-share-dialog-row.is-unselectable {
  cursor: default;
}

/* An in-flight turn cannot be shared yet: mute its text rather than fading the
   whole row, so the reason flag stays legible. Mirrors the muted-placeholder
   treatment in UserMsgIndexDrawer. */
.session-share-dialog-row.is-unselectable .session-share-dialog-preview {
  color: var(--text-muted);
  font-style: italic;
}

/* ── Checkbox ──
   Native checkbox chrome is drawn by the OS and ignores the theme entirely, so
   it is replaced with the app's standard custom box (same construction as
   QuickCommandEditModal / the settings toggles). */
.session-share-dialog-check {
  flex-shrink: 0;
  -webkit-appearance: none;
  appearance: none;
  width: 15px;
  height: 15px;
  margin: 0;
  border: 1.5px solid color-mix(in srgb, var(--text-secondary) 55%, transparent);
  border-radius: var(--radius-xs);
  background: var(--bg-primary);
  cursor: pointer;
  position: relative;
  transition: background var(--duration-base), border-color var(--duration-base);
}

.session-share-dialog-check:checked {
  background: var(--accent-color);
  border-color: var(--accent-color);
}

.session-share-dialog-check:checked::after {
  content: '';
  position: absolute;
  left: 3.5px;
  top: 0.5px;
  width: 4px;
  height: 8px;
  border: solid #fff;
  border-width: 0 1.5px 1.5px 0;
  transform: rotate(45deg);
}

.session-share-dialog-check:disabled {
  cursor: default;
  opacity: var(--opacity-muted);
}

.session-share-dialog-check:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 1px;
}

/* ── Role tag ──
   The app's standard role chip: square-ish, tinted from a theme colour, no
   hardcoded palette. Identical construction to the conversation-index drawer's
   .msg-role-tag so the two message lists read as one family. */
.session-share-dialog-role {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  padding: 0 var(--space-3);
  border-radius: var(--radius-xs);
  border: 1px solid transparent;
  font-size: 10px;
  font-weight: var(--font-weight-semibold);
  line-height: 1.6;
  letter-spacing: 0.3px;
}

.session-share-dialog-role.role-user {
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  border-color: color-mix(in srgb, var(--accent-color) 24%, transparent);
}

.session-share-dialog-role.role-assistant {
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
  border-color: color-mix(in srgb, var(--text-secondary) 24%, transparent);
}

.session-share-dialog-preview {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-sm);
  color: var(--text-primary);
}

/* An excluded row reads as secondary. This is what makes the selection legible
   at a glance, since every row starts checked. */
.session-share-dialog-row:not(.is-checked) .session-share-dialog-preview {
  color: var(--text-muted);
}

/* Tabular figures so the timestamps form a clean right edge down the list. */
.session-share-dialog-time,
.session-share-dialog-flag {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.session-share-dialog-flag {
  color: var(--color-yellow);
  font-variant-numeric: normal;
}

.session-share-dialog-more {
  text-align: center;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
</style>

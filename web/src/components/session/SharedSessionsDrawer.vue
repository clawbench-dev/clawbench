<template>
  <BottomSheet :open="drawer.effectiveOpen.value" auto @close="drawer.close()">
    <template #header>
      <MessageSquareShare :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('sharedSessions.title') }}</span>
      <button
        v-if="items.length > 0 && !busy"
        class="shared-sessions-clear"
        :disabled="clearing"
        :title="t('sharedSessions.clearAll')"
        @click.stop="clearAll"
      >
        <RefreshCw v-if="clearing" :size="13" class="shared-sessions-clear-spin" />
        <Trash2 v-else :size="13" />
        {{ clearing ? t('common.loading') : t('sharedSessions.clear') }}
      </button>
    </template>

    <div class="shared-sessions-body">
      <!-- Loading -->
      <div v-if="busy" class="shared-sessions-hint">
        <LoadingIndicator size="md" />
      </div>

      <!-- Error -->
      <div v-else-if="errorMsg" class="shared-sessions-hint shared-sessions-error">
        {{ errorMsg }}
        <button class="shared-sessions-retry" @click="loadList">{{ t('common.retry') }}</button>
      </div>

      <!-- Empty -->
      <div v-else-if="items.length === 0" class="shared-sessions-hint">
        {{ t('sharedSessions.empty') }}
      </div>

      <!-- List -->
      <div v-else class="shared-sessions-list">
        <div
          v-for="item in items"
          :key="item.token"
          class="shared-session-row"
          :class="{ archived: item.archived, clickable: !item.archived }"
          :role="item.archived ? undefined : 'button'"
          :tabindex="item.archived ? undefined : 0"
          @click="!item.archived && openConversation(item)"
          @keydown.enter="!item.archived && openConversation(item)"
        >
          <div class="shared-session-main">
            <AgentIcon :backend="item.backend" :name="item.backend" :size="22" />
            <div class="shared-session-info">
              <div class="shared-session-name-row">
                <span class="shared-session-name" :title="item.title">{{ item.title || t('share.sharedConversation') }}</span>
                <!-- An archived conversation keeps a working link but cannot be
                     opened, so the badge explains why the row is not clickable. -->
                <span v-if="item.archived" class="shared-session-badge" :title="t('sharedSessions.archivedHint')">
                  {{ t('sharedSessions.archived') }}
                </span>
              </div>
              <span class="shared-session-meta">
                <span v-if="item.messageCount > 0">{{ t('sharedSessions.messageCount', { count: item.messageCount }) }}</span>
                <span v-if="item.messageCount > 0 && item.createdAt" class="shared-session-sep">·</span>
                <span v-if="item.createdAt">{{ item.createdAt }}</span>
              </span>
            </div>
          </div>

          <!-- keydown.stop as well as click.stop: the row above handles
               keydown.enter to open the conversation, and Enter on a focused
               action button bubbles up to it — so revoking by keyboard also
               jumped into the conversation. Click does not bubble the same way
               for buttons, which is why only the keyboard path was affected. -->
          <div class="shared-session-actions" @click.stop @keydown.stop>
            <a
              class="shared-session-btn"
              :href="shareUrl(item)"
              target="_blank"
              rel="noopener noreferrer"
              :title="t('sharedSessions.openInNewTab')"
            >
              <ExternalLink :size="14" />
            </a>
            <button class="shared-session-btn" :title="t('sharedSessions.copyLink')" @click="copyLink(item)">
              <Copy :size="14" />
            </button>
            <button
              class="shared-session-btn danger"
              :disabled="revokingToken === item.token"
              :title="t('sharedSessions.revoke')"
              @click="revoke(item)"
            >
              <Trash2 :size="14" />
            </button>
          </div>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { ref, watch } from 'vue'
import { MessageSquareShare, Copy, ExternalLink, Trash2, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast.ts'
import { copyText } from '@/utils/clipboard.ts'
import { useSessionShare } from '@/composables/useSessionShare'
import { appLog } from '@/utils/appLog'

const TAG = 'SharedSessionsDrawer'

const emit = defineEmits(['selectSession', 'close'])

const { t } = useI18n()
const dialog = useDialog()
const toast = useToast()
const { markShared, markUnshared, resetSessionShareState } = useSessionShare()

// Bound to the chat tab, like the file drawer is bound to browse: it is an
// action popover, not a panel, so it must not auto-reopen on a tab return.
const drawer = useTabDrawer('chat', { autoRestore: false })

const busy = ref(false)
const errorMsg = ref('')
const items = ref([])
const revokingToken = ref('')
const clearing = ref(false)

function openDrawer() {
  drawer.open()
}

// Absolute public URL for a share link.
function shareUrl(item) {
  return window.location.origin + '/share/' + item.token
}

// Reload whenever the drawer becomes visible (first open, or returning to the
// chat tab after autoRestore:false closed it).
watch(() => drawer.effectiveOpen.value, (isOpen) => {
  if (isOpen) void loadList()
})

async function loadList() {
  busy.value = true
  errorMsg.value = ''
  try {
    const resp = await fetch('/api/share/session/list')
    if (!resp.ok) throw new Error(resp.statusText)
    const data = await resp.json()
    items.value = data.shares || []
    // Seed the row badges from the authoritative server list in one pass,
    // instead of asking per session.
    for (const item of items.value) markShared(item.sessionId)
  } catch (err) {
    errorMsg.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

function openConversation(item) {
  // An archived session is not addressable by id (GetSessionFullInfo filters
  // archived=0), so the row is not clickable for those.
  emit('selectSession', item.sessionId)
  drawer.close()
}

function copyLink(item) {
  copyText(shareUrl(item), () => {
    toast.show(t('sharedSessions.copied'), { icon: '✅', type: 'success', duration: 2000 })
  })
}

async function revoke(item) {
  const name = item.title || t('share.sharedConversation')
  const confirmed = await dialog.confirm(t('sharedSessions.confirmRevoke', { name }), { dangerous: true })
  if (!confirmed) return
  revokingToken.value = item.token
  try {
    const resp = await fetch('/api/share/session/list', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: item.token }),
    })
    if (!resp.ok) throw new Error(resp.statusText)
    items.value = items.value.filter(i => i.token !== item.token)
    markUnshared(item.sessionId)
    toast.show(t('sharedSessions.revoked'), { icon: '🔗', type: 'success', duration: 2000 })
  } catch (err) {
    appLog.w(TAG, 'revoke failed', err)
    toast.show(err instanceof Error ? err.message : String(err), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    revokingToken.value = ''
  }
}

async function clearAll() {
  const confirmed = await dialog.confirm(t('sharedSessions.confirmClearAll'), { dangerous: true })
  if (!confirmed) return
  clearing.value = true
  try {
    const resp = await fetch('/api/share/session/list', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ all: true }),
    })
    if (!resp.ok) throw new Error(resp.statusText)
    items.value = []
    resetSessionShareState()
    toast.show(t('sharedSessions.revoked'), { icon: '🔗', type: 'success', duration: 2000 })
  } catch (err) {
    appLog.w(TAG, 'clear-all failed', err)
    toast.show(err instanceof Error ? err.message : String(err), { icon: '⚠️', type: 'error', duration: 3000 })
  } finally {
    clearing.value = false
  }
}

defineExpose({ open: openDrawer })
</script>

<style scoped>
.shared-sessions-body {
  display: flex;
  flex-direction: column;
  max-height: 60vh;
  overflow-y: auto;
  padding: var(--space-2) var(--space-7) var(--space-7);
}

.shared-sessions-hint {
  padding: 24px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--text-muted, #656d76);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-5);
}

.shared-sessions-error { color: #cf222e; }
.shared-sessions-retry {
  padding: var(--space-3) 14px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  font-size: var(--font-size-md);
  cursor: pointer;
}

.shared-sessions-list {
  display: flex;
  flex-direction: column;
}

.shared-session-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding: var(--space-5) var(--space-2);
  border-bottom: 1px solid var(--border-color, rgba(128,128,128,.15));
}
.shared-session-row:last-child { border-bottom: none; }
.shared-session-row.clickable { cursor: pointer; }
.shared-session-row.clickable:hover { background: var(--bg-tertiary, #eaeef2); }
/* An archived conversation is dimmed: its link works, but the conversation
   itself is no longer openable. */
.shared-session-row.archived .shared-session-name {
  opacity: var(--opacity-muted);
}

.shared-session-main {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  min-width: 0;
}

.shared-session-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: var(--space-1);
}

.shared-session-name-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.shared-session-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #1f2328);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.shared-session-badge {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-3);
  border-radius: var(--radius-sm);
  background: rgba(128,128,128,.15);
  color: var(--text-secondary, #57606a);
}

.shared-session-meta {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--text-muted, #656d76);
}
.shared-session-sep { opacity: .6; }

.shared-session-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
}

.shared-session-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary, #57606a);
  cursor: pointer;
}
.shared-session-btn:hover { background: var(--bg-tertiary, #eaeef2); color: var(--accent-color, #0969da); }
.shared-session-btn.danger:hover { color: #cf222e; background: #fef2f2; }
.shared-session-btn:disabled { opacity: var(--opacity-disabled); cursor: default; }

.shared-sessions-clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  height: 28px;
  padding: 0 var(--space-4);
  margin-left: auto;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: #cf222e;
  font-size: var(--font-size-md);
  cursor: pointer;
  flex-shrink: 0;
}
.shared-sessions-clear:disabled { opacity: var(--opacity-muted); cursor: default; }
.shared-sessions-clear-spin { animation: shared-sessions-clear-spin 0.8s linear infinite; }
@keyframes shared-sessions-clear-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
@media (hover: hover) {
  .shared-sessions-clear:not(:disabled):hover { background: #fef2f2; }
}

/* Dark themes: the light-pink hover surfaces above were tuned for light themes.
   Keep the red tint but adapt it to the dark surface (same convention as the
   shared-files drawer). */
[data-theme-base="dark"] .shared-session-btn.danger:hover {
  color: #fca5a5;
  background: rgba(239, 68, 68, 0.15);
}
[data-theme-base="dark"] .shared-sessions-clear {
  color: #fca5a5;
}
[data-theme-base="dark"] .shared-sessions-clear:not(:disabled):hover {
  color: #fca5a5;
  background: rgba(239, 68, 68, 0.15);
}
</style>

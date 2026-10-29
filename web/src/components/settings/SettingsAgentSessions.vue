<template>
  <div class="settings-agent-sessions">
    <!-- Header summary. The count comes from the SERVER (`total`), which is the
         same number the detail page prints — the client never derives it from
         the loaded pages, so the two cannot disagree while more rows load. -->
    <div class="settings-agent-sessions__summary">
      {{ t('settings.items.agentSessionsSummary', { count: total }) }}
    </div>

    <div v-if="loading" class="settings-agent-sessions__hint">
      {{ t('settings.items.agentSessionsLoading') }}
    </div>

    <div v-else-if="error" class="settings-agent-sessions__hint settings-agent-sessions__hint--error">
      {{ t('settings.items.agentSessionsLoadFailed') }}
      <button class="settings-agent-sessions__retry" @click="load(props.agentId)">
        {{ t('settings.items.agentSessionsRetry') }}
      </button>
    </div>

    <div v-else-if="sessions.length === 0" class="settings-agent-sessions__hint">
      {{ t('settings.items.agentSessionsEmpty') }}
    </div>

    <template v-else>
      <div class="settings-agent-sessions__list">
        <button
          v-for="item in sessions"
          :key="item.entryId"
          class="settings-agent-sessions__row"
          :class="{ 'settings-agent-sessions__row--unopenable': !item.sessionId }"
          :disabled="!item.sessionId"
          @click="openSession(item)"
        >
          <span class="settings-agent-sessions__row-main">
            <span class="settings-agent-sessions__row-title">
              {{ item.title || t('settings.items.agentSessionsUntitled') }}
            </span>
            <!-- A member row whose group row is gone (dangling group_id) is
                 still counted, so it must still be listed — but there is no
                 conversation behind it to open. Say so instead of rendering a
                 button that silently does nothing. -->
            <span v-if="!item.sessionId" class="settings-agent-sessions__row-missing">
              {{ t('settings.items.agentSessionsMissing') }}
            </span>
            <span class="settings-agent-sessions__row-meta">
              <span
                class="settings-agent-sessions__role"
                :class="`settings-agent-sessions__role--${item.role}`"
              >{{ roleLabel(item) }}</span>
              <!-- The list spans projects, so the owning project has to be on
                   the row — otherwise two identically titled chats are
                   indistinguishable. -->
              <span v-if="item.projectPath" class="settings-agent-sessions__project">
                {{ baseName(item.projectPath) }}
              </span>
              <span v-if="item.archived" class="settings-agent-sessions__archived">
                {{ t('settings.items.agentSessionsArchived') }}
              </span>
            </span>
          </span>
          <ChevronRight v-if="item.sessionId" :size="14" class="settings-agent-sessions__chevron" />
        </button>

        <!-- Zero-height sentinel: the observer prefetches the next page slightly
             before the true bottom, matching the session search drawer. -->
        <div ref="sentinelRef" class="settings-agent-sessions__sentinel"></div>
        <div v-if="loadingMore" class="settings-agent-sessions__hint">
          {{ t('settings.items.agentSessionsLoadingMore') }}
        </div>
        <!-- Append failure: the loaded rows stay put and only this footer
             changes, so a transient blip does not wipe the list. -->
        <div v-else-if="loadMoreError" class="settings-agent-sessions__hint settings-agent-sessions__hint--error">
          {{ t('settings.items.agentSessionsLoadMoreFailed') }}
          <button class="settings-agent-sessions__retry" @click="retryLoadMore">
            {{ t('settings.items.agentSessionsRetry') }}
          </button>
        </div>
        <div v-else-if="!hasMore" class="settings-agent-sessions__hint settings-agent-sessions__hint--end">
          {{ t('settings.items.agentSessionsEnd') }}
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { inject, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronRight } from 'lucide-vue-next'
import { useAgentSessions, type AgentSessionEntry } from '@/composables/useAgentSessions'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { store } from '@/stores/app'
import { appLog } from '@/utils/appLog'

const TAG = 'AgentSessions'

const props = defineProps<{ agentId: string }>()

const { t } = useI18n()
// Injected by App.vue (same provider ProjectDetailSetting uses). Needed because
// restoring a cross-project archived chat requires the project cookie to point
// at the owning project first.
const hotSwitchProject = inject<((path: string) => Promise<void>) | null>('hotSwitchProject', null)
const toast = useToast()
const dialog = useDialog()
const {
  sessions, total, hasMore, loading, loadingMore, error, loadMoreError,
  load, loadMore, retryLoadMore,
} = useAgentSessions()

const sentinelRef = ref<HTMLElement | null>(null)
let sentinelObserver: IntersectionObserver | null = null

/** Last path segment, for the project chip. */
function baseName(path: string): string {
  const trimmed = path.replace(/[/\\]+$/, '')
  const idx = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'))
  return idx >= 0 ? trimmed.slice(idx + 1) : trimmed
}

/**
 * Role badge text.
 *
 * `member` and `host` differ only in whether the agent runs the group, but a
 * host that also speaks (it is a normal member too) would be indistinguishable
 * from a plain member without the badge — hence three labels, not two.
 */
function roleLabel(item: AgentSessionEntry): string {
  if (item.role === 'host') {
    return item.memberName
      ? t('settings.items.agentSessionsRoleHostNamed', { name: item.memberName })
      : t('settings.items.agentSessionsRoleHost')
  }
  if (item.role === 'member') {
    return item.memberName
      ? t('settings.items.agentSessionsRoleMemberNamed', { name: item.memberName })
      : t('settings.items.agentSessionsRoleMember')
  }
  return t('settings.items.agentSessionsRoleChat')
}

// ── Infinite scroll ───────────────────────────────────────────

function disconnectSentinel() {
  if (sentinelObserver) {
    sentinelObserver.disconnect()
    sentinelObserver = null
  }
}

/**
 * Nearest ancestor that actually scrolls.
 *
 * The list itself does NOT scroll: the settings page body (`.settings-page__body`)
 * is the scroll container, and this component is rendered inside it. Passing
 * `root` set to the list element would be a silent no-op — an IntersectionObserver root that is
 * not a scroll container computes intersection against its own box, and the
 * sentinel is inside that box, so it reports "intersecting" forever and every
 * page is fetched the moment it renders (i.e. no lazy loading at all, which is
 * the one thing this page exists for).
 *
 * Walking up is deliberate: the exact ancestor differs between the narrow
 * layout and the wide-screen dock, so hard-coding a class would break in one of
 * them. `null` (viewport) is the fallback when nothing scrolls — correct but
 * conservative.
 */
function scrollParent(el: HTMLElement | null): HTMLElement | null {
  let node = el?.parentElement ?? null
  while (node) {
    const style = getComputedStyle(node)
    if (/(auto|scroll|overlay)/.test(style.overflowY)) return node
    node = node.parentElement
  }
  return null
}

/**
 * Re-bind the observer to the CURRENT sentinel node. It must be re-created
 * whenever the list grows: the sentinel element is re-rendered, so an observer
 * still holding the old (detached) node would never fire again.
 */
function observeSentinel() {
  disconnectSentinel()
  const el = sentinelRef.value
  if (!el) return
  sentinelObserver = new IntersectionObserver((entries) => {
    if (entries[0]?.isIntersecting && hasMore.value && !loading.value && !loadingMore.value) {
      void loadMore()
    }
  }, { root: scrollParent(el), rootMargin: '120px' })
  sentinelObserver.observe(el)
}

onUnmounted(disconnectSentinel)

watch(
  () => [sessions.value.length, hasMore.value, loading.value, loadingMore.value, loadMoreError.value] as const,
  () => {
    // A failed append must not keep re-observing: the sentinel stays visible, so
    // it would fire loadMore again in a loop. The footer's retry button is the
    // only way forward until the user acts.
    if (!hasMore.value || loadMoreError.value) {
      disconnectSentinel()
      return
    }
    nextTick(() => observeSentinel())
  },
  { immediate: true },
)

onMounted(() => {
  void load(props.agentId)
})

// The sub-route is keyed by agent id, but a deep-link or a replace could land
// on another agent without remounting; reload explicitly rather than trusting
// the key.
watch(() => props.agentId, (id) => {
  if (id) void load(id)
})

// ── Opening a row ─────────────────────────────────────────────

/**
 * Open one conversation.
 *
 * Archived chats are restorable but not readable (`GET history` 404s for them),
 * so they are restored first — the same flow the session search drawer uses.
 *
 * Order matters for a CROSS-PROJECT row: `/api/ai/session/resume` validates the
 * session against the CALLER'S project cookie, so restoring first would 403.
 * The project must be switched first; once the cookie points at the owning
 * project, the same resume call succeeds. Then the session is opened — without
 * that final step the user would be left on the new project's default session.
 *
 * If the switch is refused (the user cancelled, or it failed) nothing is
 * restored and nothing is opened: a half-applied jump would strand them.
 */
async function openSession(item: AgentSessionEntry) {
  const crossProject = !!item.projectPath && item.projectPath !== store.state.projectRoot

  if (crossProject) {
    if (!hotSwitchProject) return
    try {
      await hotSwitchProject(item.projectPath)
    } catch (err) {
      appLog.e(TAG, 'failed to switch project before opening session', err)
      return
    }
  }

  if (item.archived) {
    const title = item.title || t('settings.items.agentSessionsUntitled')
    const ok = await dialog.confirm(
      t('settings.items.agentSessionsResumeConfirm', { title }),
      {
        title: t('settings.items.agentSessionsResume'),
        confirmText: t('common.confirm'),
      },
    )
    if (!ok) return
    try {
      const resp = await fetch('/api/ai/session/resume', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: item.sessionId }),
      })
      if (!resp.ok) {
        toast.show(t('settings.items.agentSessionsResumeFailed'), { icon: 'warning', type: 'error' })
        return
      }
    } catch (err) {
      appLog.e(TAG, 'failed to resume archived session', err)
      toast.show(t('settings.items.agentSessionsResumeFailed'), { icon: 'warning', type: 'error' })
      return
    }
  }

  window.dispatchEvent(new CustomEvent('clawbench-open-session', {
    // projectPath is empty for the same-project case on purpose: the handler
    // treats a path equal to the current project as same-project anyway, but
    // omitting it skips the comparison entirely.
    detail: { sessionId: item.sessionId, projectPath: crossProject ? item.projectPath : undefined },
  }))
}
</script>

<style scoped>
.settings-agent-sessions {
  padding-bottom: var(--space-8);
}

.settings-agent-sessions__summary {
  padding: var(--space-4) var(--space-7);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  background: var(--bg-tertiary);
}

.settings-agent-sessions__list {
  display: flex;
  flex-direction: column;
  background: var(--bg-primary);
}

/* A row is a real button (it navigates), so it must reset the UA button look
   entirely — background, border, font, alignment, padding. */
.settings-agent-sessions__row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-5) var(--space-7);
  border: none;
  border-radius: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  position: relative;
}

.settings-agent-sessions__row::after {
  content: '';
  position: absolute;
  left: calc(16 * var(--dp));
  right: 0;
  bottom: 0;
  height: 1px;
  background: var(--border-color);
}

.settings-agent-sessions__row:last-of-type::after {
  display: none;
}

.settings-agent-sessions__row:active {
  background: var(--bg-tertiary);
}

.settings-agent-sessions__row-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.settings-agent-sessions__row-title {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.settings-agent-sessions__row-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.settings-agent-sessions__role {
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  line-height: 1.6;
}

/* Host vs member differ by SHAPE, not only hue: 36 themes include pairs where
   the accent and the orange are identical, so colour alone cannot carry it.
   The host badge gets a border (an outline the member badge lacks). */
.settings-agent-sessions__role--host {
  border: 1px solid var(--accent-color);
  color: var(--accent-color);
  background: transparent;
}

.settings-agent-sessions__project {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: calc(160 * var(--dp));
}

.settings-agent-sessions__archived {
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  border: 1px solid var(--border-color);
}

.settings-agent-sessions__row--unopenable {
  cursor: default;
}

.settings-agent-sessions__row--unopenable:active {
  background: transparent;
}

.settings-agent-sessions__row-missing {
  font-size: var(--font-size-xs);
  color: var(--text-hint);
}

.settings-agent-sessions__chevron {
  flex-shrink: 0;
  color: var(--text-muted);
}

.settings-agent-sessions__sentinel {
  height: 1px;
}

.settings-agent-sessions__hint {
  padding: var(--space-6);
  text-align: center;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}

.settings-agent-sessions__hint--error {
  color: var(--color-red, var(--text-muted));
}

.settings-agent-sessions__hint--end {
  color: var(--text-hint);
}

.settings-agent-sessions__retry {
  margin-left: var(--space-3);
  padding: 0 var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--accent-color);
  font: inherit;
  cursor: pointer;
}
</style>

<template>
  <!-- The bar exists only to surface OTHER projects' active sessions. With none
       there is nothing to switch to, so hide it entirely (see the watcher below
       for the stranded-tab fallback). -->
  <div v-if="total > 0" class="session-tabs" role="tablist">
    <button
      class="session-tab"
      :class="{ active: activeTab === 'project' }"
      role="tab"
      :aria-selected="activeTab === 'project'"
      @click="$emit('update:activeTab', 'project')"
    >
      {{ t('session.tabProject') }}
    </button>
    <button
      class="session-tab"
      :class="{ active: activeTab === 'cross' }"
      role="tab"
      :aria-selected="activeTab === 'cross'"
      @click="$emit('update:activeTab', 'cross')"
    >
      {{ t('session.tabCross') }}
      <span class="session-tab-badge">{{ total }}</span>
    </button>
  </div>
</template>

<script setup>
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCrossProjectSessions } from '@/composables/useCrossProjectSessions'

const props = defineProps({
  activeTab: { type: String, default: 'project' },
})
const emit = defineEmits(['update:activeTab'])

const { t } = useI18n()
const { total } = useCrossProjectSessions()

// When the last other-project active session goes away the bar unmounts, but the
// wrapper's activeTab may still be 'cross' — leaving the list stuck on a pane the
// user can no longer switch away from. Snap back to the project pane.
watch(total, (n) => {
  if (n === 0 && props.activeTab !== 'project') emit('update:activeTab', 'project')
}, { immediate: true })
</script>

<style scoped>
/* Rendered by the wrapper so it can live outside the scroll area — in the
   pinned sidebar it sits at the bottom of the list column, and in the mobile
   drawer it goes into BottomSheet's #footer slot, which stays pinned instead of
   being pushed off-screen by auto height. */
.session-tabs {
  display: flex;
  /* Never grow/shrink vertically: in the sidebar this is the last row of the
     list column, so growing would steal height from the session list. */
  flex: 0 0 auto;
  /* Stretch across the available width in both hosts: sidebar (column flex,
     so align-self governs width) and the BottomSheet footer (row flex, where
     width:100% fills the content box). */
  align-self: stretch;
  width: 100%;
  border-top: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-secondary, #fff);
}

.session-tab {
  flex: 1 1 0;
  min-width: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  /* Compact vertical rhythm — the bar is a secondary control, not content. */
  padding: var(--space-3) var(--space-3);
  border: none;
  background: transparent;
  color: var(--text-muted, #999);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  line-height: var(--line-height-snug);
  cursor: pointer;
  transition: color var(--duration-base), background var(--duration-base);
}

.session-tab + .session-tab {
  border-left: 1px solid var(--border-color, #dee2e6);
}

.session-tab.active {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 8%, transparent);
}

.session-tab-badge {
  min-width: 16px;
  padding:0 var(--space-2);
  border-radius: var(--radius-sm);
  background: var(--accent-color, #0066cc);
  color: #fff;
  font-size: var(--font-size-2xs);
  line-height: 16px;
  text-align: center;
}

@media (hover: hover) {
  .session-tab:not(.active):hover {
    color: var(--text-primary, #1a1a1a);
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
}
</style>

<template>
  <!-- The bar exists only to surface OTHER projects' sessions. With none there
       is nothing to switch to, so hide it entirely (SessionList snaps the pane
       back to 'project' when the last card goes away). -->
  <div v-if="visible" class="session-tabs" role="tablist">
    <button
      class="session-tab"
      :class="{ active: activeTab === 'project' }"
      role="tab"
      data-session-tab="project"
      :aria-selected="activeTab === 'project'"
      @click="$emit('update:activeTab', 'project')"
    >
      {{ t('session.tabProject') }}
    </button>
    <button
      class="session-tab"
      :class="{ active: activeTab === 'cross' }"
      role="tab"
      data-session-tab="cross"
      :aria-selected="activeTab === 'cross'"
      @click="$emit('update:activeTab', 'cross')"
    >
      {{ t('session.tabCross') }}
      <!-- Active-session count, not the project count: the same number the
           strip's rows carried, so the badge means the same thing in both
           layouts. Idle-only projects show no badge rather than a "0". -->
      <span v-if="total > 0" class="session-tab-badge count-badge">{{ total }}</span>
    </button>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCrossProjectSessions } from '@/composables/useCrossProjectSessions'
import { localConfig } from '@/composables/useSettingsConfig'

defineProps({
  activeTab: { type: String, default: 'project' },
})
defineEmits(['update:activeTab'])

const { t } = useI18n()
const { groups, total } = useCrossProjectSessions()

// Same predicate as SessionList: the bar and the pane must appear and vanish
// together, or the user can end up on a pane with no way back.
const visible = computed(
  () => localConfig.sessionOtherProjectMode === 'tabs' && groups.value.length > 0,
)
</script>

<style scoped>
/* Rendered by the host so it can live outside the scroll area — in the pinned
   sidebar it sits at the bottom of the list column, and in the mobile drawer it
   goes into BottomSheet's #footer slot, which stays pinned instead of being
   pushed off-screen by auto height. */
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
  background: var(--accent-color, #0066cc);
  color: #fff;
}

@media (hover: hover) {
  .session-tab:not(.active):hover {
    color: var(--text-primary, #1a1a1a);
    background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  }
}
</style>

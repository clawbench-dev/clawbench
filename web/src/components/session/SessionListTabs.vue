<template>
  <div class="session-tabs" role="tablist">
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
      <span v-if="total > 0" class="session-tab-badge">{{ total }}</span>
    </button>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { useCrossProjectSessions } from '@/composables/useCrossProjectSessions'

defineProps({
  activeTab: { type: String, default: 'project' },
})
defineEmits(['update:activeTab'])

const { t } = useI18n()
const { total } = useCrossProjectSessions()
</script>

<style scoped>
/* Rendered by the wrapper so it can live outside the scroll area — in the
   pinned sidebar it sits at the bottom of the list column, and in the mobile
   drawer it goes into BottomSheet's #footer slot (which is flex-shrink:0 and
   therefore stays pinned instead of being pushed off-screen by auto height).
   width:100% makes it fill the footer too, whose flex row otherwise packs
   children to the right. */
.session-tabs {
  display: flex;
  flex-shrink: 0;
  width: 100%;
  border-top: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-secondary, #fff);
}

.session-tab {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 9px 8px;
  border: none;
  background: transparent;
  color: var(--text-muted, #999);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
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
  padding: 0 4px;
  border-radius: 8px;
  background: var(--accent-color, #0066cc);
  color: #fff;
  font-size: 10px;
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

<template>
  <div class="session-list-header-content">
    <List :size="16" class="bs-header-icon" />
    <span class="bs-header-title">{{ t('session.title') }}</span>
    <div v-if="sessionMaxCount > 0" class="session-counter">
      <div class="session-counter-bar">
        <div class="session-counter-fill" :style="{ width: sessionPct + '%', background: sessionBarColor }"></div>
        <span class="session-counter-text">{{ sessionCount }}/{{ sessionMaxCount }}</span>
      </div>
    </div>
    <div class="session-header-actions">
      <slot name="actions" />
      <button class="header-action-btn" data-action="search" @click.stop="$emit('open-search')" :title="t('sessionSearch.title')">
        <Search :size="16" />
      </button>
      <button class="header-action-btn" data-action="create" @click.stop="$emit('create')" :title="t('session.newSession')">
        <Plus :size="16" />
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { List, Search, Plus } from 'lucide-vue-next'

const props = defineProps({
  sessionCount: { type: Number, default: 0 },
  sessionMaxCount: { type: Number, default: 0 },
})

defineEmits(['open-search', 'create'])

const { t } = useI18n()

const sessionPct = computed(() => props.sessionMaxCount > 0 ? Math.min((props.sessionCount / props.sessionMaxCount) * 100, 100) : 0)
const sessionBarColor = computed(() => {
  if (props.sessionCount >= props.sessionMaxCount && props.sessionMaxCount > 0) return '#ef4444'
  if (sessionPct.value >= 80) return '#f59e0b'
  return 'var(--accent-color, #0066cc)'
})
</script>

<style scoped>
/* Header content — a flex row that lays out icon/title/counter/actions. Used
   directly inside BottomSheet's own .bs-header (drawer) or a wrapper header
   provided by the sidebar, so it must be a self-contained flex container. */
.session-list-header-content {
  display: flex;
  align-items: center;
  gap: 3px;
  flex: 1;
  min-width: 0;
  width: 100%;
  white-space: nowrap;
  flex-wrap: nowrap;
  overflow: hidden;
}
.session-list-header-content .bs-header-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-counter {
  flex-shrink: 0;
}
.session-counter-bar {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 42px;
  height: 16px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--text-primary) 18%, transparent);
  overflow: hidden;
}
.session-counter-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  border-radius: 8px;
  transition: width 0.3s ease, background 0.3s ease;
}
.session-counter-text {
  position: relative;
  z-index: 1;
  font-size: 9px;
  font-weight: 600;
  color: #fff;
  line-height: 1;
  letter-spacing: 0.3px;
  text-shadow: 0 0 2px rgba(0, 0, 0, 0.3);
}
.session-header-actions {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}
</style>

<style>
/* Shared session-list header action buttons — unscoped so the same styling
   applies to buttons injected into the #actions slot by parents (e.g. the
   sidebar's unpin/close buttons and the drawer's pin button), which are
   rendered by the parent component, not by this one. */
.header-action-btn {
  margin-left: 6px;
  width: 24px;
  height: 24px;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: background 0.15s;
}
.header-action-btn:hover {
  background: rgba(0, 102, 204, 0.1);
}
</style>

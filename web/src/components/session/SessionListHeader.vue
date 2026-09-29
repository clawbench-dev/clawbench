<template>
  <div class="session-list-header-content">
    <div class="session-header-leading">
      <List :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('session.title') }}</span>
    </div>
    <!-- Quota meter. It sits between two equal-flex groups (leading / actions),
         so it lands on the header's center line whenever both sides fit.
         Exact centering and non-overlap are mutually exclusive on a narrow
         sidebar: with 4–5 buttons the action row alone needs more than half the
         header, so a truly centered meter would sit on top of the buttons.
         Hence the trailing group's `min-width: min-content`: when it cannot
         shrink any further the meter slides left (still beside the actions)
         instead of overlapping them. Measured at 220/280/360/480px. -->
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
      <template v-if="pinned">
        <RefreshButton :size="16" class="header-action-btn" data-action="refresh" :loading="refreshing" :disabled="refreshing" :title="t('session.refresh')" @click.stop="triggerRefresh" />
      </template>
      <!-- Trailing slot: hosts put the sidebar pin/unpin toggle here so it is the
           right-most control in the header, after search/create/refresh. The
           leading #actions slot is rendered before the built-in buttons. -->
      <slot name="actions-end" />
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { List, Search, Plus } from 'lucide-vue-next'
import RefreshButton from '@/components/common/RefreshButton.vue'

const props = defineProps({
  sessionCount: { type: Number, default: 0 },
  sessionMaxCount: { type: Number, default: 0 },
  // When pinned (fixed sidebar), additionally show a manual refresh button.
  pinned: { type: Boolean, default: false },
  // Drives the refresh-button spin for the real load duration; the parent
  // (SessionSidebar) tracks this while its loadSessions() is in flight.
  refreshing: { type: Boolean, default: false },
})

const emit = defineEmits(['open-search', 'create', 'refresh'])

function triggerRefresh() {
  if (props.refreshing) return
  emit('refresh')
}

const { t } = useI18n()

const sessionPct = computed(() => props.sessionMaxCount > 0 ? Math.min((props.sessionCount / props.sessionMaxCount) * 100, 100) : 0)
const sessionBarColor = computed(() => {
  if (props.sessionCount >= props.sessionMaxCount && props.sessionMaxCount > 0) return '#ef4444'
  if (sessionPct.value >= 80) return '#f59e0b'
  return 'var(--accent-color, #0066cc)'
})
</script>

<style scoped>
/* Header content — a flex row: an equal-flex leading group (icon + title), the
   auto-width quota meter, and an equal-flex actions group. Equal `flex: 1 1 0`
   on the two side groups gives them the same share of the free space, which is
   what puts the middle meter on the header's center line. Used directly inside
   BottomSheet's own .bs-header (drawer) or a wrapper header provided by the
   sidebar, so it must be a self-contained flex container. */
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
.session-header-leading {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 3px;
  /* At the narrowest sidebar with every optional button present the header
     simply cannot fit title + meter + actions, and this group is what gives
     (the actions floor at min-content). Clipping here keeps the losing content
     from painting underneath the meter; without it the icon box spills over the
     meter and reads as an overlap. */
  overflow: hidden;
}
.session-header-leading .bs-header-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* `flex: 0 0 auto` — the meter keeps its natural width; it is the side groups
   that absorb the slack, and centering depends on them being equal. */
.session-counter {
  flex: 0 0 auto;
}
.session-counter-bar {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 42px;
  height: 16px;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--text-primary) 18%, transparent);
  overflow: hidden;
}
.session-counter-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  border-radius: var(--radius-sm);
  transition: width 0.3s ease, background 0.3s ease;
}
.session-counter-text {
  position: relative;
  z-index: 1;
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  color: #fff;
  line-height: 1;
  letter-spacing: 0.3px;
  text-shadow: 0 0 2px rgba(0, 0, 0, 0.3);
}
/* `min-width: min-content` floors the shrink at the buttons' own width: without
   it the group would keep shrinking and the meter would overlap the buttons on
   a narrow sidebar. With it the meter slides left instead. */
.session-header-actions {
  flex: 1 1 0;
  min-width: min-content;
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
}
</style>

<style>
/* Shared session-list header action buttons — unscoped so the same styling
   applies to buttons injected into the #actions slot by parents (e.g. the
   sidebar's unpin/close buttons and the drawer's pin button), which are
   rendered by the parent component, not by this one. */
.header-action-btn {
  margin-left: var(--space-3);
  width: 24px;
  height: 24px;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-xs);
  transition: background var(--duration-base);
}
@media (hover: hover) {
  .header-action-btn:hover {
    background: rgba(0, 102, 204, 0.1);
  }
}
</style>

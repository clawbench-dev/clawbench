<template>
  <div class="session-group-header" @click="$emit('toggle')">
    <slot name="icon" />
    <span class="session-group-title">{{ title }}</span>
    <span v-if="count !== null" class="session-group-count">{{ count }}</span>
    <span v-if="subtitle" class="session-group-subtitle" :title="subtitleTitle || subtitle">{{ subtitle }}</span>
    <ChevronDown :size="14" class="session-group-chevron" :class="{ collapsed }" />
  </div>
</template>

<script setup>
import { ChevronDown } from 'lucide-vue-next'

/**
 * Shared collapsible group header for the session list.
 *
 * Used by both the project pane (Pinned / Recent sections) and the cross-project
 * pane (one header per other project) so the two stay visually identical — a
 * translucent tinted bar that sticks to the top of the scroll container, with an
 * optional leading icon, a count badge, an optional subtitle (the project path),
 * and a chevron that rotates when collapsed.
 *
 * The parent owns the collapsed state (in-memory only) and passes it back in;
 * this component is purely presentational.
 */
defineProps({
  title: { type: String, required: true },
  // Session count badge. `null` hides it (0 is a valid, shown value).
  count: { type: Number, default: null },
  // Secondary line after the count — the project path in the cross-project pane.
  subtitle: { type: String, default: '' },
  // Tooltip for the subtitle; falls back to the subtitle text itself.
  subtitleTitle: { type: String, default: '' },
  collapsed: { type: Boolean, default: false },
})

defineEmits(['toggle'])
</script>

<style scoped>
.session-group-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-6) var(--space-2);
  /* Same faint tint the cross-project headers already used, now shared so the
     session sections line up with them. */
  background: color-mix(in srgb, var(--text-primary) 4%, transparent);
  position: sticky;
  top: 0;
  z-index: 2;
  cursor: pointer;
  user-select: none;
  -webkit-user-select: none;
}

.session-group-title {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary, #495057);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex-shrink: 0;
  max-width: 60%;
}

.session-group-count {
  font-size: var(--font-size-2xs);
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #e9ecef);
  border-radius: var(--radius-sm);
  padding: 0 5px;
  line-height: 16px;
  flex-shrink: 0;
}

.session-group-subtitle {
  font-size: var(--font-size-2xs);
  color: var(--text-muted, #999);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}

.session-group-chevron {
  margin-left: auto;
  color: var(--text-muted, #999);
  transition: transform var(--duration-slow) ease;
  flex-shrink: 0;
}

.session-group-chevron.collapsed {
  transform: rotate(-90deg);
}
</style>

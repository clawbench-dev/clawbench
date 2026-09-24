<template>
  <nav class="settings-breadcrumb" data-horizontal-scroll="true" :aria-label="t('nav.settings')">
    <template v-for="(crumb, i) in crumbs" :key="crumb.depth">
      <span v-if="i > 0" class="crumb-sep">›</span>
      <span
        class="crumb"
        :class="{ current: i === crumbs.length - 1 }"
        @click="i < crumbs.length - 1 && $emit('navigate', crumb.depth)"
      >{{ crumb.label }}</span>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

export interface BreadcrumbCrumb {
  /** Stack depth this crumb targets; 0 = the settings index. */
  depth: number
  label: string
}

defineProps<{
  crumbs: BreadcrumbCrumb[]
}>()

defineEmits<{
  /** Jump to an ancestor level (the last crumb never emits). */
  navigate: [depth: number]
}>()

const { t } = useI18n()
</script>

<style scoped>
.settings-breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  overflow-x: auto;
  scrollbar-width: none;
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  color: var(--text-muted, #6c757d);
}

.settings-breadcrumb::-webkit-scrollbar {
  display: none;
}

.crumb {
  padding: 3px var(--space-3);
  border-radius: var(--radius-xs);
  white-space: nowrap;
  cursor: pointer;
  color: var(--text-secondary, #495057);
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .crumb:hover {
    background: var(--bg-tertiary, #e9ecef);
    color: var(--accent-color, #4a90d9);
  }
}

.crumb:active {
  background: var(--bg-tertiary, #e9ecef);
}

.crumb.current {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary, #212529);
  cursor: default;
}

@media (hover: hover) {
  .crumb.current:hover {
    background: none;
    color: var(--text-primary, #212529);
  }
}

.crumb-sep {
  color: var(--text-muted, #6c757d);
  font-size: var(--font-size-xs);
  margin: 0 1px;
  user-select: none;
}
</style>

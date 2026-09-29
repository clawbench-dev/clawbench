<template>
  <nav class="settings-breadcrumb" data-horizontal-scroll="true" :aria-label="t('nav.settings')">
    <template v-for="(crumb, i) in crumbs" :key="crumb.depth">
      <span v-if="i > 0" class="crumb-sep">›</span>
      <span
        class="crumb"
        :class="{ current: i === crumbs.length - 1 }"
        @click="i < crumbs.length - 1 && $emit('navigate', crumb.depth)"
      >
        <!-- The root crumb carries the section glyph, like the task breadcrumb's
             root does — so the header still reads as "the settings panel" once
             the plain title has been replaced by the trail. -->
        <Settings v-if="i === 0" :size="14" class="crumb-icon" />
        <span>{{ crumb.label }}</span>
      </span>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { Settings } from 'lucide-vue-next'
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
/* Same row treatment as the task breadcrumb: no container gap (the crumb's own
   padding plus the separator's margin provide it), so the two headers' trails
   sit on the same rhythm. */
.settings-breadcrumb {
  display: flex;
  align-items: center;
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
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  padding: 3px var(--space-3);
  border-radius: var(--radius-xs);
  white-space: nowrap;
  cursor: pointer;
  color: var(--text-secondary, #495057);
  transition: background var(--duration-base), color var(--duration-base);
}

/* The root glyph holds its size in the scrolling flex row. */
.crumb-icon {
  flex-shrink: 0;
}

@media (hover: hover) {
  .crumb:hover {
    background: var(--bg-secondary, #f8f9fa);
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

<template>
  <!-- Shows which rows the settings page is narrowed to, and how to get out.
       Purely presentational: the banner sits OUTSIDE the render components so
       it survives their re-render (the filters are immutable props). -->
  <div class="settings-search-filter">
    <div class="settings-search-filter__text">
      <span class="settings-search-filter__title">{{ t('settings.search.filteredTitle') }}</span>
      <span class="settings-search-filter__query">「{{ query }}」</span>
    </div>
    <button type="button" class="settings-search-filter__clear" @click="$emit('clear')">
      <X :size="12" />
      <span>{{ t('settings.search.showAll') }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

defineProps<{
  /** The query as typed, shown so the user sees which page they are in. */
  query: string
}>()

defineEmits<{
  /** Drop the filter: the page re-renders in full. */
  clear: []
}>()

const { t } = useI18n()
</script>

<style scoped>
.settings-search-filter {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  /* Matches the page padding of .settings-category, so the banner lines up
     with the cards under it. */
  margin: var(--space-4) var(--space-4) 0;
  padding: var(--space-3) var(--space-4) var(--space-3) var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
}

.settings-search-filter__text {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
}

.settings-search-filter__title {
  color: var(--text-secondary);
  white-space: nowrap;
}

.settings-search-filter__query {
  color: var(--text-primary);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.settings-search-filter__clear {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  padding: calc(3 * var(--dp)) var(--space-4);
  border: none;
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

@media (hover: hover) {
  .settings-search-filter__clear:hover {
    color: var(--text-primary);
    background: color-mix(in srgb, var(--text-primary) 10%, var(--bg-tertiary));
  }
}
</style>

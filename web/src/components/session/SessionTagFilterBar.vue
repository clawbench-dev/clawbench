<template>
  <!-- Hidden entirely when the project has no tags in use: an empty filter row
       would just steal vertical space from the list without offering anything.
       The host owns fetching (see SessionList) so the row, the request and the
       list reload share one source of truth. -->
  <div v-if="tags.length > 0" class="session-tag-filter" role="group" :aria-label="t('sessionTags.filterLabel')">
    <button
      v-for="tag in tags"
      :key="tag.name"
      type="button"
      class="session-tag-filter-chip"
      :class="{ active: tag.name === activeTag }"
      :style="tagAccentStyle(tag.name)"
      :aria-pressed="tag.name === activeTag"
      :title="tag.name"
      @click="$emit('toggle', tag.name)"
    >
      <span class="session-tag-filter-name">{{ tag.name }}</span>
      <span v-if="tag.count > 0" class="session-tag-filter-count">{{ tag.count }}</span>
    </button>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { tagAccentStyle } from '@/utils/tagColor.ts'

defineProps({
  // Tags in use in the current project, as returned by
  // GET /api/ai/session/tags?inUse=1 (each with a session count).
  tags: { type: Array, default: () => [] },
  // Currently applied tag name, or '' for no filter. Single-select: the parent
  // clears it when the same chip is clicked again.
  activeTag: { type: String, default: '' },
})

defineEmits(['toggle'])

const { t } = useI18n()
</script>

<style scoped>
/* Sits between the header and the scroll area, so it must never grow or shrink
   vertically — in the sidebar that would steal height from the list. */
.session-tag-filter {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  overflow-x: auto;
  overflow-y: hidden;
  /* Chips are a single row; wrapping would make the bar's height depend on how
     many tags exist, which shifts the list below it. */
  white-space: nowrap;
  border-bottom: 1px solid var(--border-color, rgba(0, 0, 0, 0.08));
  scrollbar-width: none;
}
.session-tag-filter::-webkit-scrollbar {
  display: none;
}

.session-tag-filter-chip {
  --tag-accent: var(--tag-accent-light);
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 0 var(--space-3);
  /* Reset UA button styles: this is a <button>, not an <a>, so without an
     explicit border/background reset the browser paints its own chrome. */
  border: 1px solid color-mix(in srgb, var(--tag-accent) 30%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--tag-accent) 12%, transparent);
  color: var(--tag-accent);
  font-size: var(--font-size-xs);
  line-height: 20px;
  cursor: pointer;
  max-width: 160px;
  transition: background var(--duration-fast, 120ms) ease, color var(--duration-fast, 120ms) ease;
}

.session-tag-filter-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-tag-filter-count {
  flex-shrink: 0;
  opacity: 0.7;
}

/* Selected state: fill with the accent so the active filter is unmistakable
   even when several chips are on screen. */
.session-tag-filter-chip.active {
  background: var(--tag-accent);
  border-color: var(--tag-accent);
  color: #fff;
}

.session-tag-filter-chip:hover:not(.active) {
  background: color-mix(in srgb, var(--tag-accent) 22%, transparent);
}

:root[data-theme-base="dark"] .session-tag-filter-chip {
  --tag-accent: var(--tag-accent-dark);
}
</style>

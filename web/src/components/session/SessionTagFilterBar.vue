<template>
  <!-- Hidden entirely when the project has no tags in use: an empty filter row
       would just steal vertical space from the list without offering anything.
       The host owns fetching (see SessionList) so the row, the request and the
       list reload share one source of truth. -->
  <div v-if="tags.length > 0" class="session-tag-filter" role="group" :aria-label="t('sessionTags.filterLabel')">
    <!-- Caption line. Without it the chips were an unlabelled cluster floating
         between the header and the list, which read as decoration rather than a
         filter control. The caption also gives the clear button a home. -->
    <div class="session-tag-filter-head">
      <Tags :size="12" class="session-tag-filter-icon" aria-hidden="true" />
      <span class="session-tag-filter-label">{{ t('sessionTags.filterTitle') }}</span>
      <button
        v-if="activeTag"
        type="button"
        class="session-tag-filter-clear"
        :title="t('sessionTags.filterClear')"
        :aria-label="t('sessionTags.filterClear')"
        @click="$emit('toggle', activeTag)"
      >
        <X :size="12" />
      </button>
    </div>
    <div class="session-tag-filter-chips">
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
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { Tags, X } from 'lucide-vue-next'
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
/* Sits between the header and the scroll area. It is `flex: 0 0 auto`, so the
   list below absorbs whatever height it takes — including when the chips wrap
   onto a second row. `.session-tag-filter-chips` caps that growth (see below)
   so a project with many tags cannot push the list off screen. */
.session-tag-filter {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  /* Horizontal padding matches `.session-item` so the chips line up with the
     session titles underneath. At the old var(--space-3) they sat almost flush
     with the panel edge while every row below was indented, which is what made
     the bar look crammed against its own border. */
  padding: var(--space-4) var(--space-6) var(--space-5);
  border-bottom: 1px solid var(--border-color, rgba(0, 0, 0, 0.08));
}

.session-tag-filter-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.session-tag-filter-icon {
  color: var(--text-muted, #999);
  flex-shrink: 0;
}

.session-tag-filter-label {
  /* flex: 1 so the clear button is pushed to the far right; the label itself
     ellipsises rather than wrapping, keeping the head a fixed single line. */
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary, #495057);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-tag-filter-clear {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  /* Reset UA button chrome: this is a <button>, so without an explicit border
     and background the browser paints its own. */
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  transition: background var(--duration-fast, 120ms) ease, color var(--duration-fast, 120ms) ease;
}

@media (hover: hover) {
  .session-tag-filter-clear:hover {
    color: var(--accent-color, #0066cc);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}

.session-tag-filter-chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  /* Wrapping (rather than the old nowrap + overflow-x:auto) keeps every tag
     reachable — horizontal overflow was simply clipped, so tags past the first
     row could not be seen or clicked at all on a narrow sidebar.
     The cap stops a long tag list from consuming the whole pane: past ~3 rows
     the area scrolls vertically instead of growing further. */
  max-height: 78px;
  overflow-y: auto;
  overflow-x: hidden;
}

.session-tag-filter-chip {
  --tag-accent: var(--tag-accent-light);
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 0 var(--space-4);
  /* Reset UA button styles: this is a <button>, not an <a>, so without an
     explicit border/background reset the browser paints its own chrome. */
  border: 1px solid color-mix(in srgb, var(--tag-accent) 30%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--tag-accent) 12%, transparent);
  color: var(--tag-accent);
  font-size: var(--font-size-xs);
  line-height: 20px;
  cursor: pointer;
  /* Caps an individual long tag name, but never wider than the row — with
     flex-wrap a chip that overflows just moves to the next line. */
  max-width: 100%;
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
   even when several chips are on screen.
 *
 * The label colour has to follow the theme, not be hardcoded white. The light
 * palette entries are dark (their accent needs a light label: white scores
 * 6.72:1) while the dark-theme entries are light (they need a dark label:
 * black scores 8.54:1, white only 1.67:1). A fixed #fff left every dark-theme
 * chip at 1.67-2.46:1, i.e. unreadable. */
.session-tag-filter-chip.active {
  background: var(--tag-accent);
  border-color: var(--tag-accent);
  color: #fff;
}

:root[data-theme-base="dark"] .session-tag-filter-chip.active {
  color: #111;
}

.session-tag-filter-chip:hover:not(.active) {
  background: color-mix(in srgb, var(--tag-accent) 22%, transparent);
}

:root[data-theme-base="dark"] .session-tag-filter-chip {
  --tag-accent: var(--tag-accent-dark);
}
</style>

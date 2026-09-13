<template>
  <PopupMenu
    :show="show"
    :target-element="targetElement"
    anchor="left"
    :max-width="380"
    :max-height="300"
    :menu-items-count="items.length"
    @update:show="$emit('update:show', $event)"
  >
    <button
      v-for="(item, idx) in items"
      :key="item.source + ':' + item.key"
      class="completion-item"
      :class="['completion-item--' + item.source, { 'completion-item--active': idx === activeIndex }]"
      :data-completion-idx="idx"
      @mousedown.prevent="$emit('select', item)"
      @click.stop
    >
      <component :is="sourceMeta(item.source).icon" :size="14" class="completion-source-icon" :style="{ color: sourceMeta(item.source).color }" />
      <component v-if="item.icon" :is="item.icon" :path="item.key" :size="14" class="completion-item-icon" />
      <span class="completion-text">
        <span class="completion-label" v-html="renderLabel(item)"></span>
        <span v-if="item.description" class="completion-desc">{{ middleEllipsis(item.description, 34) }}</span>
      </span>
      <span class="completion-source" :class="'completion-source--' + item.source" :style="{ color: sourceMeta(item.source).color }">
        {{ t(sourceMeta(item.source).labelKey) }}
      </span>
    </button>
  </PopupMenu>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { SOURCE_META } from '@/utils/completionSources.ts'
import { middleEllipsis } from '@/utils/completionMatch.ts'
import { escapeHtml } from '@/utils/html.ts'
import type { CompletionItem, CompletionSource } from '@/utils/completionMatch.ts'

defineProps<{
  items: CompletionItem[]
  activeIndex: number
  show: boolean
  targetElement?: HTMLElement | null
}>()

defineEmits<{
  select: [item: CompletionItem]
  'update:show': [value: boolean]
}>()

const { t } = useI18n()

function sourceMeta(source: CompletionSource) {
  return SOURCE_META[source]
}

/**
 * Render the label with the fuzzy-matched characters wrapped in <mark>.
 * Falls back to the escaped label when no positions were recorded.
 */
function renderLabel(item: CompletionItem): string {
  const label = item.label || ''
  if (!item.positions || item.positions.length === 0) return escapeHtml(label)
  const hit = new Set(item.positions)
  let out = ''
  for (let i = 0; i < label.length; i++) {
    const ch = escapeHtml(label[i])
    out += hit.has(i) ? '<mark>' + ch + '</mark>' : ch
  }
  return out
}
</script>

<style scoped>
.completion-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  background: none;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}

.completion-item--active {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

@media (hover: hover) {
  .completion-item:hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  }
}

.completion-source-icon {
  flex-shrink: 0;
}

.completion-item-icon {
  flex-shrink: 0;
}

.completion-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
}

.completion-label {
  font-size: var(--font-size-md);
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.completion-label mark {
  background: rgba(255, 230, 0, 0.5);
  color: inherit;
  padding: 0 1px;
  font-weight: 700;
}

:root[data-theme-base="dark"] .completion-label mark {
  background: rgba(255, 230, 0, 0.35);
}

/* Directory hint: muted, middle-ellipsised in JS so both the leading and
   trailing path segments stay visible on long paths. */
.completion-desc {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-align: left;
}

.completion-source {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: 600;
  white-space: nowrap;
}
</style>

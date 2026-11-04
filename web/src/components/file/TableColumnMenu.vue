<template>
  <PopupMenu
    :show="show"
    :target-element="targetElement"
    :max-width="252"
    :menu-items-count="12"
    @update:show="emit('update:show', $event)"
  >
    <div class="cm-scroll">
      <!-- Sort -->
      <button class="cm-item" type="button" :class="{ 'is-active': sorted === 'asc' }" @click="pick('sort', 'asc')">
        <ArrowUp :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.sortAsc') }}</span>
      </button>
      <button class="cm-item" type="button" :class="{ 'is-active': sorted === 'desc' }" @click="pick('sort', 'desc')">
        <ArrowDown :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.sortDesc') }}</span>
      </button>
      <button v-if="sorted" class="cm-item" type="button" @click="pick('sort', null)">
        <X :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.sortClear') }}</span>
      </button>

      <div class="cm-sep" />

      <!-- Filter -->
      <button class="cm-item" type="button" @click="pick('open-values')">
        <ListFilter :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.filterByValue') }}</span>
        <span v-if="filterCount" class="cm-badge">{{ filterCount }}</span>
      </button>
      <button v-if="filterCount" class="cm-item" type="button" @click="pick('clear-filter')">
        <FilterX :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.clearFilter') }}</span>
      </button>

      <div class="cm-sep" />

      <!-- Columns -->
      <button class="cm-item" type="button" :disabled="!canHide" @click="pick('hide')">
        <EyeOff :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.hide') }}</span>
      </button>
      <button class="cm-item" type="button" :disabled="!canHide" @click="pick('show-only')">
        <Eye :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.showOnly') }}</span>
      </button>
      <button class="cm-item" type="button" :class="{ 'is-active': frozen }" @click="pick('toggle-freeze')">
        <component :is="frozen ? PinOff : Pin" :size="14" class="cm-icon" />
        <span class="cm-label">{{ frozen ? t('file.table.menu.unfreeze') : t('file.table.menu.freeze') }}</span>
      </button>

      <template v-if="numeric">
        <div class="cm-sep" />
        <button class="cm-item" type="button" :class="{ 'is-active': dataBar }" @click="pick('toggle-data-bar')">
          <BarChartHorizontal :size="14" class="cm-icon" />
          <span class="cm-label">{{ dataBar ? t('file.table.menu.hideDataBar') : t('file.table.menu.showDataBar') }}</span>
        </button>
      </template>

      <div class="cm-sep" />

      <!-- Copy -->
      <button class="cm-item" type="button" @click="pick('copy-column')">
        <Copy :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.copyColumn') }}</span>
      </button>
      <button class="cm-item" type="button" @click="pick('copy-header')">
        <Type :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.copyHeader') }}</span>
      </button>

      <div class="cm-sep" />

      <!-- Stats live in their own sheet: seven rows of numbers do not belong
           inside a menu that is also a list of actions. -->
      <button class="cm-item" type="button" @click="pick('open-stats')">
        <BarChart3 :size="14" class="cm-icon" />
        <span class="cm-label">{{ t('file.table.menu.stats') }}</span>
      </button>
    </div>
  </PopupMenu>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  ArrowDown,
  ArrowUp,
  BarChart3,
  BarChartHorizontal,
  Copy,
  Eye,
  EyeOff,
  FilterX,
  ListFilter,
  Pin,
  PinOff,
  Type,
  X,
} from 'lucide-vue-next'
import PopupMenu from '@/components/common/PopupMenu.vue'

defineProps<{
  show: boolean
  targetElement: HTMLElement | null
  numeric: boolean
  sorted: 'asc' | 'desc' | null
  frozen: boolean
  dataBar: boolean
  /** How many values are selected in this column's value filter. */
  filterCount: number
  /** False when hiding would leave no visible column. */
  canHide: boolean
}>()

const emit = defineEmits<{
  'update:show': [value: boolean]
  sort: [direction: 'asc' | 'desc' | null]
  'open-values': []
  hide: []
  'show-only': []
  'toggle-freeze': []
  'toggle-data-bar': []
  'copy-column': []
  'copy-header': []
  'clear-filter': []
  'open-stats': []
}>()

const { t } = useI18n()

type MenuEvent =
  | 'sort'
  | 'open-values'
  | 'hide'
  | 'show-only'
  | 'toggle-freeze'
  | 'toggle-data-bar'
  | 'copy-column'
  | 'copy-header'
  | 'clear-filter'
  | 'open-stats'

function pick(event: MenuEvent, value?: 'asc' | 'desc' | null) {
  emit('update:show', false)
  switch (event) {
    case 'sort': emit('sort', value ?? null); break
    case 'open-values': emit('open-values'); break
    case 'hide': emit('hide'); break
    case 'show-only': emit('show-only'); break
    case 'toggle-freeze': emit('toggle-freeze'); break
    case 'toggle-data-bar': emit('toggle-data-bar'); break
    case 'copy-column': emit('copy-column'); break
    case 'copy-header': emit('copy-header'); break
    case 'clear-filter': emit('clear-filter'); break
    case 'open-stats': emit('open-stats'); break
  }
}
</script>

<style scoped>
/* Layout only — the PopupMenu root is the scroller (it owns the inline
   max-height/overflow). Giving this wrapper its own overflow produced a second
   bar right next to the root's: two visible scrollbars in one 252px menu. */
.cm-scroll {
  display: flex;
  flex-direction: column;
  padding: var(--space-1) 0;
}

.cm-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: calc(6 * var(--dp)) var(--space-5);
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-md);
  line-height: 1.3;
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
}

.cm-item:disabled {
  color: var(--text-muted);
  cursor: default;
}

.cm-item.is-active {
  color: var(--accent-color);
}

.cm-icon {
  flex-shrink: 0;
  color: var(--text-secondary);
}

.cm-item.is-active .cm-icon {
  color: var(--accent-color);
}

.cm-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.cm-badge {
  flex-shrink: 0;
  min-width: calc(16 * var(--dp));
  padding: 0 var(--space-2);
  border-radius: var(--radius-full);
  background: var(--accent-color);
  color: #fff;
  font-size: var(--font-size-2xs);
  line-height: calc(16 * var(--dp));
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.cm-sep {
  height: 1px;
  margin: var(--space-1) 0;
  background: var(--border-color);
}

@media (hover: hover) {
  .cm-item:not(:disabled):hover {
    background: var(--bg-hover);
  }
}
</style>

<style>
/* The column menu's scrollbar is its own — deliberately NOT the app-wide one.
   The global bar is 10px (4px on touch) because it belongs to full-width
   panels; inside a 252px menu it eats a visible slice of every row, and on a
   phone it reads as a fat grey stripe against the dark panel.
   The menu root (PopupMenu's own div) is the scroller, hence the :has() anchor
   — the inner .cm-scroll only lays out the items.
   Tokens are local so a future global bar change cannot drag this one along. */
.popup-menu:has(> .cm-scroll) {
  --col-menu-scrollbar-width: 4px;
  --col-menu-scrollbar-thumb: color-mix(in srgb, var(--text-muted) 38%, transparent);
}

.popup-menu:has(> .cm-scroll)::-webkit-scrollbar {
  width: var(--col-menu-scrollbar-width);
  height: var(--col-menu-scrollbar-width);
}

.popup-menu:has(> .cm-scroll)::-webkit-scrollbar-track {
  background: transparent;
}

.popup-menu:has(> .cm-scroll)::-webkit-scrollbar-thumb {
  background: var(--col-menu-scrollbar-thumb);
  border-radius: var(--radius-full);
}

.popup-menu:has(> .cm-scroll)::-webkit-scrollbar-thumb:hover {
  background: var(--text-secondary);
}

@media (pointer: coarse) {
  .popup-menu:has(> .cm-scroll) {
    --col-menu-scrollbar-width: 2px;
  }
}

/* Firefox: no ::-webkit-scrollbar, and setting scrollbar-color in Chromium
   disables every webkit rule above (see css/scrollbar.css), so it is gated the
   same way the global bar is. */
@supports not selector(::-webkit-scrollbar) {
  .popup-menu:has(> .cm-scroll) {
    scrollbar-width: thin;
    scrollbar-color: var(--col-menu-scrollbar-thumb) transparent;
  }
}
</style>

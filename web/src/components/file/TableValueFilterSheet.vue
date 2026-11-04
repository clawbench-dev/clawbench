<template>
  <BottomSheet
    :open="open"
    :title="title"
    auto
    size="narrow"
    @close="emit('update:open', false)"
  >
    <div class="vf">
      <div class="vf-search">
        <Search :size="14" class="vf-search-icon" />
        <input
          v-model="query"
          class="vf-search-input"
          type="text"
          :placeholder="t('file.table.menu.searchValues')"
          :aria-label="t('file.table.menu.searchValues')"
        />
      </div>

      <p class="vf-hint">{{ t('file.table.menu.valueFilterHint') }}</p>

      <div class="vf-list">
        <label v-for="item in visibleValues" :key="item.value" class="vf-row">
          <input
            class="vf-check"
            type="checkbox"
            :checked="isChecked(item.value)"
            @change="toggle(item.value)"
          />
          <span class="vf-value" :class="{ 'is-empty': item.value === '' }" :title="item.value">
            {{ item.value === '' ? t('file.table.menu.emptyValue') : item.value }}
          </span>
          <span class="vf-count">{{ item.count }}</span>
        </label>
        <div v-if="visibleValues.length === 0" class="vf-empty">{{ t('file.table.menu.noMatchingValues') }}</div>
      </div>
    </div>

    <template #footer>
      <button class="vf-btn" type="button" :disabled="!selected.length" @click="emit('update:selected', [])">
        {{ t('file.table.menu.showAllValues') }}
      </button>
      <button class="vf-btn vf-btn--primary" type="button" @click="emit('update:open', false)">
        {{ t('common.ok') }}
      </button>
    </template>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Search } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'

const props = defineProps<{
  open: boolean
  title: string
  values: Array<{ value: string; count: number }>
  /** Selected values. Empty means "no filter" (every value is shown). */
  selected: string[]
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  'update:selected': [value: string[]]
}>()

const { t } = useI18n()

const query = ref('')

// Reset the search whenever the sheet is opened for a (possibly different)
// column; a leftover query would hide the values the user came to look at.
watch(() => props.open, (open) => { if (open) query.value = '' })

const visibleValues = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return props.values
  return props.values.filter(item => item.value.toLowerCase().includes(needle))
})

const selectedSet = computed(() => new Set(props.selected))

function isChecked(value: string): boolean {
  // Nothing selected = no filter = everything is on.
  return props.selected.length === 0 || selectedSet.value.has(value)
}

function toggle(value: string) {
  if (props.selected.length === 0) {
    // Coming from "everything on", the first uncheck means "everything except
    // this one" — seed the selection with all the other values.
    const next = props.values.map(item => item.value).filter(v => v !== value)
    emit('update:selected', next)
    return
  }
  const next = new Set(props.selected)
  if (next.has(value)) next.delete(value)
  else next.add(value)
  emit('update:selected', [...next])
}
</script>

<style scoped>
.vf {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 0;
  /* .bs-body ships padding-free (see BottomSheet.vue) — the inset is ours. */
  padding: var(--space-6) var(--space-7);
}

.vf-search {
  position: relative;
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.vf-search-icon {
  position: absolute;
  left: var(--space-4);
  color: var(--text-muted);
  pointer-events: none;
}

.vf-search-input {
  width: 100%;
  box-sizing: border-box;
  padding: calc(6 * var(--dp)) var(--space-4) calc(6 * var(--dp)) calc(26 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-family: inherit;
  font-size: var(--font-size-md);
  line-height: var(--input-line-height, normal);
  outline: none;
}

.vf-search-input:focus {
  border-color: var(--accent-color);
}

.vf-hint {
  margin: 0;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.vf-list {
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  min-height: 0;
  max-height: calc(320 * var(--dp));
}

.vf-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: calc(6 * var(--dp)) var(--space-2);
  border-bottom: 1px solid var(--border-color);
  cursor: pointer;
}

.vf-row:last-child {
  border-bottom: none;
}

.vf-check {
  flex-shrink: 0;
  width: calc(14 * var(--dp));
  height: calc(14 * var(--dp));
  margin: 0;
  accent-color: var(--accent-color);
  cursor: pointer;
}

.vf-value {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
  font-size: var(--font-size-md);
}

.vf-value.is-empty {
  color: var(--text-muted);
  font-style: italic;
}

.vf-count {
  flex-shrink: 0;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  font-variant-numeric: tabular-nums;
}

.vf-empty {
  padding: calc(20 * var(--dp)) 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  text-align: center;
}

.vf-btn {
  padding: calc(6 * var(--dp)) var(--space-7);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--text-secondary);
  font-family: inherit;
  font-size: var(--font-size-md);
  cursor: pointer;
}

.vf-btn:disabled {
  opacity: 0.45;
  cursor: default;
}

.vf-btn--primary {
  border-color: var(--accent-color);
  background: var(--accent-color);
  color: #fff;
}
</style>

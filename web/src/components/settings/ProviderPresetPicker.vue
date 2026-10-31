<template>
  <!-- Provider picker: a brand-icon list with search instead of a native
       <select>. The catalogue has 200+ entries whose names mean little on
       their own; the icon + host makes the right row recognizable at a glance.
       BottomSheet is the app's own modal, so it inherits the theme and the
       mobile drag/close behaviour. -->
  <BottomSheet :open="open" auto @close="close">
    <template #header>
      <Boxes :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('settings.items.providerPresetTitle') }}</span>
    </template>

    <div class="preset-picker">
      <div class="preset-search">
        <Search :size="14" class="preset-search-icon" />
        <input
          v-model="query"
          class="preset-search-input"
          type="text"
          autocomplete="off"
          :placeholder="t('settings.items.providerPresetSearch')"
        />
      </div>

      <div class="preset-list">
        <button
          v-if="!query"
          type="button"
          class="preset-row preset-row--none"
          :class="{ 'preset-row--active': !selectedId }"
          @click="choose(null)"
        >
          <span class="preset-icon preset-icon--none"><Ban :size="16" /></span>
          <span class="preset-text">
            <span class="preset-name">{{ t('settings.items.providerPresetNone') }}</span>
          </span>
          <Check v-if="!selectedId" :size="16" class="preset-check" />
        </button>

        <button
          v-for="p in filtered"
          :key="p.id"
          type="button"
          class="preset-row"
          :class="{ 'preset-row--active': selectedId === p.id }"
          :data-preset-id="p.id"
          @click="choose(p)"
        >
          <span class="preset-icon">
            <ProviderIcon :provider-id="p.id" :name="p.name" :size="22" />
          </span>
          <span class="preset-text">
            <span class="preset-name">{{ p.name }}</span>
            <span class="preset-meta">{{ hostOf(p.baseUrl) }}</span>
          </span>
          <span class="preset-proto">{{ p.protocol }}</span>
          <Check v-if="selectedId === p.id" :size="16" class="preset-check" />
        </button>

        <div v-if="filtered.length === 0" class="preset-empty">
          {{ t('settings.items.providerPresetEmpty') }}
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Ban, Boxes, Check, Search } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'

export interface PresetOption {
  id: string
  name: string
  protocol: string
  baseUrl: string
}

const props = defineProps<{
  open: boolean
  presets: PresetOption[]
  selectedId: string
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  select: [preset: PresetOption | null]
}>()

const { t } = useI18n()
const query = ref('')

// A fresh sheet should not remember the previous search.
watch(
  () => props.open,
  (open) => {
    if (open) query.value = ''
  },
)

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.presets
  return props.presets.filter(
    (p) =>
      p.name.toLowerCase().includes(q) ||
      p.id.toLowerCase().includes(q) ||
      p.baseUrl.toLowerCase().includes(q),
  )
})

/** Host is the part that actually distinguishes two same-named gateways. */
function hostOf(baseUrl: string): string {
  if (!baseUrl) return t('settings.items.providerNoBaseUrl')
  try {
    return new URL(baseUrl).host
  } catch {
    return baseUrl
  }
}

function close() {
  emit('update:open', false)
}

function choose(preset: PresetOption | null) {
  emit('select', preset)
  close()
}
</script>

<style scoped>
.preset-picker {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
/* Sticky so the list can scroll underneath without losing the filter. */
.preset-search {
  position: sticky;
  top: 0;
  z-index: 1;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin: 0 var(--space-6) var(--space-3);
  padding: 0 var(--space-4);
  height: calc(32 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
}
.preset-search-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}
.preset-search-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.preset-list {
  display: flex;
  flex-direction: column;
  padding-bottom: var(--space-4);
}
.preset-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-3) var(--space-6);
  border: none;
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
}
.preset-row:hover,
.preset-row--active {
  background: color-mix(in srgb, var(--accent-color) 8%, transparent);
}
.preset-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  flex-shrink: 0;
}
.preset-icon--none {
  color: var(--text-muted);
  background: transparent;
  border: 1px dashed var(--border-color);
}
.preset-text {
  display: flex;
  flex-direction: column;
  gap: calc(1 * var(--dp));
  min-width: 0;
  flex: 1;
}
.preset-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.preset-meta {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.preset-proto {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
  flex-shrink: 0;
}
.preset-check {
  color: var(--accent-color);
  flex-shrink: 0;
}
.preset-empty {
  padding: var(--space-6);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
</style>

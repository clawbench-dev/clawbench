<template>
  <!-- Provider preset list, rendered INLINE inside ProviderFormDialog (a body
       view, not its own ModalDialog). It always opened from inside the form,
       which stacked a second scrim over it and cost two back presses on a
       phone; the form now swaps between views inside one shell instead.
       A brand-icon list with search rather than a native <select>: the
       catalogue has 200+ entries whose names mean little on their own, and the
       icon + host makes the right row recognizable at a glance. -->
  <div class="preset-picker" data-role="preset-picker">
    <div class="preset-search">
      <SearchInput v-model="query" :placeholder="t('settings.items.providerPresetSearch')" />
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
        <ProviderIcon tile :provider-id="p.id" :name="p.name" :size="30" />
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
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Ban, Check } from 'lucide-vue-next'
import SearchInput from '@/components/common/SearchInput.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'

export interface PresetOption {
  id: string
  name: string
  protocol: string
  baseUrl: string
}

const props = defineProps<{
  /** Whether this panel is the visible view of the form dialog. */
  active: boolean
  presets: PresetOption[]
  selectedId: string
}>()

const emit = defineEmits<{
  select: [preset: PresetOption | null]
  done: []
}>()

const { t } = useI18n()
const query = ref('')

// A fresh sheet should not remember the previous search.
watch(
  () => props.active,
  (active) => {
    if (active) query.value = ''
  },
  { immediate: true },
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

function choose(preset: PresetOption | null) {
  emit('select', preset)
  emit('done')
}
</script>

<style scoped>
.preset-picker {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
/* Sticky so the list can scroll underneath without losing the filter. The
   field itself is the shared SearchInput pill; this wrapper only owns the
   gutter, the gap above the field and the sticky offset (a fallthrough class
   would land on the pill's own root and fight its geometry). It paints the
   card's own --bg-secondary, not the pill's fill, so the scrolling list
   disappears behind it instead of showing through. */
.preset-search {
  position: sticky;
  top: 0;
  z-index: 1;
  margin: 0 var(--space-6) var(--space-3);
  padding-top: var(--space-4);
  background: var(--bg-secondary);
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
.preset-icon--none {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(30 * var(--dp));
  height: calc(30 * var(--dp));
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  background: transparent;
  border: 1px dashed var(--border-color);
  flex-shrink: 0;
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

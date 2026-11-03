<template>
  <!-- Model set for one provider. The endpoint is the only authority on what it
       serves, so the list comes from its /models probe; when the gateway does
       not publish one we fall back to the catalogue snapshot (the response says
       which). Anything missing can still be typed in.
       ModalDialog (not BottomSheet) for the same reason as ProviderPresetPicker:
       it always opens from inside ProviderFormDialog and must paint above it. -->
  <ModalDialog
    :open="open"
    :title="t('settings.items.providerModelsTitle')"
    :z-index="2600"
    @close="close"
  >
    <template #header>
      <Layers :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ t('settings.items.providerModelsTitle') }}</span>
    </template>

    <div class="models-picker">
      <div class="models-search">
        <SearchInput v-model="query" :placeholder="t('settings.items.providerModelsSearch')" />
      </div>

      <div class="models-source">
        <span v-if="loading">{{ t('settings.items.providerModelsLoading') }}</span>
        <span v-else-if="source === 'live'">{{ t('settings.items.providerModelsFromLive') }}</span>
        <span v-else-if="source === 'catalog'">{{ t('settings.items.providerModelsFromCatalog') }}</span>
        <span v-else class="models-source--warn">{{ loadError || t('settings.items.providerModelsEmpty') }}</span>
        <span class="models-count">{{ t('settings.items.providerModelsSelected', { count: selected.length }) }}</span>
      </div>

      <div class="models-list">
        <button
          v-for="id in visible"
          :key="id"
          type="button"
          class="models-row"
          :class="{ 'models-row--on': selected.includes(id) }"
          @click="toggle(id)"
        >
          <span class="models-check">
            <Check v-if="selected.includes(id)" :size="12" />
          </span>
          <span class="models-id">{{ id }}</span>
        </button>
        <div v-if="!loading && visible.length === 0" class="models-empty">
          {{ t('settings.items.providerModelsNoMatch') }}
        </div>
      </div>

      <div class="models-custom">
        <input
          v-model="custom"
          class="models-custom-input"
          type="text"
          :placeholder="t('settings.items.providerModelsCustom')"
          @keydown.enter="addCustom"
        />
        <button class="fbtn" :disabled="!custom.trim()" @click="addCustom">{{ t('settings.items.providerModelsAdd') }}</button>
      </div>
    </div>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Layers } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'

const props = defineProps<{
  open: boolean
  providerId: number | null
  selected: string[]
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  'update:selected': [value: string[]]
}>()

const { t } = useI18n()
const query = ref('')
const custom = ref('')
const loading = ref(false)
const source = ref<'' | 'live' | 'catalog'>('')
const loadError = ref('')
const available = ref<string[]>([])

/** Bumped on every load. The dialog is only hidden on close (ModalDialog keeps
 *  it mounted), so a probe started for the previous provider can land after the
 *  next open and overwrite the list or clear the spinner early. Declared before
 *  the immediate watcher below, which calls load() during setup. */
let loadGeneration = 0

// Reload on every open: the endpoint may have gained models since last time,
// and a stale list here would silently drop them from the provider.
watch(
  () => props.open,
  (open) => {
    if (!open) return
    query.value = ''
    custom.value = ''
    void load()
  },
  { immediate: true },
)

async function load() {
  if (props.providerId === null) return
  const generation = ++loadGeneration
  loading.value = true
  loadError.value = ''
  try {
    const data = await apiGet<{ models?: string[]; source?: 'live' | 'catalog'; error?: string }>(
      `/api/providers/${props.providerId}/models`,
    )
    if (generation !== loadGeneration) return
    available.value = data.models ?? []
    source.value = data.source ?? ''
    loadError.value = clipError(data.error ?? '')
  } catch (err) {
    if (generation !== loadGeneration) return
    appLog.w('ProviderModelsPicker', 'load models failed', err)
    available.value = []
    source.value = ''
    loadError.value = clipError(err instanceof Error ? err.message : String(err))
  } finally {
    if (generation === loadGeneration) loading.value = false
  }
}

/** The endpoint relays the upstream body verbatim (an HTML error page can be a
 *  megabyte) and this line renders it inline — clip before display. */
function clipError(text: string): string {
  const s = text.trim()
  return s.length > 300 ? s.slice(0, 300) + '…' : s
}

/** Union of what the endpoint offers and what the user already picked, so a
 *  previously saved id that the probe no longer returns stays visible (and
 *  removable) instead of vanishing from the list. */
const allIDs = computed(() => {
  const out = [...available.value]
  for (const id of props.selected) {
    if (!out.includes(id)) out.push(id)
  }
  return out
})

const visible = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return allIDs.value
  return allIDs.value.filter((id) => id.toLowerCase().includes(q))
})

function toggle(id: string) {
  const next = props.selected.includes(id)
    ? props.selected.filter((x) => x !== id)
    : [...props.selected, id]
  emit('update:selected', next)
}

function addCustom() {
  const id = custom.value.trim()
  if (!id) return
  if (!props.selected.includes(id)) emit('update:selected', [...props.selected, id])
  if (!available.value.includes(id)) available.value = [...available.value, id]
  custom.value = ''
}

function close() {
  emit('update:open', false)
}
</script>

<style scoped>
.models-picker {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 0;
  padding-top: var(--space-4);
}
/* Gutter only: the field is the shared SearchInput pill (same as every other
   filter box in the app). */
.models-search {
  margin: 0 var(--space-6);
}
.models-source {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: 0 var(--space-6);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.models-source--warn {
  color: var(--color-orange);
}
.models-count {
  flex-shrink: 0;
}
.models-list {
  display: flex;
  flex-direction: column;
  max-height: 50vh;
  overflow-y: auto;
}
.models-row {
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
.models-row:hover {
  background: var(--bg-secondary);
}
.models-check {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(18 * var(--dp));
  height: calc(18 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: calc(4 * var(--dp));
  flex-shrink: 0;
  color: var(--accent-color);
}
.models-row--on .models-check {
  border-color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
}
.models-id {
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.models-empty {
  padding: var(--space-6);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.models-custom {
  display: flex;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-6) var(--space-4);
  border-top: 1px solid var(--border-color);
  /* Pinned: inside a dialog the body is the scroller, and a long list would
     otherwise push the "add a model id" row out of reach. */
  position: sticky;
  bottom: 0;
  background: var(--bg-secondary);
}
.models-custom-input {
  flex: 1;
  min-width: 0;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.models-custom-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
</style>

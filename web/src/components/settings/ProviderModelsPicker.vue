<template>
  <!-- Model set for one provider, rendered INLINE inside ProviderFormDialog
       (a body section, not its own ModalDialog). A nested dialog here stacked a
       second scrim over the form and cost two back presses on a phone; the
       form now swaps between "form" and "manage models" views inside one shell
       instead. The endpoint is the only authority on what it serves, so the
       list comes from its /models probe; when the gateway does not publish one
       we fall back to the catalogue snapshot (the response says which).
       Anything missing can still be typed in. -->
  <div class="models-picker" data-role="models-picker">
    <div class="models-search">
      <SearchInput v-model="query" :placeholder="t('settings.items.providerModelsSearch')" />
    </div>

    <div class="models-toolbar">
      <span class="models-source" :class="{ 'models-source--warn': warn }" :title="warn || undefined">
        {{ sourceText }}
      </span>
      <span class="models-count">{{ t('settings.items.providerModelsSelected', { count: selected.length }) }}</span>
      <span class="models-toolbar-spacer" />
      <button type="button" class="models-bulk" :disabled="visible.length === 0" @click="selectVisible">
        {{ t('settings.items.providerModelsAll') }}
      </button>
      <button type="button" class="models-bulk" :disabled="selected.length === 0" @click="clearAll">
        {{ t('settings.items.providerModelsClear') }}
      </button>
    </div>

    <div class="models-list">
      <div v-for="id in visible" :key="id" class="models-item">
        <div class="models-row" :class="{ 'models-row--on': isSelected(id) }">
          <button
            type="button"
            class="models-check"
            :class="{ 'models-check--on': isSelected(id) }"
            :aria-pressed="isSelected(id)"
            :aria-label="id"
            @click="toggle(id)"
          >
            <Check v-if="isSelected(id)" :size="12" />
          </button>
          <button type="button" class="models-id" @click="toggle(id)">{{ id }}</button>
          <!-- The default model lives on the row it belongs to (a separate
               <select> duplicated the whole list one screen below it). A star
               is one tap and self-explanatory; disabled until picked. -->
          <button
            type="button"
            class="models-star"
            :class="{ 'models-star--on': defaultModel === id }"
            :disabled="!isSelected(id)"
            :title="defaultModel === id ? t('settings.items.providerModelsUnstar') : t('settings.items.providerModelsStar')"
            :aria-label="defaultModel === id ? t('settings.items.providerModelsUnstar') : t('settings.items.providerModelsStar')"
            @click="setDefault(id)"
          >
            <Star :size="15" :fill="defaultModel === id ? 'currentColor' : 'none'" />
          </button>
          <!-- The only honest way to know a model works: ask it. A listed id
               can still be rejected by the endpoint, and a gateway can serve
               a model it never lists. -->
          <button
            type="button"
            class="models-test"
            :disabled="testingModel !== null || !canProbe"
            :title="t('settings.items.providerModelTestHint')"
            :aria-label="t('settings.items.providerModelTestHint')"
            :data-model-test="id"
            @click="testModel(id)"
          >
            <component :is="testIcon(id)" :size="13" :class="{ 'models-spin': testingModel === id }" />
          </button>
        </div>
        <div
          v-if="modelResults[id]"
          class="models-result"
          :class="modelResults[id].ok ? 'models-result--ok' : 'models-result--fail'"
        >
          <component :is="modelResults[id].ok ? CheckCircle2 : AlertCircle" :size="12" />
          <span>{{ modelResults[id].text }}</span>
        </div>
      </div>
      <div v-if="loading" class="models-empty">{{ t('settings.items.providerModelsLoading') }}</div>
      <div v-else-if="visible.length === 0" class="models-empty">{{ emptyText }}</div>
    </div>

    <div class="models-custom">
      <input
        v-model="custom"
        class="models-custom-input"
        type="text"
        :placeholder="t('settings.items.providerModelsCustom')"
        @keydown.enter="addCustom"
      />
      <button type="button" class="fbtn" :disabled="!custom.trim()" @click="addCustom">
        {{ t('settings.items.providerModelsAdd') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, Check, CheckCircle2, Loader2, Play, Star } from 'lucide-vue-next'
import SearchInput from '@/components/common/SearchInput.vue'
import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'

const props = defineProps<{
  /** Whether this panel is the visible view of the form dialog. Loading and
   *  state resets key on it, so a hidden panel stays quiet. */
  active: boolean
  providerId: number | null
  selected: string[]
  defaultModel: string
  /** Values currently typed in the form, so a probe tests what is on screen
   *  (a key or endpoint that is not saved yet can still be verified). `null`
   *  disables the per-model probe (create mode has no id to probe with). */
  probe: { protocol: string; baseUrl: string; apiKey: string; authMode: string } | null
}>()

const emit = defineEmits<{
  'update:selected': [value: string[]]
  'update:defaultModel': [value: string]
}>()

const { t } = useI18n()
const query = ref('')
const custom = ref('')
const loading = ref(false)
const source = ref<'' | 'live' | 'catalog'>('')
const loadError = ref('')
const available = ref<string[]>([])

/** Bumped on every load. The dialog stays mounted while hidden, so a probe
 *  started for the previous provider can land after the next open and
 *  overwrite the list or clear the spinner early. Declared before the
 *  immediate watcher below, which calls load() during setup. */
let loadGeneration = 0

/** One probe at a time keeps the results readable and the endpoint unhammered. */
const testingModel = ref<string | null>(null)
const modelResults = ref<Record<string, { ok: boolean; text: string }>>({})

/** Bumped on every probe and on every activation. A probe can outlive the view
 *  it was started from (close → reopen → another provider): without this token
 *  a late promise writes the previous provider's verdict into the new list and
 *  releases the in-flight flag of a probe that is still running. Declared
 *  before the immediate watcher below, which resets it during setup. */
let probeGeneration = 0

watch(
  () => props.active,
  (active) => {
    if (!active) return
    query.value = ''
    custom.value = ''
    // A verdict describes the draft that was on screen when its probe ran;
    // re-entering the view (or switching providers) invalidates them all.
    probeGeneration++
    testingModel.value = null
    modelResults.value = {}
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

const warn = computed(() => (!loading.value && source.value === '' ? (loadError.value || t('settings.items.providerModelsEmpty')) : ''))

const sourceText = computed(() => {
  if (loading.value) return t('settings.items.providerModelsLoading')
  if (source.value === 'live') return t('settings.items.providerModelsFromLive')
  if (source.value === 'catalog') return t('settings.items.providerModelsFromCatalog')
  return warn.value
})

const emptyText = computed(() => (warn.value ? warn.value : t('settings.items.providerModelsNoMatch')))

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

function isSelected(id: string) {
  return props.selected.includes(id)
}

function toggle(id: string) {
  const next = props.selected.includes(id)
    ? props.selected.filter((x) => x !== id)
    : [...props.selected, id]
  emit('update:selected', next)
}

function setDefault(id: string) {
  // A second tap on the star of the current default unsets it rather than
  // being a no-op: the provider then has no default and agents fall back to
  // their own discovery, which is a legitimate choice.
  emit('update:defaultModel', props.defaultModel === id ? '' : id)
}

function selectVisible() {
  const next = [...props.selected]
  for (const id of visible.value) {
    if (!next.includes(id)) next.push(id)
  }
  emit('update:selected', next)
}

function clearAll() {
  emit('update:selected', [])
}

function addCustom() {
  const id = custom.value.trim()
  if (!id) return
  if (!props.selected.includes(id)) emit('update:selected', [...props.selected, id])
  if (!available.value.includes(id)) available.value = [...available.value, id]
  custom.value = ''
}

// ── Per-model probe ─────────────────────────────────────────────────────────
// State lives at the top of the script because the activation watcher resets
// it during setup.

const canProbe = computed(() => props.providerId !== null && props.probe !== null)

function testIcon(id: string) {
  if (testingModel.value === id) return Loader2
  if (modelResults.value[id]) return modelResults.value[id].ok ? CheckCircle2 : AlertCircle
  return Play
}

async function testModel(id: string) {
  const payload = props.probe
  if (!payload || props.providerId === null || testingModel.value !== null) return
  const generation = ++probeGeneration
  testingModel.value = id
  const next = { ...modelResults.value }
  delete next[id]
  modelResults.value = next
  try {
    const res = await apiPost<{ ok: boolean; status?: number; latencyMs?: number; error?: string }>(
      '/api/providers/test',
      {
        id: props.providerId,
        model: id,
        protocol: payload.protocol,
        baseUrl: payload.baseUrl,
        apiKey: payload.apiKey,
        authMode: payload.authMode,
      },
      // A generated token is slower than the default 10s budget.
      { timeoutMs: 45_000 },
    )
    if (generation !== probeGeneration) return
    modelResults.value = {
      ...modelResults.value,
      [id]: res.ok
        ? { ok: true, text: t('settings.items.providerModelTestOk', { model: id, ms: res.latencyMs ?? 0 }) }
        : {
            ok: false,
            text: t('settings.items.providerModelTestFail', {
              model: id,
              reason: res.error || t('settings.items.providerTestFailed'),
            }),
          },
    }
  } catch (err) {
    if (generation !== probeGeneration) return
    modelResults.value = {
      ...modelResults.value,
      [id]: {
        ok: false,
        text: t('settings.items.providerModelTestFail', {
          model: id,
          reason: err instanceof Error ? err.message : String(err),
        }),
      },
    }
  } finally {
    // Only the probe that still owns the view may release it; an older one
    // returning late must not clear a probe that is legitimately in flight.
    if (generation === probeGeneration) testingModel.value = null
  }
}

/** Removing a row must take its verdict with it, or the list would describe a
 *  model that is no longer configured. */
watch(
  () => props.selected,
  (ids) => {
    const stale = Object.keys(modelResults.value).filter((id) => !ids.includes(id))
    if (stale.length === 0) return
    const next = { ...modelResults.value }
    for (const id of stale) delete next[id]
    modelResults.value = next
  },
)
</script>

<style scoped>
.models-picker {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 0;
  padding-top: var(--space-4);
}
.models-search {
  margin: 0 var(--space-6);
}
.models-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 0 var(--space-6);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  min-width: 0;
}
.models-source {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.models-source--warn {
  color: var(--color-orange);
}
.models-count {
  flex-shrink: 0;
  color: var(--text-secondary);
}
.models-toolbar-spacer {
  flex: 1;
}
.models-bulk {
  border: none;
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  cursor: pointer;
  padding: var(--space-1) var(--space-2);
  flex-shrink: 0;
  border-radius: var(--radius-sm);
}
.models-bulk:disabled {
  color: var(--text-hint);
  cursor: default;
}
.models-bulk:not(:disabled):hover {
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}
.models-list {
  display: flex;
  flex-direction: column;
  /* Bounded so the custom-id row stays reachable; the modal body scrolls too,
     but the list scrolling internally keeps the toolbar pinned. */
  flex: 1 1 auto;
  min-height: calc(120 * var(--dp));
  max-height: 50vh;
  overflow-y: auto;
}
.models-item {
  display: flex;
  flex-direction: column;
}
.models-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-2) var(--space-6);
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
  background: transparent;
  color: var(--accent-color);
  cursor: pointer;
  padding: 0;
}
.models-check--on {
  border-color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 14%, transparent);
}
.models-id {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  padding: 0;
}
.models-star,
.models-test {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(26 * var(--dp));
  height: calc(26 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-hint);
  cursor: pointer;
  padding: 0;
  flex-shrink: 0;
}
.models-star--on {
  color: var(--accent-color);
}
.models-star:disabled {
  cursor: default;
  opacity: var(--opacity-disabled);
}
.models-star:hover:not(:disabled) {
  color: var(--accent-color);
}
.models-test:disabled {
  cursor: default;
  opacity: var(--opacity-disabled);
}
.models-test:hover:not(:disabled) {
  color: var(--text-primary);
}
.models-result {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: 0 var(--space-6) var(--space-2) calc(28 * var(--dp));
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  overflow-wrap: anywhere;
}
.models-result--ok {
  color: var(--color-green);
}
.models-result--fail {
  color: var(--color-red);
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
.models-spin {
  animation: models-spin 1s linear infinite;
}
@keyframes models-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .models-spin {
    animation-duration: 3s;
  }
}
</style>

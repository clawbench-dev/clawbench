<template>
  <!-- Create / edit one provider. The list page only shows what is configured;
       every field that can be changed lives here so the list stays a list. -->
  <ModalDialog
    :open="open"
    :title="provider ? t('settings.items.providerEditTitle') : t('settings.items.providerAddTitle')"
    :z-index="2500"
    @close="close"
  >
    <div class="provider-form">
      <!-- The catalogue has 200+ entries, so the picker is a searchable list
           with brand icons rather than a native <select>. A new provider can be
           seeded from one; an existing one keeps whatever it was saved with. -->
      <div v-if="!provider" class="provider-preset-row">
        <button type="button" class="provider-input provider-preset-trigger" @click="presetPickerOpen = true">
          <template v-if="selectedPreset">
            <ProviderIcon tile :provider-id="selectedPreset.id" :name="selectedPreset.name" :size="20" />
            <span class="provider-preset-name">{{ selectedPreset.name }}</span>
            <span class="provider-preset-proto">{{ selectedPreset.protocol }}</span>
          </template>
          <span v-else class="provider-preset-placeholder">{{ t('settings.items.providerPresetPlaceholder') }}</span>
          <ChevronsUpDown :size="14" class="provider-preset-chevron" />
        </button>
      </div>

      <div class="provider-grid">
        <input v-model="draft.name" class="provider-input" :placeholder="t('settings.items.providerName')" />
        <select v-model="draft.protocol" class="provider-input provider-select">
          <option value="anthropic">anthropic</option>
          <option value="openai">openai</option>
          <option value="gemini">gemini</option>
        </select>
      </div>
      <input v-model="draft.baseUrl" class="provider-input" :placeholder="t('settings.items.providerBaseUrl')" />
      <div class="provider-key-field">
        <input
          v-model="draft.apiKey"
          :type="showKey ? 'text' : 'password'"
          autocomplete="off"
          class="provider-input provider-key-input"
          :placeholder="provider ? t('settings.items.providerKeyKeep') : t('settings.items.providerKey')"
        />
        <!-- The stored key is never sent with the list; the eye asks for it
             explicitly, so a plaintext key only travels when someone is
             actually looking at it. -->
        <button
          type="button"
          class="provider-key-toggle"
          :disabled="keyLoading"
          :title="showKey ? t('settings.items.providerKeyHide') : t('settings.items.providerKeyReveal')"
          :aria-label="showKey ? t('settings.items.providerKeyHide') : t('settings.items.providerKeyReveal')"
          @click="toggleKeyVisibility"
        >
          <component :is="showKey ? EyeOff : Eye" :size="14" />
        </button>
      </div>
      <div v-if="keyError" class="provider-error">
        <AlertCircle :size="13" />
        <span>{{ keyError }}</span>
      </div>
      <select v-model="draft.authMode" class="provider-input provider-select provider-select--full">
        <option value="auto">{{ t('settings.items.providerAuthAuto') }}</option>
        <option value="x-api-key">x-api-key</option>
        <option value="bearer">bearer</option>
      </select>

      <!-- Only codex reads this: its managed config block tells codex whether
           the endpoint speaks the Responses API or plain chat/completions. A
           gateway that serves only the latter 404s every turn when this says
           "responses", with nothing in the UI pointing at the cause. -->
      <span class="provider-models-label">{{ t('settings.items.providerWireLabel') }}</span>
      <select
        v-model="draft.wireApi"
        class="provider-input provider-select provider-select--full"
        data-role="wire-api"
      >
        <option value="responses">{{ t('settings.items.providerWireResponses') }}</option>
        <option value="chat">{{ t('settings.items.providerWireChat') }}</option>
      </select>

      <label v-if="provider" class="provider-clear">
        <input v-model="clearKey" type="checkbox" />
        {{ t('settings.items.providerClearKey') }}
      </label>

      <!-- The curated model set. Agents bound to this provider inherit it, so
           this is the only place a model can be "known to work". -->
      <div v-if="provider" class="provider-models">
        <div class="provider-models-head">
          <span class="provider-models-label">{{ t('settings.items.providerModelsLabel') }}</span>
          <button type="button" class="fbtn" @click="modelsPickerOpen = true">
            <Layers :size="13" />
            {{ t('settings.items.providerModelsChoose') }}
          </button>
        </div>
        <div v-if="draft.models.length === 0" class="provider-models-empty">
          {{ t('settings.items.providerModelsNone') }}
        </div>
        <div v-else class="provider-models-chips">
          <span v-for="id in draft.models" :key="id" class="provider-model-chip">
            {{ id }}
            <button type="button" class="provider-model-chip-x" @click="removeModel(id)">×</button>
          </span>
        </div>
        <div v-if="draft.models.length > 0" class="provider-models-default">
          <span>{{ t('settings.items.providerDefaultModel') }}</span>
          <select v-model="draft.defaultModel" class="provider-input provider-select">
            <option v-for="id in draft.models" :key="id" :value="id">{{ id }}</option>
          </select>
        </div>
      </div>

      <div v-if="errorMessage" class="provider-error">
        <AlertCircle :size="13" />
        <span>{{ errorMessage }}</span>
      </div>
    </div>

    <template #footer>
      <button class="fbtn" @click="close">{{ t('settings.items.providerCancel') }}</button>
      <button class="fbtn fbtn-primary" :disabled="!canSave || saving" @click="submit">
        {{ provider ? t('settings.items.providerSave') : t('settings.items.providerAdd') }}
      </button>
    </template>

    <!-- Both pickers are their own ModalDialog at a higher z-index: a
         BottomSheet would paint in the overlay band (1000) and be buried under
         this dialog, which also outranks every drawer on the back gesture. -->
    <ProviderModelsPicker
      :open="modelsPickerOpen"
      :provider-id="provider?.id ?? null"
      :selected="draft.models"
      @update:open="modelsPickerOpen = $event"
      @update:selected="onModelsSelected"
    />

    <ProviderPresetPicker
      :open="presetPickerOpen"
      :presets="presets"
      :selected-id="presetId"
      @update:open="presetPickerOpen = $event"
      @select="onPresetSelect"
    />
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, ChevronsUpDown, Eye, EyeOff, Layers } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import ModalDialog from '@/components/common/ModalDialog.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import ProviderPresetPicker, { type PresetOption } from './ProviderPresetPicker.vue'
import ProviderModelsPicker from './ProviderModelsPicker.vue'

export interface ProviderRow {
  id: number
  name: string
  protocol: string
  baseUrl: string
  authMode: string
  /** OpenAI wire protocol codex should speak: 'responses' | 'chat'. */
  wireApi?: string
  presetId?: string
  models?: string[]
  defaultModel?: string
  hasKey: boolean
  agentCount: number
  /** Names of the agents bound to this provider (list endpoint fills it). */
  agents?: string[]
}

/** Everything the dialog collects; the caller decides POST vs PUT. */
export interface ProviderFormValue {
  name: string
  protocol: string
  baseUrl: string
  apiKey: string
  authMode: string
  wireApi: string
  presetId: string
  clearApiKey: boolean
  models: string[]
  defaultModel: string
}

const props = defineProps<{
  open: boolean
  /** `null` = create; a row = edit that provider. */
  provider: ProviderRow | null
  presets: PresetOption[]
  saving?: boolean
  /** Server-side failure text, owned by the caller so the dialog can stay open
   *  with the user's input intact for a retry. */
  errorMessage?: string
}>()

const emit = defineEmits<{
  close: []
  submit: [value: ProviderFormValue]
}>()

const { t } = useI18n()

const presetPickerOpen = ref(false)
const modelsPickerOpen = ref(false)
const presetId = ref('')
const clearKey = ref(false)
/** Password vs plaintext for the key field, and whether the stored key has
 *  already been fetched for this open. */
const showKey = ref(false)
const keyLoaded = ref(false)
const keyLoading = ref(false)
const keyError = ref('')

const draft = reactive({
  name: '',
  protocol: 'anthropic',
  baseUrl: '',
  apiKey: '',
  authMode: 'auto',
  wireApi: 'responses',
  presetId: '',
  models: [] as string[],
  defaultModel: '',
})

const canSave = computed(() => draft.name.trim() !== '')

/** The chosen catalogue entry, for the trigger row (icon + name). */
const selectedPreset = computed<PresetOption | undefined>(() =>
  props.presets.find((p) => p.id === presetId.value),
)

function resetFromProvider() {
  const p = props.provider
  presetPickerOpen.value = false
  modelsPickerOpen.value = false
  clearKey.value = false
  showKey.value = false
  keyLoaded.value = false
  keyLoading.value = false
  keyError.value = ''
  presetId.value = p?.presetId ?? ''
  draft.name = p?.name ?? ''
  draft.protocol = p?.protocol ?? 'anthropic'
  draft.baseUrl = p?.baseUrl ?? ''
  draft.apiKey = ''
  draft.authMode = p?.authMode || 'auto'
  draft.wireApi = p?.wireApi || 'responses'
  draft.presetId = p?.presetId ?? ''
  draft.models = [...(p?.models ?? [])]
  draft.defaultModel = p?.defaultModel ?? ''
}

/** Reveal / re-hide the key. An edit with a stored key fetches it on first
 *  reveal — the list response never carries it, and we do not want to pull a
 *  plaintext secret just because the dialog was opened. A key the user has
 *  already typed is never overwritten by the fetch. */
async function toggleKeyVisibility() {
  if (showKey.value) {
    showKey.value = false
    return
  }
  const p = props.provider
  if (p && p.hasKey && !keyLoaded.value && draft.apiKey === '') {
    keyLoading.value = true
    keyError.value = ''
    try {
      const data = await apiGet<{ apiKey?: string }>(`/api/providers/${p.id}?reveal=1`)
      // An empty answer while the list says a key exists means the server did
      // not understand `reveal` (older binary). Say so instead of rendering an
      // empty field that looks like the key was lost.
      if (!data.apiKey) throw new Error(t('settings.items.providerKeyRevealFailed'))
      draft.apiKey = data.apiKey
      keyLoaded.value = true
    } catch (err) {
      keyError.value = err instanceof Error ? err.message : String(err)
      appLog.w('ProviderFormDialog', 'reveal provider key failed', keyError.value)
      return
    } finally {
      keyLoading.value = false
    }
  }
  showKey.value = true
}

// Re-seed on every open so a cancelled edit never leaks into the next create.
watch(() => props.open, (open) => {
  if (open) resetFromProvider()
}, { immediate: true })

function onPresetSelect(preset: PresetOption | null) {
  if (!preset) {
    // "No preset" keeps whatever the user has typed; it only drops the link to
    // the catalogue.
    presetId.value = ''
    draft.presetId = ''
    return
  }
  presetId.value = preset.id
  draft.name = preset.name
  draft.protocol = preset.protocol
  draft.baseUrl = preset.baseUrl
  draft.presetId = preset.id
}

function onModelsSelected(ids: string[]) {
  draft.models = ids
  if (!ids.includes(draft.defaultModel)) draft.defaultModel = ids[0] ?? ''
}

function removeModel(id: string) {
  onModelsSelected(draft.models.filter((m) => m !== id))
}

function submit() {
  if (!canSave.value || props.saving) return
  emit('submit', {
    name: draft.name.trim(),
    protocol: draft.protocol,
    baseUrl: draft.baseUrl.trim(),
    apiKey: draft.apiKey,
    authMode: draft.authMode,
    wireApi: draft.wireApi,
    presetId: draft.presetId,
    clearApiKey: clearKey.value,
    models: [...draft.models],
    defaultModel: draft.defaultModel,
  })
}

function close() {
  emit('close')
}
</script>

<style scoped>
/* Geometry and type tokens mirror the provider list rows: both are rendered
 * inside the same settings card, so they must share the 30dp control height
 * and the 16px inset. Using the wrong token names here (--text-sm /
 * --border-primary) silently falls back to inherited values — that is exactly
 * how this page drifted out of alignment before. */
.provider-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-7) var(--space-6);
}
.provider-grid {
  display: flex;
  gap: var(--space-3);
}
/* Same per-axis sizing rule as MCPSetting: a bare `flex` on a column child
 * would override the control height. */
.provider-input {
  box-sizing: border-box;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.provider-grid > .provider-input:not(.provider-select) {
  flex: 1;
  min-width: calc(120 * var(--dp));
}
.provider-form > .provider-input {
  align-self: stretch;
  flex: none;
}
/* The key field owns the reveal button, so it needs the stretch behaviour the
 * bare input used to get from being a direct child. */
.provider-key-field {
  position: relative;
  display: flex;
  align-self: stretch;
  height: calc(30 * var(--dp));
}
.provider-key-field > .provider-input {
  flex: 1;
  min-width: 0;
  /* Leave room for the button so a long key never runs underneath it. */
  padding-right: calc(34 * var(--dp));
}
.provider-key-toggle {
  position: absolute;
  top: 50%;
  right: calc(4 * var(--dp));
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.provider-key-toggle:hover:not(:disabled) {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
}
.provider-key-toggle:disabled {
  cursor: default;
  opacity: 0.5;
}
.provider-select {
  flex: 0 0 calc(120 * var(--dp));
  padding: 0 var(--space-3);
}
/* A select that is a direct child must stretch like the inputs above it. */
.provider-select--full {
  align-self: stretch;
  flex: none;
}
/* Trigger row for the preset picker: looks like the inputs around it, but
 * opens the in-app picker instead of a native dropdown. */
.provider-preset-trigger {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: 0 var(--space-4);
  text-align: left;
  cursor: pointer;
}
.provider-preset-name {
  font-size: var(--font-size-md);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-preset-proto {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
  flex-shrink: 0;
}
.provider-preset-placeholder {
  flex: 1;
  font-size: var(--font-size-md);
  color: var(--text-muted);
}
.provider-preset-chevron {
  margin-left: auto;
  color: var(--text-muted);
  flex-shrink: 0;
}
.provider-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
.provider-models {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}
.provider-models-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.provider-models-label {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}
.provider-models-empty {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.provider-models-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.provider-model-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-2) 0 var(--space-3);
  height: calc(22 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}
.provider-model-chip-x {
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: var(--font-size-md);
  line-height: 1;
  padding: 0;
}
.provider-models-default {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.provider-models-default .provider-select {
  flex: 0 0 calc(160 * var(--dp));
}
.provider-clear {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
}
.provider-error {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  overflow-wrap: anywhere;
  color: var(--color-red);
}
</style>

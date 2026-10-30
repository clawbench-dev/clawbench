<template>
  <!-- Global model-provider list. A provider is one endpoint + credential;
       agents reference it from their own settings page, so a key rotation is
       one edit here instead of one edit per agent. -->
  <div class="provider-setting">
    <div class="provider-hint">{{ description || t('settings.items.providerDesc') }}</div>

    <div v-if="providers.length === 0" class="provider-empty">
      {{ t('settings.items.providerEmpty') }}
    </div>

    <div v-for="p in providers" :key="p.id" class="provider-row" :data-provider-id="p.id">
      <div class="provider-row-head">
        <span class="provider-name">{{ p.name }}</span>
        <span class="provider-proto">{{ p.protocol }}</span>
        <span v-if="p.hasKey" class="provider-key-badge">{{ t('settings.items.providerKeySet') }}</span>
        <span v-else class="provider-key-missing">{{ t('settings.items.providerNoKey') }}</span>
        <span v-if="p.agentCount > 0" class="provider-usage">
          {{ t('settings.items.providerUsedBy', { count: p.agentCount }) }}
        </span>
        <button
          class="fbtn"
          :disabled="testingId !== null"
          :title="t('settings.items.providerTestHint')"
          @click="testProvider(p)"
        >
          <component :is="testingId === p.id ? Loader2 : PlugZap" :size="13" :class="{ 'provider-spin': testingId === p.id }" />
          {{ testingId === p.id ? t('settings.items.providerTesting') : t('settings.items.providerTest') }}
        </button>
        <button class="fbtn" @click="startEdit(p)">
          <Pencil :size="13" />
          {{ t('settings.items.providerEdit') }}
        </button>
        <button class="fbtn" @click="removeProvider(p)">
          <Trash2 :size="13" />
          {{ t('settings.items.providerRemove') }}
        </button>
      </div>
      <div class="provider-summary">{{ p.baseUrl || t('settings.items.providerNoBaseUrl') }}</div>
      <div v-if="testResults[p.id]" class="provider-test-result" :class="testResults[p.id].ok ? 'provider-test-result--ok' : 'provider-test-result--fail'">
        <component :is="testResults[p.id].ok ? CheckCircle2 : AlertCircle" :size="13" />
        <span>{{ testResults[p.id].message }}</span>
      </div>
    </div>

    <div class="provider-form">
      <div v-if="!editingId" class="provider-preset-row">
        <select v-model="presetId" class="provider-input provider-select" @change="applyPreset">
          <option value="">{{ t('settings.items.providerPresetPlaceholder') }}</option>
          <option v-for="preset in presets" :key="preset.id" :value="preset.id">
            {{ preset.name }} · {{ preset.protocol }}
          </option>
        </select>
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
      <input
        v-model="draft.apiKey"
        type="password"
        autocomplete="off"
        class="provider-input"
        :placeholder="editingId ? t('settings.items.providerKeyKeep') : t('settings.items.providerKey')"
      />
      <div class="provider-grid">
        <select v-model="draft.authMode" class="provider-input provider-select">
          <option value="auto">{{ t('settings.items.providerAuthAuto') }}</option>
          <option value="x-api-key">x-api-key</option>
          <option value="bearer">bearer</option>
        </select>
        <button class="fbtn fbtn-primary" :disabled="!canSave || saving" @click="saveDraft">
          {{ editingId ? t('settings.items.providerSave') : t('settings.items.providerAdd') }}
        </button>
        <button v-if="editingId" class="fbtn" @click="cancelEdit">
          {{ t('settings.items.providerCancel') }}
        </button>
      </div>
      <label v-if="editingId" class="provider-clear">
        <input v-model="clearKey" type="checkbox" />
        {{ t('settings.items.providerClearKey') }}
      </label>
    </div>

    <div v-if="error" class="provider-error">
      <AlertCircle :size="13" />
      <span>{{ error }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, CheckCircle2, Loader2, Pencil, PlugZap, Trash2 } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { apiDelete, apiGet, apiPost, apiPut } from '@/utils/api'
import { useDialog } from '@/composables/useDialog'
import { appLog } from '@/utils/appLog'

defineProps<{ description?: string }>()

interface ProviderRow {
  id: number
  name: string
  protocol: string
  baseUrl: string
  authMode: string
  hasKey: boolean
  agentCount: number
}

interface Preset {
  id: string
  name: string
  protocol: string
  baseUrl: string
}

const { t } = useI18n()
const { confirm } = useDialog()

const providers = ref<ProviderRow[]>([])
const presets = ref<Preset[]>([])
const presetId = ref('')
const saving = ref(false)
const error = ref('')
const editingId = ref<number | null>(null)
const clearKey = ref(false)
const testingId = ref<number | null>(null)
const testResults = ref<Record<number, { ok: boolean; message: string }>>({})

const draft = reactive({
  name: '',
  protocol: 'anthropic',
  baseUrl: '',
  apiKey: '',
  authMode: 'auto',
})

const canSave = computed(() => draft.name.trim() !== '')

function resetDraft() {
  draft.name = ''
  draft.protocol = 'anthropic'
  draft.baseUrl = ''
  draft.apiKey = ''
  draft.authMode = 'auto'
  presetId.value = ''
  clearKey.value = false
  editingId.value = null
}

async function loadProviders() {
  try {
    const data = await apiGet<{ providers: ProviderRow[] }>('/api/providers')
    providers.value = data.providers ?? []
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    appLog.w('ProviderSetting', 'load providers failed', error.value)
  }
}

async function loadPresets() {
  try {
    const data = await apiGet<{ presets: Preset[] }>('/api/providers/presets')
    presets.value = data.presets ?? []
  } catch (err) {
    // Presets are a convenience; the manual form still works.
    appLog.w('ProviderSetting', 'load presets failed', err)
  }
}

/** Fill the form from a catalogue entry. Only fields the preset knows are
 *  overwritten; a typed key is never touched. */
function applyPreset() {
  const preset = presets.value.find((p) => p.id === presetId.value)
  if (!preset) return
  draft.name = preset.name
  draft.protocol = preset.protocol
  draft.baseUrl = preset.baseUrl
}

async function saveDraft() {
  if (!canSave.value || saving.value) return
  saving.value = true
  error.value = ''
  try {
    if (editingId.value === null) {
      await apiPost('/api/providers', {
        name: draft.name.trim(),
        protocol: draft.protocol,
        baseUrl: draft.baseUrl.trim(),
        apiKey: draft.apiKey,
        authMode: draft.authMode,
      })
    } else {
      await apiPut(`/api/providers/${editingId.value}`, {
        name: draft.name.trim(),
        protocol: draft.protocol,
        baseUrl: draft.baseUrl.trim(),
        authMode: draft.authMode,
        apiKey: draft.apiKey,
        clearApiKey: clearKey.value,
      })
    }
    resetDraft()
    await loadProviders()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
    appLog.w('ProviderSetting', 'save provider failed', error.value)
  } finally {
    saving.value = false
  }
}

function startEdit(p: ProviderRow) {
  editingId.value = p.id
  presetId.value = ''
  clearKey.value = false
  draft.name = p.name
  draft.protocol = p.protocol
  draft.baseUrl = p.baseUrl
  draft.authMode = p.authMode || 'auto'
  draft.apiKey = ''
}

function cancelEdit() {
  resetDraft()
}

async function removeProvider(p: ProviderRow) {
  const ok = await confirm(t('settings.items.providerDeleteConfirm', { name: p.name }), { dangerous: true })
  if (!ok) return
  error.value = ''
  try {
    await apiDelete(`/api/providers/${p.id}`)
    if (editingId.value === p.id) resetDraft()
    delete testResults.value[p.id]
    await loadProviders()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

async function testProvider(p: ProviderRow) {
  if (testingId.value !== null) return
  testingId.value = p.id
  delete testResults.value[p.id]
  try {
    const result = await apiPost<{ ok: boolean; status?: number; models?: string[]; error?: string }>(
      '/api/providers/test',
      { id: p.id },
    )
    const message = result.ok
      ? t('settings.items.providerTestOk', { count: result.models?.length ?? 0 })
      : (result.error || t('settings.items.providerTestFailed'))
    testResults.value = { ...testResults.value, [p.id]: { ok: result.ok, message } }
  } catch (err) {
    testResults.value = {
      ...testResults.value,
      [p.id]: { ok: false, message: err instanceof Error ? err.message : String(err) },
    }
  } finally {
    testingId.value = null
  }
}

onMounted(() => {
  void loadProviders()
  void loadPresets()
})
</script>

<style scoped>
.provider-setting {
  display: flex;
  flex-direction: column;
  gap: calc(10 * var(--dp));
  padding: calc(4 * var(--dp)) 0;
}
.provider-hint {
  font-size: var(--text-sm);
  color: var(--text-secondary);
  line-height: 1.5;
}
.provider-empty {
  font-size: var(--text-sm);
  color: var(--text-tertiary);
  padding: calc(6 * var(--dp)) 0;
}
.provider-row {
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-md);
  padding: calc(8 * var(--dp)) calc(10 * var(--dp));
  display: flex;
  flex-direction: column;
  gap: calc(4 * var(--dp));
}
.provider-row-head {
  display: flex;
  align-items: center;
  gap: calc(8 * var(--dp));
  flex-wrap: wrap;
}
.provider-name {
  font-weight: 600;
  font-size: var(--text-sm);
}
.provider-proto,
.provider-key-badge,
.provider-key-missing,
.provider-usage {
  font-size: var(--text-xs);
  padding: 1px calc(6 * var(--dp));
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}
.provider-key-missing {
  color: var(--warning, var(--text-secondary));
}
.provider-summary {
  font-size: var(--text-xs);
  color: var(--text-tertiary);
  word-break: break-all;
}
.provider-form {
  display: flex;
  flex-direction: column;
  gap: calc(8 * var(--dp));
  border-top: 1px solid var(--border-primary);
  padding-top: calc(10 * var(--dp));
}
.provider-grid {
  display: flex;
  gap: calc(8 * var(--dp));
  align-items: center;
}
.provider-input {
  flex: 1;
  min-width: 0;
  height: calc(30 * var(--dp));
  padding: 0 calc(8 * var(--dp));
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--text-sm);
}
.provider-select {
  flex: 0 0 auto;
  min-width: calc(120 * var(--dp));
}
.provider-clear {
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  font-size: var(--text-xs);
  color: var(--text-secondary);
}
.provider-error,
.provider-test-result {
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  font-size: var(--text-xs);
}
.provider-error,
.provider-test-result--fail {
  color: var(--error, #e5484d);
}
.provider-test-result--ok {
  color: var(--success, #30a46c);
}
.provider-spin {
  animation: provider-spin 1s linear infinite;
}
@keyframes provider-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>

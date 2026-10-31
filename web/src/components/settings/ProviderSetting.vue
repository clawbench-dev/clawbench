<template>
  <!-- Global model-provider list. A provider is one endpoint + credential;
       agents reference it from their own settings page, so a key rotation is
       one edit here instead of one edit per agent. Creating and editing happen
       in ProviderFormDialog — this page only ever shows what is configured. -->
  <div class="provider-setting">
    <div class="provider-hint">{{ description || t('settings.items.providerDesc') }}</div>

    <!-- Authoritative catalogue status. This snapshot is what supplies model
         lists when an endpoint has no /models and before an ACP agent reports
         its own list, so its age is worth showing. -->
    <div class="provider-catalog">
      <span class="provider-catalog-text">
        {{ catalogFetchedAt
          ? t('settings.items.providerCatalogUpdated', { date: catalogDate, count: catalogCount })
          : t('settings.items.providerCatalogMissing') }}
        <span v-if="catalogStale" class="provider-catalog-stale">{{ t('settings.items.providerCatalogStale') }}</span>
      </span>
      <button class="fbtn" :disabled="refreshingCatalog" @click="refreshCatalog">
        <component :is="refreshingCatalog ? Loader2 : RefreshCw" :size="13" :class="{ 'provider-spin': refreshingCatalog }" />
        {{ refreshingCatalog ? t('settings.items.providerCatalogRefreshing') : t('settings.items.providerCatalogRefresh') }}
      </button>
    </div>

    <div class="provider-toolbar">
      <button class="fbtn fbtn-primary" @click="openCreate">
        <Plus :size="13" />
        {{ t('settings.items.providerAdd') }}
      </button>
    </div>

    <div v-if="providers.length === 0" class="provider-empty">
      {{ t('settings.items.providerEmpty') }}
    </div>

    <div v-for="p in providers" :key="p.id" class="provider-row" :data-provider-id="p.id">
      <div class="provider-row-main">
        <ProviderIcon tile :provider-id="iconIdFor(p)" :name="p.name" :size="34" />
        <div class="provider-row-body">
          <div class="provider-row-head">
            <span class="provider-name">{{ p.name }}</span>
            <span class="provider-proto">{{ p.protocol }}</span>
            <span class="provider-proto">{{ authLabel(p.authMode) }}</span>
            <span v-if="p.hasKey" class="provider-key-badge">{{ t('settings.items.providerKeySet') }}</span>
            <span v-else class="provider-key-missing">{{ t('settings.items.providerNoKey') }}</span>
            <span v-if="p.agentCount > 0" class="provider-usage">
              {{ t('settings.items.providerUsedBy', { count: p.agentCount }) }}
            </span>
          </div>
          <div class="provider-summary">{{ p.baseUrl || t('settings.items.providerNoBaseUrl') }}</div>
          <div class="provider-detail">{{ detailLine(p) }}</div>
          <div v-if="testResults[p.id]" class="provider-test-result" :class="testResults[p.id].ok ? 'provider-test-result--ok' : 'provider-test-result--fail'">
            <component :is="testResults[p.id].ok ? CheckCircle2 : AlertCircle" :size="13" />
            <span>{{ testResults[p.id].message }}</span>
          </div>
        </div>
      </div>
      <div class="provider-actions">
        <button
          class="fbtn"
          :disabled="testingId !== null"
          :title="t('settings.items.providerTestHint')"
          @click="testProvider(p)"
        >
          <component :is="testingId === p.id ? Loader2 : PlugZap" :size="13" :class="{ 'provider-spin': testingId === p.id }" />
          {{ testingId === p.id ? t('settings.items.providerTesting') : t('settings.items.providerTest') }}
        </button>
        <button class="fbtn" @click="openEdit(p)">
          <Pencil :size="13" />
          {{ t('settings.items.providerEdit') }}
        </button>
        <button class="fbtn" @click="removeProvider(p)">
          <Trash2 :size="13" />
          {{ t('settings.items.providerRemove') }}
        </button>
      </div>
    </div>

    <ProviderFormDialog
      :open="formOpen"
      :provider="editingProvider"
      :presets="presets"
      :saving="saving"
      :error-message="formError"
      @close="closeForm"
      @submit="saveDraft"
    />

    <div v-if="error" class="provider-error">
      <AlertCircle :size="13" />
      <span>{{ error }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, CheckCircle2, Loader2, Pencil, Plus, PlugZap, RefreshCw, Trash2 } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { apiDelete, apiGet, apiPost, apiPut } from '@/utils/api'
import { useDialog } from '@/composables/useDialog'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import ProviderFormDialog, { type ProviderFormValue, type ProviderRow } from './ProviderFormDialog.vue'
import type { PresetOption } from './ProviderPresetPicker.vue'
import { appLog } from '@/utils/appLog'

defineProps<{ description?: string }>()

const { t } = useI18n()
const { confirm } = useDialog()

const providers = ref<ProviderRow[]>([])
const presets = ref<PresetOption[]>([])
const saving = ref(false)
/** List-level failures (load / delete / catalogue). Form failures live in
 *  `formError` so the dialog can show them without closing. */
const error = ref('')
const formError = ref('')
const formOpen = ref(false)
const editingProvider = ref<ProviderRow | null>(null)
const testingId = ref<number | null>(null)
const testResults = ref<Record<number, { ok: boolean; message: string }>>({})

// Authoritative catalogue state (models.dev snapshot).
const catalogFetchedAt = ref('')
const catalogCount = ref(0)
const catalogStale = ref(true)
const refreshingCatalog = ref(false)
const catalogDate = computed(() => {
  if (!catalogFetchedAt.value) return ''
  const d = new Date(catalogFetchedAt.value)
  return Number.isNaN(d.getTime()) ? catalogFetchedAt.value : d.toLocaleDateString()
})

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
    const data = await apiGet<{ presets: PresetOption[] }>('/api/providers/presets')
    presets.value = data.presets ?? []
  } catch (err) {
    // Presets are a convenience; the manual form still works.
    appLog.w('ProviderSetting', 'load presets failed', err)
  }
}

/** A provider created by hand carries no `presetId`; its display name still
 *  names the brand, and `resolveProviderIconId` folds "Moonshot AI" onto the
 *  same family slug the catalogue uses. */
function iconIdFor(p: ProviderRow): string {
  return p.presetId || p.name
}

function authLabel(mode: string): string {
  return !mode || mode === 'auto' ? t('settings.items.providerAuthAutoShort') : mode
}

/** One muted line under the endpoint: what this provider offers and who uses
 *  it. Kept as a single joined string so it wraps as prose on a phone instead
 *  of stacking three separate rows per provider. */
function detailLine(p: ProviderRow): string {
  const models = p.models ?? []
  const parts = [
    models.length > 0
      ? t('settings.items.providerModelCount', { count: models.length })
      : t('settings.items.providerNoModels'),
  ]
  if (p.defaultModel) parts.push(t('settings.items.providerDefaultModelValue', { model: p.defaultModel }))
  if (p.agents?.length) parts.push(t('settings.items.providerAgentsValue', { agents: p.agents.join(', ') }))
  return parts.join(' · ')
}

function openCreate() {
  editingProvider.value = null
  formError.value = ''
  formOpen.value = true
}

function openEdit(p: ProviderRow) {
  editingProvider.value = p
  formError.value = ''
  formOpen.value = true
}

function closeForm() {
  formOpen.value = false
  formError.value = ''
}

async function saveDraft(value: ProviderFormValue) {
  if (saving.value) return
  saving.value = true
  formError.value = ''
  try {
    if (editingProvider.value === null) {
      await apiPost('/api/providers', {
        name: value.name,
        protocol: value.protocol,
        baseUrl: value.baseUrl,
        apiKey: value.apiKey,
        authMode: value.authMode,
        presetId: value.presetId,
      })
    } else {
      await apiPut(`/api/providers/${editingProvider.value.id}`, {
        name: value.name,
        protocol: value.protocol,
        baseUrl: value.baseUrl,
        authMode: value.authMode,
        apiKey: value.apiKey,
        clearApiKey: value.clearApiKey,
        models: value.models,
        defaultModel: value.defaultModel,
      })
    }
    closeForm()
    await loadProviders()
  } catch (err) {
    // Keep the dialog open with the typed values so the user can correct them.
    formError.value = err instanceof Error ? err.message : String(err)
    appLog.w('ProviderSetting', 'save provider failed', formError.value)
  } finally {
    saving.value = false
  }
}

async function removeProvider(p: ProviderRow) {
  const ok = await confirm(t('settings.items.providerDeleteConfirm', { name: p.name }), { dangerous: true })
  if (!ok) return
  error.value = ''
  try {
    await apiDelete(`/api/providers/${p.id}`)
    if (editingProvider.value?.id === p.id) closeForm()
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

async function loadCatalog() {
  try {
    const data = await apiGet<{ fetchedAt?: string; providers?: unknown[]; stale?: boolean }>('/api/catalog')
    catalogFetchedAt.value = data.fetchedAt ?? ''
    catalogCount.value = data.providers?.length ?? 0
    catalogStale.value = data.stale ?? true
  } catch (err) {
    appLog.w('ProviderSetting', 'load catalog status failed', err)
  }
}

async function refreshCatalog() {
  if (refreshingCatalog.value) return
  refreshingCatalog.value = true
  error.value = ''
  try {
    await apiPost('/api/catalog', {})
    await loadCatalog()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    refreshingCatalog.value = false
  }
}

onMounted(() => {
  void loadProviders()
  void loadPresets()
  void loadCatalog()
})
</script>

<style scoped>
/* Geometry and type tokens mirror MCPSetting: both are "list" cards rendered
 * inside the same settings card, so they must share the 16px inset, the row
 * chrome and the 30dp control height. Using the wrong token names here
 * (--text-sm / --border-primary) silently falls back to inherited values —
 * that is exactly how this page drifted out of alignment before. */
.provider-setting {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.provider-hint {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.provider-catalog {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
}
.provider-catalog-text {
  min-width: 0;
  overflow-wrap: anywhere;
}
.provider-catalog-stale {
  margin-left: var(--space-2);
  color: var(--color-orange);
}
/* The create action belongs to the list, not to a row: it sits on its own line
 * so the (long) catalogue sentence keeps the width it needs on a phone. */
.provider-toolbar {
  display: flex;
  justify-content: flex-end;
}
.provider-empty {
  color: var(--text-muted);
  font-size: var(--font-size-md);
}
/* The card is two stacked rows: what this provider is, then what you can do
 * with it. Mixing the buttons into the info row made a narrow pane look like
 * one crowded paragraph, so the actions always get their own line. */
.provider-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.provider-row-main {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  min-width: 0;
}
.provider-row-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  flex: 1;
  min-width: 0;
}
.provider-row-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.provider-name {
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
}
.provider-proto,
.provider-key-badge,
.provider-key-missing,
.provider-usage {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
  white-space: nowrap;
}
.provider-key-missing {
  color: var(--color-orange);
  border-color: color-mix(in srgb, var(--color-orange) 40%, transparent);
}
/* Its own row, separated from the info block by a hairline so the buttons read
 * as the card's footer rather than as part of the provider's description. */
.provider-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}
.provider-summary {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  word-break: break-all;
}
.provider-detail {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  overflow-wrap: anywhere;
}
.provider-error,
.provider-test-result {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  overflow-wrap: anywhere;
}
.provider-error,
.provider-test-result--fail {
  color: var(--color-red);
}
.provider-test-result--ok {
  color: var(--color-green);
}
.provider-spin {
  animation: provider-spin 1s linear infinite;
}
@keyframes provider-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .provider-spin {
    animation-duration: 3s;
  }
}
</style>

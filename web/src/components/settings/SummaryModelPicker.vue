<template>
  <!-- Shared summary-model picker. The model is always one a configured
       provider actually serves, so the row reads "provider · model" and the
       sheet lists provider → models instead of asking for an endpoint again. -->
  <BottomSheet :open="open" auto @close="close">
    <template #header>
      <Sparkles :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('settings.items.summaryModelTitle') }}</span>
    </template>

    <div class="summary-picker">
      <div class="summary-search">
        <SearchInput v-model="query" :placeholder="t('settings.items.summaryModelSearch')" />
      </div>

      <div v-if="loading" class="summary-empty">{{ t('settings.items.providerModelsLoading') }}</div>
      <div v-else-if="baseGroups.length === 0" class="summary-empty">
        {{ providers.length === 0
          ? t('settings.items.summaryModelNoProviders')
          : t('settings.items.summaryModelNoCompatibleProviders') }}
      </div>
      <div v-else-if="groups.length === 0" class="summary-empty">
        {{ t('settings.items.providerModelsNoMatch') }}
      </div>

      <div v-else class="summary-list">
        <div
          v-for="group in groups"
          :key="group.provider.id"
          class="summary-group"
          :class="{ 'summary-group--disabled': group.noEndpoint }"
        >
          <div class="summary-group-head">
            <ProviderIcon :provider-id="group.provider.presetId || String(group.provider.id)" :name="group.provider.name" :size="18" />
            <span class="summary-group-name">{{ group.provider.name }}</span>
          </div>
          <!-- A provider without a base_url only injects credentials into an
               agent's environment: it has nowhere to send a completion
               request, so the server rejects binding it (and the summary
               features would stay silently off). -->
          <div v-if="group.noEndpoint" class="summary-group-hint">
            {{ t('settings.items.summaryModelProviderNoEndpoint') }}
          </div>
          <template v-else>
            <div v-if="group.resolving" class="summary-group-hint">
              {{ t('settings.items.providerModelsLoading') }}
            </div>
            <button
              v-for="model in group.models"
              :key="model"
              type="button"
              class="summary-row"
              :class="{ 'summary-row--active': isCurrent(group.provider.id, model) }"
              @click="choose(group.provider.id, model)"
            >
              <span class="summary-row-model">{{ model }}</span>
              <Check v-if="isCurrent(group.provider.id, model)" :size="16" class="summary-row-check" />
            </button>

            <!-- A provider without a curated list accepts any model id, so typing
                 one is the only way to pick a model its endpoint does not
                 publish (many Anthropic-compatible gateways have no /models). -->
            <template v-if="!group.curated">
              <div v-if="!group.resolving" class="summary-group-hint">
                {{ group.error
                  ? t('settings.items.summaryModelProbeFailed', { reason: group.error })
                  : t('settings.items.summaryModelManualHint') }}
              </div>
              <div class="summary-manual">
                <input
                  v-model="manualDraft[group.provider.id]"
                  class="summary-manual-input"
                  type="text"
                  autocomplete="off"
                  :placeholder="t('settings.items.providerModelsCustom')"
                  @keydown.enter.prevent="useManual(group.provider.id)"
                />
                <button
                  type="button"
                  class="summary-manual-use"
                  :disabled="!manualText(group.provider.id)"
                  @click="useManual(group.provider.id)"
                >
                  {{ t('settings.items.summaryModelUse') }}
                </button>
              </div>
            </template>
          </template>
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Sparkles } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import { apiGet, apiPatch } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()

interface ProviderRow {
  id: number
  name: string
  protocol: string
  presetId?: string
  /** Endpoint the provider calls. Empty = key-only provider (no endpoint), which
   *  can never back the summary — see docs/spec/features/model-providers.md. */
  baseUrl?: string
  models?: string[]
}

const { t } = useI18n()
const { serverConfig, loadConfig } = useSettingsConfig()
const toast = useToast()

const query = ref('')
const loading = ref(false)
const providers = ref<ProviderRow[]>([])
/** Models resolved for a provider that curates none, via the same
 *  live-probe → catalog resolution the provider editor uses. */
const resolvedModels = ref<Record<number, string[]>>({})
const resolvingIds = ref<number[]>([])
const probeErrors = ref<Record<number, string>>({})
const manualDraft = reactive<Record<number, string>>({})

const currentProviderId = computed<number | null>(() => {
  const raw = (serverConfig.value.ai_summary as { provider_id?: number } | undefined)?.provider_id
  return typeof raw === 'number' && raw > 0 ? raw : null
})
const currentModel = computed<string>(() => {
  const raw = (serverConfig.value.ai_summary as { model?: string } | undefined)?.model
  return typeof raw === 'string' ? raw : ''
})

watch(
  () => props.open,
  (open) => {
    if (!open) return
    query.value = ''
    void load()
  },
  { immediate: true },
)

async function load() {
  loading.value = true
  resolvedModels.value = {}
  probeErrors.value = {}
  resolvingIds.value = []
  for (const key of Object.keys(manualDraft)) delete manualDraft[Number(key)]
  try {
    const data = await apiGet<{ providers: ProviderRow[] }>('/api/providers')
    providers.value = data.providers ?? []
  } catch (err) {
    appLog.w('SummaryModelPicker', 'load providers failed', err)
    providers.value = []
  } finally {
    loading.value = false
  }
  // The sheet is already usable at this point: curated lists render
  // immediately and each uncatalogued provider fills in when its probe lands.
  void resolveUncatalogued()
}

function isEligible(p: ProviderRow): boolean {
  return p.protocol === 'anthropic' || p.protocol === 'openai'
}

/** A provider with no base_url has no endpoint to summarize against. It stays
 *  visible (so the user can see why it is not offered) but is not selectable. */
function hasEndpoint(p: ProviderRow): boolean {
  return (p.baseUrl ?? '').trim() !== ''
}

async function resolveUncatalogued() {
  const pending = providers.value.filter(
    (p) => isEligible(p) && hasEndpoint(p) && (p.models?.length ?? 0) === 0,
  )
  await Promise.all(pending.map(resolveOne))
}

async function resolveOne(p: ProviderRow) {
  resolvingIds.value = [...resolvingIds.value, p.id]
  try {
    const data = await apiGet<{ models?: string[]; error?: string }>(
      `/api/providers/${p.id}/models`,
    )
    resolvedModels.value = { ...resolvedModels.value, [p.id]: data.models ?? [] }
    if (data.error) probeErrors.value = { ...probeErrors.value, [p.id]: data.error }
  } catch (err) {
    appLog.w('SummaryModelPicker', 'resolve provider models failed', err)
    probeErrors.value = {
      ...probeErrors.value,
      [p.id]: err instanceof Error ? err.message : String(err),
    }
  } finally {
    resolvingIds.value = resolvingIds.value.filter((id) => id !== p.id)
  }
}

/** Every provider that can back the summary, with the models it offers. A
 *  provider that curates a list is authoritative (the server rejects models
 *  outside it); otherwise the resolved list is a suggestion and typing a model
 *  id stays possible. */
const baseGroups = computed(() => {
  return providers.value.filter(isEligible).map((p) => {
    const curated = p.models ?? []
    let models = curated.length > 0 ? curated : (resolvedModels.value[p.id] ?? [])
    // The saved choice stays visible even when the endpoint no longer lists it,
    // so the current selection is never invisible in the sheet.
    if (currentProviderId.value === p.id && currentModel.value && !models.includes(currentModel.value)) {
      models = [...models, currentModel.value]
    }
    return {
      provider: p,
      curated: curated.length > 0,
      noEndpoint: !hasEndpoint(p),
      resolving: resolvingIds.value.includes(p.id),
      error: probeErrors.value[p.id] ?? '',
      models,
    }
  })
})

const groups = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return baseGroups.value
  return baseGroups.value
    .map((g) => {
      if (g.provider.name.toLowerCase().includes(q)) return g
      return { ...g, models: g.models.filter((m) => m.toLowerCase().includes(q)) }
    })
    // A group with nothing left to show is noise — except an uncatalogued one,
    // whose manual input is still a valid answer to any query.
    .filter((g) => g.models.length > 0 || (!g.curated && g.provider.name.toLowerCase().includes(q)))
})

function isCurrent(providerId: number, model: string): boolean {
  return currentProviderId.value === providerId && currentModel.value === model
}

function manualText(providerId: number): string {
  return (manualDraft[providerId] ?? '').trim()
}

function useManual(providerId: number) {
  const model = manualText(providerId)
  if (!model) return
  void choose(providerId, model)
}

async function choose(providerId: number, model: string) {
  // Defensive: the group is rendered disabled, but a stale click must not
  // produce a request the server will reject anyway.
  const provider = providers.value.find((p) => p.id === providerId)
  if (!provider || !hasEndpoint(provider)) return
  try {
    // One PATCH for the pair: validating them separately would reject the
    // provider change while the old model is still attached.
    await apiPatch('/api/config', { ai_summary: { provider_id: providerId, model } })
    await loadConfig()
    close()
  } catch (err) {
    appLog.w('SummaryModelPicker', 'save summary model failed', err)
    // Stay open on rejection: the server refuses a model outside a curated
    // list, and closing would leave the user with no way to correct it.
    toast.show(err instanceof Error ? err.message : String(err), { type: 'error', icon: 'error' })
  }
}

function close() {
  emit('update:open', false)
}
</script>

<style scoped>
.summary-picker {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 0;
}
/* Gutter only: the field is the shared SearchInput pill. */
.summary-search {
  margin: 0 var(--space-6);
}
.summary-list {
  display: flex;
  flex-direction: column;
  max-height: 55vh;
  overflow-y: auto;
}
.summary-group + .summary-group {
  border-top: 1px solid var(--border-color);
  margin-top: var(--space-2);
  padding-top: var(--space-2);
}
/* Shown but not selectable: the group explains why it cannot back the summary. */
.summary-group--disabled .summary-group-head {
  opacity: 0.6;
}
.summary-group-head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-6) var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.summary-group-name {
  font-weight: var(--font-weight-medium);
}
.summary-group-hint {
  padding: 0 var(--space-6) var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.summary-manual {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin: 0 var(--space-6) var(--space-3);
}
.summary-manual-input {
  flex: 1;
  min-width: 0;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  outline: none;
}
.summary-manual-use {
  flex-shrink: 0;
  height: calc(30 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}
.summary-manual-use:disabled {
  opacity: 0.5;
  cursor: default;
}
.summary-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-3) var(--space-7);
  border: none;
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
}
.summary-row:hover {
  background: var(--bg-secondary);
}
.summary-row--active {
  background: color-mix(in srgb, var(--accent-color) 8%, transparent);
}
.summary-row-model {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.summary-row-check {
  color: var(--accent-color);
  flex-shrink: 0;
}
.summary-empty {
  padding: var(--space-6);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
</style>

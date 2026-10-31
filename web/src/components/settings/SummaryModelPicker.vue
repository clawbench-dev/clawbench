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
        <Search :size="14" class="summary-search-icon" />
        <input
          v-model="query"
          class="summary-search-input"
          type="text"
          autocomplete="off"
          :placeholder="t('settings.items.summaryModelSearch')"
        />
      </div>

      <div v-if="loading" class="summary-empty">{{ t('settings.items.providerModelsLoading') }}</div>
      <div v-else-if="groups.length === 0" class="summary-empty">
        {{ t('settings.items.summaryModelNoProviders') }}
      </div>

      <div v-else class="summary-list">
        <div v-for="group in groups" :key="group.provider.id" class="summary-group">
          <div class="summary-group-head">
            <ProviderIcon :provider-id="group.provider.presetId || String(group.provider.id)" :name="group.provider.name" :size="18" />
            <span class="summary-group-name">{{ group.provider.name }}</span>
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
        </div>
      </div>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Search, Sparkles } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import { apiGet, apiPatch } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useSettingsConfig } from '@/composables/useSettingsConfig'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [value: boolean] }>()

interface ProviderRow {
  id: number
  name: string
  protocol: string
  presetId?: string
  models?: string[]
}

const { t } = useI18n()
const { serverConfig, loadConfig } = useSettingsConfig()

const query = ref('')
const loading = ref(false)
const providers = ref<ProviderRow[]>([])

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
  try {
    const data = await apiGet<{ providers: ProviderRow[] }>('/api/providers')
    providers.value = data.providers ?? []
  } catch (err) {
    appLog.w('SummaryModelPicker', 'load providers failed', err)
    providers.value = []
  } finally {
    loading.value = false
  }
}

/** Only providers that curate models can back the summary: without a list
 *  there is nothing to choose, and an arbitrary id would be a guess. */
const groups = computed(() => {
  const q = query.value.trim().toLowerCase()
  return providers.value
    .filter((p) => (p.models?.length ?? 0) > 0 && (p.protocol === 'anthropic' || p.protocol === 'openai'))
    .map((p) => ({
      provider: p,
      models: (p.models ?? []).filter(
        (m) => !q || m.toLowerCase().includes(q) || p.name.toLowerCase().includes(q),
      ),
    }))
    .filter((g) => g.models.length > 0)
})

function isCurrent(providerId: number, model: string): boolean {
  return currentProviderId.value === providerId && currentModel.value === model
}

async function choose(providerId: number, model: string) {
  try {
    // One PATCH for the pair: validating them separately would reject the
    // provider change while the old model is still attached.
    await apiPatch('/api/config', { ai_summary: { provider_id: providerId, model } })
    await loadConfig()
  } catch (err) {
    appLog.w('SummaryModelPicker', 'save summary model failed', err)
  } finally {
    close()
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
.summary-search {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin: 0 var(--space-6);
  padding: 0 var(--space-4);
  height: calc(32 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
}
.summary-search-icon {
  color: var(--text-muted);
  flex-shrink: 0;
}
.summary-search-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--font-size-md);
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

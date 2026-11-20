<template>
  <!-- Global model-provider list. A provider is one endpoint + credential;
       agents reference it from their own settings page, so a key rotation is
       one edit here instead of one edit per agent. Creating and editing happen
       in ProviderFormDialog — this page only ever shows what is configured. -->
  <div class="provider-setting">
    <!-- Two audiences, one page: ordinary endpoints share the generic list and
         form, while an own-service account (CodeBuddy / Cursor / Command Code /
         Antigravity) needs a field the generic form cannot express — which
         backend it belongs to, plus CodeBuddy's service edition and the address
         its enterprise editions live at. One tab per backend keeps every panel
         specific without teaching the generic form about any of them. -->
    <div class="provider-tabs" role="tablist">
      <button
        type="button"
        class="provider-tab"
        :class="{ 'provider-tab--on': tab === 'general' }"
        role="tab"
        :aria-selected="tab === 'general'"
        data-tab="general"
        @click="tab = 'general'"
      >
        {{ t('settings.items.providerTabGeneral') }}
      </button>
      <button
        v-for="b in ACCOUNT_BACKENDS"
        :key="b"
        type="button"
        class="provider-tab"
        :class="{ 'provider-tab--on': tab === b }"
        role="tab"
        :aria-selected="tab === b"
        :data-tab="b"
        @click="tab = b"
      >
        {{ t(ACCOUNT_BACKEND_LABELS[b]) }}
      </button>
    </div>

    <template v-if="tab === 'general'">
      <!-- The description and the primary action share one row: "add" used to sit
           alone on its own line under a four-line paragraph, which cost a whole
           screenful before the first provider. -->
      <div class="provider-head">
        <p class="provider-hint">{{ description || t('settings.items.providerDesc') }}</p>
        <button class="fbtn fbtn-primary provider-add" data-action="provider-add" @click="openCreate">
          <Plus :size="14" />
          {{ t('settings.items.providerAdd') }}
        </button>
      </div>

      <div v-if="providers.length === 0" class="provider-empty">
        {{ t('settings.items.providerEmpty') }}
      </div>

      <div v-else class="provider-list">
        <!-- The whole row opens the editor. The two things you do *to* a provider
             without opening it (test, delete) are compact icon buttons on the
             right; three full-width pills per card read as a toolbar and pushed
             the information they belong to out of view. -->
        <div
          v-for="p in providers"
          :key="p.id"
          class="provider-row"
          :data-provider-id="p.id"
          data-action="provider-edit"
          @click="openEdit(p)"
        >
          <ProviderIcon tile :provider-id="iconIdFor(p)" :name="p.name" :size="34" />
          <div class="provider-row-body">
            <div class="provider-row-head">
              <!-- The name is the keyboard-reachable "open" affordance: the row
                   itself is a convenience tap target, but a focusable wrapper
                   around the two action buttons would be an interactive element
                   inside an interactive element. -->
              <button type="button" class="provider-name" @click.stop="openEdit(p)">{{ p.name }}</button>
              <span class="provider-key-badge" :class="p.hasKey ? 'provider-key-badge--ok' : 'provider-key-badge--missing'">
                {{ p.hasKey ? t('settings.items.providerKeySet') : t('settings.items.providerNoKey') }}
              </span>
            </div>
            <div class="provider-meta">{{ metaLine(p) }}</div>
            <div class="provider-summary">{{ p.baseUrl || t('settings.items.providerNoBaseUrl') }}</div>
            <div class="provider-detail">{{ detailLine(p) }}</div>
            <div
              v-if="testResults[p.id]"
              class="provider-test-result"
              :class="testResults[p.id].ok ? 'provider-test-result--ok' : 'provider-test-result--fail'"
            >
              <component :is="testResults[p.id].ok ? CheckCircle2 : AlertCircle" :size="13" />
              <span>{{ testResults[p.id].message }}</span>
            </div>
          </div>
          <div class="provider-row-actions">
            <button
              v-if="p.protocol !== 'private'"
              type="button"
              class="provider-icon-btn"
              data-action="provider-test"
              :disabled="testingId !== null"
              :title="t('settings.items.providerTestHint')"
              :aria-label="t('settings.items.providerTest')"
              @click.stop="testProvider(p)"
            >
              <component :is="testingId === p.id ? Loader2 : PlugZap" :size="16" :class="{ 'provider-spin': testingId === p.id }" />
            </button>
            <button
              type="button"
              class="provider-icon-btn provider-icon-btn--danger"
              data-action="provider-remove"
              :title="t('settings.items.providerRemove')"
              :aria-label="t('settings.items.providerRemove')"
              @click.stop="removeProvider(p)"
            >
              <Trash2 :size="16" />
            </button>
          </div>
        </div>
      </div>

      <!-- Catalogue state is bookkeeping, not a setting: it moved below the list
           so it stops competing with the providers themselves. -->
      <div class="provider-catalog">
        <span class="provider-catalog-text">
          {{ catalogFetchedAt
            ? t('settings.items.providerCatalogUpdated', { date: catalogDate, count: catalogCount })
            : t('settings.items.providerCatalogMissing') }}
          <span v-if="catalogStale" class="provider-catalog-stale">{{ t('settings.items.providerCatalogStale') }}</span>
        </span>
        <button
          type="button"
          class="provider-catalog-refresh"
          :disabled="refreshingCatalog"
          :title="t('settings.items.providerCatalogRefresh')"
          :aria-label="t('settings.items.providerCatalogRefresh')"
          @click="refreshCatalog"
        >
          <component :is="refreshingCatalog ? Loader2 : RefreshCw" :size="13" :class="{ 'provider-spin': refreshingCatalog }" />
        </button>
      </div>

      <div v-if="error" class="provider-error">
        <AlertCircle :size="13" />
        <span>{{ error }}</span>
      </div>
    </template>

    <OwnServiceAccountForm
      v-else
      :backend="tab"
      :providers="providers"
      :selected-id="accountId"
      @saved="onAccountSaved"
      @apply="onAccountApply"
    />

    <ProviderFormDialog
      :open="formOpen"
      :provider="editingProvider"
      :presets="presets"
      :saving="saving"
      :error-message="formError"
      :just-created="justCreated"
      @close="closeForm"
      @submit="saveDraft"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertCircle, CheckCircle2, Loader2, Plus, PlugZap, RefreshCw, Trash2 } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { apiDelete, apiGet, apiPatch, apiPost, apiPut } from '@/utils/api'
import { useDialog } from '@/composables/useDialog'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import ProviderFormDialog, { type ProviderFormValue, type ProviderRow } from './ProviderFormDialog.vue'
import OwnServiceAccountForm from './OwnServiceAccountForm.vue'
import type { PresetOption } from './ProviderPresetPicker.vue'
import { rememberProviderNames } from '@/utils/providerNames'
import { appLog } from '@/utils/appLog'

defineProps<{ description?: string }>()

const { t } = useI18n()
const { confirm } = useDialog()
const { agents, loadAgents } = useAgents()
const toast = useToast()

const providers = ref<ProviderRow[]>([])
/** Backends whose credential is for their OWN service: the generic form cannot
 *  say which one a row belongs to, so each gets its own panel. */
const ACCOUNT_BACKENDS = ['codebuddy', 'cursor', 'commandcode', 'antigravity'] as const
type AccountBackend = (typeof ACCOUNT_BACKENDS)[number]
const ACCOUNT_BACKEND_LABELS: Record<AccountBackend, string> = {
  codebuddy: 'settings.items.accountNameCodeBuddy',
  cursor: 'settings.items.accountNameCursor',
  commandcode: 'settings.items.accountNameCommandCode',
  antigravity: 'settings.items.accountNameAntigravity',
}

function isAccountBackend(value: string): value is AccountBackend {
  return (ACCOUNT_BACKENDS as readonly string[]).includes(value)
}

/** Which half of the page is showing. Own-service rows belong to a backend
 *  panel: the generic form would drop their backend and (CodeBuddy's) edition. */
const tab = ref<'general' | AccountBackend>('general')
/** Row an account panel should open (set when a private row is clicked). */
const accountId = ref<number | null>(null)
const presets = ref<PresetOption[]>([])
const saving = ref(false)
/** List-level failures (load / delete / catalogue). Form failures live in
 *  `formError` so the dialog can show them without closing. */
const error = ref('')
const formError = ref('')
const formOpen = ref(false)
const editingProvider = ref<ProviderRow | null>(null)
/** True for the dialog session that immediately followed a successful create,
 *  so the form can continue on its model view and say the provider exists. */
const justCreated = ref(false)
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
    // Publish the names for screens that only hold a provider id (the summary
    // model row), so it can show "provider · model" without its own request.
    rememberProviderNames(providers.value)
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

/** One muted line under the name: how it authenticates and who depends on it. */
function metaLine(p: ProviderRow): string {
  // The private protocol has no auth mode of its own: the backend injects its
  // fixed credential variable, so showing "auto" there would be noise.
  const parts = [protocolLabel(p)]
  if (p.protocol !== 'private') parts.push(authLabel(p.authMode))
  if (p.agentCount > 0) parts.push(t('settings.items.providerUsedBy', { count: p.agentCount }))
  return parts.join(' · ')
}

/** An own-service credential is named after the backend it belongs to; the
 *  three wire protocols are already their own names. */
function protocolLabel(p: ProviderRow): string {
  if (p.protocol !== 'private') return p.protocol
  const key = p.backend ? ACCOUNT_BACKEND_LABELS[p.backend as AccountBackend] : ''
  return key ? t('settings.items.accountLabel', { name: t(key) }) : t('settings.items.providerProtocolPrivate')
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
  justCreated.value = false
  formError.value = ''
  formOpen.value = true
}

function openEdit(p: ProviderRow) {
  // An own-service row carries fields the generic form does not know about, and
  // its save would clear them. Hand it to the panel that owns them. A row with
  // no backend predates the split; the panel it lands in adopts it on save.
  if (p.protocol === 'private') {
    accountId.value = p.id
    tab.value = isAccountBackend(p.backend ?? '') ? (p.backend as AccountBackend) : 'codebuddy'
    return
  }
  editingProvider.value = p
  justCreated.value = false
  formError.value = ''
  formOpen.value = true
}

/** Keep the just-saved account selected, then refresh the shared list. */
async function onAccountSaved(id: number) {
  accountId.value = id
  await loadProviders()
  await offerApplyToAgents(id)
}

/** The account panel's own "apply" button: same flow, but the user explicitly
 *  asked for it, so "nothing to do" must say so instead of staying silent. */
function onAccountApply(id: number) {
  void offerApplyToAgents(id, { interactive: true })
}

/**
 * After an own-service account is saved, every agent of that backend is very
 * likely meant to run on it — that account is usually why the user came to this
 * page at all. Ask once, then bind them all, instead of making the user repeat
 * the same provider pick in each agent's settings page.
 *
 * Only agents not already on this provider are counted, so re-saving an account
 * that everything already uses stays silent. The binding is refused per agent
 * with a reason (ownership, protocol, consent); those reasons are surfaced
 * rather than collapsed into one "save failed".
 */
async function offerApplyToAgents(providerId: number, opts: { interactive?: boolean } = {}) {
  const provider = providers.value.find((p) => p.id === providerId)
  if (!provider || provider.protocol !== 'private' || !provider.backend) return
  const label = ACCOUNT_BACKEND_LABELS[provider.backend as AccountBackend]
  const backendName = label ? t(label) : provider.backend
  // A keyless row can still be saved (the form only requires a key when
  // creating), but binding agents to it makes every turn fail with "provider
  // has no API key" — so it gets no offer.
  if (!provider.hasKey) {
    if (opts.interactive) {
      toast.show(t('settings.items.accountApplyNoKey'), { icon: 'warning', type: 'error' })
    }
    return
  }

  await loadAgents()
  const targets = agents.value.filter((a) => a.backend === provider.backend && a.providerId !== providerId)
  if (targets.length === 0) {
    if (opts.interactive) {
      toast.show(t('settings.items.accountApplyNone', { backend: backendName }), { icon: 'info', type: 'info' })
    }
    return
  }

  const ok = await confirm(
    t('settings.items.accountApplyMessage', { count: targets.length, backend: backendName, provider: provider.name }),
    {
      title: t('settings.items.accountApplyTitle'),
      confirmText: t('settings.items.accountApplyConfirm'),
      cancelText: t('settings.items.accountApplyCancel'),
    },
  )
  if (!ok) return

  const failures: string[] = []
  for (const agent of targets) {
    try {
      await apiPatch('/api/agents', { id: agent.id, provider_id: providerId })
    } catch (err) {
      const reason = err instanceof Error && err.message ? err.message : String(err)
      failures.push(`${agent.name}: ${reason}`)
    }
  }

  // The provider's "used by N agents" line and every agent picker read from
  // these two lists; both are now stale.
  await Promise.all([loadAgents(true), loadProviders()])

  if (failures.length > 0) {
    // The list-level error banner only renders on the general tab, and the user
    // is standing on an account tab here — the toast is the only place this is
    // visible.
    toast.show(t('settings.items.accountApplyFailed', { count: failures.length, reason: failures[0] }), {
      icon: 'warning',
      type: 'error',
      duration: 5000,
    })
    return
  }
  toast.show(t('settings.items.accountApplyDone', { count: targets.length, provider: provider.name }), { icon: 'success' })
}

function closeForm() {
  formOpen.value = false
  justCreated.value = false
  formError.value = ''
}

async function saveDraft(value: ProviderFormValue) {
  if (saving.value) return
  saving.value = true
  formError.value = ''
  try {
    if (editingProvider.value === null) {
      // The create response IS the new row: the dialog turns into its editor
      // and continues on the model view, so "create then pick models" is one
      // uninterrupted flow instead of create → close → find it → edit → models.
      const created = await apiPost<ProviderRow>('/api/providers', {
        name: value.name,
        protocol: value.protocol,
        baseUrl: value.baseUrl,
        apiKey: value.apiKey,
        authMode: value.authMode,
        wireApi: value.wireApi,
        presetId: value.presetId,
      })
      editingProvider.value = created
      justCreated.value = true
      await loadProviders()
      return
    }
    await apiPut(`/api/providers/${editingProvider.value.id}`, {
      name: value.name,
      protocol: value.protocol,
      baseUrl: value.baseUrl,
      authMode: value.authMode,
      wireApi: value.wireApi,
      apiKey: value.apiKey,
      clearApiKey: value.clearApiKey,
      models: value.models,
      defaultModel: value.defaultModel,
    })
    // The verdict belonged to the configuration that was just replaced; a green
    // "connection OK" left over an edited endpoint is worse than no verdict.
    delete testResults.value[editingProvider.value.id]
    closeForm()
    await loadProviders()
  } catch (err) {
    // Keep the dialog open with the typed values so the user can correct them.
    formError.value = providerSaveErrorText(err, value)
    appLog.w('ProviderSetting', 'save provider failed', formError.value)
  } finally {
    saving.value = false
  }
}

/**
 * The server localizes `error` itself, so it is usable as-is — except for the
 * one rejection this form can explain better: an auth mode the protocol cannot
 * express (`ProviderInvalid` + `detail.field === 'authMode'`). Its raw reason
 * names internal identifiers; the form already knows both values, so it can say
 * what to change instead.
 */
function providerSaveErrorText(err: unknown, value: ProviderFormValue): string {
  const typed = err as { msgKey?: string; detail?: { field?: string } } | null
  if (typed?.msgKey === 'ProviderInvalid' && typed.detail?.field === 'authMode') {
    return t('settings.items.providerAuthUnsupported', {
      mode: value.authMode,
      protocol: value.protocol,
    })
  }
  return err instanceof Error && err.message ? err.message : String(err)
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
      : clipError(result.error || t('settings.items.providerTestFailed'))
    testResults.value = { ...testResults.value, [p.id]: { ok: result.ok, message } }
  } catch (err) {
    testResults.value = {
      ...testResults.value,
      [p.id]: { ok: false, message: clipError(err instanceof Error ? err.message : String(err)) },
    }
  } finally {
    testingId.value = null
  }
}

/** The /models probe relays the upstream body verbatim (an HTML error page can
 *  be a megabyte), and this card renders it inline — so clip before display. */
function clipError(text: string): string {
  const s = text.trim()
  return s.length > 300 ? s.slice(0, 300) + '…' : s
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
 * chrome and the control height. Using the wrong token names here
 * (--text-sm / --border-primary) silently falls back to inherited values —
 * that is exactly how this page drifted out of alignment before. */
.provider-setting {
  padding: var(--space-6) var(--space-7);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.provider-tabs {
  display: flex;
  gap: var(--space-2);
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--border-color);
  /* Five tabs do not fit a phone; scrolling beats wrapping into two rows. */
  overflow-x: auto;
  scrollbar-width: none;
}
.provider-tabs::-webkit-scrollbar {
  display: none;
}
.provider-tab {
  flex: none;
}
.provider-tab {
  padding: calc(4 * var(--dp)) var(--space-4);
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  font-size: var(--font-size-md);
  font-family: inherit;
  cursor: pointer;
}
.provider-tab:hover {
  color: var(--text-primary);
}
.provider-tab--on {
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-weight: var(--font-weight-semibold);
}
.provider-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
}
.provider-hint {
  flex: 1;
  min-width: 0;
  margin: 0;
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.provider-add {
  flex-shrink: 0;
}
.provider-empty {
  color: var(--text-muted);
  font-size: var(--font-size-md);
}
.provider-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
/* A row is a surface you can tap, not a card with a toolbar under it. */
.provider-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  padding: var(--space-4) var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-secondary);
  cursor: pointer;
  min-width: 0;
  transition: border-color var(--duration-base), background var(--duration-base);
}
.provider-row:hover,
.provider-row:focus-visible {
  border-color: color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
  outline: none;
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
  border: none;
  background: transparent;
  padding: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  cursor: pointer;
  text-align: left;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-name:hover {
  text-decoration: underline;
}
/* Key state is a colored dot + word rather than another bordered chip: the row
   already had four boxes competing with the name. */
.provider-key-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  white-space: nowrap;
}
.provider-key-badge::before {
  content: '';
  width: calc(5 * var(--dp));
  height: calc(5 * var(--dp));
  border-radius: var(--radius-full);
  background: currentColor;
  flex-shrink: 0;
}
.provider-key-badge--ok {
  color: var(--color-green);
}
.provider-key-badge--missing {
  color: var(--color-orange);
}
.provider-meta {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  overflow-wrap: anywhere;
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
/* Icon actions sit in their own column so the row body keeps its full width. */
.provider-row-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
}
.provider-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(32 * var(--dp));
  height: calc(32 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
}
.provider-icon-btn:hover:not(:disabled) {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
}
.provider-icon-btn--danger:hover:not(:disabled) {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
}
.provider-icon-btn:disabled {
  cursor: default;
  opacity: var(--opacity-disabled);
}
.provider-catalog {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  color: var(--text-hint);
  line-height: var(--line-height-normal);
}
.provider-catalog-text {
  min-width: 0;
  overflow-wrap: anywhere;
}
.provider-catalog-stale {
  color: var(--color-orange);
}
.provider-catalog-refresh {
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
.provider-catalog-refresh:hover:not(:disabled) {
  color: var(--text-primary);
}
.provider-catalog-refresh:disabled {
  cursor: default;
  opacity: var(--opacity-disabled);
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

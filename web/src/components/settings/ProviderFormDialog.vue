<template>
  <!-- Create / edit one provider. The list page only shows what is configured;
       every field that can be changed lives here so the list stays a list.

       ONE shell, three views (form / preset / models) with a back arrow in the
       header. The pickers used to be their own ModalDialogs stacked on top of
       this one: two scrims, two close buttons and two back presses on a phone,
       plus a form you could no longer see while choosing. Swapping the body
       keeps a single layer and makes "back" mean "return to the form". -->
  <ModalDialog
    :open="open"
    :title="dialogTitle"
    :z-index="2500"
    @close="close"
  >
    <template #header>
      <button
        v-if="view !== 'form'"
        type="button"
        class="provider-back"
        :aria-label="t('common.back')"
        :title="t('common.back')"
        @click="view = 'form'"
      >
        <ChevronLeft :size="16" />
      </button>
      <span class="modal-title">{{ dialogTitle }}</span>
    </template>

    <div v-if="justCreated" class="provider-created" role="status">
      <CheckCircle2 :size="14" />
      <span>{{ t('settings.items.providerCreatedHint') }}</span>
    </div>

    <div v-if="view === 'form'" class="provider-form">
      <!-- The catalogue has 200+ entries, so presets open as their own view
           rather than a native <select>. A new provider can be seeded from one;
           an existing one keeps whatever it was saved with. -->
      <button
        v-if="!provider"
        type="button"
        class="provider-preset-trigger"
        @click="view = 'preset'"
      >
        <template v-if="selectedPreset">
          <ProviderIcon tile :provider-id="selectedPreset.id" :name="selectedPreset.name" :size="22" />
          <span class="provider-preset-text">
            <span class="provider-preset-name">{{ selectedPreset.name }}</span>
            <span class="provider-preset-proto">{{ selectedPreset.protocol }}</span>
          </span>
        </template>
        <span v-else class="provider-preset-placeholder">{{ t('settings.items.providerPresetPlaceholder') }}</span>
        <ChevronRight :size="15" class="provider-preset-chevron" />
      </button>

      <!-- ── Connection ─────────────────────────────────────────────────── -->
      <section class="provider-section" data-section="conn">
        <h3 class="provider-section-title">{{ t('settings.items.providerSectionConn') }}</h3>

        <label class="provider-field">
          <span class="provider-label">{{ t('settings.items.providerName') }}</span>
          <input
            v-model="draft.name"
            class="provider-input"
            data-role="name"
            :placeholder="t('settings.items.providerNamePlaceholder')"
          />
        </label>

        <div class="provider-field">
          <span class="provider-label">{{ t('settings.items.providerProtocol') }}</span>
          <div class="provider-seg" data-role="protocol">
            <button
              v-for="p in PROTOCOLS"
              :key="p"
              type="button"
              class="provider-seg-btn"
              :class="{ 'provider-seg-btn--on': draft.protocol === p }"
              :data-protocol="p"
              :aria-pressed="draft.protocol === p"
              @click="draft.protocol = p"
            >
              {{ p }}
            </button>
          </div>
        </div>

        <label class="provider-field">
          <span class="provider-label">{{ t('settings.items.providerBaseUrl') }}</span>
          <input
            v-model="draft.baseUrl"
            class="provider-input"
            data-role="base-url"
            inputmode="url"
            autocapitalize="off"
            spellcheck="false"
            :placeholder="t('settings.items.providerBaseUrlPlaceholder')"
          />
          <span class="provider-hint">{{ t('settings.items.providerBaseUrlHint') }}</span>
        </label>

        <div class="provider-field">
          <span class="provider-label">{{ t('settings.items.providerAuthLabel') }}</span>
          <select v-model="draft.authMode" class="provider-input provider-select" data-role="auth-mode">
            <option v-for="m in authModeOptions" :key="m.value" :value="m.value" :disabled="m.disabled">{{ m.label }}</option>
          </select>
          <!-- A protocol that cannot express the mode makes every turn fail, so an
               existing row in that state says so here instead of looking fine. -->
          <div v-if="authModeUnsupported" class="provider-error" data-role="auth-mode-warning">
            <AlertCircle :size="13" />
            <span>{{ t('settings.items.providerAuthUnsupported', { mode: draft.authMode, protocol: draft.protocol }) }}</span>
          </div>
        </div>
      </section>

      <!-- ── Credentials ────────────────────────────────────────────────── -->
      <section class="provider-section" data-section="creds">
        <h3 class="provider-section-title">{{ t('settings.items.providerSectionCreds') }}</h3>

        <div class="provider-field">
          <div class="provider-label-row">
            <span class="provider-label">{{ t('settings.items.providerKey') }}</span>
            <span
              v-if="provider"
              class="provider-key-state"
              :class="{ 'provider-key-state--ok': provider.hasKey && !clearKey }"
            >
              {{ provider.hasKey && !clearKey ? t('settings.items.providerKeyStateSaved') : t('settings.items.providerKeyStateMissing') }}
            </span>
          </div>
          <div class="provider-key-field">
            <input
              :value="keyValue"
              :type="showKey ? 'text' : 'password'"
              autocomplete="off"
              class="provider-input provider-key-input"
              data-role="api-key"
              :placeholder="provider ? t('settings.items.providerKeyKeep') : t('settings.items.providerKey')"
              @input="onKeyInput"
              @focus="onKeyFocus"
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
              <component :is="showKey ? EyeOff : Eye" :size="15" />
            </button>
          </div>
          <div v-if="keyError" class="provider-error" data-role="key-error">
            <AlertCircle :size="13" />
            <span>{{ keyError }}</span>
          </div>
          <!-- Clearing is a destructive one-way action, so it is a text action
               with an explicit undo instead of a checkbox that silently rides
               along with the next save. -->
          <div v-if="provider?.hasKey" class="provider-clear">
            <button v-if="!clearKey" type="button" class="provider-clear-btn" @click="clearKey = true">
              <Trash2 :size="12" />
              {{ t('settings.items.providerClearKey') }}
            </button>
            <template v-else>
              <span class="provider-clear-pending">{{ t('settings.items.providerClearKeyPending') }}</span>
              <button type="button" class="provider-clear-undo" @click="clearKey = false">
                {{ t('settings.items.providerUndo') }}
              </button>
            </template>
          </div>
        </div>
      </section>

      <!-- ── Codex wire API (openai only) ───────────────────────────────── -->
      <!-- Only codex reads this: its managed config block tells codex whether
           the endpoint speaks the Responses API or plain chat/completions. A
           gateway that serves only the latter 404s every turn when this says
           "responses", with nothing in the UI pointing at the cause.
           Codex only accepts the openai protocol (see model.providerEnvTable),
           so for anthropic/gemini providers this control has no meaning at all
           and used to look like a fourth, peer-level protocol choice. -->
      <section v-if="draft.protocol === 'openai'" class="provider-section" data-section="wire">
        <h3 class="provider-section-title">{{ t('settings.items.providerWireSection') }}</h3>
        <div class="provider-field">
          <select v-model="draft.wireApi" class="provider-input provider-select" data-role="wire-api">
            <option value="responses">{{ t('settings.items.providerWireResponses') }}</option>
            <option value="chat">{{ t('settings.items.providerWireChat') }}</option>
          </select>
          <!-- Codex reads this from its config file at process start, so a live
               connection keeps the old value; without the note the change looks
               like it did nothing. -->
          <span class="provider-hint">{{ t('settings.items.providerWireHint') }}</span>
        </div>
      </section>

      <!-- ── Models ─────────────────────────────────────────────────────── -->
      <!-- The curated model set. Agents bound to this provider inherit it, so
           this is the only place a model can be "known to work". The full
           editor is its own view; the form only summarizes it. -->
      <section v-if="provider" class="provider-section" data-section="models">
        <div class="provider-section-head">
          <h3 class="provider-section-title">{{ t('settings.items.providerModelsLabel') }}</h3>
          <button type="button" class="provider-link" data-action="provider-models-open" @click="view = 'models'">
            {{ t('settings.items.providerModelsManage') }}
            <ChevronRight :size="14" />
          </button>
        </div>
        <p class="provider-models-summary">{{ modelsSummary }}</p>
        <div v-if="draft.models.length > 0" class="provider-models-chips">
          <span
            v-for="id in draft.models"
            :key="id"
            class="provider-model-chip"
            :class="{ 'provider-model-chip--default': id === draft.defaultModel }"
          >
            <Star v-if="id === draft.defaultModel" :size="10" class="provider-model-chip-star" fill="currentColor" />
            <span class="provider-model-chip-id">{{ id }}</span>
            <button
              type="button"
              class="provider-model-chip-x"
              :aria-label="t('settings.items.providerRemove')"
              @click="removeModel(id)"
            >×</button>
          </span>
        </div>
      </section>

      <div v-if="connResult" class="provider-note" :class="connResult.ok ? 'provider-note--ok' : 'provider-note--fail'">
        <component :is="connResult.ok ? CheckCircle2 : AlertCircle" :size="13" />
        <span>{{ connResult.message }}</span>
      </div>

      <div v-if="errorMessage" class="provider-error">
        <AlertCircle :size="13" />
        <span>{{ errorMessage }}</span>
      </div>
    </div>

    <ProviderPresetPicker
      v-if="view === 'preset'"
      :active="true"
      :presets="presets"
      :selected-id="presetId"
      @select="onPresetSelect"
      @done="view = 'form'"
    />

    <ProviderModelsPicker
      v-if="view === 'models'"
      :active="true"
      :provider-id="provider?.id ?? null"
      :selected="draft.models"
      :default-model="draft.defaultModel"
      :probe="modelsProbe"
      @update:selected="onModelsSelected"
      @update:default-model="draft.defaultModel = $event"
    />

    <template #footer>
      <template v-if="view === 'form'">
        <!-- Testing belongs here, next to the values it tests: the list page
             could only test what was already saved, so a wrong key was found
             after saving it. Draft values ride along with the probe. -->
        <button
          v-if="provider"
          type="button"
          class="fbtn provider-test-conn"
          data-action="provider-test-conn"
          :disabled="testingConn"
          @click="testConnection"
        >
          <component :is="testingConn ? Loader2 : PlugZap" :size="13" :class="{ 'provider-spin': testingConn }" />
          {{ testingConn ? t('settings.items.providerTestingConn') : t('settings.items.providerTestConn') }}
        </button>
        <span class="provider-footer-spacer" />
        <button class="fbtn" data-action="provider-cancel" @click="close">
          {{ justCreated ? t('common.close') : t('settings.items.providerCancel') }}
        </button>
        <button class="fbtn fbtn-primary" data-action="provider-save" :disabled="!canSave || saving" @click="submit">
          {{ provider ? t('settings.items.providerSave') : t('settings.items.providerAdd') }}
        </button>
      </template>
      <template v-else>
        <button class="fbtn" @click="view = 'form'">{{ t('common.back') }}</button>
        <span class="provider-footer-spacer" />
        <button v-if="view === 'models'" class="fbtn fbtn-primary" data-action="provider-models-done" @click="view = 'form'">
          {{ t('settings.items.providerModelsDone') }}
        </button>
      </template>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertCircle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Eye,
  EyeOff,
  Loader2,
  PlugZap,
  Star,
  Trash2,
} from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
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
  /** Flipped by the caller right after a create succeeded: the provider now
   *  exists, so continue on the model view instead of closing and making the
   *  user find it in the list again. */
  justCreated?: boolean
}>()

const emit = defineEmits<{
  close: []
  submit: [value: ProviderFormValue]
}>()

const { t } = useI18n()

const PROTOCOLS = ['anthropic', 'openai', 'gemini'] as const

/** Which of the three bodies is showing. One ModalDialog, no nesting. */
const view = ref<'form' | 'preset' | 'models'>('form')

const dialogTitle = computed(() => {
  if (view.value === 'preset') return t('settings.items.providerPresetTitle')
  if (view.value === 'models') return t('settings.items.providerModelsTitle')
  return props.provider ? t('settings.items.providerEditTitle') : t('settings.items.providerAddTitle')
})

const presetId = ref('')
const clearKey = ref(false)
/** Password vs plaintext for the key field, and whether the stored key has
 *  already been fetched for this open. */
const showKey = ref(false)
const keyLoaded = ref(false)
const keyLoading = ref(false)
const keyError = ref('')

/** Dots standing in for a stored key that has not been revealed. The value is
 *  never fetched just to fill the field (see toggleKeyVisibility), so without
 *  it the box looks empty and "a key is saved" is indistinguishable from "no
 *  key yet". `draft.apiKey` stays empty while the mask is shown: an empty
 *  apiKey on submit means "keep the stored one", and the mask must never be
 *  sent as if it were the key. */
const KEY_MASK_CHAR = '•'
const KEY_MASK = KEY_MASK_CHAR.repeat(12)
const keyMasked = ref(false)
/** Hidden once "clear the stored key" is ticked: the dots would otherwise claim
 *  a secret is still configured while the save is about to remove it. */
const keyValue = computed(() => (keyMasked.value && !clearKey.value ? KEY_MASK : draft.apiKey))

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

/**
 * Auth modes each wire protocol can actually express.
 *
 * `bearer` needs a distinct auth-token variable (ANTHROPIC_AUTH_TOKEN), and only
 * the Anthropic-style backends have one. Asking for it on openai/gemini makes
 * the backend refuse every turn ("does not support Bearer auth"), while the
 * dialog's own connection test still passes — it always sends an
 * `Authorization: Bearer` header — so offering the option here is what let
 * users walk into a provider that could never serve a request.
 */
const AUTH_MODES_BY_PROTOCOL: Record<string, string[]> = {
  anthropic: ['auto', 'x-api-key', 'bearer'],
  openai: ['auto', 'x-api-key'],
  gemini: ['auto', 'x-api-key'],
}

const allowedAuthModes = computed(() => AUTH_MODES_BY_PROTOCOL[draft.protocol] ?? ['auto'])

/** A row saved before the narrowing (or written through the API) can still
 *  carry an impossible pair. It stays visible and flagged rather than being
 *  silently rewritten on open. */
const authModeUnsupported = computed(() => !allowedAuthModes.value.includes(draft.authMode))

const authModeOptions = computed(() => {
  const label = (mode: string) => (mode === 'auto' ? t('settings.items.providerAuthAuto') : mode)
  const out = allowedAuthModes.value.map((value) => ({ value, label: label(value), disabled: false }))
  if (authModeUnsupported.value) {
    // Keep the stored value selectable-but-disabled so the control displays it
    // (a <select> whose value is missing from its options renders blank) and
    // the user cannot re-pick it after correcting the field.
    out.push({ value: draft.authMode, label: label(draft.authMode), disabled: true })
  }
  return out
})

const modelsSummary = computed(() => {
  if (draft.models.length === 0) return t('settings.items.providerModelsNone')
  const count = t('settings.items.providerModelsSummary', { count: draft.models.length })
  if (!draft.defaultModel) return count
  return `${count} · ${t('settings.items.providerDefaultModelValue', { model: draft.defaultModel })}`
})

/** What the per-model probe sends besides the model id: the values on screen,
 *  not necessarily the ones already stored. */
const modelsProbe = computed(() => ({
  protocol: draft.protocol,
  baseUrl: draft.baseUrl.trim(),
  apiKey: draft.apiKey,
  authMode: draft.authMode,
}))

/** The chosen catalogue entry, for the trigger row (icon + name). */
const selectedPreset = computed<PresetOption | undefined>(() =>
  props.presets.find((p) => p.id === presetId.value),
)

// ── Connection test ─────────────────────────────────────────────────────────

const testingConn = ref(false)
const connResult = ref<{ ok: boolean; message: string } | null>(null)

/** A verdict belongs to the values that produced it; editing any of them makes
 *  it stale. Endpoint, key and protocol are the ones the probe actually uses. */
watch(
  () => [draft.protocol, draft.baseUrl, draft.apiKey, draft.authMode].join('\u0000'),
  () => { connResult.value = null },
)

async function testConnection() {
  const p = props.provider
  if (!p || testingConn.value) return
  testingConn.value = true
  connResult.value = null
  try {
    const res = await apiPost<{ ok: boolean; status?: number; models?: string[]; error?: string }>(
      '/api/providers/test',
      {
        id: p.id,
        protocol: draft.protocol,
        baseUrl: draft.baseUrl.trim(),
        apiKey: draft.apiKey,
        authMode: draft.authMode,
      },
      // A generated token is slower than the default 10s budget.
      { timeoutMs: 45_000 },
    )
    connResult.value = res.ok
      ? { ok: true, message: t('settings.items.providerTestOk', { count: res.models?.length ?? 0 }) }
      : { ok: false, message: clipError(res.error || t('settings.items.providerTestFailed')) }
  } catch (err) {
    connResult.value = {
      ok: false,
      message: clipError(err instanceof Error ? err.message : String(err)),
    }
  } finally {
    testingConn.value = false
  }
}

/** The /models probe relays the upstream body verbatim (an HTML error page can
 *  be a megabyte), and this card renders it inline — so clip before display. */
function clipError(text: string): string {
  const s = text.trim()
  return s.length > 300 ? s.slice(0, 300) + '…' : s
}

// ── Seeding ─────────────────────────────────────────────────────────────────

function resetFromProvider() {
  const p = props.provider
  view.value = 'form'
  clearKey.value = false
  showKey.value = false
  keyLoaded.value = false
  keyLoading.value = false
  keyError.value = ''
  connResult.value = null
  testingConn.value = false
  keyMasked.value = p?.hasKey === true
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
      keyMasked.value = false
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

/** Select the mask on focus so the first keystroke replaces it instead of
 *  landing in the middle of a run of bullets. */
function onKeyFocus(event: FocusEvent) {
  if (!keyMasked.value) return
  const el = event.target as HTMLInputElement | null
  el?.select()
}

function onKeyInput(event: Event) {
  const el = event.target as HTMLInputElement
  let next = el.value
  if (keyMasked.value) {
    keyMasked.value = false
    // Clicking into the mask without selecting it leaves bullets in the box;
    // strip them instead of storing them as part of the key.
    next = next.split(KEY_MASK_CHAR).join('')
  }
  draft.apiKey = next
  // A typed key contradicts "clear the stored one": the server applies
  // clearApiKey first and would drop the new secret, so the save would report
  // success while leaving the provider with NO key at all.
  if (next !== '') clearKey.value = false
  // Keep the DOM in sync when the mask prefix was removed.
  if (el.value !== next) el.value = next
}

// Re-seed on every open so a cancelled edit never leaks into the next create.
watch(() => props.open, (open) => {
  if (open) resetFromProvider()
}, { immediate: true })

// A create that just succeeded turns this dialog into the edit dialog for the
// new provider; the caller opens the model view so the next step is obvious.
watch(() => props.justCreated, (created) => {
  if (created) view.value = 'models'
}, { immediate: true })

// Android back / edge swipe: inside a picker it returns to the form instead of
// closing the dialog. `PRIORITY_OVERLAY + 2` outranks ModalDialog's own
// handler (PRIORITY_OVERLAY + 1), which then only sees the form view.
let unregisterBack: (() => void) | null = null
watch(() => props.open, (open) => {
  if (open && !unregisterBack) {
    unregisterBack = registerBackHandler({
      id: 'provider-form-view',
      canGoBack: () => view.value !== 'form',
      goBack: () => { view.value = 'form' },
      priority: PRIORITY_OVERLAY + 2,
    })
  } else if (!open && unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
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
    // The two are mutually exclusive on the server (clear wins). Only ask for
    // a clear when there is no replacement key to send, whatever the checkbox
    // ended up showing (e.g. the key was revealed by the eye afterwards).
    clearApiKey: clearKey.value && draft.apiKey === '',
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
 * inside the same settings card, so they must share the control height and the
 * 16px inset. Using the wrong token names here (--text-sm / --border-primary)
 * silently falls back to inherited values — that is exactly how this page
 * drifted out of alignment before. */
.provider-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
  padding: var(--space-5) var(--space-6) var(--space-6);
}
/* Header back arrow for the picker views. 28dp matches .modal-close-btn so the
   two ends of the header read as one row of controls. */
.provider-back {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  padding: 0;
  flex-shrink: 0;
}
.provider-back:hover {
  background: color-mix(in srgb, var(--text-primary) 12%, transparent);
  color: var(--text-primary);
}
.provider-created {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  margin: var(--space-4) var(--space-6) 0;
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-green) 12%, transparent);
  color: var(--color-green);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
}

/* ── Sections ── */
.provider-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
/* A hairline between groups is what makes the form read as three questions
   ("what is it / how do I log in / what does it serve") instead of eight
   equally important boxes. */
.provider-section + .provider-section {
  padding-top: var(--space-5);
  border-top: 1px solid var(--border-color);
}
.provider-section-title {
  margin: 0;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted);
  letter-spacing: calc(0.4 * var(--dp));
}
.provider-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.provider-link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  border: none;
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  cursor: pointer;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  flex-shrink: 0;
}
.provider-link:hover {
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}

/* ── Fields ── */
.provider-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}
.provider-label-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
}
.provider-label {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}
.provider-input {
  box-sizing: border-box;
  width: 100%;
  height: calc(34 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-lg);
  min-width: 0;
}
.provider-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
.provider-select {
  padding: 0 var(--space-3);
  cursor: pointer;
}
/* Segmented protocol switch: three values that used to hide behind a <select>
   even though the protocol decides which other fields exist. */
.provider-seg {
  display: flex;
  gap: var(--space-1);
  padding: var(--space-1);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
}
.provider-seg-btn {
  flex: 1;
  min-width: 0;
  height: calc(28 * var(--dp));
  border: none;
  border-radius: calc(var(--radius-sm) - 1px);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
  padding: 0 var(--space-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-seg-btn--on {
  background: var(--accent-color);
  color: var(--bg-primary);
  font-weight: var(--font-weight-medium);
}
/* Muted explanatory line under a control. */
.provider-hint {
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  color: var(--text-muted);
  overflow-wrap: anywhere;
}

/* ── Key ── */
.provider-key-field {
  position: relative;
  display: flex;
  align-self: stretch;
  height: calc(34 * var(--dp));
}
.provider-key-field > .provider-input {
  flex: 1;
  min-width: 0;
  /* Leave room for the button so a long key never runs underneath it. */
  padding-right: calc(38 * var(--dp));
}
.provider-key-toggle {
  position: absolute;
  top: 50%;
  right: calc(4 * var(--dp));
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 0;
}
.provider-key-toggle:hover:not(:disabled) {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
}
.provider-key-toggle:disabled {
  cursor: default;
  opacity: var(--opacity-disabled);
}
.provider-key-state {
  font-size: var(--font-size-xs);
  color: var(--color-orange);
  flex-shrink: 0;
}
.provider-key-state--ok {
  color: var(--color-green);
}
.provider-clear {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  font-size: var(--font-size-sm);
}
.provider-clear-btn,
.provider-clear-undo {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  border: none;
  background: transparent;
  font-size: var(--font-size-sm);
  cursor: pointer;
  padding: var(--space-1) 0;
}
.provider-clear-btn {
  color: var(--color-red);
}
.provider-clear-undo {
  color: var(--accent-color);
  text-decoration: underline;
}
.provider-clear-pending {
  color: var(--text-muted);
}

/* ── Preset trigger ── */
.provider-preset-trigger {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  min-height: calc(44 * var(--dp));
  padding: var(--space-2) var(--space-4);
  border: 1px dashed var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  text-align: left;
  cursor: pointer;
  color: var(--text-primary);
}
.provider-preset-trigger:hover {
  border-color: var(--accent-color);
}
.provider-preset-text {
  display: flex;
  flex-direction: column;
  gap: calc(1 * var(--dp));
  min-width: 0;
  flex: 1;
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

/* ── Models summary ── */
.provider-models-summary {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
  overflow-wrap: anywhere;
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
  height: calc(24 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  max-width: 100%;
}
/* The default model is marked on its own chip: a separate <select> one screen
   below the chips made the relationship invisible. */
.provider-model-chip--default {
  border-color: color-mix(in srgb, var(--accent-color) 55%, transparent);
  color: var(--text-primary);
}
.provider-model-chip-star {
  color: var(--accent-color);
  flex-shrink: 0;
}
.provider-model-chip-x {
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: var(--font-size-md);
  line-height: 1;
  padding: 0 var(--space-1);
}
.provider-model-chip-x:hover {
  color: var(--color-red);
}
/* A model id can be long; truncate it instead of stretching the chip past the
   dialog and pushing the buttons out of reach. */
.provider-model-chip-id {
  max-width: calc(220 * var(--dp));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Messages ── */
.provider-error,
.provider-note {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  overflow-wrap: anywhere;
}
.provider-error {
  color: var(--color-red);
}
.provider-note--ok {
  color: var(--color-green);
}
.provider-note--fail {
  color: var(--color-red);
}

/* ── Footer ── */
.provider-test-conn {
  margin-right: auto;
}
.provider-footer-spacer {
  flex: 1;
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

<template>
  <!-- One backend's own-service account, configured on its own panel instead of
       through the generic provider form: that form can express a credential but
       not WHICH backend it belongs to, nor (for CodeBuddy) which edition of the
       service to talk to and the address the enterprise editions live at.

       One row per account, edited in place. The row is still a private-protocol
       provider, so binding, listing and deletion stay in the shared code paths. -->
  <div class="oa-form">
    <p class="oa-hint">{{ hint }}</p>

    <label class="oa-field">
      <span class="oa-label">{{ t('settings.items.providerName') }}</span>
      <input
        v-model="draft.name"
        class="oa-input"
        data-role="account-name"
        :placeholder="defaultName"
      />
    </label>

    <label v-if="isCodeBuddy" class="oa-field">
      <span class="oa-label">{{ t('settings.items.codebuddyProtocolType') }}</span>
      <select v-model="draft.deployment" class="oa-input oa-select" data-role="account-deployment">
        <option v-for="d in DEPLOYMENTS" :key="d.value" :value="d.value">{{ t(d.label) }}</option>
      </select>
      <span class="oa-hint">{{ t('settings.items.codebuddyDeploymentHint') }}</span>
    </label>

    <!-- Only the enterprise editions have an address of their own; the others
         use a fixed official host, so showing the box there would invite a
         value nothing reads. -->
    <label v-if="needsEndpoint" class="oa-field">
      <span class="oa-label">{{ t('settings.items.codebuddyDomain') }}</span>
      <input
        v-model="draft.baseUrl"
        class="oa-input"
        data-role="account-domain"
        inputmode="url"
        autocapitalize="off"
        spellcheck="false"
        :placeholder="t('settings.items.codebuddyDomainPlaceholder')"
      />
      <span class="oa-hint">{{ t('settings.items.codebuddyDomainHint') }}</span>
    </label>

    <div class="oa-field">
      <div class="oa-label-row">
        <span class="oa-label">{{ t('settings.items.providerKey') }}</span>
        <span v-if="target" class="oa-key-state" :class="{ 'oa-key-state--ok': target.hasKey }">
          {{ target.hasKey ? t('settings.items.providerKeyStateSaved') : t('settings.items.providerKeyStateMissing') }}
        </span>
      </div>
      <div class="oa-key-field">
        <input
          v-model="draft.apiKey"
          :type="showKey ? 'text' : 'password'"
          class="oa-input"
          data-role="account-key"
          autocomplete="off"
          :placeholder="target ? t('settings.items.codebuddyKeyKeep') : t('settings.items.providerKey')"
        />
        <button
          type="button"
          class="oa-key-toggle"
          :title="showKey ? t('settings.items.providerKeyHide') : t('settings.items.providerKeyReveal')"
          :aria-label="showKey ? t('settings.items.providerKeyHide') : t('settings.items.providerKeyReveal')"
          @click="showKey = !showKey"
        >
          <component :is="showKey ? EyeOff : Eye" :size="15" />
        </button>
      </div>
    </div>

    <p v-if="error" class="oa-error" data-role="account-error">{{ error }}</p>
    <p v-else-if="savedId !== null && savedId === target?.id" class="oa-ok" data-role="account-saved">
      {{ t('settings.items.codebuddySaved') }}
    </p>

    <div class="oa-actions">
      <!-- Manual repeat of the offer that follows a save: the user may have
           declined it, or agents may have been added since. Only a saved
           account has an id to bind them to. -->
      <button
        v-if="target"
        type="button"
        class="fbtn"
        data-action="account-apply"
        :disabled="saving"
        @click="applyToAgents"
      >
        {{ t('settings.items.accountApplyButton') }}
      </button>
      <button
        type="button"
        class="fbtn fbtn-primary"
        data-action="account-save"
        :disabled="!canSave || saving"
        @click="save"
      >
        <Loader2 v-if="saving" :size="13" class="oa-spin" />
        {{ target ? t('settings.items.providerSave') : t('settings.items.accountAdd') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Eye, EyeOff, Loader2 } from 'lucide-vue-next'
import '@/assets/modal-footer-btn.css'
import { apiPost, apiPut } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import type { ProviderRow } from './ProviderFormDialog.vue'

const props = defineProps<{
  /** Which backend's account this panel edits. */
  backend: string
  /** Every configured provider; the panel picks this backend's private rows. */
  providers: ProviderRow[]
  /** Row the user asked to edit (an account clicked in the general list). */
  selectedId: number | null
}>()

/** `saved` carries the row's id: the parent keeps that account selected so the
 *  panel does not jump to another one after the list reloads. `apply` asks the
 *  parent to bind this backend's agents to the account (the same flow it runs
 *  automatically after a save). */
const emit = defineEmits<{ saved: [id: number]; apply: [id: number] }>()

const { t } = useI18n()

/**
 * The dropdown options are the editions CodeBuddy itself documents. "" is the
 * hosted service every account used before this control existed, so it stays
 * the default and needs no address.
 */
const DEPLOYMENTS = [
  { value: '', label: 'settings.items.codebuddyDeploymentPublic' },
  { value: 'internal', label: 'settings.items.codebuddyDeploymentInternal' },
  { value: 'ioa', label: 'settings.items.codebuddyDeploymentIOA' },
  { value: 'cloudhosted', label: 'settings.items.codebuddyDeploymentDedicated' },
  { value: 'selfhosted', label: 'settings.items.codebuddyDeploymentSelfHosted' },
] as const

const NEEDS_ENDPOINT = ['cloudhosted', 'selfhosted']

const NAME_KEYS: Record<string, string> = {
  codebuddy: 'settings.items.accountNameCodeBuddy',
  cursor: 'settings.items.accountNameCursor',
  commandcode: 'settings.items.accountNameCommandCode',
  antigravity: 'settings.items.accountNameAntigravity',
}

const isCodeBuddy = computed(() => props.backend === 'codebuddy')

const defaultName = computed(() => t(NAME_KEYS[props.backend] ?? 'settings.items.providerName'))

const hint = computed(() => (isCodeBuddy.value
  ? t('settings.items.codebuddyFormHint')
  : t('settings.items.accountKeyOnlyHint', { name: defaultName.value })))

const draft = reactive({
  name: '',
  deployment: '',
  baseUrl: '',
  apiKey: '',
})

const saving = ref(false)
const showKey = ref(false)
const error = ref('')
/** Row id the last successful save produced. Kept as an id (not a boolean) so
 *  switching to another account hides the note instead of claiming it saved. */
const savedId = ref<number | null>(null)

/**
 * The row this panel owns: the one the user clicked, else the first account of
 * this backend. A legacy row with no backend is adopted by the panel it was
 * opened from — that is how such a row gets assigned an owner.
 */
const target = computed<ProviderRow | null>(() => {
  const picked = props.selectedId === null ? undefined : props.providers.find((p) => p.id === props.selectedId)
  if (picked && picked.protocol === 'private' && (!picked.backend || picked.backend === props.backend)) {
    return picked
  }
  return props.providers.find((p) => p.protocol === 'private' && p.backend === props.backend) ?? null
})

const needsEndpoint = computed(() => isCodeBuddy.value && NEEDS_ENDPOINT.includes(draft.deployment))

const canSave = computed(() => {
  if (!draft.name.trim()) return false
  if (needsEndpoint.value && !/^https?:\/\/\S+$/i.test(draft.baseUrl.trim())) return false
  // A new account has no stored credential to fall back on.
  if (!target.value && !draft.apiKey.trim()) return false
  return true
})

/** Re-seed whenever the panel starts owning a different row (open, save, or the
 *  user clicking another account in the list). */
watch(
  () => target.value?.id ?? null,
  () => seed(),
  { immediate: true },
)

function seed() {
  const p = target.value
  draft.name = p?.name ?? defaultName.value
  draft.deployment = p?.deployment ?? ''
  draft.baseUrl = p?.baseUrl ?? ''
  // The stored key never travels to the list; an empty box means "keep it".
  draft.apiKey = ''
  showKey.value = false
  error.value = ''
}

function applyToAgents() {
  if (saving.value || !target.value) return
  emit('apply', target.value.id)
}

async function save() {
  if (!canSave.value || saving.value) return
  saving.value = true
  error.value = ''
  const deployment = isCodeBuddy.value ? draft.deployment : ''
  const baseUrl = needsEndpoint.value ? draft.baseUrl.trim() : ''
  try {
    let savedRowId: number
    if (target.value) {
      const updated = await apiPut<ProviderRow>(`/api/providers/${target.value.id}`, {
        name: draft.name.trim(),
        protocol: 'private',
        backend: props.backend,
        deployment,
        baseUrl,
        // Empty means "keep the stored key" (the server ignores an empty one).
        apiKey: draft.apiKey,
      })
      savedRowId = updated.id
    } else {
      const created = await apiPost<ProviderRow>('/api/providers', {
        name: draft.name.trim(),
        protocol: 'private',
        backend: props.backend,
        deployment,
        baseUrl,
        apiKey: draft.apiKey,
      })
      savedRowId = created.id
    }
    savedId.value = savedRowId
    emit('saved', savedRowId)
  } catch (err) {
    error.value = err instanceof Error && err.message ? err.message : String(err)
    appLog.w('OwnServiceAccountForm', 'save failed', error.value)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
/* Metrics mirror ProviderFormDialog (.provider-form / .provider-field /
   .provider-input) so the two credential forms read as one family. */
.oa-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-5) var(--space-6) var(--space-6);
}
.oa-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}
.oa-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.oa-label {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}
.oa-input {
  box-sizing: border-box;
  width: 100%;
  height: calc(34 * var(--dp));
  padding: 0 var(--space-5);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-lg);
  font-family: inherit;
  min-width: 0;
}
.oa-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
.oa-select {
  cursor: pointer;
}
.oa-key-field {
  position: relative;
  display: flex;
  align-items: center;
}
.oa-key-field > .oa-input {
  padding-right: calc(36 * var(--dp));
}
.oa-key-toggle {
  position: absolute;
  right: calc(4 * var(--dp));
  display: flex;
  align-items: center;
  justify-content: center;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.oa-key-toggle:hover {
  color: var(--text-primary);
}
.oa-key-state {
  font-size: var(--font-size-xs);
  color: var(--color-orange);
}
.oa-key-state--ok {
  color: var(--color-green);
}
.oa-hint {
  margin: 0;
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  color: var(--text-muted);
}
.oa-error {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-red);
}
.oa-ok {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-green);
}
.oa-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
}
.oa-spin {
  animation: oa-spin 1s linear infinite;
}
@keyframes oa-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>

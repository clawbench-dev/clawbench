<template>
  <!-- CodeBuddy's own-service account, configured on its own screen instead of
       through the generic "private protocol" path: that path can express a
       credential but not WHICH edition of the service to talk to, nor the
       address the enterprise editions live at.

       One row per account, edited in place. A private-protocol provider is the
       same thing in the database, so binding, listing and deletion stay in the
       shared code paths. -->
  <div class="cb-form">
    <p class="cb-hint">{{ t('settings.items.codebuddyFormHint') }}</p>

    <label class="cb-field">
      <span class="cb-label">{{ t('settings.items.providerName') }}</span>
      <input
        v-model="draft.name"
        class="cb-input"
        data-role="cb-name"
        :placeholder="t('settings.items.codebuddyNamePlaceholder')"
      />
    </label>

    <label class="cb-field">
      <span class="cb-label">{{ t('settings.items.codebuddyProtocolType') }}</span>
      <select v-model="draft.deployment" class="cb-input cb-select" data-role="cb-deployment">
        <option v-for="d in DEPLOYMENTS" :key="d.value" :value="d.value">{{ t(d.label) }}</option>
      </select>
      <span class="cb-hint">{{ t('settings.items.codebuddyDeploymentHint') }}</span>
    </label>

    <!-- Only the enterprise editions have an address of their own; the others
         use a fixed official host, so showing the box there would invite a
         value nothing reads. -->
    <label v-if="needsEndpoint" class="cb-field">
      <span class="cb-label">{{ t('settings.items.codebuddyDomain') }}</span>
      <input
        v-model="draft.baseUrl"
        class="cb-input"
        data-role="cb-domain"
        inputmode="url"
        autocapitalize="off"
        spellcheck="false"
        :placeholder="t('settings.items.codebuddyDomainPlaceholder')"
      />
      <span class="cb-hint">{{ t('settings.items.codebuddyDomainHint') }}</span>
    </label>

    <div class="cb-field">
      <div class="cb-label-row">
        <span class="cb-label">{{ t('settings.items.providerKey') }}</span>
        <span v-if="target" class="cb-key-state" :class="{ 'cb-key-state--ok': target.hasKey }">
          {{ target.hasKey ? t('settings.items.providerKeyStateSaved') : t('settings.items.providerKeyStateMissing') }}
        </span>
      </div>
      <div class="cb-key-field">
        <input
          v-model="draft.apiKey"
          :type="showKey ? 'text' : 'password'"
          class="cb-input"
          data-role="cb-key"
          autocomplete="off"
          :placeholder="target ? t('settings.items.codebuddyKeyKeep') : t('settings.items.providerKey')"
        />
        <button
          type="button"
          class="cb-key-toggle"
          :title="showKey ? t('settings.items.providerKeyHide') : t('settings.items.providerKeyReveal')"
          :aria-label="showKey ? t('settings.items.providerKeyHide') : t('settings.items.providerKeyReveal')"
          @click="showKey = !showKey"
        >
          <component :is="showKey ? EyeOff : Eye" :size="15" />
        </button>
      </div>
    </div>

    <p v-if="error" class="cb-error" data-role="cb-error">{{ error }}</p>
    <p v-else-if="savedId !== null && savedId === target?.id" class="cb-ok" data-role="cb-saved">
      {{ t('settings.items.codebuddySaved') }}
    </p>

    <div class="cb-actions">
      <button
        type="button"
        class="fbtn fbtn-primary"
        data-action="cb-save"
        :disabled="!canSave || saving"
        @click="save"
      >
        <Loader2 v-if="saving" :size="13" class="cb-spin" />
        {{ target ? t('settings.items.providerSave') : t('settings.items.providerAdd') }}
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
  /** Every configured provider; the form edits the private-protocol one. */
  providers: ProviderRow[]
  /** Row the user asked to edit (a private row clicked in the general list). */
  selectedId: number | null
}>()

/** `saved` carries the row's id: the parent keeps that account selected so the
 *  form does not jump to another one after the list reloads. */
const emit = defineEmits<{ saved: [id: number] }>()

const { t } = useI18n()

/**
 * The dropdown options are the editions CodeBuddy itself documents. "" is the
 * hosted service every existing row already used, so it stays the default and
 * needs no address.
 */
const DEPLOYMENTS = [
  { value: '', label: 'settings.items.codebuddyDeploymentPublic' },
  { value: 'internal', label: 'settings.items.codebuddyDeploymentInternal' },
  { value: 'ioa', label: 'settings.items.codebuddyDeploymentIOA' },
  { value: 'cloudhosted', label: 'settings.items.codebuddyDeploymentDedicated' },
  { value: 'selfhosted', label: 'settings.items.codebuddyDeploymentSelfHosted' },
] as const

const NEEDS_ENDPOINT = ['cloudhosted', 'selfhosted']

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

/** The row this form owns: the one the user clicked, else the first account. */
const target = computed<ProviderRow | null>(() => {
  const picked = props.selectedId === null ? undefined : props.providers.find((p) => p.id === props.selectedId)
  if (picked && picked.protocol === 'private') return picked
  return props.providers.find((p) => p.protocol === 'private') ?? null
})

const needsEndpoint = computed(() => NEEDS_ENDPOINT.includes(draft.deployment))

const canSave = computed(() => {
  if (!draft.name.trim()) return false
  if (needsEndpoint.value && !/^https?:\/\/\S+$/i.test(draft.baseUrl.trim())) return false
  // A new account has no stored credential to fall back on.
  if (!target.value && !draft.apiKey.trim()) return false
  return true
})

/** Re-seed whenever the form starts owning a different row (open, save, or the
 *  user clicking another account in the list). */
watch(
  () => target.value?.id ?? null,
  () => seed(),
  { immediate: true },
)

function seed() {
  const p = target.value
  draft.name = p?.name ?? 'CodeBuddy'
  draft.deployment = p?.deployment ?? ''
  draft.baseUrl = p?.baseUrl ?? ''
  // The stored key never travels to the list; an empty box means "keep it".
  draft.apiKey = ''
  showKey.value = false
  error.value = ''
}

async function save() {
  if (!canSave.value || saving.value) return
  saving.value = true
  error.value = ''
  const deployment = draft.deployment
  const baseUrl = needsEndpoint.value ? draft.baseUrl.trim() : ''
  try {
    let savedRowId: number
    if (target.value) {
      const updated = await apiPut<ProviderRow>(`/api/providers/${target.value.id}`, {
        name: draft.name.trim(),
        protocol: 'private',
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
    appLog.w('CodeBuddyProviderForm', 'save failed', error.value)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
/* Metrics mirror ProviderFormDialog (.provider-form / .provider-field /
   .provider-input) so the two credential forms read as one family. */
.cb-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-5) var(--space-6) var(--space-6);
}
.cb-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}
.cb-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.cb-label {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}
.cb-input {
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
.cb-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
.cb-select {
  cursor: pointer;
}
.cb-key-field {
  position: relative;
  display: flex;
  align-items: center;
}
.cb-key-field > .cb-input {
  padding-right: calc(36 * var(--dp));
}
.cb-key-toggle {
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
.cb-key-toggle:hover {
  color: var(--text-primary);
}
.cb-key-state {
  font-size: var(--font-size-xs);
  color: var(--color-orange);
}
.cb-key-state--ok {
  color: var(--color-green);
}
.cb-hint {
  margin: 0;
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  color: var(--text-muted);
}
.cb-error {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-red);
}
.cb-ok {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-green);
}
.cb-actions {
  display: flex;
  justify-content: flex-end;
}
.cb-spin {
  animation: cb-spin 1s linear infinite;
}
@keyframes cb-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>

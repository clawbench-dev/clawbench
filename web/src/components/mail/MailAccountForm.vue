<template>
  <div class="mail-form">
    <section class="mail-section">
      <h3 class="mail-section-title">{{ t('mail.setup.sectionAccount') }}</h3>

      <div class="mail-field">
        <span class="mail-label">{{ t('mail.setup.provider') }}</span>
        <MenuSelect
          :model-value="presetId"
          :options="presetOptions"
          block
          @update:model-value="onPresetChange"
        />
      </div>
      <p v-if="activeHint" class="mail-hint">{{ activeHint }}</p>

      <label class="mail-field">
        <span class="mail-label">{{ t('mail.setup.email') }}</span>
        <input
          v-model.trim="form.email"
          class="mail-input"
          type="email"
          autocomplete="username"
          :placeholder="t('mail.setup.emailPlaceholder')"
          @blur="onEmailBlur"
        />
      </label>

      <label class="mail-field">
        <span class="mail-label">{{ t('mail.setup.password') }}</span>
        <input
          v-model="form.password"
          class="mail-input"
          type="password"
          autocomplete="current-password"
          :placeholder="editing ? t('mail.setup.passwordKeep') : t('mail.setup.passwordPlaceholder')"
        />
      </label>

      <label class="mail-field">
        <span class="mail-label">{{ t('mail.setup.displayName') }}</span>
        <input
          v-model.trim="form.name"
          class="mail-input"
          type="text"
          :placeholder="t('mail.setup.displayNamePlaceholder')"
        />
      </label>
    </section>

    <section class="mail-section">
      <div class="mail-section-head">
        <h3 class="mail-section-title">{{ t('mail.setup.sectionServers') }}</h3>
        <button type="button" class="mail-link" @click="advanced = !advanced">
          {{ advanced ? t('mail.setup.hideAdvanced') : t('mail.setup.showAdvanced') }}
        </button>
      </div>

      <template v-if="advanced">
        <div class="mail-field">
          <span class="mail-label">{{ t('mail.setup.imapHost') }}</span>
          <div class="mail-field-row">
            <input v-model.trim="form.imapHost" class="mail-input" type="text" placeholder="imap.example.com" />
            <input v-model.number="form.imapPort" class="mail-input mail-input--port" type="number" />
          </div>
        </div>
        <div class="mail-field">
          <span class="mail-label">{{ t('mail.setup.imapSecurity') }}</span>
          <MenuSelect
            :model-value="form.imapSecurity"
            :options="securityOptions"
            block
            @update:model-value="(v) => (form.imapSecurity = v as MailSecurity)"
          />
        </div>

        <div class="mail-field">
          <span class="mail-label">{{ t('mail.setup.smtpHost') }}</span>
          <div class="mail-field-row">
            <input v-model.trim="form.smtpHost" class="mail-input" type="text" placeholder="smtp.example.com" />
            <input v-model.number="form.smtpPort" class="mail-input mail-input--port" type="number" />
          </div>
        </div>
        <div class="mail-field">
          <span class="mail-label">{{ t('mail.setup.smtpSecurity') }}</span>
          <MenuSelect
            :model-value="form.smtpSecurity"
            :options="securityOptions"
            block
            @update:model-value="(v) => (form.smtpSecurity = v as MailSecurity)"
          />
        </div>

        <label class="mail-check">
          <input v-model="form.insecureTls" type="checkbox" />
          <span>{{ t('mail.setup.insecureTls') }}</span>
        </label>
        <p class="mail-hint">{{ t('mail.setup.insecureTlsHint') }}</p>
      </template>
      <p v-else-if="serverSummary" class="mail-hint">{{ serverSummary }}</p>
    </section>

    <p v-if="error" class="mail-error">{{ error }}</p>
    <p v-if="testOk" class="mail-ok">{{ t('mail.setup.testOk') }}</p>

    <div class="mail-actions">
      <button v-if="showCancel" class="fbtn" :disabled="saving" @click="emit('cancel')">
        {{ t('common.cancel') }}
      </button>
      <button class="fbtn fbtn-primary" :disabled="saving" @click="save">
        {{ saving ? t('mail.setup.saving') : t('mail.setup.saveAndTest') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The mailbox form, used by the settings page's "add / edit" view.
 *
 * Field metrics, section rules and button style are copied from
 * ProviderFormDialog: it is the settings area's only other endpoint+credential
 * form, and an account form that looked different from it was the reason this
 * page read as bolted on.
 */
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import MenuSelect from '@/components/common/MenuSelect.vue'
import { appLog } from '@/utils/appLog'
import {
  mailErrorMessage,
  type MailAccount,
  type MailAccountInput,
  type MailPreset,
  type MailSecurity,
} from '@/utils/mailTypes'
import { useMailAccounts } from '@/composables/useMailAccounts'
import { refreshMailUnread } from '@/composables/useMailUnread'

const props = defineProps<{
  account?: MailAccount | null
  presets: MailPreset[]
  showCancel?: boolean
}>()
const emit = defineEmits<{ saved: [account: MailAccount]; cancel: [] }>()

const { t } = useI18n()
const accountsApi = useMailAccounts()

const editing = computed(() => !!props.account)
const presetId = ref(guessPreset(props.account?.email ?? '', props.presets))
const advanced = ref(false)
const saving = ref(false)
const error = ref('')
const testOk = ref(false)

const form = reactive<MailAccountInput>({
  name: props.account?.name ?? '',
  email: props.account?.email ?? '',
  username: props.account?.username ?? '',
  password: '',
  imapHost: props.account?.imapHost ?? '',
  imapPort: props.account?.imapPort ?? 993,
  imapSecurity: props.account?.imapSecurity ?? 'tls',
  smtpHost: props.account?.smtpHost ?? '',
  smtpPort: props.account?.smtpPort ?? 465,
  smtpSecurity: props.account?.smtpSecurity ?? 'tls',
  insecureTls: props.account?.insecureTls ?? false,
  enabled: props.account?.enabled ?? true,
})

const presetOptions = computed(() => props.presets.map((p) => ({ value: p.id, label: p.label })))
const securityOptions = computed(() => [
  { value: 'tls', label: 'SSL/TLS' },
  { value: 'starttls', label: 'STARTTLS' },
  { value: 'none', label: t('mail.setup.noEncryption') },
])

const activePreset = computed(() => props.presets.find((p) => p.id === presetId.value) ?? null)
const activeHint = computed(() => {
  const key = activePreset.value?.hintKey
  return key ? t(key) : ''
})

/** One line describing the servers while the section is collapsed. */
const serverSummary = computed(() => {
  if (!form.imapHost && !form.smtpHost) return ''
  return `IMAP ${form.imapHost || '—'}:${form.imapPort} · SMTP ${form.smtpHost || '—'}:${form.smtpPort}`
})

function guessPreset(email: string, presets: MailPreset[]): string {
  const domain = email.split('@')[1]?.toLowerCase() ?? ''
  const hit = presets.find((p) => p.domains?.some((d) => domain === d || domain.endsWith(`.${d}`)))
  return hit?.id ?? 'custom'
}

/** Picking a provider fills the server fields; the user can still edit them. */
function onPresetChange(value: string | number): void {
  presetId.value = String(value)
  applyPreset()
}

function applyPreset(): void {
  const p = activePreset.value
  if (!p || !p.imapHost) return
  form.imapHost = p.imapHost
  form.imapPort = p.imapPort
  form.imapSecurity = p.imapSecurity
  form.smtpHost = p.smtpHost
  form.smtpPort = p.smtpPort
  form.smtpSecurity = p.smtpSecurity
}

// Fill the servers for a fresh install where the email is typed after picking
// a provider: apply on blur as well as on select.
function onEmailBlur(): void {
  if (form.imapHost) return
  presetId.value = guessPreset(form.email, props.presets)
  applyPreset()
}

async function save(): Promise<void> {
  error.value = ''
  testOk.value = false
  if (!form.email || !form.email.includes('@')) {
    error.value = t('mail.setup.invalidEmail')
    return
  }
  if (!form.imapHost || !form.smtpHost) {
    applyPreset()
    if (!form.imapHost || !form.smtpHost) {
      error.value = t('mail.setup.missingServers')
      return
    }
  }
  saving.value = true
  try {
    const payload: MailAccountInput = {
      ...form,
      username: form.username || form.email,
      name: form.name || form.email,
    }
    const saved = props.account
      ? await accountsApi.update(props.account.id, payload)
      : await accountsApi.create(payload)
    try {
      await accountsApi.test(saved.id)
      testOk.value = true
    } catch (e) {
      // Saved but unreachable: keep the account so the user can fix the
      // settings instead of retyping everything.
      error.value = mailErrorMessage(e, t)
      emit('saved', saved)
      return
    }
    emit('saved', saved)
    void refreshMailUnread()
  } catch (e) {
    appLog.w('MailAccountForm', 'save failed', e)
    error.value = mailErrorMessage(e, t)
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
/* Metrics mirror ProviderFormDialog (.provider-form / .provider-section /
   .provider-field / .provider-input) so the two credential forms read as one
   family. */
.mail-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
  padding: var(--space-5) var(--space-6) var(--space-6);
}
.mail-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.mail-section + .mail-section {
  padding-top: var(--space-5);
  border-top: 1px solid var(--border-color);
}
.mail-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.mail-section-title {
  margin: 0;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted);
  letter-spacing: calc(0.4 * var(--dp));
}
.mail-link {
  border: none;
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  cursor: pointer;
  padding: 0;
}
.mail-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}
.mail-field-row {
  display: flex;
  gap: var(--space-3);
}
.mail-label {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}
.mail-input {
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
.mail-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 calc(2 * var(--dp)) var(--focus-ring);
}
.mail-input--port {
  width: calc(96 * var(--dp));
  flex: none;
}
.mail-check {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}
.mail-hint {
  margin: 0;
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  color: var(--text-muted);
}
.mail-error {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-red);
}
.mail-ok {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-green);
}
.mail-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
}
</style>

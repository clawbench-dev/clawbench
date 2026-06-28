<template>
  <div class="password-dialog-overlay" @click.self="handleClose">
    <div class="password-dialog">
      <div class="password-dialog__header">{{ t('settings.changePasswordTitle') }}</div>

      <div class="password-dialog__field">
        <label class="password-dialog__label">{{ t('settings.currentPassword') }}</label>
        <div class="password-dialog__input-row">
          <input
            :type="showCurrent ? 'text' : 'password'"
            class="password-dialog__input"
            v-model="currentPassword"
            :placeholder="t('settings.currentPasswordPlaceholder')"
            @keydown.enter="focusNew"
            autocomplete="current-password"
          />
          <button class="password-dialog__eye" @click="showCurrent = !showCurrent" type="button" tabindex="-1">
            <component :is="showCurrent ? EyeOff : Eye" :size="18" />
          </button>
        </div>
      </div>

      <div class="password-dialog__field">
        <label class="password-dialog__label">{{ t('settings.newPassword') }}</label>
        <div class="password-dialog__input-row">
          <input
            ref="newPasswordRef"
            :type="showNew ? 'text' : 'password'"
            class="password-dialog__input"
            v-model="newPassword"
            :placeholder="t('settings.newPasswordPlaceholder')"
            @keydown.enter="focusConfirm"
            autocomplete="new-password"
          />
          <button class="password-dialog__eye" @click="showNew = !showNew" type="button" tabindex="-1">
            <component :is="showNew ? EyeOff : Eye" :size="18" />
          </button>
        </div>
        <div v-if="newPasswordErrors.length" class="password-dialog__hints">
          <div v-for="err in newPasswordErrors" :key="err" class="password-dialog__hint password-dialog__hint--error">
            {{ err }}
          </div>
        </div>
        <div v-if="newPasswordStrength" class="password-dialog__strength">
          <div class="password-dialog__strength-bar">
            <div
              class="password-dialog__strength-fill"
              :class="`password-dialog__strength-fill--${newPasswordStrength}`"
            ></div>
          </div>
          <span class="password-dialog__strength-label">{{ strengthLabel }}</span>
        </div>
      </div>

      <div class="password-dialog__field">
        <label class="password-dialog__label">{{ t('settings.confirmPassword') }}</label>
        <div class="password-dialog__input-row">
          <input
            ref="confirmPasswordRef"
            :type="showConfirm ? 'text' : 'password'"
            class="password-dialog__input"
            v-model="confirmPassword"
            :placeholder="t('settings.confirmPasswordPlaceholder')"
            @keydown.enter="submit"
            autocomplete="new-password"
          />
          <button class="password-dialog__eye" @click="showConfirm = !showConfirm" type="button" tabindex="-1">
            <component :is="showConfirm ? EyeOff : Eye" :size="18" />
          </button>
        </div>
        <div v-if="confirmPasswordError" class="password-dialog__hint password-dialog__hint--error">
          {{ confirmPasswordError }}
        </div>
      </div>

      <div v-if="localError" class="password-dialog__error">{{ localError }}</div>
      <div v-if="serverError" class="password-dialog__error">{{ serverError }}</div>

      <div class="password-dialog__actions">
        <button class="password-dialog__btn password-dialog__btn--cancel" @click="handleClose" :disabled="submitting">
          {{ t('common.cancel') }}
        </button>
        <button
          class="password-dialog__btn password-dialog__btn--submit"
          :disabled="!canSubmit || submitting"
          @click="submit"
        >
          {{ submitting ? t('settings.changingPassword') : t('settings.changePasswordBtn') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiPost } from '@/utils/api'
import { Eye, EyeOff } from 'lucide-vue-next'

const emit = defineEmits<{
  close: []
  changed: [needsRestart: boolean]
}>()

const { t } = useI18n()

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const submitting = ref(false)
const localError = ref('')
const serverError = ref('')

const showCurrent = ref(false)
const showNew = ref(false)
const showConfirm = ref(false)

const newPasswordRef = ref<HTMLInputElement | null>(null)
const confirmPasswordRef = ref<HTMLInputElement | null>(null)

function focusNew() {
  newPasswordRef.value?.focus()
}

function focusConfirm() {
  confirmPasswordRef.value?.focus()
}

// --- Validation helpers ---

function hasLetter(s: string): boolean {
  return /[a-zA-Z]/.test(s)
}

function hasDigit(s: string): boolean {
  return /[0-9]/.test(s)
}

// --- Real-time per-field validation ---

const newPasswordErrors = computed(() => {
  if (newPassword.value === '') return []
  const errors: string[] = []
  if (newPassword.value.length < 8) errors.push(t('settings.passwordTooShort'))
  if (newPassword.value.length > 32) errors.push(t('settings.passwordTooLong'))
  if (!hasLetter(newPassword.value) || !hasDigit(newPassword.value)) errors.push(t('settings.passwordNoLetterDigit'))
  return errors
})

const confirmPasswordError = computed(() => {
  if (confirmPassword.value === '') return ''
  if (newPassword.value !== confirmPassword.value) return t('settings.passwordMismatch')
  return ''
})

// --- Password strength indicator ---

const newPasswordStrength = computed<'weak' | 'medium' | 'strong' | null>(() => {
  if (newPasswordErrors.value.length > 0 || newPassword.value === '') return null
  const len = newPassword.value.length
  if (len >= 20) return 'strong'
  if (len >= 12) return 'medium'
  return 'weak'
})

const strengthLabel = computed(() => {
  switch (newPasswordStrength.value) {
    case 'weak': return t('settings.passwordStrengthWeak')
    case 'medium': return t('settings.passwordStrengthMedium')
    case 'strong': return t('settings.passwordStrengthStrong')
    default: return ''
  }
})

// --- Submit logic ---

const newPasswordValid = computed(() => {
  return newPassword.value.length >= 8 &&
    newPassword.value.length <= 32 &&
    hasLetter(newPassword.value) &&
    hasDigit(newPassword.value)
})

const canSubmit = computed(() => {
  return (
    currentPassword.value !== '' &&
    newPasswordValid.value &&
    confirmPassword.value !== '' &&
    newPassword.value === confirmPassword.value
  )
})

function validate(): string | null {
  if (!currentPassword.value) {
    return t('settings.currentPasswordRequired')
  }
  if (newPasswordErrors.value.length > 0) {
    return newPasswordErrors.value[0]
  }
  if (newPassword.value !== confirmPassword.value) {
    return t('settings.passwordMismatch')
  }
  if (newPassword.value === currentPassword.value) {
    return t('settings.passwordSameAsOld')
  }
  return null
}

async function submit() {
  localError.value = ''
  serverError.value = ''

  const validationError = validate()
  if (validationError) {
    localError.value = validationError
    return
  }

  submitting.value = true
  try {
    const result = await apiPost<{ needs_restart?: boolean }>('/api/config/password', {
      current_password: currentPassword.value,
      new_password: newPassword.value,
    })
    emit('changed', result.needs_restart ?? true)
  } catch (err: any) {
    const errorCode = err?.message || ''
    if (errorCode === 'wrong_password') {
      serverError.value = t('settings.wrongCurrentPassword')
    } else if (errorCode === 'password_too_short') {
      serverError.value = t('settings.passwordTooShort')
    } else if (errorCode === 'password_too_long') {
      serverError.value = t('settings.passwordTooLong')
    } else if (errorCode === 'password_no_letter_digit') {
      serverError.value = t('settings.passwordNoLetterDigit')
    } else if (errorCode === 'empty_password') {
      serverError.value = t('settings.currentPasswordRequired')
    } else if (err?.message?.includes('Too Many Requests') || errorCode === 'TooManyLoginAttempts') {
      serverError.value = t('settings.passwordTooManyAttempts')
    } else {
      serverError.value = t('settings.passwordChangeFailed')
    }
  } finally {
    submitting.value = false
  }
}

function handleClose() {
  if (submitting.value) return
  emit('close')
}
</script>

<style scoped>
.password-dialog-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 16px;
}

.password-dialog {
  background: var(--bg-primary);
  border-radius: 16px;
  padding: 24px;
  width: 100%;
  max-width: 380px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
}

.password-dialog__header {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 20px;
  text-align: center;
}

.password-dialog__field {
  margin-bottom: 16px;
}

.password-dialog__label {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.password-dialog__input-row {
  position: relative;
  display: flex;
  align-items: center;
}

.password-dialog__input {
  width: 100%;
  min-width: 0;
  padding: 10px 40px 10px 12px;
  font-size: 15px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
  box-sizing: border-box;
}

.password-dialog__input:focus {
  border-color: var(--accent-color);
}

.password-dialog__eye {
  position: absolute;
  right: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  color: var(--text-tertiary);
  cursor: pointer;
  padding: 2px;
}

.password-dialog__eye:hover {
  color: var(--text-secondary);
}

.password-dialog__hints {
  margin-top: 4px;
}

.password-dialog__hint {
  font-size: 12px;
  margin-top: 2px;
  padding-left: 2px;
}

.password-dialog__hint--error {
  color: var(--color-red, #e74c3c);
}

.password-dialog__strength {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
}

.password-dialog__strength-bar {
  flex: 1;
  height: 4px;
  background: var(--bg-tertiary);
  border-radius: 2px;
  overflow: hidden;
}

.password-dialog__strength-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.2s, background 0.2s;
}

.password-dialog__strength-fill--weak {
  width: 33%;
  background: var(--color-red, #e74c3c);
}

.password-dialog__strength-fill--medium {
  width: 66%;
  background: var(--color-orange, #f39c12);
}

.password-dialog__strength-fill--strong {
  width: 100%;
  background: var(--color-green, #27ae60);
}

.password-dialog__strength-label {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.password-dialog__error {
  font-size: 13px;
  color: var(--color-red, #e74c3c);
  margin-bottom: 12px;
  padding: 8px 12px;
  background: color-mix(in srgb, var(--color-red, #e74c3c) 10%, transparent);
  border-radius: 8px;
}

.password-dialog__actions {
  display: flex;
  gap: 12px;
  margin-top: 20px;
}

.password-dialog__btn {
  flex: 1;
  padding: 12px;
  border: none;
  border-radius: 10px;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
}

.password-dialog__btn--cancel {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.password-dialog__btn--submit {
  background: var(--accent-color);
  color: #fff;
}

.password-dialog__btn--submit:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .password-dialog__btn--cancel:hover {
    background: var(--bg-secondary);
  }
  .password-dialog__btn--submit:not(:disabled):hover {
    background: var(--accent-hover);
  }
}

.password-dialog__btn--cancel:active {
  background: var(--bg-secondary);
}

.password-dialog__btn--submit:not(:disabled):active {
  background: var(--accent-hover);
}
</style>

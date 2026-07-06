<template>
  <Teleport to="body">
    <Transition name="dlg">
      <div v-if="visible" class="group-dialog-overlay" @click.self="handleClose">
        <div class="group-dialog">
          <div class="group-dialog__title">{{ t(titleKey) }}</div>

          <div class="group-dialog__fields">
            <div v-for="field in fieldSpecs" :key="field.key" class="group-dialog__field">
              <label class="group-dialog__label">
                <span v-if="isRequired(field.key)" class="group-dialog__required">*</span>
                {{ t(field.labelKey) }}
              </label>
              <label v-if="field.type === 'switch'" class="group-dialog__switch">
                <input type="checkbox" class="group-dialog__switch-input" v-model="editValues[field.key]" @change="touched = true" />
                <span class="group-dialog__switch-track"></span>
              </label>
              <input
                v-else-if="field.type === 'password'"
                :type="showPassword[field.key] ? 'text' : 'password'"
                class="group-dialog__input"
                :class="{ 'group-dialog__input--error': isRequired(field.key) && editValues[field.key] === '' && touched }"
                v-model="editValues[field.key]"
                :placeholder="t(field.descriptionKey || field.labelKey)"
                @input="touched = true"
              />
              <input
                v-else-if="field.type === 'number'"
                type="number"
                class="group-dialog__input"
                :class="{ 'group-dialog__input--error': isRequired(field.key) && editValues[field.key] === '' && touched }"
                v-model="editValues[field.key]"
                :min="field.min"
                :max="field.max"
                :step="field.step"
                :placeholder="t(field.descriptionKey || field.labelKey)"
                @input="touched = true"
              />
              <select
                v-else-if="field.type === 'select'"
                class="group-dialog__input group-dialog__select"
                :class="{ 'group-dialog__input--error': isRequired(field.key) && editValues[field.key] === '' && touched }"
                v-model="editValues[field.key]"
                @change="touched = true"
              >
                <option v-for="opt in field.options" :key="opt.value" :value="opt.value">
                  {{ t(opt.labelKey) }}
                </option>
              </select>
              <input
                v-else
                type="text"
                class="group-dialog__input"
                :class="{ 'group-dialog__input--error': isRequired(field.key) && editValues[field.key] === '' && touched }"
                v-model="editValues[field.key]"
                :placeholder="t(field.descriptionKey || field.labelKey)"
                @input="touched = true"
              />
              <button
                v-if="field.type === 'password'"
                class="group-dialog__eye"
                @click="showPassword[field.key] = !showPassword[field.key]"
                type="button"
                tabindex="-1"
              >
                <component :is="showPassword[field.key] ? EyeOff : Eye" :size="16" />
              </button>
            </div>
          </div>

          <div v-if="serverError" class="group-dialog__error">{{ serverError }}</div>

          <div class="group-dialog__hint">
            {{ t('settings.groupConfig.requiredHint') }}
          </div>

          <div class="group-dialog__actions">
            <button class="group-dialog__btn group-dialog__btn--cancel" @click="handleClose" :disabled="submitting">
              {{ t('common.cancel') }}
            </button>
            <button
              class="group-dialog__btn group-dialog__btn--submit"
              :disabled="!canSubmit || submitting"
              @click="handleSubmit"
            >
              {{ submitting ? '...' : t('settings.groupConfig.confirm') }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Eye, EyeOff } from 'lucide-vue-next'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { apiPatch } from '@/utils/api'
import type { ItemSpec, GroupConfigTrigger } from './settingsFieldMap'

const props = defineProps<{
  visible: boolean
  trigger: GroupConfigTrigger
  triggerItem: ItemSpec
  triggerValue: any
  /** All ItemSpecs for the category (used to look up field definitions) */
  allItems: ItemSpec[]
}>()

const emit = defineEmits<{
  close: []
  saved: [needsRestart: boolean, changedColdFields: string[]]
}>()

const { t } = useI18n()
const { getServerValueWithDefault } = useSettingsConfig()

const titleKey = computed(() => props.trigger.dialogTitleKey)

// Resolve ItemSpec for each field key
const fieldSpecs = computed(() => {
  const specs: ItemSpec[] = []
  for (const key of props.trigger.fields) {
    // Look up from allItems, or create a minimal spec
    const found = props.allItems.find(i => i.key === key)
    if (found) {
      specs.push(found)
    }
  }
  return specs
})

// Edit state
const editValues = reactive<Record<string, any>>({})
const showPassword = reactive<Record<string, boolean>>({})
const touched = ref(false)
const submitting = ref(false)
const serverError = ref('')

// Initialize edit values when dialog opens
watch(() => props.visible, (v) => {
  if (v) {
    touched.value = false
    serverError.value = ''
    for (const field of fieldSpecs.value) {
      editValues[field.key] = getServerValueWithDefault(field.key) ?? (field.type === 'number' ? 0 : '')
      if (field.type === 'password') {
        showPassword[field.key] = false
        // Don't pre-fill password — start empty
        editValues[field.key] = ''
      }
    }
  }
})

function isRequired(key: string): boolean {
  return props.trigger.requiredFields.includes(key)
}

const canSubmit = computed(() => {
  // All required fields must be non-empty
  for (const key of props.trigger.requiredFields) {
    const val = editValues[key]
    if (val === '' || val === undefined || val === null) return false
  }
  return true
})

/** Build nested object from dot-path key + value (e.g., 'frp.server_addr', '1.2.3.4' → { frp: { server_addr: '1.2.3.4' } }) */
function buildNestedObject(entries: [string, any][]): Record<string, any> {
  const result: Record<string, any> = {}
  for (const [dotPath, value] of entries) {
    const parts = dotPath.split('.')
    let obj: any = result
    for (let i = 0; i < parts.length - 1; i++) {
      if (!obj[parts[i]]) obj[parts[i]] = {}
      obj = obj[parts[i]]
    }
    obj[parts[parts.length - 1]] = value
  }
  return result
}

async function handleSubmit() {
  touched.value = true
  if (!canSubmit.value) return

  submitting.value = true
  serverError.value = ''

  try {
    // Build PATCH payload: trigger field + all sub-fields
    const entries: [string, any][] = [
      [props.triggerItem.key, props.triggerValue],
    ]
    for (const field of fieldSpecs.value) {
      let val = editValues[field.key]
      if (field.type === 'number') val = Number(val)
      // Skip empty password (keep existing)
      if (field.type === 'password' && val === '') continue
      entries.push([field.key, val])
    }

    const payload = buildNestedObject(entries)
    const result = await apiPatch<{ needs_restart?: boolean; changed_cold_fields?: string[] }>('/api/config', payload)

    // Optimistically update local cache (no additional server calls — apiPatch already sent)
    const { serverConfig } = useSettingsConfig()
    for (const [dotPath, val] of entries) {
      const parts = dotPath.split('.')
      let obj: any = serverConfig.value
      for (let i = 0; i < parts.length - 1; i++) {
        if (obj && parts[i] in obj) obj = obj[parts[i]]
        else break
      }
      if (obj) obj[parts[parts.length - 1]] = val
    }

    emit('saved', result.needs_restart ?? false, result.changed_cold_fields ?? [])
  } catch (err: any) {
    serverError.value = err?.message || t('settings.saveFailed')
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
.group-dialog-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 3000;
  padding: 16px;
}

.group-dialog {
  background: var(--bg-primary);
  border-radius: 16px;
  padding: 20px 18px 16px;
  width: 100%;
  max-width: 380px;
  max-height: 80vh;
  overflow-y: auto;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.25);
  animation: dlg-in 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.group-dialog__title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 16px;
}

.group-dialog__fields {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.group-dialog__field {
  position: relative;
}

.group-dialog__label {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.group-dialog__required {
  color: var(--color-red, #e74c3c);
  margin-right: 2px;
  font-weight: 600;
}

.group-dialog__input {
  width: 100%;
  min-width: 0;
  padding: 10px 12px;
  font-size: 14px;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  outline: none;
  box-sizing: border-box;
  transition: border-color 0.15s;
}

.group-dialog__input:focus {
  border-color: var(--accent-color);
}

.group-dialog__select {
  appearance: auto;
  cursor: pointer;
}

.group-dialog__input--error {
  border-color: var(--color-red, #e74c3c);
}

/* Switch toggle (same style as SettingsItem) */
.group-dialog__switch {
  position: relative;
  display: inline-block;
  width: 51px;
  height: 31px;
  cursor: pointer;
  flex-shrink: 0;
}

.group-dialog__switch-input {
  opacity: 0;
  width: 0;
  height: 0;
  position: absolute;
}

.group-dialog__switch-track {
  position: absolute;
  inset: 0;
  border-radius: 15.5px;
  background: var(--bg-tertiary);
  transition: background 0.2s ease;
}

.group-dialog__switch-track::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 27px;
  height: 27px;
  border-radius: 50%;
  background: var(--bg-primary);
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
}

.group-dialog__switch-input:checked + .group-dialog__switch-track {
  background: var(--color-green);
}

.group-dialog__switch-input:checked + .group-dialog__switch-track::after {
  transform: translateX(20px);
}

.group-dialog__eye {
  position: absolute;
  right: 10px;
  bottom: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  color: var(--text-tertiary);
  cursor: pointer;
  padding: 2px;
}

.group-dialog__eye:hover {
  color: var(--text-secondary);
}

.group-dialog__error {
  font-size: 13px;
  color: var(--color-red, #e74c3c);
  margin-top: 12px;
  padding: 8px 12px;
  background: color-mix(in srgb, var(--color-red, #e74c3c) 10%, transparent);
  border-radius: 8px;
}

.group-dialog__hint {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 12px;
}

.group-dialog__actions {
  display: flex;
  gap: 10px;
  margin-top: 16px;
}

.group-dialog__btn {
  flex: 1;
  padding: 10px;
  border: none;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.12s;
  -webkit-tap-highlight-color: transparent;
}

.group-dialog__btn:active { opacity: 0.7; }

.group-dialog__btn--cancel {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.group-dialog__btn--submit {
  background: var(--accent-color);
  color: #fff;
}

.group-dialog__btn--submit:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>

<style>
@keyframes dlg-in {
  from { opacity: 0; transform: scale(0.9); }
  to { opacity: 1; transform: scale(1); }
}
</style>

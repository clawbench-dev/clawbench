<template>
  <div class="settings-restart-overlay" @click.self="$emit('later')">
    <div class="settings-restart-dialog">
      <div class="settings-restart-dialog__header">{{ t('settings.restartConfirmTitle') }}</div>
      <p class="settings-restart-dialog__message">{{ t('settings.restartConfirmMessage') }}</p>
      <ul v-if="changedFields.length > 0" class="settings-restart-dialog__list">
        <li v-for="field in displayFields" :key="field">{{ field }}</li>
      </ul>
      <div class="settings-restart-dialog__actions">
        <button class="fbtn settings-restart-dialog__btn settings-restart-dialog__btn--later" @click="$emit('later')">
          {{ t('settings.restartLater') }}
        </button>
        <button class="fbtn fbtn-primary settings-restart-dialog__btn settings-restart-dialog__btn--restart" @click="$emit('restart')">
          {{ t('settings.restartNow') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { serverFieldToLabelKey } from './settingsFieldMap'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import '@/assets/modal-footer-btn.css'

const props = defineProps<{
  changedFields: string[]
}>()

const emit = defineEmits<{
  restart: []
  later: []
}>()

const { t } = useI18n()
let unregisterBack: (() => void) | null = null

onMounted(() => {
  unregisterBack = registerBackHandler({
    id: 'settings-restart-dialog',
    canGoBack: () => true,
    goBack: () => emit('later'),
    priority: PRIORITY_OVERLAY,
  })
})

onBeforeUnmount(() => {
  if (unregisterBack) { unregisterBack(); unregisterBack = null }
})

const displayFields = computed(() =>
  props.changedFields.map(key => {
    const labelKey = serverFieldToLabelKey[key]
    return labelKey ? t(labelKey) : key
  })
)
</script>

<style scoped>
.settings-restart-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10;
  -webkit-backdrop-filter: blur(4px);
  backdrop-filter: blur(4px);
}

.settings-restart-dialog {
  background: var(--bg-primary);
  border-radius: 14px;
  padding: 20px;
  margin: 24px;
  max-width: 320px;
  width: 100%;
  box-shadow: var(--shadow-md);
}

.settings-restart-dialog__header {
  font-size: 17px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 8px;
  text-align: center;
}

.settings-restart-dialog__message {
  font-size: 14px;
  color: var(--text-secondary);
  margin: 0 0 12px;
  text-align: center;
}

.settings-restart-dialog__list {
  margin: 0 0 20px;
  padding-left: 20px;
  font-size: 14px;
  color: var(--text-secondary);
  line-height: 1.6;
}

.settings-restart-dialog__list li {
  margin-bottom: 2px;
}

.settings-restart-dialog__actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Layout only — visuals come from the shared .fbtn pills. */
.settings-restart-dialog__btn {
  width: 100%;
}
</style>

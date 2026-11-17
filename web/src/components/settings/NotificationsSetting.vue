<template>
  <div class="notifications-setting">
    <SettingsCard :title="t('settings.items.notificationsLogSection')">
      <p class="notifications-note">{{ t('settings.items.notificationsLogDesc') }}</p>
      <p class="notifications-note">{{ t('settings.items.notificationsRetention') }}</p>
      <div class="notifications-actions">
        <button class="fbtn" type="button" :disabled="clearing" @click="clear">
          {{ clearing ? t('settings.items.notificationsClearing') : t('settings.items.notificationsClear') }}
        </button>
      </div>
    </SettingsCard>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiDelete } from '@/utils/api'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import SettingsCard from './SettingsCard.vue'
import '@/assets/modal-footer-btn.css'

const { t } = useI18n()
const toast = useToast()
const dialog = useDialog()

const clearing = ref(false)

async function clear(): Promise<void> {
  const ok = await dialog.confirm(t('settings.items.notificationsClearConfirm'), {
    title: t('settings.items.notificationsClear'),
    confirmText: t('settings.items.notificationsClear'),
    dangerous: true,
  })
  if (!ok) return
  clearing.value = true
  try {
    await apiDelete('/api/notifications')
    toast.show(t('settings.items.notificationsCleared'), { icon: 'success', type: 'success' })
  } catch {
    toast.show(t('settings.saveFailed'), { icon: 'warning', type: 'error' })
  } finally {
    clearing.value = false
  }
}
</script>

<style scoped>
.notifications-setting {
  padding: var(--space-4);
  /* Shared page surface, same as the standard `.settings-category` wrapper this
     custom plugin page replaces. Using --bg-secondary here painted the whole
     page as one big card-coloured slab, so the card inside had no contrast and
     the tab looked darker than every other settings tab. */
  background: var(--panel-page-bg);
  min-height: 100%;
}

.notifications-setting > :deep(.settings-card:last-child) {
  margin-bottom: 0;
}

.notifications-note {
  margin: 0;
  padding: var(--space-3) var(--space-7) 0;
  font-size: var(--font-size-sm);
  line-height: 1.5;
  color: var(--text-muted);
}

.notifications-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-4);
  padding: var(--space-5) var(--space-7) var(--space-6);
}
</style>

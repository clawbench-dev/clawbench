<template>
  <div v-if="supported" class="native-notification-status" role="status" aria-live="polite">
    <dl class="native-notification-status__values">
      <div class="native-notification-status__row">
        <dt>{{ t('settings.items.nativeNotificationPermission') }}</dt>
        <dd>{{ t(permissionLabelKey) }}</dd>
      </div>
      <div class="native-notification-status__row">
        <dt>{{ t('settings.items.nativeNotificationDelivery') }}</dt>
        <dd>{{ t(deliveryLabelKey) }}</dd>
      </div>
    </dl>
    <p v-if="status?.permission === 'denied' && !readError" class="native-notification-status__hint">
      {{ t('settings.items.nativeNotificationDeniedHint') }}
    </p>
    <p v-if="status?.backgroundDelivery === 'while-process-running'" class="native-notification-status__hint">
      {{ t('settings.items.nativeNotificationProcessLimit') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useNativeNotificationStatus } from '@/composables/useNativeNotificationStatus'

const { t } = useI18n()
const { supported, status, readError, permissionLabelKey, deliveryLabelKey } = useNativeNotificationStatus()
</script>

<style scoped>
.native-notification-status {
  padding: var(--space-4) var(--space-7);
}

.native-notification-status__values {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  margin: 0;
  font-size: var(--font-size-md);
}

.native-notification-status__row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-6);
}

.native-notification-status__row dt {
  color: var(--text-primary);
}

.native-notification-status__row dd {
  margin: 0;
  color: var(--text-secondary);
}

.native-notification-status__row dt,
.native-notification-status__row dd {
  min-width: 0;
  overflow-wrap: anywhere;
}

.native-notification-status__hint {
  margin: var(--space-4) 0 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  overflow-wrap: anywhere;
}
</style>

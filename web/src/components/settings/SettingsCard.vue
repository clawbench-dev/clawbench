<template>
  <div class="settings-card">
    <div class="settings-card__body">
      <div v-if="title" class="settings-card__header">{{ title }}</div>
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  title?: string
}>()
</script>

<style scoped>
.settings-card {
  margin-bottom: var(--space-4);
}

/* Compact iOS-style card: white rounded body on the grey page background */
.settings-card__body {
  border-radius: 0;
  overflow: hidden;
  background: var(--bg-primary);
}

/* Group title inside the card, distinct from card body and page background */
.settings-card__header {
  font-size: var(--font-size-sm);
  color: var(--text-muted);
  padding:5px var(--space-7);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: var(--font-weight-medium);
  position: relative;
  background: var(--bg-tertiary);
}

/* Inside the card, items become transparent rows with an inset divider */
.settings-card__body :deep(.settings-item) {
  background: transparent;
  padding: var(--space-4) var(--space-7);
}
.settings-card__body :deep(.settings-item::after) {
  left: 16px;
}
.settings-card__body :deep(.settings-item:last-child::after) {
  display: none;
}
</style>

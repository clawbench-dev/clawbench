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
  margin-bottom: 8px;
}

/* Compact iOS-style card: white rounded body on the grey page background */
.settings-card__body {
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-primary);
}

/* Group title as a header row inside the card */
.settings-card__header {
  font-size: 12px;
  color: var(--text-muted);
  padding: 8px 16px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
  position: relative;
}
.settings-card__header::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 16px;
  right: 0;
  height: 0.5px;
  background: var(--border-color);
}

/* Inside the card, items become transparent rows with an inset divider */
.settings-card__body :deep(.settings-item) {
  background: transparent;
  padding: 8px 16px;
}
.settings-card__body :deep(.settings-item::after) {
  left: 16px;
}
.settings-card__body :deep(.settings-item:last-child::after) {
  display: none;
}
</style>

<template>
  <div class="async-component-loader" :class="{ minimal }" role="status" aria-live="polite">
    <div class="async-loader-spinner" />
    <span v-if="label" class="async-loader-label">{{ label }}</span>
    <slot />
  </div>
</template>

<script setup lang="ts">
defineProps<{
  label?: string
  minimal?: boolean
}>()
</script>

<style scoped>
.async-component-loader {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  height: 100%;
  min-height: 120px;
  padding: 32px;
  box-sizing: border-box;
}

.async-component-loader.minimal {
  min-height: 0;
  padding: 16px;
}

.async-loader-spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--bg-tertiary, #e9ecef);
  border-top-color: var(--accent-color, #0066cc);
  border-radius: 50%;
  animation: async-loader-spin 0.8s linear infinite;
  flex-shrink: 0;
}

.async-loader-label {
  font-size: 13px;
  color: var(--text-muted, #999);
}

@keyframes async-loader-spin {
  to { transform: rotate(360deg); }
}
</style>

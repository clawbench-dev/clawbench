<template>
  <div class="async-component-error">
    <span class="li-msg">{{ message }}</span>
    <button class="li-retry" role="button" @click="$emit('retry')">{{ t('common.retry') }}</button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  label?: string
  error?: unknown
}>()
defineEmits<{ (e: 'retry'): void }>()

const { t } = useI18n()

const message = computed(() => props.label || t('common.componentLoadFailed'))
</script>

<style scoped>
.async-component-error {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-6);
  height: 100%;
  min-height: calc(120 * var(--dp));
  padding: calc(32 * var(--dp));
  color: var(--text-muted);
  font-size: var(--font-size-md);
}

.async-component-error .li-retry {
  padding: var(--space-2) calc(14 * var(--dp));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
}

@media (hover: hover) {
  .async-component-error .li-retry:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}
</style>

<template>
  <div class="async-component-error">
    <span class="li-msg">{{ message }}</span>
    <button class="li-retry" role="button" @click="$emit('retry')">重试</button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  label?: string
  error?: unknown
}>()
defineEmits<{ (e: 'retry'): void }>()

const message = computed(() => props.label || '组件加载失败，请点击重试。')
</script>

<style scoped>
.async-component-error {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  height: 100%;
  min-height: 120px;
  padding: 32px;
  color: var(--text-muted);
  font-size: 13px;
}

.async-component-error .li-retry {
  padding: 4px 14px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
}

@media (hover: hover) {
  .async-component-error .li-retry:hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}
</style>

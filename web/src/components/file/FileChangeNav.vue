<template>
  <div v-if="count > 0" class="file-change-nav">
    <span class="fcn-label">{{ t('file.changeNav.count', { count }) }}</span>
    <span v-if="count > 1" class="fcn-pos">{{ index + 1 }}/{{ count }}</span>
    <button
      class="fcn-btn fcn-btn-prev"
      type="button"
      :disabled="index <= 0"
      :title="t('file.changeNav.prev')"
      :aria-label="t('file.changeNav.prev')"
      @click.stop="emit('prev')"
    >
      <ChevronUp :size="14" />
    </button>
    <button
      class="fcn-btn fcn-btn-next"
      type="button"
      :disabled="index >= count - 1"
      :title="t('file.changeNav.next')"
      :aria-label="t('file.changeNav.next')"
      @click.stop="emit('next')"
    >
      <ChevronDown :size="14" />
    </button>
    <button
      class="fcn-btn fcn-btn-clear"
      type="button"
      :title="t('file.changeNav.clear')"
      :aria-label="t('file.changeNav.clear')"
      @click.stop="emit('clear')"
    >
      <X :size="14" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { ChevronUp, ChevronDown, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

defineProps<{ count: number; index: number }>()
const emit = defineEmits(['prev', 'next', 'clear'])
const { t } = useI18n()
</script>

<style scoped>
/* Floating pill at the top-right of the content area. Sits above the preview
   so it stays put while the content scrolls beneath it. */
.file-change-nav {
  position: absolute;
  top: var(--space-4);
  right: var(--space-6);
  z-index: 3;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-full);
  background: var(--bg-elevated);
  box-shadow: var(--shadow-md);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  user-select: none;
}

.fcn-label {
  white-space: nowrap;
}

.fcn-pos {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.fcn-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .fcn-btn:hover:not(:disabled) {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
}

.fcn-btn:disabled {
  opacity: var(--opacity-disabled);
  cursor: default;
}

.fcn-btn:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}
</style>

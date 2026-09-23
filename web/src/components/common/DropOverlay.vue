<template>
  <Transition name="drop-fade">
    <div v-if="visible" class="drop-overlay">
      <slot name="icon">
        <Upload :size="32" :stroke-width="1.5" />
      </slot>
      <span>{{ label }}</span>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { Upload } from 'lucide-vue-next'

/**
 * Full-bleed "drop here" overlay for a drag-and-drop target.
 *
 * Shared by the file manager (drop to upload into the current directory) and
 * the terminal (drop to upload into the shell's current directory). Both hosts
 * are `position: relative`, so the overlay anchors with `inset: 0`.
 *
 * `pointer-events: none` is load-bearing: without it the overlay would become
 * the drop target itself and swallow the host's dragenter/dragover/drop events.
 *
 * The class name `.drop-overlay` is kept verbatim from the file manager's
 * original inline markup so existing tests and selectors keep working.
 */
defineProps<{
  visible: boolean
  label: string
}>()
</script>

<style scoped>
.drop-overlay {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-5);
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 10%, var(--bg-primary, #fff));
  color: var(--accent-color, #4a90d9);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-medium);
  pointer-events: none;
  border-radius: var(--radius-xs);
}

[data-theme-base="dark"] .drop-overlay {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, var(--bg-primary, #1a1a1a));
}

/* Named `drop-fade` (not `paste-fade`) so this component owns its own
   transition instead of depending on a sibling's scoped style block. */
.drop-fade-enter-active,
.drop-fade-leave-active {
  transition: opacity 0.3s ease;
}

.drop-fade-enter-from,
.drop-fade-leave-to {
  opacity: 0;
}
</style>

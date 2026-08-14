<template>
  <div
    class="loading-indicator"
    :class="[`size-${size}`, { inline, overlay, 'is-center': center }]"
    role="status"
    aria-live="polite"
  >
    <div class="li-spinner" />
    <span v-if="label" class="li-label">{{ label }}</span>
    <slot />
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    label?: string
    size?: 'sm' | 'md' | 'lg'
    inline?: boolean
    overlay?: boolean
    center?: boolean
  }>(),
  {
    label: undefined,
    size: 'md',
    inline: false,
    overlay: false,
    center: true,
  },
)
</script>

<style scoped>
.loading-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  box-sizing: border-box;
  color: var(--text-muted, #999);
  font-size: 13px;
}

/* Default: vertical block used for empty content areas */
.loading-indicator:not(.inline) {
  flex-direction: column;
  padding: 24px 16px;
  min-height: 80px;
}

.loading-indicator.overlay {
  position: absolute;
  inset: 0;
  z-index: 5;
  background: var(--bg-primary, #fff);
  opacity: 0.85;
}

.loading-indicator.inline {
  padding: 0;
  min-height: 0;
}

.loading-indicator.is-center {
  justify-content: center;
}

.li-spinner {
  border: 3px solid var(--border-color, #e9ecef);
  border-top-color: var(--accent-color, #0066cc);
  border-radius: 50%;
  animation: li-spin 0.8s linear infinite;
  flex-shrink: 0;
}

.size-sm .li-spinner {
  width: 14px;
  height: 14px;
  border-width: 2px;
}

.size-md .li-spinner {
  width: 28px;
  height: 28px;
}

.size-lg .li-spinner {
  width: 36px;
  height: 36px;
}

@keyframes li-spin {
  to { transform: rotate(360deg); }
}
</style>

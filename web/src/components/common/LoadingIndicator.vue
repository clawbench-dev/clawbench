<template>
  <div
    class="loading-indicator"
    :class="[`size-${size}`, { inline, overlay, fixed, 'is-center': center }]"
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
    fixed?: boolean
    center?: boolean
  }>(),
  {
    label: undefined,
    size: 'md',
    inline: false,
    overlay: false,
    fixed: false,
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

/* Full-screen overlay (covers the entire viewport) */
.loading-indicator.fixed {
  position: fixed;
  inset: 0;
  z-index: 9999;
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
  position: relative;
  flex-shrink: 0;
}

/* Rotating gradient ring */
.li-spinner::before {
  content: '';
  box-sizing: border-box;
  display: block;
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 3px solid var(--border-color, #e9ecef);
  border-top-color: var(--accent-color, #0066cc);
  border-right-color: var(--accent-color, #0066cc);
  animation: li-spin 0.8s cubic-bezier(0.6, 0.2, 0.4, 0.8) infinite;
}

/* Pulsing center dot */
.li-spinner::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 50%;
  width: 32%;
  height: 32%;
  transform: translate(-50%, -50%);
  border-radius: 50%;
  background: var(--accent-color, #0066cc);
  animation: li-pulse 1.2s ease-in-out infinite;
}

.size-sm .li-spinner {
  width: 14px;
  height: 14px;
}

.size-sm .li-spinner::before {
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

/* Overlays: respect the size prop (sm/md/lg) instead of forcing a fixed size,
   so full-area masks match the content-area loading indicators. */
.loading-indicator.overlay .li-spinner,
.loading-indicator.fixed .li-spinner {
  filter: drop-shadow(0 4px 14px rgba(0, 102, 204, 0.25));
}

.loading-indicator.overlay .li-spinner::before,
.loading-indicator.fixed .li-spinner::before {
  border-width: 3px;
}

@keyframes li-spin {
  to { transform: rotate(360deg); }
}

@keyframes li-pulse {
  0%, 100% { opacity: 0.35; transform: translate(-50%, -50%) scale(0.7); }
  50% { opacity: 1; transform: translate(-50%, -50%) scale(1); }
}
</style>

<style>
/* Shared fade transition for loading overlays (non-scoped, applied by callers
   wrapping <LoadingIndicator> in <Transition name="loading-fade">). */
.loading-fade-enter-active {
  transition: opacity 0.12s ease-out;
}
.loading-fade-leave-active {
  transition: opacity 0.18s ease-in;
}
.loading-fade-enter-from,
.loading-fade-leave-to {
  opacity: 0;
}
</style>

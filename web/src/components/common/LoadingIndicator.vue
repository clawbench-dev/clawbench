<template>
  <div
    class="loading-indicator"
    :class="[`size-${size}`, { inline, overlay, fixed, 'is-center': center }]"
    role="status"
    aria-live="polite"
  >
    <div class="li-spinner" aria-hidden="true"></div>
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
  gap: var(--space-5);
  box-sizing: border-box;
  font-size: var(--font-size-md);
}

.li-label {
  color: var(--text-muted, #999);
}

/* Default: vertical block used for empty content areas */
.loading-indicator:not(.inline) {
  flex-direction: column;
  padding:24px var(--space-7);
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
  z-index: var(--z-popover);
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

/* ── Classic single-ring spinner (same language as the chat tool banner) ──
   Gray ring with a single accent arc on top, rotating linearly. No separate
   track, no center dot — the arc color is consumed via the --li-color custom
   property so callers override it by setting the property on this root.
   The ring base color can be overridden via --li-track-color (used on dark
   backdrops where the default --border-color gray is invisible). */
.li-spinner {
  --li-size: 28px; /* default md */
  --li-border: calc(var(--li-size) / 9);
  box-sizing: border-box;
  flex-shrink: 0;
  width: var(--li-size);
  height: var(--li-size);
  border-radius: 50%;
  border: var(--li-border) solid var(--li-track-color, var(--border-color, #e9ecef));
  border-top-color: var(--li-color, var(--accent-color, #0066cc));
  animation: li-spin 0.8s linear infinite;
}

.size-sm .li-spinner {
  --li-size: 14px;
}

.size-md .li-spinner {
  --li-size: 28px;
}

.size-lg .li-spinner {
  --li-size: 36px;
}

/* Overlays: a gentle glow lifts the loader off busy backgrounds. */
.loading-indicator.overlay .li-spinner,
.loading-indicator.fixed .li-spinner {
  filter: drop-shadow(0 4px 14px color-mix(in srgb, var(--li-color, var(--accent-color, #0066cc)) 25%, transparent));
}

@keyframes li-spin {
  to { transform: rotate(360deg); }
}
</style>

<style>
/* Shared fade transition for loading overlays (non-scoped, applied by callers
   wrapping <LoadingIndicator> in <Transition name="loading-fade">). */
.loading-fade-enter-active {
  transition: opacity var(--duration-base) ease-out;
}
.loading-fade-leave-active {
  transition: opacity var(--duration-slow) ease-in;
}
.loading-fade-enter-from,
.loading-fade-leave-to {
  opacity: 0;
}
</style>

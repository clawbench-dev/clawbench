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

/* The ring (.li-spinner, its size tiers and @keyframes li-spin) is declared
   GLOBALLY in css/components.css — see the note there. It has to be global
   because some callers cannot render this component: chat tool-call bodies and
   mermaid diagrams are injected as HTML strings, and the localhost open button
   paints its spinner from a ::after pseudo-element. None of those carry a
   data-v-* attribute, so a scoped rule would silently miss them.

   What stays here is only the wrapper's own layout, which the injected callers
   do not use. */
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
  opacity: var(--opacity-hover);
}

/* Full-screen overlay (covers the entire viewport) */
.loading-indicator.fixed {
  position: fixed;
  inset: 0;
  z-index: var(--z-popover);
  background: var(--bg-primary, #fff);
  opacity: var(--opacity-hover);
}

.loading-indicator.inline {
  padding: 0;
  min-height: 0;
}

.loading-indicator.is-center {
  justify-content: center;
}

/* Overlays: a gentle glow lifts the loader off busy backgrounds. This one stays
   scoped — it targets the component's own wrapper (`.overlay` / `.fixed`), which
   the injected callers never render. */
.loading-indicator.overlay .li-spinner,
.loading-indicator.fixed .li-spinner {
  filter: drop-shadow(0 4px 14px color-mix(in srgb, var(--li-color, var(--accent-color, #0066cc)) 25%, transparent));
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

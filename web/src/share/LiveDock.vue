<template>
  <div class="live-dock">
    <!-- Brand. Replaces the header's logo on the viewer page; the dot is the
         "live" signal. Optional, because the host's picture sits inside the app
         where the logo would be redundant — the pill is what has to match. -->
    <span v-if="brand" class="live-dock-brand" :title="title" :aria-label="title">
      <img class="live-dock-logo" src="/logo-64.png" alt="ClawBench" />
      <span v-if="live" class="live-dock-dot" />
    </span>
    <slot />
  </div>
</template>

<script setup lang="ts">
/**
 * The floating pill that carries the controls over a live picture.
 *
 * One implementation for both sides of a share: the viewer's page
 * (`LiveShareView`) and the host's picture of a member's screen
 * (`MemberScreenSurface`). They show different actions, but the pill — size,
 * spacing, theme tokens, the way it floats over the video without taking height
 * from it — must be the same object, or the two ends of the same call look like
 * two different products.
 */
withDefaults(defineProps<{ brand?: boolean; live?: boolean; title?: string }>(), {
  brand: false,
  live: false,
  title: '',
})
</script>

<style scoped>
/*
 * Floating dock, centred over the bottom of the video.
 *
 * Absolute rather than a flex row: a real bar would take height from the
 * stream, and the whole point of this surface is the picture. The translucent
 * background keeps the controls legible over bright content without the blur
 * pass that some WebViews composite badly.
 */
.live-dock {
  position: absolute;
  left: 50%;
  bottom: calc(var(--space-6, 12px) + env(safe-area-inset-bottom, 0px));
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--bg-secondary, #161b22) 86%, transparent);
  border: 1px solid var(--border-color, #30363d);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.35);
  z-index: 5;
}

/* Brand inside the dock. Sized to the dock's inner height so the pill keeps a
   single visual rhythm; the dot reuses the status colour. */
.live-dock-brand {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 var(--space-2, 4px) 0 var(--space-3, 6px);
  flex-shrink: 0;
}

.live-dock-logo {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: block;
}

.live-dock-dot {
  position: absolute;
  right: 1px;
  bottom: 0;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-success, #3fb950);
  box-shadow: 0 0 0 2px var(--bg-secondary, #161b22);
}
</style>

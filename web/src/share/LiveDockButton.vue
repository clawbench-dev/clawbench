<template>
  <button
    class="live-dock-btn"
    :class="{ 'live-dock-btn--live': live }"
    type="button"
  >
    <slot name="icon" />
    <span class="live-dock-label"><slot /></span>
    <span v-if="badge > 0" class="live-dock-badge">{{ badge > 9 ? '9+' : badge }}</span>
  </button>
</template>

<script setup lang="ts">
/**
 * One control in a `LiveDock` pill: icon, label, optional unread badge.
 *
 * Everything else (`data-action`, `:disabled`, `role="switch"`, `aria-checked`,
 * `title`, `@click`) is an attribute and falls through to the `<button>`, so the
 * callers keep the semantics they need without this component knowing about
 * microphones, screen capture or picture frames.
 */
withDefaults(defineProps<{ live?: boolean; badge?: number }>(), {
  live: false,
  badge: 0,
})
</script>

<style scoped>
.live-dock-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: 0 var(--space-5, 10px);
  /* Compact bar, not a form control: this pill floats over the picture and is
     read at a glance, so every extra dp of height eats the video. 28dp keeps
     the label's line box (13px × 1.6 ≈ 20dp) with ~4dp of air above and below —
     the dock's own 2 × 2dp padding brings the pill to 32dp. It used to be 36dp,
     which made the tinted sound button read as a slab next to its label. */
  min-height: calc(28 * var(--dp));
  border: none;
  border-radius: var(--radius-full, 999px);
  background: transparent;
  color: var(--text-secondary, #8b949e);
  cursor: pointer;
  font-size: var(--font-size-sm, 13px);
  line-height: 1.6;
  white-space: nowrap;
  /* Tapping repeatedly must not select the label. */
  user-select: none;
  transition:
    background var(--duration-fast, 0.1s) ease,
    color var(--duration-fast, 0.1s) ease;
}

.live-dock-btn:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.06));
  color: var(--text-primary, #e6edf3);
}

/* Unavailable on this platform (plain-http microphone, browser-side screen
   capture, remote control outside the client). The button keeps its place —
   removing it would read as "the host did not allow this" — but must look
   unusable, otherwise a click that does nothing looks broken. */
.live-dock-btn:disabled {
  opacity: var(--opacity-disabled, 0.4);
  cursor: not-allowed;
}

.live-dock-btn:disabled:hover {
  background: transparent;
  color: var(--text-secondary, #8b949e);
}

/* "On" state: a tint of the theme's own accent, never a semantic colour. The
   dock floats over arbitrary shared content, so every control has to come from
   the palette the rest of the app uses — a green here reads as a different
   product (and on several themes it collides with the accent anyway). */
.live-dock-btn--live {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 20%, transparent);
  color: var(--accent-color, #4a90d9);
}

.live-dock-btn--live:hover {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 30%, transparent);
  color: var(--accent-color, #4a90d9);
}

/* The label is the first thing to drop when space runs out: the icon and the
   tint still carry the meaning, and the title has the full sentence. */
@media (max-width: 520px) {
  .live-dock-label {
    display: none;
  }

  .live-dock-btn {
    padding: var(--space-2, 4px) var(--space-3, 6px);
    /* Icon only from here on, so the horizontal target collapses with the
       label: pin the width to the button's height and it stays a square. */
    min-width: calc(28 * var(--dp));
  }
}

/* Unread counter on the dock button. Accent, not red: red means an error or a
   destructive action in this UI, while an unread line is ordinary traffic. */
.live-dock-badge {
  min-width: calc(16 * var(--dp));
  padding: 0 calc(4 * var(--dp));
  border-radius: var(--radius-full, 999px);
  background: var(--accent-color, #4a90d9);
  color: #fff;
  font-size: calc(10 * var(--dp));
  line-height: calc(16 * var(--dp));
  text-align: center;
}
</style>

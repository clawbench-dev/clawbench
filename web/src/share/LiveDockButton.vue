<template>
  <button
    class="live-dock-btn"
    :class="{ 'live-dock-btn--live': live, 'live-dock-btn--call': call }"
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
withDefaults(defineProps<{ live?: boolean; call?: boolean; badge?: number }>(), {
  live: false,
  call: false,
  badge: 0,
})
</script>

<style scoped>
.live-dock-btn {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2, 4px);
  padding: var(--space-2, 4px) var(--space-5, 10px);
  /* Touch target, not decoration: this pill floats over the picture on a phone,
     where it is the only way to mute, chat or go fullscreen. Below 44dp the
     mic and the control button next to it are pressed by mistake. The dock's
     own padding (2 × 4dp) brings the pill to 52dp — the same offset the
     control banner above it is positioned with. */
  min-height: calc(44 * var(--dp));
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

/* Live state: tinted rather than a solid green fill, which would shout next to
   the status chip in the header. */
.live-dock-btn--live {
  background: color-mix(in srgb, var(--color-success, #3fb950) 18%, transparent);
  color: var(--color-success, #3fb950);
}

/* The sound prompt is the only control the viewer MUST act on, so it gets the
   accent tint rather than the neutral one. */
.live-dock-btn--call {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 22%, transparent);
  color: var(--accent-color, #4a90d9);
}

.live-dock-btn--call:hover {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 32%, transparent);
  color: var(--accent-color, #4a90d9);
}

.live-dock-btn--live:hover {
  background: color-mix(in srgb, var(--color-success, #3fb950) 26%, transparent);
  color: var(--color-success, #3fb950);
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
       label unless it is pinned to the same 44dp square. */
    min-width: calc(44 * var(--dp));
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

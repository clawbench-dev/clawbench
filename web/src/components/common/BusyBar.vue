<template>
  <Transition name="busy-bar-fade">
    <div v-if="visible" class="busy-bar" role="progressbar" :aria-label="label">
      <div class="busy-bar-fill"></div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
/**
 * BusyBar — a 3px indeterminate progress bar pinned to the top edge of its
 * (position: relative) host.
 *
 * Why it exists: some actions are slow enough that nothing visible happens for
 * seconds after the click — forking a session and syncing an ACP session both
 * POST and only touch the message area once the response lands. A spinner
 * inside the button that was clicked is not enough: on a narrow viewport the
 * action bar scrolls horizontally, so the button may not even be on screen.
 * A full-width bar at the panel's top edge is visible regardless of scroll
 * position or viewport width, and it reads as "still working" rather than
 * "frozen".
 *
 * It is ABSOLUTE, not in flow, so showing/hiding it never shifts the messages
 * or the input box.
 *
 * ── No `prefers-reduced-motion` opt-out (deliberate) ──
 * The sweep IS the information: it is the only thing distinguishing "working"
 * from "hung". Freezing it would leave a static half-filled bar, which reads as
 * a stalled transfer — exactly the ambiguity this component exists to remove.
 * This also matches the rest of the codebase: the sibling indeterminate bar in
 * `TransferProgressBar.vue` has no opt-out either, and `RefreshButton` / the
 * running comet drive their motion through WAAPI, which never consults the
 * preference. Opting out here alone would make this the only indicator in the
 * app that stops for those users.
 * See docs/spec/client/design-guide.md § 动效 for the two other documented
 * exceptions (session status slot, recommendation fly-in).
 */
defineProps<{
  /** True while the tracked action is in flight. */
  visible: boolean
  /** Accessible name for the bar, e.g. "Forking session". Indeterminate, so
   *  there is no aria-valuenow — only a name to announce what is running. */
  label: string
}>()
</script>

<style scoped>
.busy-bar {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 3px;
  overflow: hidden;
  /* Above the message list and the swipe indicator, below menus/toasts.
     The host is `position: relative`, so this scopes to the panel. */
  z-index: 20;
  pointer-events: none;
  background: var(--bg-tertiary, #f0f0f0);
}

.busy-bar-fill {
  width: 30%;
  height: 100%;
  border-radius: var(--radius-xs);
  background: var(--accent-color, #4a90d9);
  /* Same sweep geometry as the transfer bar's indeterminate fill so the two
     read as one family. */
  animation: busy-bar-sweep 1.2s ease-in-out infinite;
}

@keyframes busy-bar-sweep {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(400%); }
}

.busy-bar-fade-enter-active {
  transition: opacity var(--duration-base) ease-out;
}
.busy-bar-fade-leave-active {
  transition: opacity var(--duration-slow) ease-in;
}
.busy-bar-fade-enter-from,
.busy-bar-fade-leave-to {
  opacity: 0;
}
</style>

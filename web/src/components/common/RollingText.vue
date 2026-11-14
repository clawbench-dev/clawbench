<template>
  <span class="rolling-text" :data-text="text">
    <!-- The sizer is a ::before with `content: attr(data-text)` rather than a
         real element: the animated lines are absolutely positioned, so the box
         needs something to give it width — but a real node would duplicate the
         text in textContent (copy, tests, screen readers). -->
    <TransitionGroup name="rolling-text" tag="span" class="rolling-text-track">
      <span :key="shown" class="rolling-text-line">{{ shown }}</span>
    </TransitionGroup>
  </span>
</template>

<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue'

/**
 * Text that rolls like an airport departure board when it changes: the old
 * line slides down out of the box while the new one drops in from above.
 *
 * Used for "what is running right now", which changes several times a second
 * while a turn streams. Rolling on every frame would be an unreadable blur, so
 * the value is only committed after it has been stable for ROLL_DELAY_MS —
 * continuous churn shows nothing until it settles on the latest line.
 */
const props = defineProps<{ text: string }>()

const ROLL_DELAY_MS = 180
const shown = ref(props.text)
let timer: ReturnType<typeof setTimeout> | null = null

watch(
  () => props.text,
  (next) => {
    if (next === shown.value) return
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      shown.value = next
      timer = null
    }, ROLL_DELAY_MS)
  },
)

onUnmounted(() => {
  if (timer) clearTimeout(timer)
})
</script>

<style scoped>
.rolling-text {
  position: relative;
  display: block;
  overflow: hidden;
  max-width: 100%;
}

.rolling-text::before {
  content: attr(data-text);
  display: block;
  visibility: hidden;
  white-space: nowrap;
  overflow: hidden;
}

.rolling-text-track {
  position: absolute;
  inset: 0;
  display: block;
}

.rolling-text-line {
  position: absolute;
  inset: 0;
  display: block;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* New line drops in from above, the old one keeps going down — the board
   scrolls top-to-bottom. */
.rolling-text-enter-from {
  transform: translateY(-100%);
  opacity: 0;
}

.rolling-text-leave-to {
  transform: translateY(100%);
  opacity: 0;
}

.rolling-text-enter-active,
.rolling-text-leave-active {
  transition: transform var(--duration-slow) ease, opacity var(--duration-slow) ease;
}

@media (prefers-reduced-motion: reduce) {
  .rolling-text-enter-active,
  .rolling-text-leave-active {
    transition: none;
  }
}
</style>

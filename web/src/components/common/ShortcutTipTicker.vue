<template>
  <div v-if="tip" class="stt">
    <div ref="viewportRef" class="stt-viewport">
      <div class="stt-vert" :class="{ 'stt-vert-out': vertPhase === 'out', 'stt-vert-in': vertPhase === 'in' }">
        <span ref="hscrollRef" class="stt-hscroll" :style="hscrollStyle">
          <span class="stt-context">{{ t(tip.contextKey) }}</span>
          <kbd v-for="k in tip.keys || []" :key="k" class="stt-kbd">{{ k }}</kbd>
          <span class="stt-action">{{ t(tip.actionKey) }}</span>
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { SHORTCUT_TIPS, type ShortcutTipDef } from '@/config/shortcutTips'

const props = withDefaults(defineProps<{
  tips?: ShortcutTipDef[]
  showMs?: number
  horizDelayMs?: number
  horizMsPerPx?: number
  horizPauseMs?: number
  vertMs?: number
}>(), {
  tips: () => SHORTCUT_TIPS,
  showMs: 10000,
  horizDelayMs: 800,
  horizMsPerPx: 8,
  horizPauseMs: 800,
  vertMs: 160,
})

const { t } = useI18n()

const viewportRef = ref<HTMLElement | null>(null)
const hscrollRef = ref<HTMLElement | null>(null)
const currentIndex = ref(0)
const isHScroll = ref(false)
const overflowPx = ref(0)
const horizDurationMs = ref(0)
const vertPhase = ref<'out' | 'in' | ''>('')

const tip = computed(() => props.tips[currentIndex.value] ?? null)

const hscrollStyle = computed(() => {
  if (!isHScroll.value) return {}
  return {
    transition: `transform ${horizDurationMs.value}ms linear ${props.horizDelayMs}ms`,
    transform: `translateX(${-overflowPx.value}px)`,
  }
})

let horizTimer: ReturnType<typeof setTimeout> | null = null
let vertTimer: ReturnType<typeof setTimeout> | null = null
let ro: ResizeObserver | null = null

function clearTimers() {
  if (horizTimer !== null) clearTimeout(horizTimer)
  if (vertTimer !== null) clearTimeout(vertTimer)
  horizTimer = null
  vertTimer = null
}

/** Measure whether the current tip overflows the viewport; start horizontal scroll if so. */
async function schedule() {
  clearTimers()
  isHScroll.value = false
  overflowPx.value = 0
  await nextTick()
  if (!viewportRef.value || !hscrollRef.value) return
  const overflow = hscrollRef.value.scrollWidth - viewportRef.value.clientWidth
  if (overflow > 0) {
    overflowPx.value = overflow
    horizDurationMs.value = Math.max(400, Math.min(4000, overflow * props.horizMsPerPx))
    isHScroll.value = true
    // horizontal scroll (delay) + duration + pause, then switch vertically
    horizTimer = setTimeout(beginVerticalSwitch, props.horizDelayMs + horizDurationMs.value + props.horizPauseMs)
  } else {
    horizTimer = setTimeout(beginVerticalSwitch, props.showMs)
  }
}

/** Slide the current tip out upward, swap to the next, slide it in. */
function beginVerticalSwitch() {
  if (props.tips.length === 0) return
  vertPhase.value = 'out'
  vertTimer = setTimeout(() => {
    currentIndex.value = (currentIndex.value + 1) % props.tips.length
    isHScroll.value = false
    overflowPx.value = 0
    // trigger the slide-in from below (start phase then flush to animate)
    vertPhase.value = 'in'
    requestAnimationFrame(() => {
      vertPhase.value = 'in'
    })
    vertTimer = setTimeout(() => {
      vertPhase.value = ''
      void schedule()
    }, props.vertMs + 80)
  }, props.vertMs + 40)
}

onMounted(() => {
  void schedule()
  ro = new ResizeObserver(() => { void schedule() })
  if (viewportRef.value) ro.observe(viewportRef.value)
})

onBeforeUnmount(() => {
  clearTimers()
  ro?.disconnect()
  ro = null
})

// Re-schedule when the tip list changes (e.g. test injection / dynamic content)
watch(() => props.tips, () => {
  currentIndex.value = 0
  vertPhase.value = ''
  clearTimers()
  void schedule()
})
</script>

<style scoped>
.stt {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  height: 100%;
  display: flex;
  align-items: center;
}

.stt-viewport {
  width: 100%;
  overflow: hidden;
  white-space: nowrap;
}

.stt-vert {
  will-change: transform, opacity;
}

.stt-vert-out {
  transform: translateY(-10px);
  opacity: 0;
  transition: transform 160ms ease, opacity 160ms ease;
}

.stt-vert-in {
  transform: translateY(0);
  opacity: 1;
  transition: transform 200ms ease, opacity 200ms ease;
}

.stt-hscroll {
  display: inline-block;
  will-change: transform;
  font-size: 11px;
  color: var(--text-muted);
  line-height: 1;
  vertical-align: middle;
}

.stt-context {
  color: var(--text-secondary);
}

.stt-kbd {
  display: inline-block;
  margin: 0 2px;
  padding: 1px 5px;
  border: 1px solid var(--border-color);
  border-bottom-width: 2px;
  border-radius: 4px;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono, monospace);
  vertical-align: middle;
  white-space: nowrap;
}

.stt-action {
  margin-left: 4px;
}
</style>

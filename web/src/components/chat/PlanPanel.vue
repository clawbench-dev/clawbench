<template>
  <div v-if="entries.length > 0" class="plan-panel">
    <!-- Collapsed chip -->
    <div v-if="collapsed" class="plan-chip" :class="{ 'plan-chip--updated': hasUpdate }" @click="$emit('toggle-collapse')">
      <span class="plan-chip__pulse"></span>
      <span class="plan-chip__text">{{ chipText }}</span>
      <ChevronDown :size="12" class="plan-chip__toggle" />
    </div>

    <!-- Expanded timeline -->
    <div v-else class="plan-expanded">
      <div class="plan-expanded__header" @click="$emit('toggle-collapse')">
        <span class="plan-expanded__title">{{ t('chat.plan.title') }}</span>
        <ChevronUp :size="12" class="plan-expanded__toggle" />
      </div>
      <div ref="timelineRef" class="plan-expanded__timeline">
        <div v-for="(entry, idx) in entries" :key="idx" class="plan-entry" :class="'plan-entry--' + entry.status">
          <!-- Vertical connector line -->
          <div v-if="idx < entries.length - 1" class="plan-entry__line"
            :class="{
              'plan-entry__line--solid': entry.status === 'completed',
              'plan-entry__line--dashed': entry.status !== 'completed',
              'plan-entry__line--pulsing': entry.status === 'in_progress',
            }">
          </div>
          <!-- Status node -->
          <div class="plan-entry__node">
            <span v-if="entry.status === 'completed'" class="plan-entry__check">✓</span>
            <span v-else-if="entry.status === 'in_progress'" class="plan-entry__dot"></span>
            <span v-else class="plan-entry__circle"></span>
          </div>
          <!-- Entry content -->
          <span class="plan-entry__text" :class="{ 'plan-entry__text--done': entry.status === 'completed' }">{{ entry.content }}</span>
          <!-- Priority dot -->
          <span class="plan-entry__priority-dot" :class="'plan-entry__priority-dot--' + entry.priority"></span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ChevronUp } from 'lucide-vue-next'
import type { PlanEntry } from '@/composables/usePlanProgress'
import { activeEntryIndex, centeredScrollTop } from '@/utils/planScroll'

const props = defineProps<{
  entries: PlanEntry[]
  collapsed: boolean
  hasUpdate: boolean
}>()

defineEmits<{
  'toggle-collapse': []
}>()

const { t } = useI18n()

const chipText = computed(() => {
  const inProgress = props.entries.find(e => e.status === 'in_progress')
  if (inProgress) return inProgress.content
  const completed = props.entries.filter(e => e.status === 'completed').length
  const total = props.entries.length
  return t('chat.plan.completedCount', { completed, total })
})

// ── Active-entry centering ─────────────────────────────────
// The running plan step stays visible in the middle of the timeline: when the
// panel first appears / is expanded, and whenever execution advances to a new
// step. Rows are measured viewport-relative via getBoundingClientRect (the
// timeline is not a positioned ancestor, so offsetTop would be mis-framed).
// No entry is in_progress → the list is never force-scrolled.
const timelineRef = ref<HTMLElement | null>(null)

/** Scroll so the in_progress entry is vertically centered in the timeline. */
function centerActiveEntry() {
  const el = timelineRef.value
  if (!el) return
  const idx = activeEntryIndex(props.entries)
  if (idx < 0) return
  const row = el.children[idx] as HTMLElement | undefined
  if (!row) return
  const rowRect = row.getBoundingClientRect()
  const elRect = el.getBoundingClientRect()
  const maxScrollTop = Math.max(el.scrollHeight - el.clientHeight, 0)
  const target = centeredScrollTop({
    scrollTop: el.scrollTop,
    rowCenter: rowRect.top + rowRect.height / 2 - elRect.top,
    containerHeight: el.clientHeight,
    maxScrollTop,
  })
  if (target !== el.scrollTop) el.scrollTop = target
}

// Center when the expanded timeline is (re)created: on mount with entries, when
// entries appear, and when the user expands from the collapsed chip. Primitive
// key (count, not the array) — same-length entry replacements must NOT re-fire
// (that would yank the user on every non-advancing plan_update).
watch(
  () => (props.collapsed ? -1 : props.entries.length),
  () => {
    if (!props.collapsed && props.entries.length > 0) {
      nextTick(centerActiveEntry)
    }
  },
  { immediate: true },
)

// Center when execution advances to a new step.
let lastActiveIdx = activeEntryIndex(props.entries)
watch(
  () => props.entries.map(e => e.status).join(','),
  (statuses) => {
    if (props.collapsed) return
    const idx = statuses.split(',').indexOf('in_progress')
    if (idx !== lastActiveIdx) {
      nextTick(centerActiveEntry)
      lastActiveIdx = idx
    }
  },
)

onMounted(() => {
  // Remount case: the component can appear already-expanded with entries (e.g.
  // restored plan state). The immediate watcher above runs during setup when
  // the timeline ref is not yet bound, so center here once the DOM is ready.
  if (!props.collapsed && props.entries.length > 0) {
    centerActiveEntry()
  }
})
</script>

<style scoped>
.plan-panel {
  width: auto;
  margin: 0 10px 8px;
}

/* ── Collapsed chip ── */
.plan-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 16px;
  background: var(--bg-tertiary, #e9ecef);
  border: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
  transition: border-color 0.3s ease;
}

.plan-chip--updated {
  border-color: #06b6d4;
  animation: plan-chip-glow 0.5s ease-out;
}

:root[data-theme-base="dark"] .plan-chip--updated {
  border-color: #22d3ee;
}

.plan-chip__pulse {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #06b6d4;
  animation: pulse 1.5s ease-in-out infinite;
  flex-shrink: 0;
}

:root[data-theme-base="dark"] .plan-chip__pulse {
  background: #22d3ee;
}

.plan-chip__text {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary, #495057);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.plan-chip__toggle {
  color: var(--text-muted, #6c757d);
  flex-shrink: 0;
}

/* ── Expanded timeline ── */
.plan-expanded {
  background: var(--bg-secondary, #f8f9fa);
  border: 1px solid var(--border-color, #dee2e6);
  border-radius: 8px;
  padding: 8px 12px;
}

.plan-expanded__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
  cursor: pointer;
}

.plan-expanded__title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary, #212529);
}

.plan-expanded__toggle {
  color: var(--text-muted, #6c757d);
  cursor: pointer;
}

.plan-expanded__timeline {
  display: flex;
  flex-direction: column;
  max-height: 240px;
  overflow-y: auto;
}

/* ── Timeline entry ── */
.plan-entry {
  display: flex;
  align-items: flex-start;
  position: relative;
  padding-left: 20px;
  min-height: 28px;
  gap: 6px;
}

/* Vertical line segment */
.plan-entry__line {
  position: absolute;
  left: 7px;
  top: 16px;
  bottom: -12px;
  width: 0;
  border-left: 2px solid var(--border-color, #dee2e6);
}

.plan-entry:last-child .plan-entry__line {
  display: none;
}

.plan-entry__line--dashed {
  border-left-style: dashed;
}

.plan-entry__line--pulsing {
  border-left-style: solid;
  border-left-color: var(--color-cyan, #06b6d4);
  animation: pulse-line 1.5s ease-in-out infinite;
}

:root[data-theme-base="dark"] .plan-entry__line--pulsing {
  border-left-color: #22d3ee;
}

/* Status node — neutral, no priority color */
.plan-entry__node {
  position: absolute;
  left: 0;
  top: 4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--border-color, #dee2e6);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-secondary, #f8f9fa);
  box-sizing: border-box;
}

.plan-entry--completed .plan-entry__node {
  background: var(--color-green, #16a34a);
  border-color: var(--color-green, #16a34a);
  animation: check-in 0.3s ease-out;
}

:root[data-theme-base="dark"] .plan-entry--completed .plan-entry__node {
  background: var(--color-green, #3fb950);
  border-color: var(--color-green, #3fb950);
}

.plan-entry--in_progress .plan-entry__node {
  border-color: var(--color-cyan, #06b6d4);
}

:root[data-theme-base="dark"] .plan-entry--in_progress .plan-entry__node {
  border-color: #22d3ee;
}

.plan-entry__check {
  font-size: 10px;
  color: #fff;
  line-height: 1;
}

.plan-entry__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--color-cyan, #06b6d4);
  animation: pulse 1.5s ease-in-out infinite;
}

:root[data-theme-base="dark"] .plan-entry__dot {
  background: #22d3ee;
}

.plan-entry__circle {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  border: 1.5px solid var(--text-muted, #6c757d);
}

/* Entry text */
.plan-entry__text {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary, #495057);
  line-height: 1.4;
  padding-top: 2px;
  min-width: 0;
}

.plan-entry__text--done {
  color: var(--text-muted, #6c757d);
}

/* ── Priority dot ── */
.plan-entry__priority-dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 5px;
}

.plan-entry__priority-dot--high {
  background: #ef4444;
}

.plan-entry__priority-dot--medium {
  background: #eab308;
}

.plan-entry__priority-dot--low {
  background: #3b82f6;
}

/* ── Animations ── */
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

@keyframes pulse-line {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

@keyframes check-in {
  0% { transform: scale(0); opacity: 0; }
  50% { transform: scale(1.2); }
  100% { transform: scale(1); opacity: 1; }
}

@keyframes plan-chip-glow {
  0% { border-color: #06b6d4; box-shadow: 0 0 6px rgba(6, 182, 212, 0.5); }
  100% { border-color: var(--border-color, #dee2e6); box-shadow: none; }
}

:root[data-theme-base="dark"] .plan-chip-glow {
  0% { border-color: #22d3ee; box-shadow: 0 0 6px rgba(34, 211, 238, 0.5); }
  100% { border-color: var(--border-color, #30363d); box-shadow: none; }
}
</style>

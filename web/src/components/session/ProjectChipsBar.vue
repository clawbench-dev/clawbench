<template>
  <div v-if="otherProjects.length > 0" class="project-chips-bar" ref="barRef">
    <div class="project-chips-scroll">
      <button
        v-for="item in visibleProjects"
        :key="item.path"
        class="project-chip"
        :title="item.path"
        @click="selectProject(item)"
      >
        <span class="project-chip-name">{{ item.name }}</span>
      </button>
    </div>
    <!-- Off-screen row used to measure each chip's natural width. Chips are
         text-sized, so we cannot predict how many fit without measuring. -->
    <div class="project-chips-measure" aria-hidden="true" ref="measureRef">
      <button
        v-for="item in otherProjects"
        :key="item.path"
        class="project-chip"
      >
        <span class="project-chip-name">{{ item.name }}</span>
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, inject, watch, onMounted, onBeforeUnmount, nextTick, computed } from 'vue'
import { useRecentProjects } from '@/composables/useRecentProjects'
import { computeVisibleChipCount } from '@/utils/chipLayout'
import { appLog } from '@/utils/appLog'

const CHIP_GAP = 6

const props = defineProps({
  projectRoot: { type: String, default: '' },
  homeDir: { type: String, default: '' },
})

const { items, load } = useRecentProjects()
const hotSwitchProject = inject('hotSwitchProject')

const barRef = ref(null)
const measureRef = ref(null)
const visibleCount = ref(0)

// The current project is already shown in the app header, so the chips are a
// quick switcher for the *other* recent projects only.
const otherProjects = computed(() => items.value.filter((p) => p.path !== props.projectRoot))
const visibleProjects = computed(() => otherProjects.value.slice(0, visibleCount.value))

function measure() {
  const bar = barRef.value
  const row = measureRef.value
  if (!bar || !row) return
  const widths = Array.from(row.children).map(
    (el) => el.getBoundingClientRect().width || 0,
  )
  // The bar has 8px horizontal padding on each side.
  const available = bar.clientWidth - 16
  visibleCount.value = computeVisibleChipCount(widths, available, CHIP_GAP)
}

let observer = null

async function refresh() {
  await load(props.homeDir)
  await nextTick()
  measure()
}

onMounted(async () => {
  await refresh()
  if (typeof ResizeObserver !== 'undefined' && barRef.value) {
    observer = new ResizeObserver(measure)
    observer.observe(barRef.value)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})

watch(() => props.projectRoot, refresh)

async function selectProject(item) {
  try {
    if (hotSwitchProject) {
      await hotSwitchProject(item.path)
    } else {
      const resp = await fetch('/api/project', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: item.path }),
      })
      if (resp.ok) window.location.reload()
    }
  } catch (e) {
    appLog.w('ProjectChipsBar', 'Failed to switch project', { path: item.path, error: e })
  }
}

defineExpose({ refresh, measure })
</script>

<style scoped>
.project-chips-bar {
  position: relative;
  flex-shrink: 0;
  border-bottom: 1px solid var(--border-color, rgba(0, 0, 0, 0.12));
}
.project-chips-scroll {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  overflow: hidden;
  white-space: nowrap;
}
/* Measurement row: laid out at natural width but kept out of view so it never
   affects the bar's height or scroll size. */
.project-chips-measure {
  position: absolute;
  top: 0;
  left: 0;
  visibility: hidden;
  pointer-events: none;
  display: flex;
  gap: 6px;
  white-space: nowrap;
}
.project-chip {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  max-width: 110px;
  height: 24px;
  padding: 0 10px;
  border: 1px solid var(--border-color, rgba(0, 0, 0, 0.12));
  border-radius: 12px;
  background: var(--bg-primary, #fff);
  color: var(--text-secondary, #666);
  font-size: 12px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.project-chip-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (hover: hover) {
  .project-chip:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
    color: var(--accent-color, #0066cc);
  }
}
</style>

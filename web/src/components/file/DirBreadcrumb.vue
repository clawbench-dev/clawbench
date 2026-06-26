<template>
  <div v-if="parts.length > 0" class="dir-breadcrumb">
    <span class="crumb" @click="$emit('navigate', '')">
      <CircleDot :size="14" />
    </span>
    <template v-for="(part, i) in parts" :key="i">
      <span class="crumb-sep">›</span>
      <span
        class="crumb"
        :class="{ current: i === parts.length - 1 }"
        @click="i < parts.length - 1 && $emit('navigate', reconstructPath(parts.slice(0, i + 1)))"
      >{{ part }}</span>
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { CircleDot } from 'lucide-vue-next'
import { splitPath } from '@/utils/path.ts'

const props = defineProps({
  path: { type: String, default: '' },
})

defineEmits(['navigate'])

// Reconstruct a path from breadcrumb segments,
// using the appropriate separator for the platform.
function reconstructPath(segments) {
  if (segments.length === 0) return ''
  // Windows: first segment like "C:\" already includes the root separator
  if (/^[A-Za-z]:\\$/.test(segments[0])) {
    return segments[0] + segments.slice(1).join('\\')
  }
  // Join with "/" (relative path, no leading slash)
  return segments.join('/')
}

const parts = computed(() => {
  if (!props.path || props.path === '.') return []
  const segments = splitPath(props.path).filter(p => p !== '')
  // On Windows, merge bare drive letter "C:" into "C:\"
  // so it displays as a single root crumb, not a broken segment
  if (segments.length > 0 && /^[A-Za-z]:$/.test(segments[0])) {
    segments[0] = segments[0] + '\\'
  }
  return segments
})
</script>

<style scoped>
.dir-breadcrumb {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  font-size: 13px;
  color: var(--text-muted, #999);
  scrollbar-width: none;
}
.dir-breadcrumb::-webkit-scrollbar {
  display: none;
}

.crumb {
  padding: 3px 6px;
  border-radius: 4px;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.15s;
  display: inline-flex;
  align-items: center;
}

.crumb:hover {
  background: var(--bg-secondary, #e0e0e0);
  color: var(--accent-color, #4a90d9);
}

.crumb.current {
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  cursor: default;
}

.crumb.current:hover {
  background: none;
  color: var(--text-primary, #1a1a1a);
}

.crumb-sep {
  color: var(--text-muted, #999);
  font-size: 11px;
}
</style>

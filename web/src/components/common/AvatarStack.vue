<template>
  <span v-if="members.length" class="avatar-stack" :title="resolvedTooltip">
    <span
      v-for="(m, i) in visible"
      :key="m.id"
      class="avatar-disc"
      :class="[`avatar-disc--${size}`, { 'is-host': m.isHost }]"
      :style="{ zIndex: visible.length - i }"
      :title="m.isHost ? `${m.name} (Host)` : m.name"
    >
      <AgentIcon :backend="m.backend" :name="m.name" :avatar="getAgentAvatar(m.agentId)" :size="size" />
    </span>
    <!-- Overflow count as plain text to the RIGHT, never a disc: it is extra
         info, not a member, and keeping it out of the stack leaves the overlap
         direction uniform (first on top, each later disc tucked behind).
         Suppressed when showCount is false (e.g. the dense session-list row,
         where the extra label is noise; the tooltip still lists everyone). -->
    <span v-if="showCount && overflowCount > 0" class="avatar-more">+{{ overflowCount }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import AgentIcon, { type AgentIconSize } from '@/components/common/AgentIcon.vue'
import { getAgentAvatar } from '@/composables/useAgents'

/** One member in the stack. The avatar is resolved from agentId internally, so
 *  callers pass the same wire shape they already have. */
export interface StackMember {
  id: string
  agentId: string
  name: string
  backend: string
  /** Draws the accent ring (the group host). */
  isHost?: boolean
}

const props = withDefaults(defineProps<{
  members: StackMember[]
  /** AgentIcon size token; also drives the disc diameter. */
  size?: AgentIconSize
  /** Max discs shown; the remainder becomes a "+N" text label. */
  max?: number
  /** Render the trailing "+N" label for members past `max`. Off for compact
   *  callers (the session-list row) where the label is noise. */
  showCount?: boolean
  /** Tooltip for the whole stack. Defaults to the member names joined. */
  tooltip?: string
}>(), {
  size: 'md',
  max: 4,
  showCount: true,
  tooltip: '',
})

// Show at most `max` discs; the rest are summarised by the "+N" label.
const visible = computed(() => props.members.slice(0, props.max))
const overflowCount = computed(() => Math.max(0, props.members.length - props.max))
const resolvedTooltip = computed(() =>
  props.tooltip || props.members.map(m => m.name).join(', '),
)
</script>

<style scoped>
.avatar-stack {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}
.avatar-disc {
  /* Diameter follows the icon size token so the disc crops the icon exactly.
     --disc-size / --disc-overlap are set per size class below. */
  width: var(--disc-size);
  height: var(--disc-size);
  padding: 0;
  border-radius: var(--radius-full);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-tertiary);
  /* Ring separates overlapping discs. --border-color (NOT --bg-secondary):
     in dark themes the latter is nearly the disc background, so the ring
     vanishes and the discs merge. */
  box-shadow: 0 0 0 1.5px var(--border-color);
  /* Each disc after the first slides LEFT under the previous one; z-index is
     inline (first = highest) so the first stays fully visible. */
  margin-left: calc(-1 * var(--disc-overlap));
  position: relative;
  transition: transform var(--duration-base) ease;
}
.avatar-disc:first-child { margin-left: 0; }

.avatar-disc--sm { --disc-size: var(--icon-size-sm, 14px); --disc-overlap: 6px; }
.avatar-disc--md { --disc-size: var(--icon-size-md, 18px); --disc-overlap: 6px; }
.avatar-disc--lg { --disc-size: var(--icon-size-lg, 24px); --disc-overlap: 8px; }
.avatar-disc--xl { --disc-size: var(--icon-size-xl, 40px); --disc-overlap: 12px; }

/* AgentIcon defaults to a 20%-rounded square; the disc crops it to a circle. */
.avatar-disc :deep(.agent-icon-img),
.avatar-disc :deep(.agent-icon-svg),
.avatar-disc :deep(.agent-icon-initial) {
  width: 100%;
  height: 100%;
  border-radius: var(--radius-full);
}
.avatar-disc :deep(.agent-icon-img) { object-fit: cover; }

/* Host ring (accent). No z-index here — overlap direction is DOM-driven. */
.avatar-disc.is-host {
  box-shadow: 0 0 0 1.5px var(--accent-color, #0066cc);
}

/* "+N" is plain text beside the stack — extra info, not a member. */
.avatar-more {
  margin-left: 6px;
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium, 500);
  color: var(--text-muted);
  line-height: 1;
}
</style>

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
      <AgentIcon :backend="m.backend" :name="m.name" :avatar="m.avatar || getAgentAvatar(m.agentId)" :size="size" />
    </span>
    <!-- Overflow count as plain text to the RIGHT, never a disc: it is extra
         info, not a member, and keeping it out of the stack leaves the overlap
         direction uniform (first on top, each later disc tucked behind). -->
    <span v-if="overflowCount > 0" class="avatar-more">+{{ overflowCount }}</span>
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
  /** Pre-resolved custom avatar SVG. Used by hosts that cannot reach the agent
   *  registry (the public share page freezes avatars into its snapshot), and
   *  takes precedence over `getAgentAvatar(agentId)`. */
  avatar?: string
}

const props = withDefaults(defineProps<{
  members: StackMember[]
  /** AgentIcon size token; also drives the disc diameter. */
  size?: AgentIconSize
  /** Max discs shown; the remainder becomes a "+N" text label. */
  max?: number
  /** Tooltip for the whole stack. Defaults to the member names joined. */
  tooltip?: string
}>(), {
  size: 'md',
  max: 4,
  tooltip: '',
})

// Host first, then everyone else in their original order. The host must lead
// the stack because the FIRST disc is drawn on top (highest z-index), so a host
// placed later would be tucked behind the others and hard to pick out.
const ordered = computed(() => {
  const host = props.members.find(m => m.isHost)
  if (!host) return props.members
  return [host, ...props.members.filter(m => m !== host)]
})

// Show at most `max` discs; the rest are summarised by the "+N" label.
const visible = computed(() => ordered.value.slice(0, props.max))
const overflowCount = computed(() => Math.max(0, ordered.value.length - props.max))
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
  /* Every disc carries the accent ring, so the stack reads as one accent-
     bordered family. The host is distinguished by POSITION (it leads the
     stack, so it is fully visible) rather than by a different ring colour.
     --accent-color (NOT --border-color): the latter nearly vanishes against
     the disc background in dark themes. */
  box-shadow: 0 0 0 1.5px var(--accent-color, #0066cc);
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

/* The host leads the stack (see `ordered`), so it is already fully visible;
   no ring override is needed. Kept as a hook for tests/consumers. */
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

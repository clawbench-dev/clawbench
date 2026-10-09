<template>
  <div
    v-if="isGroup"
    class="group-avatar-stack"
    role="button"
    tabindex="0"
    :title="t('group.addMembers')"
    @click="openSheet"
    @keydown.enter.prevent="openSheet"
    @keydown.space.prevent="openSheet"
  >
    <!-- Group-type badge. Leads the stack but sits OUTSIDE the avatar overlap,
         so it reads as a label about the group rather than another member. It
         reuses the session list's glyph vocabulary (crown = host mode, @ = free
         mode) and its faint-pill treatment. Decorative: the whole strip is one
         click target, so it stays out of the a11y tree and only shows a hover
         tooltip naming the mode. -->
    <span class="group-mode-badge" :title="modeLabel" aria-hidden="true">
      <component :is="modeIcon" :size="12" />
    </span>
    <AvatarStack :members="stackMembers" size="md" :max="4" />
    <GroupSettingsSheet
      ref="sheetRef"
      :groupId="sessionId"
      :members="members"
      :hostMemberId="hostMemberId"
      :maxRounds="maxRounds"
      :autoApprove="autoApprove"
      :mode="mode"
      :parallelDefault="parallelDefault"
      @changed="$emit('changed')"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Crown, AtSign } from 'lucide-vue-next'
import AvatarStack from '@/components/common/AvatarStack.vue'
import GroupSettingsSheet from './GroupSettingsSheet.vue'
import type { GroupMemberInfo } from '@/composables/useGroupChat'

const props = defineProps<{
  sessionId: string
  members: GroupMemberInfo[]
  hostMemberId: string
  isGroup: boolean
  /** The group's current maxRounds (from the roster endpoint), shown in the sheet. */
  maxRounds: number
  /** The group's current auto-approve flag, shown in the sheet (decision #61). */
  autoApprove: boolean
  /** The group's mode; "free" hides the maxRounds control in the sheet. */
  mode?: 'host' | 'free'
  /** Free-mode concurrency switch value, shown in the sheet (free mode only). */
  parallelDefault?: boolean
}>()
defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const sheetRef = ref<InstanceType<typeof GroupSettingsSheet> | null>(null)

// Map the group roster to the generic stack shape (isHost drives the ring).
const stackMembers = computed(() =>
  props.members.map(m => ({
    id: m.id,
    agentId: m.agentId,
    name: m.name,
    backend: m.backend,
    isHost: m.isHost,
  })),
)

function openSheet() {
  sheetRef.value?.open()
}

// The action bar's group-settings button opens the exact same sheet as clicking
// the stack, so it drives this component instead of mounting a second instance
// (a second instance would mean a second roster fetch and two sheets that can
// drift out of sync).
defineExpose({ open: openSheet })

// The badge mirrors the session list's mode glyphs: crown = host mode (the host
// routes turns), @ = free mode (members @-mention each other to hand over the
// floor). `mode` is undefined outside a group, which falls back to the crown —
// but the badge only renders inside a group anyway.
const modeIcon = computed(() => (props.mode === 'free' ? AtSign : Crown))
const modeLabel = computed(() => t(props.mode === 'free' ? 'group.freeMode' : 'group.hostMode'))
</script>

<style scoped>
/* The whole strip is the click target — no separate "+" button. */
.group-avatar-stack {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  cursor: pointer;
  outline: none;
}
/* Faint pill holding the group-type glyph. Same --bg-tertiary fill as the
   avatar discs so it reads as part of the same family, but muted text (not the
   accent) so it stays subordinate to the host disc's accent ring. */
.group-mode-badge {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  color: var(--text-muted, #999);
  line-height: 0;
}
.group-avatar-stack:focus-visible {
  outline: 2px solid var(--accent-color, #0066cc);
  outline-offset: 2px;
  border-radius: var(--radius-full);
}
@media (hover: hover) {
  .group-avatar-stack:hover :deep(.avatar-disc) { transform: translateY(-2px); }
  .group-avatar-stack:hover :deep(.avatar-disc:hover) { z-index: 6; }
}
</style>

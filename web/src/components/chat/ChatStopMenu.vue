<template>
  <PopupMenu
    :show="show"
    :target-element="targetElement"
    anchor="right"
    :max-width="268"
    :min-width="248"
    :max-height="360"
    :menu-items-count="Math.max(targets.length, 1) + 2"
    :close-on-click="false"
    @update:show="emit('update:show', $event)"
  >
    <div class="stop-menu">
      <div class="stop-menu-hd">
        <span class="stop-menu-title">{{ t('chat.input.stopMenuTitle', { count: targets.length }) }}</span>
        <span class="stop-menu-hint">{{ t('chat.input.stopMenuHint') }}</span>
      </div>
      <button
        v-for="target in targets"
        :key="target.id"
        class="stop-menu-item"
        data-action="stop-member"
        :data-member-id="target.id"
        @click="onStopMember(target.id)"
      >
        <AgentIcon :backend="target.backend" :name="target.name" :avatar="target.avatar" size="sm" />
        <span class="stop-menu-main">
          <span class="stop-menu-name">{{ target.name }}</span>
          <span class="stop-menu-meta">{{ metaFor(target) }}</span>
        </span>
        <span class="stop-menu-act">
          <Square :size="8" fill="currentColor" aria-hidden="true" />
          {{ t('chat.input.stopMember') }}
        </span>
      </button>
      <button
        class="stop-menu-all"
        :class="{ 'is-confirming': confirmingAll }"
        data-action="stop-all"
        @click="onStopAll"
      >
        <Square :size="9" fill="currentColor" aria-hidden="true" />
        <span>{{ confirmingAll ? t('chat.input.stopAllConfirm') : t('chat.input.stopAll') }}</span>
        <span v-if="!confirmingAll" class="stop-menu-all-hint">{{ t('chat.input.stopAllHint') }}</span>
      </button>
    </div>
  </PopupMenu>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Square } from 'lucide-vue-next'
import PopupMenu from '@/components/common/PopupMenu.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'

import type { StopTarget } from '@/utils/chatStopTargets'

const props = defineProps({
  show: Boolean,
  targetElement: { type: Object, default: null },
  targets: { type: Array as () => StopTarget[], default: () => [] },
})

const emit = defineEmits(['update:show', 'stop-member', 'stop-all'])

const { t } = useI18n()

// "Stop all" is the old whole-turn cancel — it also drops the queue, so it keeps
// the two-step confirm the stop button used to have. Choosing a single member is
// one click because opening the menu already was the deliberate first step.
const confirmingAll = ref(false)

// The elapsed time is a snapshot taken when the menu opens: the menu lives for
// a few seconds, and a ticking timer would re-render it under the user's finger
// for no gain.
const openedAt = ref(Date.now())
watch(() => props.show, (open) => {
  if (open) {
    openedAt.value = Date.now()
    confirmingAll.value = false
  }
})

function metaFor(target: StopTarget): string {
  const started = target.startedAt || 0
  if (!started) return t('chat.input.stopMemberSpeaking')
  const seconds = Math.max(0, Math.round((openedAt.value - started) / 1000))
  return t('chat.input.stopMemberSpokenFor', { seconds })
}

function onStopMember(id: string) {
  emit('update:show', false)
  emit('stop-member', id)
}

function onStopAll() {
  if (!confirmingAll.value) {
    confirmingAll.value = true
    return
  }
  confirmingAll.value = false
  emit('update:show', false)
  emit('stop-all')
}
</script>

<style scoped>
.stop-menu {
  display: flex;
  flex-direction: column;
  padding: var(--space-1) 0;
}
.stop-menu-hd {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4) var(--space-1);
}
.stop-menu-title {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
}
.stop-menu-hint {
  font-size: var(--font-size-xs);
  color: var(--text-hint);
}
.stop-menu-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-2) var(--space-4);
  border: none;
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
}
.stop-menu-item:hover,
.stop-menu-item:focus-visible {
  background: var(--bg-hover);
  outline: none;
}
.stop-menu-main {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1;
}
.stop-menu-name {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.stop-menu-meta {
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
.stop-menu-act {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
  padding: 2px var(--space-2);
  border-radius: var(--radius-full);
  font-size: var(--font-size-xs);
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
}
.stop-menu-all {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  margin-top: var(--space-1);
  padding: var(--space-3) var(--space-4);
  border: none;
  border-top: 1px solid var(--border-color);
  background: transparent;
  color: var(--color-red);
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
}
.stop-menu-all:hover,
.stop-menu-all:focus-visible {
  background: color-mix(in srgb, var(--color-red) 10%, transparent);
  outline: none;
}
.stop-menu-all.is-confirming {
  font-weight: var(--font-weight-semibold);
  background: color-mix(in srgb, var(--color-red) 14%, transparent);
}
.stop-menu-all-hint {
  margin-left: auto;
  font-size: var(--font-size-xs);
  color: var(--text-hint);
}
</style>

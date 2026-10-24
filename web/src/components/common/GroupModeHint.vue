<template>
  <span class="group-mode-hint">
    <!-- The whole chip (glyph + label) is the trigger: desktop pointers hover,
         touch devices tap. Same Crown / AtSign pairing as the session-list mode
         chip so the two surfaces read as one system. -->
    <button
      ref="chipEl"
      type="button"
      class="group-mode-hint-chip"
      :class="`group-mode-hint-chip--${mode}`"
      @click.stop="onChipClick"
      @mouseenter="onChipEnter"
      @mouseleave="onChipLeave"
    >
      <component :is="modeIcon" :size="12" class="group-mode-hint-icon" />
      {{ modeLabel }}
    </button>
    <!-- Popover reuses PopupMenu for teleport + zoom-aware positioning + outside
         click. Slot content is compiled here, so this component's scoped styles
         apply (no unscoped block needed). -->
    <PopupMenu
      :show="show"
      :target-element="chipEl"
      :max-width="280"
      :max-height="340"
      :menu-items-count="6"
      anchor="left"
      @update:show="(v: boolean) => (show = v)"
    >
      <div class="group-mode-hint-card">
        <span class="group-mode-hint-title">
          <component :is="modeIcon" :size="13" class="group-mode-hint-icon" />
          {{ modeTitle }}
        </span>
        <span class="group-mode-hint-desc">{{ modeDesc }}</span>
        <ul class="group-mode-hint-tips">
          <li v-for="(tip, i) in modeTips" :key="i">{{ tip }}</li>
        </ul>
      </div>
    </PopupMenu>
  </span>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Crown, AtSign } from 'lucide-vue-next'
import PopupMenu from '@/components/common/PopupMenu.vue'

const props = defineProps<{
  /** Group mode. Selects the glyph and which set of explanation copy to show. */
  mode: 'host' | 'free'
}>()

const { t } = useI18n()

const chipEl = ref<HTMLElement | null>(null)
const show = ref(false)

const modeIcon = computed(() => (props.mode === 'free' ? AtSign : Crown))
const modeLabel = computed(() => t(props.mode === 'free' ? 'group.freeMode' : 'group.hostMode'))
const modeTitle = computed(() => t(props.mode === 'free' ? 'chat.messageList.groupModeFreeTitle' : 'chat.messageList.groupModeHostTitle'))
const modeDesc = computed(() => t(props.mode === 'free' ? 'chat.messageList.groupModeFreeDesc' : 'chat.messageList.groupModeHostDesc'))
const modeTips = computed(() =>
  props.mode === 'free'
    ? [t('chat.messageList.groupModeFreeTip1'), t('chat.messageList.groupModeFreeTip2'), t('chat.messageList.groupModeFreeTip3')]
    : [t('chat.messageList.groupModeHostTip1'), t('chat.messageList.groupModeHostTip2'), t('chat.messageList.groupModeHostTip3')],
)

/** Desktop pointers only — every touch device claims hover, so gate on it. */
const hoverCapable = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
  && window.matchMedia('(hover: hover)').matches

function onChipEnter() {
  if (hoverCapable) show.value = true
}
function onChipLeave() {
  if (hoverCapable) show.value = false
}
function onChipClick() {
  // Hover already drives the popover on pointer devices; only touch toggles.
  if (hoverCapable) return
  show.value = !show.value
}
</script>

<style scoped>
.group-mode-hint {
  display: inline-flex;
  align-items: center;
}

/* The chip mirrors the session-list mode chip recipe (muted tertiary surface,
   full-radius pill) so the two surfaces stay visually consistent. */
.group-mode-hint-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
  padding: calc(2 * var(--dp)) var(--space-3);
  border: none;
  border-radius: var(--radius-full);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  line-height: 1.4;
  white-space: nowrap;
  cursor: default;
  -webkit-tap-highlight-color: transparent;
}

@media (hover: hover) {
  .group-mode-hint-chip:hover {
    color: var(--text-primary);
  }
}

.group-mode-hint-icon {
  flex-shrink: 0;
}

/* Popover card — same family as the group empty-state card (title / one-line /
   bulleted tips). */
.group-mode-hint-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  text-align: left;
}

.group-mode-hint-title {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

.group-mode-hint-desc {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  line-height: var(--line-height-snug);
}

.group-mode-hint-tips {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.group-mode-hint-tips li {
  position: relative;
  padding-left: var(--space-6);
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  line-height: var(--line-height-snug);
}

.group-mode-hint-tips li::before {
  content: '';
  position: absolute;
  left: calc(3 * var(--dp));
  top: 0.55em;
  width: calc(3 * var(--dp));
  height: calc(3 * var(--dp));
  border-radius: 50%;
  background: currentColor;
  opacity: var(--opacity-muted);
}
</style>

<template>
  <BottomSheet :open="open" auto :title="t('terminal.helpTitle')" @close="$emit('close')">
    <template #header>
      <CircleHelpIcon :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ t('terminal.helpTitle') }}</span>
    </template>

    <div class="th-content">
      <section v-for="sec in sections" :key="sec.key" class="th-section">
        <h3 class="th-section-title">{{ t(sec.titleKey) }}</h3>
        <ul class="th-list">
          <li v-for="item in sec.items" :key="item.key" class="th-item">
            <span class="th-name">{{ item.name ?? t(item.nameKey) }}</span>
            <span class="th-desc">{{ t(item.descKey) }}</span>
          </li>
        </ul>
      </section>
    </div>
  </BottomSheet>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import { CircleHelp as CircleHelpIcon } from 'lucide-vue-next'

const props = defineProps({
  open: Boolean,
  /** Touch-capable platform — gestures only apply here. */
  gestures: Boolean,
  /** Android app mode — hardware volume keys forward arrows. */
  appMode: Boolean,
  /** macOS desktop — the copy chord is Cmd+C there, not Ctrl+Shift+C. */
  mac: Boolean,
  /** Copy-on-select is enabled — the selection reaches the clipboard by itself. */
  copyOnSelect: Boolean,
})
defineEmits(['close'])

const { t } = useI18n()

interface HelpItem {
  key: string
  nameKey: string
  descKey: string
  /** Literal label shown instead of t(nameKey) — used when the key label varies by platform. */
  name?: string
}
interface HelpSection {
  key: string
  titleKey: string
  items: HelpItem[]
}

const sections = computed<HelpSection[]>(() => {
  const list: HelpSection[] = []

  // Gestures are touch-only.
  if (props.gestures) {
    list.push({
      key: 'gestures',
      titleKey: 'terminal.helpSectionGestures',
      items: [
        { key: 'swipe', nameKey: 'terminal.helpGestureSwipe', descKey: 'terminal.helpGestureSwipeDesc' },
        { key: 'doubletap', nameKey: 'terminal.helpGestureDoubleTap', descKey: 'terminal.helpGestureDoubleTapDesc' },
        { key: 'twofinger', nameKey: 'terminal.helpGestureTwoFinger', descKey: 'terminal.helpGestureTwoFingerDesc' },
        { key: 'pinch', nameKey: 'terminal.helpGesturePinch', descKey: 'terminal.helpGesturePinchDesc' },
      ],
    })
  }

  // Physical keyboard shortcuts (Ctrl+C etc.) only apply to desktop keyboards,
  // not touch devices. On touch, those are toolbar buttons / gestures instead.
  if (!props.gestures) {
    list.push({
      key: 'shortcuts',
      titleKey: 'terminal.helpSectionShortcuts',
      items: [
        { key: 'ctrlc', nameKey: 'terminal.helpShortcutCtrlC', descKey: 'terminal.helpShortcutCtrlCDesc' },
        { key: 'ctrld', nameKey: 'terminal.helpShortcutCtrlD', descKey: 'terminal.helpShortcutCtrlDDesc' },
        { key: 'ctrlz', nameKey: 'terminal.helpShortcutCtrlZ', descKey: 'terminal.helpShortcutCtrlZDesc' },
        { key: 'ctrll', nameKey: 'terminal.helpShortcutCtrlL', descKey: 'terminal.helpShortcutCtrlLDesc' },
      ],
    })
  }

  const common: HelpItem[] = [
    { key: 'keys', nameKey: 'terminal.helpCommonKeys', descKey: 'terminal.helpCommonKeysDesc' },
    { key: 'repeat', nameKey: 'terminal.helpCommonRepeat', descKey: 'terminal.helpCommonRepeatDesc' },
  ]
  // Copy-on-select is on by default, so it leads the copy entries: it is the
  // path most users will actually take, and the explicit chords below are the
  // fallback for when it is switched off.
  if (props.copyOnSelect) {
    common.push({ key: 'copyOnSelect', nameKey: 'terminal.helpCommonCopyOnSelect', descKey: 'terminal.helpCommonCopyOnSelectDesc' })
  }
  common.push(
    // The copy chord differs by platform, so show a literal label rather than a
    // single translated string (macOS reserves Cmd+C and keeps Ctrl+C as SIGINT).
    {
      key: 'copy',
      nameKey: 'terminal.helpCommonCopy',
      descKey: 'terminal.helpCommonCopyDesc',
      name: props.mac ? 'Cmd+C' : 'Ctrl+C / Ctrl+Shift+C',
    },
    { key: 'copyInsert', nameKey: 'terminal.helpCommonCopyInsert', descKey: 'terminal.helpCommonCopyInsertDesc' },
    { key: 'rightClick', nameKey: 'terminal.helpCommonRightClick', descKey: 'terminal.helpCommonRightClickDesc' },
    { key: 'tools', nameKey: 'terminal.helpCommonTools', descKey: 'terminal.helpCommonToolsDesc' },
  )
  // Android app mode: hardware volume keys forward arrows.
  if (props.appMode) {
    common.push({ key: 'volume', nameKey: 'terminal.helpVolumeKeys', descKey: 'terminal.helpVolumeKeysDesc' })
  }
  list.push({ key: 'common', titleKey: 'terminal.helpSectionCommon', items: common })

  return list
})
</script>

<style>
.th-content {
  padding: var(--space-1) var(--space-7) var(--space-7);
}

.th-section {
  margin-bottom: 14px;
}
.th-section:last-child {
  margin-bottom: 0;
}

.th-section-title {
  margin:0 0 var(--space-4);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-bold);
  color: var(--text-secondary, #495057);
  letter-spacing: 0.02em;
}

.th-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.th-item {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-6);
  font-size: var(--font-size-md);
  line-height: var(--line-height-snug);
}

.th-name {
  flex-shrink: 0;
  font-weight: var(--font-weight-bold);
  color: var(--text-primary, #1a1a1a);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary, #f1f3f5);
  white-space: nowrap;
}

.th-desc {
  flex: 1;
  text-align: right;
  color: var(--text-muted, #6c757d);
  word-break: break-word;
}
</style>

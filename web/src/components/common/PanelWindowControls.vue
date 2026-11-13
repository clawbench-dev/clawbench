<template>
  <!--
    Window controls for the left-column panel, drawn at the right end of the
    panel's own header row: minimize (hide) and maximize/restore (give the
    panel the whole width). There is deliberately no close button — it did
    exactly what minimize does, and two identical controls in a three-cell
    block read as a mistake.

    Absolutely positioned on purpose: every panel header is a different height
    (26 / 28 / 34 / 36 design px) and a different flex row, so a flex child
    would either be squeezed or force a height. `top: 0; bottom: 0` takes the
    header's real height, and the header reserves room with
    `--panel-controls-w` (see components.css) so its own buttons never sit
    underneath.

    Wide screens only: on a single-column layout there is no second pane to
    hide or maximize.
  -->
  <div v-if="isWideScreen" class="panel-window-controls">
    <button
      class="panel-control"
      type="button"
      :title="t('nav.minimizePanel')"
      :aria-label="t('nav.minimizePanel')"
      @click.stop="minimizeLeftPanel"
    >
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M5 12h14" /></svg>
    </button>
    <button
      class="panel-control"
      type="button"
      :title="chatCollapsed ? t('nav.restorePanel') : t('nav.maximizePanel')"
      :aria-label="chatCollapsed ? t('nav.restorePanel') : t('nav.maximizePanel')"
      @click.stop="toggleChatPane"
    >
      <!-- Hand-drawn to match the desktop shell's window cluster (AppHeader):
           lucide's Square sits on a rounder, smaller box than the OS glyphs. -->
      <svg v-if="chatCollapsed" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <rect x="8" y="8" width="14" height="14" rx="1.5" />
        <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
      </svg>
      <svg v-else width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <rect x="4" y="4" width="16" height="16" rx="1.5" />
      </svg>
    </button>
    <!-- Only the file viewer passes `closable`: closing its open file is a
         distinct action. Everywhere else "close the panel" would be the same
         thing as minimize, so the button is not rendered at all. -->
    <button
      v-if="closable"
      class="panel-control panel-control--close"
      type="button"
      :title="closeTitle || t('nav.closePanel')"
      :aria-label="closeTitle || t('nav.closePanel')"
      @click.stop="emit('close')"
    >
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  getWideScreenState,
  minimizeLeftPanel,
  toggleChatPane,
} from '@/composables/useWideScreenLayout'

withDefaults(defineProps<{
  /** Render the third (close) cell. Only the file viewer needs it. */
  closable?: boolean
  /** Tooltip for that cell — the file viewer closes a file, not the panel. */
  closeTitle?: string
}>(), {
  closable: false,
  closeTitle: '',
})

const emit = defineEmits<{ (e: 'close'): void }>()

const { t } = useI18n()
const { isWideScreen, chatCollapsed } = getWideScreenState()
</script>

<style scoped>
.panel-window-controls {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  z-index: 3;
  display: flex;
  overflow: hidden;
  /* Mixed from the text colour, not a fixed background token: headers use
     --bg-primary, --bg-secondary and transparent (wallpaper) backgrounds, and
     a fixed token would show as a patch on two of the three. */
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  user-select: none;
}

.panel-control {
  width: calc(26 * var(--dp));
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast);
}

.panel-control svg {
  flex: 0 0 auto;
}

@media (hover: hover) {
  .panel-control:hover {
    background: color-mix(in srgb, var(--text-primary) 14%, transparent);
    color: var(--text-primary);
  }}

.panel-control:active {
  background: color-mix(in srgb, var(--text-primary) 22%, transparent);
}</style>

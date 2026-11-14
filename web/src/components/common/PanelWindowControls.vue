<template>
  <!--
    Window controls for the left-column panel: minimize (hide) and
    maximize/restore (give the panel the whole width). Rendered in the panel
    title bar, not in a panel's own header — a drill-down page replaces that
    header, and the controls must not disappear with it. There is deliberately
    no close button: closing the panel is what minimize does.

    Absolutely positioned inside the title bar: `top: 0; bottom: 0` fills the
    bar's height, and the bar reserves their width with padding.

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
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  getWideScreenState,
  minimizeLeftPanel,
  toggleChatPane,
} from '@/composables/useWideScreenLayout'

const { t } = useI18n()
const { isWideScreen, chatCollapsed } = getWideScreenState()
</script>

<style scoped>
.panel-window-controls {
  position: absolute;
  top: 0;
  /* Same edge inset as the header's own left padding, so the panel controls
     do not sit closer to the right edge than the section glyph does to the
     left one. */
  right: var(--panel-header-pad-x);
  /* Stop above the header's 1px bottom border: painting over it tinted the
     line under the buttons, so the border changed colour at the right end. */
  bottom: 1px;
  z-index: 3;
  display: flex;
  overflow: hidden;
  /* The overlay that hosts this cluster is pointer-events: none so the panel
     header underneath stays clickable; the buttons must opt back in. */
  pointer-events: auto;
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

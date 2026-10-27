<template>
  <!-- Quick theme picker for the public share SPA.
       Same button + popup as the app header's (components/common/AppHeader.vue):
       the state machine is shared through useThemePicker, the row styling
       through assets/theme-picker.css. Only the trigger's placement differs —
       here it sits in .share-top-actions instead of the app header cluster. -->
  <button
    ref="btnRef"
    class="share-btn share-theme-btn"
    type="button"
    :title="t('appHeader.themePicker')"
    :aria-label="t('appHeader.themePicker')"
    :aria-expanded="menuOpen"
    @click="toggle"
  >
    <Palette :size="16" />
  </button>
  <PopupMenu
    v-model:show="menuOpen"
    :target-element="btnRef"
    :max-width="200"
    :max-height="440"
    :menu-items-count="options.length"
    anchor="right"
    app-surface
    @after-leave="onMenuAfterLeave"
  >
    <div class="theme-picker app-menu-column">
      <div class="app-menu-title">{{ t('terminal.theme') }}</div>
      <div class="app-menu-scroll">
        <button
          v-for="opt in options"
          :key="opt.value"
          class="theme-item"
          role="menuitem"
          tabindex="-1"
          :class="{ active: currentValue === opt.value }"
          :style="previewStyle(opt.value)"
          @click="selectTheme(opt.value)"
          @keydown.enter="selectTheme(opt.value)"
          @keydown.space.prevent="selectTheme(opt.value)"
        >
          <span
            class="theme-swatch"
            :class="{ 'theme-swatch--auto': opt.value === 'auto' }"
            aria-hidden="true"
          ></span>
          <span class="theme-item-name">{{ opt.label }}</span>
          <component :is="baseIcon(opt.value)" :size="12" class="theme-item-base-icon" />
        </button>
      </div>
    </div>
  </PopupMenu>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Palette } from 'lucide-vue-next'
import PopupMenu from '@/components/common/PopupMenu.vue'
import { useThemePicker } from '@/composables/useThemePicker'
// Row styling. Global (the popup is teleported to <body>) and imported at the
// component that needs it, the same way the app's other primitive sheets are
// (`assets/modal-footer-btn.css` etc.). The container chrome it also relies on
// (`.app-menu-column` / `.app-menu-title` / `.menu-divider`) is in
// `css/components.css`, which the share entry already loads.
import '@/assets/theme-picker.css'

const { t } = useI18n()

const {
  btnRef,
  menuOpen,
  currentValue,
  options,
  toggle,
  selectTheme,
  onMenuAfterLeave,
  previewStyle,
  baseIcon,
} = useThemePicker()
</script>

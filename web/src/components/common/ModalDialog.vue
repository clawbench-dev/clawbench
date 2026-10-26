<template>
  <Teleport to="body">
    <div
      v-if="everOpened"
      v-show="open || leaving"
      ref="overlayRef"
      class="modal-overlay"
      :class="{ 'modal-leaving': leaving }"
      :style="{ zIndex }"
      tabindex="-1"
      @click.self="handleClose"
      @keydown.escape="handleClose"
    >
      <div
        class="modal-dialog"
        :class="{
          'modal-leaving': leaving,
          'modal-full-height': fullHeight,
          'modal-dialog--compact': size === 'compact',
        }"
        @click.stop
      >
        <div class="modal-header">
          <slot name="header">
            <span class="modal-title">{{ title }}</span>
          </slot>
          <button
            type="button"
            class="modal-close-btn"
            :aria-label="closeLabel"
            :title="closeLabel"
            @click.stop="handleClose"
          >
            <X :size="16" />
          </button>
        </div>
        <div class="modal-body">
          <slot />
        </div>
        <div class="modal-footer" :class="{ 'modal-footer-default': !$slots.footer }">
          <slot name="footer" />
        </div>
        <slot name="after" />
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch, nextTick, computed, onBeforeUnmount } from 'vue'
import { X } from 'lucide-vue-next'
import i18n from '@/i18n'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'
import '@/assets/modal-card.css'
import '@/assets/modal-footer-btn.css'

const props = defineProps({
  open: Boolean,
  title: { type: String, default: '' },
  zIndex: { type: Number, default: 2100 },
  fullHeight: { type: Boolean, default: false },
  /**
   * Named width variant. Arbitrary per-call-site widths are forbidden (see
   * ModalDialog.test.ts): a compact card is a shared primitive so its cap lives
   * in modal-card.css next to the default one.
   */
  size: { type: String, default: 'default' },
})

const emit = defineEmits(['close'])

const closeLabel = computed(() => i18n.global.t('common.close'))

const leaving = ref(false)
const everOpened = ref(false)
const overlayRef = ref(null)
let leaveTimer = null
let unregisterBack = null

// Register a back handler while open so the Android hardware back button and
// the web edge-swipe gesture (both dispatch `clawbench-back-press`) close this
// dialog instead of falling through to a page-level handler / app exit.
//
// Priority is `PRIORITY_OVERLAY + 1`: dialogs paint above every BottomSheet
// drawer (z-index 2100/2500 vs the 1000 band; drawers rank at
// `PRIORITY_OVERLAY + seq*0.001` ≤ 1000.049), so a dialog opened from inside a
// drawer must win the back press. A consumer that must intercept back BEFORE
// the dialog closes (e.g. block dismissal during an in-flight submit) registers
// its own handler at a still-higher priority.
const BACK_PRIORITY = PRIORITY_OVERLAY + 1

function registerBack() {
  if (unregisterBack) return
  unregisterBack = registerBackHandler({
    id: 'modal-dialog',
    canGoBack: () => props.open && !leaving.value,
    goBack: () => handleClose(),
    priority: BACK_PRIORITY,
  })
}

function unregisterBackHandler() {
  if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
}

watch(() => props.open, (val) => {
  clearTimeout(leaveTimer)
  if (val) {
    everOpened.value = true
    leaving.value = false
    // Auto-focus overlay so Escape key works immediately
    nextTick(() => {
      overlayRef.value?.focus()
    })
    registerBack()
  } else if (leaving.value) {
    // Close triggered externally while animating — cancel animation, hide now
    leaving.value = false
  }
  if (!val) unregisterBackHandler()
}, { immediate: true })

onBeforeUnmount(unregisterBackHandler)

function handleClose() {
  if (leaving.value) return
  leaving.value = true
  leaveTimer = setTimeout(() => {
    leaving.value = false
    leaveTimer = null
    emit('close')
  }, 250)
}

defineExpose({
  close: handleClose,
})
</script>

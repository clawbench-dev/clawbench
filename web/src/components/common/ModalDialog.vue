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
    >
      <div
        class="modal-dialog"
        :class="{
          'modal-leaving': leaving,
          'modal-full-height': fullHeight,
          'modal-dialog--narrow': size === 'narrow',
          'modal-dialog--wide': size === 'wide',
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
        <!-- ⚠️ NO PADDING — .modal-body has NO inset of its own (see
             modal-card.css). Every consumer MUST add its own padding, or the
             content sits flush against the card's left and right edges, right
             under the header, and looks broken. -->
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
import { pushEscapeLayer } from '@/utils/escapeStack'
import '@/assets/modal-card.css'
import '@/assets/modal-footer-btn.css'

const props = defineProps({
  open: Boolean,
  title: { type: String, default: '' },
  zIndex: { type: Number, default: 2100 },
  fullHeight: { type: Boolean, default: false },
  /**
   * Width tier. There are exactly two, and both live in css/variables.css as
   * `--modal-width-narrow` / `--modal-width-wide`; this prop only selects one
   * (see modal-card.css). Arbitrary per-call-site widths are forbidden
   * (ModalDialog.test.ts): a dialog chooses a tier, never a number.
   *
   *   'narrow' (default) — forms, lists, confirmations: one readable column
   *   'wide'             — diffs, code, transcripts, tables/grids
   */
  size: {
    type: String,
    default: 'narrow',
    validator: (v) => v === 'narrow' || v === 'wide',
  },
})

const emit = defineEmits(['close'])

const closeLabel = computed(() => i18n.global.t('common.close'))

const leaving = ref(false)
const everOpened = ref(false)
const overlayRef = ref(null)
let leaveTimer = null
let unregisterBack = null
let releaseEscape = null

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

// Escape is handled at window level (see utils/escapeStack), not on the
// overlay: opening a dialog from a popup menu can leave focus on the trigger
// behind the overlay, and an overlay-scoped handler never sees the key. The
// stack is also what makes nesting correct — one Escape closes only the
// topmost dialog instead of every open one.
function registerEscape() {
  if (releaseEscape) return
  releaseEscape = pushEscapeLayer(handleClose)
}

function releaseEscapeLayer() {
  if (!releaseEscape) return
  releaseEscape()
  releaseEscape = null
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
    registerEscape()
  } else if (leaving.value) {
    // Close triggered externally while animating — cancel animation, hide now
    leaving.value = false
  }
  if (!val) {
    unregisterBackHandler()
    releaseEscapeLayer()
  }
}, { immediate: true })

onBeforeUnmount(() => {
  unregisterBackHandler()
  releaseEscapeLayer()
})

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

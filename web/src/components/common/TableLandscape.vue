<template>
  <Teleport to="body">
    <div
      v-if="state"
      ref="overlayRef"
      class="table-landscape"
      :class="{ 'is-rotated': rotated }"
      role="dialog"
      aria-modal="true"
      :aria-label="t('tableBlock.landscape')"
    >
      <div class="table-landscape-panel">
        <div class="table-landscape-toolbar">
          <span class="table-landscape-title">{{ t('tableBlock.landscape') }}</span>
          <button
            ref="closeBtnRef"
            type="button"
            class="table-landscape-close"
            :title="t('tableBlock.landscapeClose')"
            :aria-label="t('tableBlock.landscapeClose')"
            @click="close"
          >
            <X :size="20" />
          </button>
        </div>
        <div class="table-landscape-scroll" @click.self="close">
          <div class="markdown-body table-landscape-body" :class="state.wrapClass" v-html="state.html" />
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import {
  closeTableLandscape,
  shouldRotateTableLandscape,
  tableLandscapeState as state,
} from '@/composables/useTableLandscape.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect.ts'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

const { t } = useI18n()
const { isTouchPrimary } = usePlatformDetect()

const overlayRef = ref<HTMLElement | null>(null)
const closeBtnRef = ref<HTMLButtonElement | null>(null)

// `innerWidth/innerHeight` (layout viewport), not `visualViewport`: the
// decision is "is this device held upright", which the layout viewport answers
// without flip-flopping while the address bar animates.
const viewport = ref({ width: window.innerWidth, height: window.innerHeight })

const rotated = computed(() => shouldRotateTableLandscape({
  isTouchPrimary: isTouchPrimary.value,
  width: viewport.value.width,
  height: viewport.value.height,
}))

let restoreFocusTo: HTMLElement | null = null
let unregisterBack: (() => void) | null = null

function onViewportChange(): void {
  viewport.value = { width: window.innerWidth, height: window.innerHeight }
}

function close(): void {
  closeTableLandscape()
}

/**
 * ESC closes; Tab is trapped inside the overlay so keyboard and screen-reader
 * users cannot wander into the page hidden behind it.
 */
function onDocumentKeydown(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close()
    return
  }
  if (event.key !== 'Tab') return

  const root = overlayRef.value
  if (!root) return
  const focusables = Array.from(root.querySelectorAll<HTMLElement>(
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
  ))
  if (focusables.length === 0) {
    event.preventDefault()
    return
  }
  const first = focusables[0]
  const last = focusables[focusables.length - 1]
  const active = document.activeElement as HTMLElement | null
  const inside = !!active && root.contains(active)

  if (event.shiftKey) {
    if (!inside || active === first) {
      event.preventDefault()
      last.focus()
    }
  } else if (!inside || active === last) {
    event.preventDefault()
    first.focus()
  }
}

function detach(): void {
  document.body.style.overflow = ''
  window.removeEventListener('resize', onViewportChange)
  window.visualViewport?.removeEventListener('resize', onViewportChange)
  document.removeEventListener('keydown', onDocumentKeydown, true)
  if (unregisterBack) {
    unregisterBack()
    unregisterBack = null
  }
  // The trigger may have been re-rendered away while the overlay was open (a
  // streaming message re-renders its table header), so only focus it when it is
  // still in the document.
  if (restoreFocusTo?.isConnected) {
    restoreFocusTo.focus({ preventScroll: true })
  }
  restoreFocusTo = null
}

watch(state, async (next) => {
  if (next) {
    restoreFocusTo = next.trigger ?? (document.activeElement as HTMLElement | null)
    document.body.style.overflow = 'hidden'
    window.addEventListener('resize', onViewportChange)
    window.visualViewport?.addEventListener('resize', onViewportChange)
    document.addEventListener('keydown', onDocumentKeydown, true)
    unregisterBack = registerBackHandler({
      id: 'table-landscape',
      canGoBack: () => state.value !== null,
      goBack: close,
      priority: PRIORITY_OVERLAY,
    })
    await nextTick()
    closeBtnRef.value?.focus({ preventScroll: true })
  } else {
    detach()
  }
}, { immediate: true })

onBeforeUnmount(() => {
  // Leaving the page with the overlay open would otherwise strand a fixed layer
  // over the next route's content.
  if (state.value) closeTableLandscape()
  detach()
})
</script>

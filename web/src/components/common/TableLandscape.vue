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
        <div
          ref="bodyEl"
          class="table-landscape-scroll markdown-body"
          @mousedown="onTableMouseDown"
          @touchstart.passive="onTableTouchStart"
          @click="onOverlayClick"
          @click.self="close"
          v-html="state.html"
        />
      </div>
    </div>

    <!-- The row viewer is the same component the chat / file preview mount. It
         lives here because the overlay is teleported out of the container whose
         delegated click handler would otherwise open it — and it must paint
         above the overlay (--z-sheet < ModalDialog's 2100). -->
    <TableRowModal
      :data="tableRowModal"
      @close="closeTableRowModal"
      @prev="tableRowPrev"
      @next="tableRowNext"
    />
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import TableRowModal from '@/components/common/TableRowModal.vue'
import { handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import {
  closeTableLandscape,
  shouldRotateTableLandscape,
  tableLandscapeState as state,
} from '@/composables/useTableLandscape.ts'
import { gt } from '@/composables/useLocale'
import { usePlatformDetect } from '@/composables/usePlatformDetect.ts'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

/**
 * Clicks on any of these leave the overlay and are replayed onto the original
 * table, so the container's own handler (file viewer, commit, external link,
 * localhost preview) runs unchanged. Everything else — header buttons, table
 * rows, blank space — is handled in place.
 */
const NAV_TARGET_SELECTOR = [
  'a[href]',
  '.md-external-link',
  '.chat-url-open-btn',
  '.chat-file-path',
  '.chat-file-open-btn',
  '.chat-commit-hash',
  '.chat-commit-open-btn',
  '.chat-worktree-btn',
].join(',')

const { t } = useI18n()
const { isTouchPrimary } = usePlatformDetect()
const {
  tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext,
  handleTableRowClick, onTableMouseDown, onTableTouchStart,
} = useTableRowExpand()

const overlayRef = ref<HTMLElement | null>(null)
const bodyEl = ref<HTMLElement | null>(null)

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

/** The rendered clone's root wrapper. */
function renderedWrapper(): HTMLElement | null {
  return bodyEl.value?.firstElementChild as HTMLElement | null
}

/**
 * The wrap toggle edits the clone only. Carry the choice back to the live table
 * (and its own toggle button) so closing the overlay does not silently undo it.
 */
function syncWrapStateToSource(): void {
  const current = state.value
  const rendered = renderedWrapper()
  if (!current || !rendered) return

  const wrapped = rendered.classList.contains('word-wrap')
  current.source.classList.toggle('word-wrap', wrapped)
  current.source.classList.toggle('no-wrap', !wrapped)

  const btn = current.source.querySelector('.table-block-wrap-btn')
  if (!btn) return
  btn.classList.toggle('is-wrapped', wrapped)
  const label = gt(wrapped ? 'tableBlock.wrapOn' : 'tableBlock.wrapOff')
  btn.setAttribute('title', label)
  btn.setAttribute('aria-label', label)
}

function close(): void {
  closeTableRowModal()
  syncWrapStateToSource()
  closeTableLandscape()
}

// ── Click routing ───────────────────────────────────────────────────────────

/** Index path from `root` down to `el`, or null when `el` is not inside it. */
function indexPath(el: Element, root: Element): number[] | null {
  const path: number[] = []
  let node: Element | null = el
  while (node && node !== root) {
    const parent: Element | null = node.parentElement
    if (!parent) return null
    path.unshift(Array.prototype.indexOf.call(parent.children, node))
    node = parent
  }
  return node === root ? path : null
}

function elementAtPath(root: Element, path: number[]): Element | null {
  let node: Element | null = root
  for (const index of path) {
    node = node.children[index] ?? null
    if (!node) return null
  }
  return node
}

/**
 * Leave the overlay, then replay the click on the element it was cloned from.
 * The source tree is structurally identical (nothing was added or removed), so
 * the index path resolves to the same node.
 */
function leaveAndForward(event: MouseEvent): void {
  const rendered = renderedWrapper()
  const current = state.value
  const target = event.target as Element
  let sourceEl: Element | null = null

  if (rendered && current) {
    const path = indexPath(target, rendered)
    if (path) sourceEl = elementAtPath(current.source, path)
  }

  close()
  if (!sourceEl) return
  sourceEl.dispatchEvent(new MouseEvent('click', {
    bubbles: true,
    cancelable: true,
    clientX: event.clientX,
    clientY: event.clientY,
  }))
}

function onOverlayClick(event: MouseEvent): void {
  const target = event.target as HTMLElement

  // The landscape slot becomes the close button inside the overlay.
  if (target.closest('[data-action="landscape-close"]')) {
    event.preventDefault()
    event.stopPropagation()
    close()
    return
  }

  // Copy menu / copy format / wrap toggle — the same delegated handler the
  // containers call, so nothing about those controls is reimplemented here.
  if (handleTableBlockClick(event)) return

  // Row viewer (touch only, same as everywhere else).
  if (handleTableRowClick(event)) return

  if (target.closest(NAV_TARGET_SELECTOR)) leaveAndForward(event)
}

// ── Focus, keyboard, lifecycle ──────────────────────────────────────────────

function closeButton(): HTMLElement | null {
  return overlayRef.value?.querySelector<HTMLElement>('[data-action="landscape-close"]') ?? null
}

/**
 * ESC closes; Tab is trapped inside the overlay so keyboard and screen-reader
 * users cannot wander into the page hidden behind it.
 */
function onDocumentKeydown(event: KeyboardEvent): void {
  // The row viewer paints above the overlay and owns ESC/Tab while it is open —
  // trapping focus back into the overlay would fight it.
  if (tableRowModal.value) return

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
    closeButton()?.focus({ preventScroll: true })
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

<template>
  <Teleport to="body">
    <div
      v-if="state"
      ref="overlayRef"
      class="block-landscape"
      :class="{ 'is-rotated': rotated }"
      role="dialog"
      aria-modal="true"
      :aria-label="t('tableBlock.landscape')"
    >
      <div class="block-landscape-panel">
        <div
          ref="bodyEl"
          class="block-landscape-scroll markdown-body"
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
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import {
  closeBlockLandscape,
  shouldRotateBlockLandscape,
  blockLandscapeState as state,
} from '@/composables/useBlockLandscape.ts'
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

const rotated = computed(() => shouldRotateBlockLandscape({
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
 * The wrap toggle edits the clone only. Carry the choice back to the live block
 * (and its own toggle button) so closing the overlay does not silently undo it.
 *
 * Tables express the mode with a class pair (`word-wrap` / `no-wrap`); code
 * blocks only have `word-wrap`, so they must not be given a `no-wrap` class
 * that no rule consumes.
 */
function syncWrapStateToSource(): void {
  const current = state.value
  const rendered = renderedWrapper()
  if (!current || !rendered) return

  const wrapped = rendered.classList.contains('word-wrap')
  const isCode = current.source.classList.contains('code-block-wrapper')
  current.source.classList.toggle('word-wrap', wrapped)
  if (!isCode) current.source.classList.toggle('no-wrap', !wrapped)

  const btn = current.source.querySelector<HTMLElement>('.table-block-wrap-btn, .code-block-wrap-btn')
  if (!btn) return
  btn.classList.toggle('is-wrapped', wrapped)
  const ns = isCode ? 'codeBlock' : 'tableBlock'
  const label = gt(wrapped ? `${ns}.wrapOn` : `${ns}.wrapOff`)
  btn.setAttribute('title', label)
  btn.setAttribute('aria-label', label)
}

function close(): void {
  closeTableRowModal()
  syncWrapStateToSource()
  closeBlockLandscape()
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
 * Identity attributes that survive a re-render. When the source wrapper has been
 * replaced while the overlay was open (a streaming message re-renders its body),
 * the clone no longer has a live counterpart at the same index path — fall back
 * to the attribute that made the element a navigation target in the first place.
 * Any live element with the same identity navigates to the same destination, so
 * picking the first one is safe.
 */
const FORWARD_IDENTITY_ATTRS = ['data-file-path', 'data-commit-sha', 'data-worktree-path', 'data-url', 'href']

function findLiveCounterpart(el: Element): Element | null {
  for (const attr of FORWARD_IDENTITY_ATTRS) {
    const value = el.getAttribute(attr)
    if (!value) continue
    const candidates = document.querySelectorAll(`${el.tagName.toLowerCase()}[${attr}]`)
    for (const candidate of Array.from(candidates)) {
      if (candidate === el) continue
      if (candidate.getAttribute(attr) === value) return candidate
    }
  }
  return null
}

/**
 * Leave the overlay, then replay the click on the element it was cloned from.
 * The source tree is structurally identical (nothing was added or removed), so
 * the index path resolves to the same node.
 *
 * The original event must be cancelled: the clone contains real `<a href>`
 * elements, and without this the browser would navigate the whole SPA to the
 * relative URL on top of the replayed click.
 */
function leaveAndForward(event: MouseEvent): void {
  event.preventDefault()
  const rendered = renderedWrapper()
  const current = state.value
  const target = event.target as Element
  let sourceEl: Element | null = null

  if (rendered && current) {
    const path = indexPath(target, rendered)
    if (path) sourceEl = elementAtPath(current.source, path)
  }
  if (!sourceEl?.isConnected) {
    sourceEl = findLiveCounterpart(target)
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

/**
 * Navigation targets are routed in the CAPTURE phase on `document`, not from the
 * overlay's own click handler.
 *
 * The global external-link layer (`installExternalLinkClick`) is a document
 * capture listener that calls `stopPropagation()` for `span.md-external-link`.
 * Registered earlier, it wins the race — a bubble handler on the overlay would
 * never see the click, so the overlay would stay open underneath the confirm
 * dialog. `stopPropagation` does not stop other listeners on the same node,
 * which is exactly what lets this one still run.
 */
function onDocumentClickCapture(event: MouseEvent): void {
  const root = overlayRef.value
  if (!root) return
  const target = event.target as Element | null
  if (!target || !root.contains(target)) return

  // The external-link layer already claimed this click and is showing its own
  // confirm dialog; only leave the overlay so the dialog is not stranded above
  // it. Forwarding would fire that dialog a second time.
  if (target.closest('.md-external-link')) {
    close()
    return
  }

  if (target.closest(NAV_TARGET_SELECTOR)) leaveAndForward(event)
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

  // Copy / copy format / wrap toggle — the same delegated handlers the
  // containers call, so nothing about those controls is reimplemented here.
  if (handleCodeBlockClick(event)) return
  if (handleTableBlockClick(event)) return

  // Row viewer (touch only, same as everywhere else).
  if (handleTableRowClick(event)) return

  // Navigation targets are handled in onDocumentClickCapture (capture phase).
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

/** Body overflow as it was before the overlay opened; restored on close. */
let previousBodyOverflow = ''
/** Whether attach() ran, so a bare detach() (immediate watch, unmount) is a no-op. */
let attached = false

function detach(): void {
  if (!attached) return
  attached = false
  // Restore, do not clear: a drawer / modal opened before this overlay also
  // locks body scroll, and wiping it would let the page scroll behind them.
  document.body.style.overflow = previousBodyOverflow
  window.removeEventListener('resize', onViewportChange)
  window.visualViewport?.removeEventListener('resize', onViewportChange)
  document.removeEventListener('keydown', onDocumentKeydown, true)
  document.removeEventListener('click', onDocumentClickCapture, true)
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
    if (!attached) {
      attached = true
      previousBodyOverflow = document.body.style.overflow
    }
    document.body.style.overflow = 'hidden'
    window.addEventListener('resize', onViewportChange)
    window.visualViewport?.addEventListener('resize', onViewportChange)
    document.addEventListener('keydown', onDocumentKeydown, true)
    document.addEventListener('click', onDocumentClickCapture, true)
    unregisterBack = registerBackHandler({
      id: 'block-landscape',
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
  // over the next route's content. Go through close() so a wrap toggle made in
  // the overlay is still carried back to the live table.
  if (state.value) close()
  detach()
})
</script>

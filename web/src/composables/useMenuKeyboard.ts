import { ref, watch, onUnmounted, type Ref } from 'vue'

/**
 * Reusable keyboard navigation for teleported dropdown menu panels.
 *
 * Any `.app-menu-item` element inside the panel becomes navigable — this covers
 * both the main list rows and the trailing "other" action rows, since both share
 * the class. Wiring is intentionally generic so every dropdown in AppHeader
 * (projects / recent files / branches) can share the same composable.
 *
 * The listener is attached at document level in the capture phase (gated by
 * `isOpen`) rather than by focusing the panel: focusing would steal focus from
 * the active tab (e.g. the file manager), whose focus handling can reclaim it
 * and break navigation. The file manager also listens for the same keys at
 * document level, so whenever we handle a key we stop propagation — otherwise
 * the underlying tab (file list navigation, Enter-to-open) reacts too.
 * ArrowUp/ArrowDown cycle the highlight (wrapping around), Enter confirms the
 * highlighted row by clicking it (reusing the row's existing click handler),
 * and Escape closes the menu. Events originating in editable fields
 * (input/textarea/contenteditable) are ignored so typing is never hijacked.
 */
export function useMenuKeyboard(options: {
  /** The teleported panel element ref. */
  panelRef: Ref<HTMLElement | null>
  /** Reactive open state of the dropdown. */
  isOpen: Ref<boolean>
  /** Optional extra action invoked by Enter alongside clicking the row. */
  onConfirm?: (element: HTMLElement) => void
}) {
  const { panelRef, isOpen, onConfirm } = options

  /** Index of the currently highlighted row; -1 means nothing highlighted. */
  const activeIndex = ref(-1)

  let panel: HTMLElement | null = null

  function getItems(): HTMLElement[] {
    if (!panel) return []
    return Array.from(panel.querySelectorAll<HTMLElement>('.app-menu-item'))
      .filter((el) => !el.hasAttribute('disabled') && el.style.display !== 'none')
  }

  function applyHighlight() {
    const items = getItems()
    items.forEach((el, i) => {
      el.classList.toggle('keyboard-hover', i === activeIndex.value)
    })
  }

  function move(delta: number) {
    const items = getItems()
    if (items.length === 0) {
      activeIndex.value = -1
      return
    }
    if (activeIndex.value === -1) {
      activeIndex.value = delta > 0 ? 0 : items.length - 1
    } else {
      activeIndex.value = (activeIndex.value + delta + items.length) % items.length
    }
    applyHighlight()
    const el = items[activeIndex.value]
    el?.scrollIntoView({ block: 'nearest' })
  }

  function confirm() {
    const items = getItems()
    if (items.length === 0) return
    const index = activeIndex.value >= 0 && activeIndex.value < items.length
      ? activeIndex.value
      : 0
    const el = items[index]
    onConfirm?.(el)
    el.click()
  }

  function isEditableTarget(target: EventTarget | null): boolean {
    const el = target as HTMLElement | null
    if (!el) return false
    const tag = el.tagName?.toLowerCase()
    return tag === 'input' || tag === 'textarea' || tag === 'select'
      || el.isContentEditable
  }

  function onKeydown(e: KeyboardEvent) {
    if (!isOpen.value) return
    // Never hijack keys while the user is typing in an editable field.
    if (isEditableTarget(e.target)) return
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp':
      case 'Enter':
      case 'Escape':
        e.preventDefault()
        // Stop the underlying tab (e.g. the file manager's document-level
        // handler) from also reacting to the same key. Capture + immediate
        // stop guarantees this composable wins for the open dropdown.
        e.stopImmediatePropagation()
        if (e.key === 'ArrowDown') move(1)
        else if (e.key === 'ArrowUp') move(-1)
        else if (e.key === 'Enter') confirm()
        else isOpen.value = false
        break
      default:
        break
    }
  }

  function bind() {
    panel = panelRef.value
    if (panel) {
      // The panel is already rendered when the post-flush watcher runs (its
      // v-if just created it), so we can resolve items immediately.
      applyHighlight()
    }
    document.addEventListener('keydown', onKeydown, true)
  }

  function unbind() {
    document.removeEventListener('keydown', onKeydown, true)
    if (panel) {
      // Clear any lingering highlight so a reused DOM node starts clean.
      panel.querySelectorAll<HTMLElement>('.keyboard-hover').forEach((el) => {
        el.classList.remove('keyboard-hover')
      })
      panel = null
    }
  }

  watch(isOpen, (open) => {
    activeIndex.value = -1
    if (open) bind()
    else unbind()
  }, { flush: 'post' })

  onUnmounted(() => {
    if (isOpen.value) unbind()
  })

  return { activeIndex }
}

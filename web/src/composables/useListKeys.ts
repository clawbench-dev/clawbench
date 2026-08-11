import { onMounted, onUnmounted } from 'vue'

export interface ListNavLike {
  down(): void
  up(): void
  confirm(): void
}

interface ListKeysOptions {
  nav: ListNavLike
  /** Returns whether the list is currently active/visible. */
  isOpen: () => boolean
}

/**
 * Registers a document-level keydown listener so ↑/↓/Enter navigate a list no
 * matter where focus sits inside the drawer (a container `@keydown` only fires
 * when the list itself has focus, which it usually does not on open).
 *
 * The handler is skipped when focus is in an editable/search field — those
 * forward arrows themselves (see SearchInput). Enter is also skipped on
 * interactive elements (button/a/[role=button]) which already confirm via
 * their own handlers, to avoid double-activating.
 */
export function useListKeys(options: ListKeysOptions) {
  const { nav, isOpen } = options

  function isEditableTarget(e: Event): boolean {
    const t = e.target as HTMLElement | null
    if (!t) return false
    const tag = t.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
    if (t.isContentEditable) return true
    return false
  }

  function handler(e: KeyboardEvent) {
    if (!isOpen()) return

    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      if (isEditableTarget(e)) return
      e.preventDefault()
      if (e.key === 'ArrowDown') nav.down()
      else nav.up()
      return
    }

    if (e.key === 'Enter') {
      if (isEditableTarget(e)) return
      const t = e.target as HTMLElement | null
      if (t?.closest?.('button, a, [role="button"]')) return
      nav.confirm()
    }
  }

  onMounted(() => document.addEventListener('keydown', handler))
  onUnmounted(() => document.removeEventListener('keydown', handler))

  return handler
}

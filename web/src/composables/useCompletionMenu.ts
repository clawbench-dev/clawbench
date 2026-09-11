/**
 * Shared interaction state machine for the chat input's completion menus
 * (slash commands and @ file references).
 *
 * Owns the open/closed state, the active index, keyboard navigation, the
 * Esc-dismissal latch and the trigger-range removal on select. The per-menu
 * differences are injected: how to parse the trigger (`getTrigger`), what to
 * do on select (`onSelect`), and whether to close afterwards (`closeOnSelect`).
 */

import { ref, type Ref } from 'vue'
import type { CompletionItem, TriggerRange } from '@/utils/completionMatch.ts'
import { isImeCompositionEvent } from '@/utils/chatInputUtils.ts'

export interface UseCompletionMenuOptions {
  /** Current candidate list. */
  items: Ref<CompletionItem[]>
  /** Parse the active trigger range from the current input text + caret. */
  getTrigger: () => TriggerRange | null
  /** Business hook run when an item is chosen (attach file, write command, …). */
  onSelect: (item: CompletionItem) => void
  /** Close the menu after a selection (slash) vs keep it open (files). */
  closeOnSelect: boolean
  /**
   * After a non-closing select, keep the menu open even though the trigger was
   * removed from the text (the @ file menu browses all candidates afterwards).
   * Cleared by Esc, close(), or a fresh trigger.
   */
  stickyAfterSelect?: boolean
  /** Read the current input text so the trigger range can be removed on select. */
  getText?: () => string
  /** Apply the trigger-stripped text back to the input (and restore the caret). */
  applyText?: (value: string, caret: number) => void
}

export interface UseCompletionMenu {
  show: Ref<boolean>
  activeIndex: Ref<number>
  /** True while the menu is open in post-select browse mode. */
  sticky: Ref<boolean>
  /** Recompute visibility/index after the text, caret or items changed. */
  refresh: () => void
  /** Handle a textarea keydown. Returns true when the event was consumed. */
  handleKeydown: (e: KeyboardEvent) => boolean
  /**
   * Select an item by its key (mouse click path). Pass `source` to disambiguate
   * when several items share a key (e.g. a built-in and an agent command with
   * the same name).
   */
  selectByKey: (key: string, source?: string) => void
  /**
   * Close the menu without releasing the Esc dismissal latch. The latch is
   * only released when the trigger context actually ends (query cleared or a
   * fresh trigger appears), so an outside click after Esc does not reopen it.
   */
  close: () => void
  /** Leave browse mode (called when the user types plain text). */
  clearSticky: () => void
}

export function useCompletionMenu(options: UseCompletionMenuOptions): UseCompletionMenu {
  const show = ref(false)
  const activeIndex = ref(-1)
  const sticky = ref(false)
  // Snapshot of the input text at the moment browse mode was entered. Any
  // subsequent edit (the user typing a message) ends browse mode — otherwise
  // the menu would hover forever after the @query was consumed by a select.
  let stickyText: string | null = null
  // Esc dismisses the menu until the current trigger context ends; without
  // this latch any further keystroke in the same @query would reopen it.
  let dismissed = false

  function clearSticky() {
    sticky.value = false
    stickyText = null
  }

  function refresh() {
    const trigger = options.getTrigger()
    if (!trigger) {
      if (sticky.value) {
        // Browse mode: the trigger is gone but the user is still picking files.
        // A text edit since entering browse mode means they moved on.
        if (options.getText && options.getText() !== stickyText) {
          clearSticky()
        } else {
          const items = options.items.value
          if (items.length === 0) {
            show.value = false
            activeIndex.value = -1
          } else {
            if (activeIndex.value < 0 || activeIndex.value >= items.length) activeIndex.value = 0
            show.value = true
          }
          return
        }
      }
      // Trigger gone (query ended / caret moved out) — release the latch.
      dismissed = false
      show.value = false
      activeIndex.value = -1
      return
    }
    // A genuine trigger overrides browse mode.
    sticky.value = false
    stickyText = null
    if (dismissed) {
      show.value = false
      return
    }
    const items = options.items.value
    if (items.length === 0) {
      show.value = false
      activeIndex.value = -1
      return
    }
    if (activeIndex.value < 0 || activeIndex.value >= items.length) {
      activeIndex.value = 0
    }
    show.value = true
  }

  function select() {
    const items = options.items.value
    const index = activeIndex.value
    if (index < 0 || index >= items.length) return
    const item = items[index]

    options.onSelect(item)

    const trigger = options.getTrigger()
    if (trigger && options.applyText && options.getText) {
      const text = options.getText()
      const before = text.slice(0, trigger.start)
      const after = text.slice(trigger.end)
      options.applyText(before + after, before.length)
    }

    if (options.closeOnSelect) {
      show.value = false
      activeIndex.value = -1
    } else {
      activeIndex.value = 0
      if (options.stickyAfterSelect) {
        sticky.value = true
        // Snapshot AFTER the trigger was stripped, so the next refresh sees the
        // same text and keeps browsing until the user actually edits.
        stickyText = options.getText ? options.getText() : null
      }
    }
  }

  function handleKeydown(e: KeyboardEvent): boolean {
    // Let the IME own the keystroke (pinyin candidate commit, etc.).
    if (isImeCompositionEvent(e)) return false
    if (!show.value) return false

    if (e.key === 'Escape') {
      e.preventDefault()
      dismissed = true
      clearSticky()
      show.value = false
      activeIndex.value = -1
      return true
    }

    const items = options.items.value
    if (items.length === 0) return false

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      activeIndex.value = (activeIndex.value + 1) % items.length
      return true
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      activeIndex.value = activeIndex.value <= 0 ? items.length - 1 : activeIndex.value - 1
      return true
    }
    if ((e.key === 'Enter' || e.key === 'Tab') && activeIndex.value >= 0) {
      e.preventDefault()
      select()
      return true
    }
    return false
  }

  function close() {
    // Deliberately keeps `dismissed`: close() is also reached from the popup's
    // outside-click handler, and releasing the latch there would let the very
    // same @query reopen on the next selectionchange.
    sticky.value = false
    show.value = false
    activeIndex.value = -1
  }

  function selectByKey(key: string, source?: string) {
    const index = options.items.value.findIndex(i =>
      i.key === key && (source === undefined || i.source === source))
    if (index < 0) return
    activeIndex.value = index
    select()
  }

  return { show, activeIndex, sticky, refresh, handleKeydown, selectByKey, close, clearSticky }
}

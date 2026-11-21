/**
 * v-chat-drag — turn an element into a drag source for the chat.
 *
 * Two payload kinds, one attribute:
 *
 *   <div v-chat-drag="{ kind: 'quote', payload }">        <!-- quote card -->
 *   <div v-chat-drag="{ kind: 'attach', path, name }">    <!-- file entry -->
 *
 * The raw helpers (`startQuoteDrag` / `startAttachDrag`) are already shared,
 * but each source had to remember three things by hand: set `draggable`, gate
 * it on the wide-screen layout, and call `cleanupDragGhost()` from `dragend`.
 * Forgetting the last one leaves the shared ghost on screen (until its 5s
 * safety timeout), and forgetting the gate produces a drag that starts on a
 * narrow screen where the chat column is not visible — "it drags but nothing
 * happens". The directive owns the whole contract, so a panel declares WHAT is
 * being dragged and nothing else.
 *
 * Do NOT put this on a text container: `draggable` makes its text
 * unselectable, and selecting text is how a snippet gets quoted. Use it on the
 * row/card, not on the content inside it.
 */
import type { Directive, DirectiveBinding } from 'vue'
import { watch, type WatchStopHandle } from 'vue'
import { getWideScreenState } from '@/composables/useWideScreenLayout'
import { cleanupDragGhost, startAttachDrag, type AttachDragOptions } from '@/utils/attachDrag'
import { startQuoteDrag, type QuoteDragPayload } from '@/utils/quoteDrag'

export interface QuoteChatDragBinding {
  kind: 'quote'
  /**
   * The card payload, or a getter.
   *
   * A getter is useful in a `v-for`: the row's data can change (a rename, a
   * status update) without the directive being re-created, and the drag reads
   * the current value at `dragstart` time.
   */
  payload: QuoteDragPayload | null | (() => QuoteDragPayload | null)
  /** Keep dragging on narrow screens too (default: only when both panes fit). */
  allowNarrow?: boolean
}

export interface AttachChatDragBinding extends AttachDragOptions {
  kind: 'attach'
  path: string
  /** Keep dragging on narrow screens too (default: only when both panes fit). */
  allowNarrow?: boolean
}

export type ChatDragBinding = QuoteChatDragBinding | AttachChatDragBinding

interface DragState {
  binding: DirectiveBinding<ChatDragBinding>
  stopLayout: WatchStopHandle | null
  /** Whether THIS element started the drag in flight (for the unmount cleanup). */
  dragging: boolean
}

const states = new WeakMap<HTMLElement, DragState>()

function resolvePayload(value: QuoteChatDragBinding['payload']): QuoteDragPayload | null {
  return typeof value === 'function' ? value() : value
}

/**
 * Keep `draggable` in step with the layout.
 *
 * On a narrow screen the drag source and the chat column can never be on
 * screen together, so allowing the drag would only produce a gesture that
 * drops nowhere (docs/spec/features/quotes-and-attachments.md §3.3). A
 * source that really does have a same-screen target can opt out with
 * `allowNarrow`.
 */
function syncDraggable(el: HTMLElement, state: DragState): void {
  const value = state.binding.value
  if (!value) {
    el.draggable = false
    return
  }
  el.draggable = value.allowNarrow === true || getWideScreenState().isWideScreen.value
}

function onDragStart(e: DragEvent): void {
  const el = e.currentTarget as HTMLElement | null
  const state = el ? states.get(el) : undefined
  const value = state?.binding.value
  // A drag can fire on a row whose layout gate just turned false (the window
  // was resized between the attribute update and the gesture). Refusing here
  // is what makes the gate authoritative rather than advisory.
  if (!el || !state || !value || !el.draggable) {
    e.preventDefault()
    return
  }

  const started = value.kind === 'quote'
    ? startQuoteDrag(e, resolvePayload(value.payload))
    : startAttachDrag(e, value.path, {
        name: value.name,
        isDir: value.isDir,
        startLine: value.startLine,
        endLine: value.endLine,
        entries: value.entries,
        effectAllowed: value.effectAllowed,
      })

  if (!started) {
    // No dataTransfer (synthetic event) or an unusable payload: cancel instead
    // of starting a drag that would drop nothing.
    e.preventDefault()
    return
  }
  state.dragging = true
}

function onDragEnd(e: DragEvent): void {
  const el = e.currentTarget as HTMLElement | null
  const state = el ? states.get(el) : undefined
  if (state) state.dragging = false
  cleanupDragGhost()
}

function mounted(el: HTMLElement, binding: DirectiveBinding<ChatDragBinding>): void {
  const state: DragState = { binding, stopLayout: null, dragging: false }
  states.set(el, state)

  el.addEventListener('dragstart', onDragStart)
  el.addEventListener('dragend', onDragEnd)

  syncDraggable(el, state)
  // A resize or rotation changes the layout while the row stays mounted; the
  // attribute has to follow, or a row created on a wide screen keeps
  // `draggable` after the window narrows.
  state.stopLayout = watch(getWideScreenState().isWideScreen, () => syncDraggable(el, state))
}

function updated(el: HTMLElement, binding: DirectiveBinding<ChatDragBinding>): void {
  const state = states.get(el)
  if (!state) return
  state.binding = binding
  syncDraggable(el, state)
}

function unmounted(el: HTMLElement): void {
  const state = states.get(el)
  el.removeEventListener('dragstart', onDragStart)
  el.removeEventListener('dragend', onDragEnd)
  state?.stopLayout?.()
  // A row removed mid-drag never receives `dragend`; without this the shared
  // ghost would linger until its safety timeout.
  if (state?.dragging) cleanupDragGhost()
  states.delete(el)
}

export const ChatDragDirective: Directive<HTMLElement, ChatDragBinding> = {
  mounted,
  updated,
  unmounted,
}

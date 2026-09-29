import { reactive, computed } from 'vue'

/**
 * Open-order stack shared by every BottomSheet instance.
 *
 * Why this exists: all BottomSheets teleport to `<body>` at the same z-index
 * tier (`--z-overlay`). When two are open at once the browser therefore orders
 * them by DOM position — and Teleport fixes that position at MOUNT time, not at
 * open time. So a drawer mounted early (an App-root sibling) but opened LATER
 * would paint UNDERNEATH the drawer that opened it. The reported case is the
 * inert-path picker (an App-root sibling) opened from the /btw drawer.
 *
 * Peer comparison must follow OPEN order, so each open drawer claims the next
 * slot here and derives its z-index from it. Closing releases the slot, which
 * drops the drawers opened before it back to their original depth.
 *
 * This state must live in a module, not in the component's `<script setup>`:
 * top-level code there runs once PER INSTANCE, so a stack declared in the SFC
 * would give every drawer its own stack and every slot would read 0.
 */

/**
 * Highest offset a drawer may take.
 *
 * The overlay band is `--z-overlay` (1000) .. `--z-overlay-raised - 1` (1049),
 * so offsets 0..49 are in-band and the maximum resolves to 1049. This is a
 * guard against a pathological stack, not a real limit: nesting is 1–3 deep.
 */
export const MAX_DRAWER_OFFSET = 49

/**
 * Monotonic instance id.
 *
 * This MUST live in a module: a counter declared at the top level of a
 * component's `<script setup>` is re-initialised for every instance (that block
 * is compiled into `setup()`), so every drawer would receive the same id — which
 * would make the stack treat them as one drawer.
 */
let _drawerId = 0
export function nextDrawerId(): number {
  return ++_drawerId
}

/** @internal Reset the id counter — tests only. */
export function _resetDrawerId(): void {
  _drawerId = 0
}

/** Instance seq numbers, oldest-opened first. */
const stack = reactive<number[]>([])

/** Claim the top slot. Idempotent: an already-open drawer is not re-pushed. */
export function pushDrawer(seq: number): void {
  if (!stack.includes(seq)) stack.push(seq)
}

/** Release a drawer's slot (no-op when it is not open). */
export function removeDrawer(seq: number): void {
  const i = stack.indexOf(seq)
  if (i !== -1) stack.splice(i, 1)
}

/**
 * This drawer's z-index offset: how many drawers were already open when it
 * opened. Clamped into `[0, MAX_OPEN_DRAWERS]` so a pathological stack cannot
 * climb into the app-header tier.
 */
export function drawerZOffset(seq: number) {
  return computed(() => {
    const slot = stack.indexOf(seq)
    return Math.min(Math.max(slot, 0), MAX_DRAWER_OFFSET)
  })
}

/** @internal Reset the stack — tests only. */
export function _resetDrawerStack(): void {
  stack.length = 0
}

/** @internal Current stack contents — tests only. */
export function _drawerStackSnapshot(): number[] {
  return [...stack]
}

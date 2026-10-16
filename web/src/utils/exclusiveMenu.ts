/**
 * Exclusive-open registry for teleported popup menus.
 *
 * A MenuSelect popup is teleported to <body>, so it is not a DOM sibling of any
 * other trigger — nothing about the markup prevents a second menu from opening
 * over the first. Without coordination, clicking trigger A then trigger B left
 * BOTH menus on screen (reproduced: two `.popup-menu` elements, overlapping).
 *
 * This MUST be a real module, not a top-level binding in a `<script setup>`
 * block: everything at the top level of `<script setup>` is compiled INTO the
 * per-instance `setup()` function, so a `let` there is created fresh for every
 * component instance and coordinates nothing. (That mistake shipped once and
 * the two-menus bug survived it.)
 *
 * A single slot is enough — the menus that must coordinate are siblings within
 * one panel — and it works for any future consumer with no wiring.
 */

interface Closable {
  close: () => void
}

let openInstance: Closable | null = null

/**
 * Claim the open slot, closing whoever held it.
 *
 * Returns a token to pass to `releaseOpenMenu` when this menu closes, so a
 * late close from a superseded instance cannot evict the current holder.
 */
export function claimOpenMenu(closable: Closable): Closable {
  if (openInstance && openInstance !== closable) openInstance.close()
  openInstance = closable
  return closable
}

/**
 * Release the open slot, but only if `closable` still holds it.
 *
 * The identity check matters: a menu that was closed by another menu's claim
 * must not then clear the NEW holder's slot (that would let two menus open
 * again on the next click).
 */
export function releaseOpenMenu(closable: Closable): void {
  if (openInstance === closable) openInstance = null
}

/** Test-only: drop the slot so cases cannot leak into each other. */
export function resetOpenMenuForTest(): void {
  openInstance = null
}

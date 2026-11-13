/**
 * Where dictated text goes.
 *
 * A *target* is a live writing surface the transcript is written into at the
 * caret. Two kinds exist:
 *
 *   - a plain `input` / `textarea`, handled right here;
 *   - any other editor that registers a host for itself (CodeMirror being the
 *     one in this app), through `registerVoiceHost`.
 *
 * Everything a target needs is expressed as `VoiceTargetHost`, so the caret
 * arithmetic below is written once and knows nothing about the concrete
 * editor. This module stays a plain DOM module (no Vue, no i18n) so that
 * arithmetic can be tested on its own — the voice pipeline only has to decide
 * *whether* there is a target, never how to splice text into one.
 *
 * Streaming rewrites the same range on every partial (the recogniser revises
 * earlier words as it hears more), so the target remembers where its own text
 * starts and how long it currently is. Nothing else in the surface is touched.
 *
 * Why not generic `contenteditable`: writing raw DOM text into one bypasses
 * the editor's model and the next render reverts it (CodeMirror keeps its own
 * state). Editors therefore have to opt in through a host that writes through
 * their own API; an unregistered contenteditable is still "no target" and
 * falls through to the capsule rather than silently losing text.
 */

/** A writable surface dictation can be spliced into. */
export interface VoiceTargetHost {
  /** Full current text of the surface. */
  read(): string
  /** Selection as offsets into `read()`. */
  range(): { from: number; to: number }
  /** Whether the surface itself owns the caret right now. */
  focused(): boolean
  /** Replace `[from, to)` with `text` and leave the caret after it. */
  replace(from: number, to: number, text: string): boolean
  /** Restore a selection (used when a dictation is cancelled). */
  select(from: number, to: number): void
  /** False once the surface is gone — a re-rendered editor is not writable. */
  alive(): boolean
  /** Where the caret is on screen, for placing the status pill. */
  rect?(): DOMRect | null
}

export interface VoiceTarget {
  /**
   * Element the overlay anchors to. For an `input`/`textarea` that is the
   * field itself; for a registered editor it is the registered container.
   */
  el: HTMLElement
  host: VoiceTargetHost
  /** Index in `host.read()` where our text starts. */
  start: number
  /** Length of the text we currently occupy. */
  inserted: number
  /** What we last wrote — used to detect that someone else changed the field. */
  applied: string
  /** Text the caret/selection covered before the first write, restored on cancel. */
  replaced: string
  hasWritten: boolean
}

/** Input types whose value supports the selection API. */
const TEXT_INPUT_TYPES = new Set(['text', 'search', 'email', 'url', 'tel'])

/**
 * How much text in front of the caret is sent as recognition context.
 *
 * Enough for the engine to hear where a sentence started, but bounded: an
 * editor can hold a whole file, and posting all of it as `context` on every
 * recording would be megabytes the engine never asked for.
 */
const CONTEXT_LIMIT = 200

function isEditableField(el: Element | null): el is HTMLInputElement | HTMLTextAreaElement {
  if (!el) return false
  if (el instanceof HTMLTextAreaElement) return !el.disabled && !el.readOnly
  if (el instanceof HTMLInputElement) {
    const type = (el.type || 'text').toLowerCase()
    // `password`: dictating a password out loud is a decision the user should
    // make deliberately, not something a global shortcut does for them.
    // `number`/`date`/…: the selection API throws on them.
    return TEXT_INPUT_TYPES.has(type) && !el.disabled && !el.readOnly
  }
  return false
}

function fieldHost(el: HTMLInputElement | HTMLTextAreaElement): VoiceTargetHost {
  return {
    read: () => el.value,
    range: () => {
      const from = el.selectionStart ?? el.value.length
      return { from, to: el.selectionEnd ?? from }
    },
    focused: () => el.ownerDocument.activeElement === el,
    replace(from, to, text) {
      try {
        el.setRangeText(text, from, to, 'end')
      } catch {
        // A field that refuses the selection API mid-flight (type switched to a
        // non-text input) must not take the recording down with it.
        return false
      }
      // Vue's v-model (and every other listener) reads the value on `input`.
      el.dispatchEvent(new Event('input', { bubbles: true }))
      return true
    },
    select(from, to) {
      try {
        el.setSelectionRange(from, to)
      } catch { /* field stopped accepting ranges */ }
    },
    alive: () => el.isConnected,
  }
}

type VoiceHostFactory = () => VoiceTargetHost | null

/**
 * Containers that can take dictation but are not `input`/`textarea`.
 *
 * A `WeakMap` (not a list) so a torn-down editor is collectable, and the
 * factory is called per lookup so a host can report itself unavailable
 * (read-only mode, disabled form field) without re-registering.
 */
const hostFactories = new WeakMap<Element, VoiceHostFactory>()

/**
 * Lets an editor accept dictation.
 *
 * `container` is the element that wraps the focusable content; the lookup walks
 * up from whatever holds the caret, so any node inside it resolves to the same
 * host. Returns an unregister function for `onUnmounted`.
 */
export function registerVoiceHost(container: Element, factory: VoiceHostFactory): () => void {
  hostFactories.set(container, factory)
  return () => {
    if (hostFactories.get(container) === factory) hostFactories.delete(container)
  }
}

function registeredHost(el: Element): { container: HTMLElement; host: VoiceTargetHost } | null {
  for (let node: Element | null = el; node; node = node.parentElement) {
    const factory = hostFactories.get(node)
    if (!factory) continue
    const host = factory()
    // A registered container that says "not now" (read-only) ends the search:
    // nesting another writable editor inside it is not a thing.
    return host && node instanceof HTMLElement ? { container: node, host } : null
  }
  return null
}

function clamp(value: number, max: number): number {
  return Math.max(0, Math.min(value, max))
}

/**
 * Wraps a writing surface as a target, capturing the caret and any selected
 * text.
 *
 * Returns null for anything that must not receive dictation — callers treat
 * that exactly like "there is no caret here" and use the capsule.
 */
export function createVoiceTarget(el: Element | null): VoiceTarget | null {
  if (!el) return null
  // Opt-out for widgets that own their keyboard handling (terminal, canvases
  // pretending to be inputs, …).
  if (el.closest('[data-voice-ignore]')) return null

  let anchor: HTMLElement
  let host: VoiceTargetHost
  if (isEditableField(el)) {
    anchor = el
    host = fieldHost(el)
  } else {
    const registered = registeredHost(el)
    if (!registered) return null
    anchor = registered.container
    host = registered.host
  }

  const text = host.read()
  const { from, to } = host.range()
  const start = clamp(from, text.length)
  const end = Math.max(start, clamp(to, text.length))
  return {
    el: anchor,
    host,
    start,
    inserted: 0,
    applied: '',
    replaced: end > start ? text.slice(start, end) : '',
    hasWritten: false,
  }
}

/**
 * The surface that currently owns the caret, if it can take dictation.
 *
 * `document.activeElement` is the only honest source: it is the element the
 * caret is actually in, which is exactly the rule the feature promises.
 */
export function findVoiceTarget(doc: Document = document): VoiceTarget | null {
  const active = doc.activeElement
  if (!active || active === doc.body) return null
  // xterm keeps focus on a hidden textarea and consumes function keys itself.
  if (active.closest('.xterm')) return null
  return createVoiceTarget(active)
}

/** Text in front of our insertion point, sent as recognition context. */
export function textBeforeTarget(t: VoiceTarget): string {
  return t.host.read().slice(0, t.start).slice(-CONTEXT_LIMIT)
}

/**
 * Puts `text` where the target's caret is, replacing whatever we wrote last
 * time (or the original selection on the first call).
 *
 * Re-anchors when the surface no longer contains what we wrote: the user may
 * have sent the message (box cleared), undone, or edited around us. In that
 * case the transcript continues from the caret if the surface still has focus,
 * otherwise from the end — never from a stale index into text that is gone.
 */
export function writeVoiceText(t: VoiceTarget, text: string): void {
  const value = t.host.read()
  const occupied = t.hasWritten ? t.inserted : t.replaced.length
  const expected = t.hasWritten ? t.applied : t.replaced

  if (value.slice(t.start, t.start + occupied) !== expected) {
    const caret = t.host.focused() ? t.host.range().from : value.length
    t.start = clamp(caret, value.length)
    t.inserted = 0
    t.applied = ''
    t.replaced = ''
    t.hasWritten = false
  }

  const end = t.hasWritten ? t.start + t.inserted : t.start + t.replaced.length
  if (!t.host.replace(t.start, end, text)) return
  t.inserted = text.length
  t.applied = text
  t.hasWritten = true
}

/**
 * Takes the dictation back out — Esc, or a cancel from the engine.
 *
 * Leaves the surface alone if the text we wrote is no longer there: at that
 * point it belongs to the user, and splicing it out would delete their edit.
 */
export function removeVoiceText(t: VoiceTarget): void {
  if (!t.hasWritten) return
  if (t.host.read().slice(t.start, t.start + t.inserted) !== t.applied) return
  if (!t.host.replace(t.start, t.start + t.inserted, t.replaced)) return
  // Put the selection back where it was before dictation started (a caret,
  // usually) — cancelling should leave the surface exactly as it was found.
  t.host.select(t.start, t.start + t.replaced.length)
  t.inserted = 0
  t.applied = ''
  t.hasWritten = false
}

/** True when the surface is still writable (a re-rendered editor is not). */
export function isTargetAlive(t: VoiceTarget | null): t is VoiceTarget {
  return !!t && t.host.alive()
}

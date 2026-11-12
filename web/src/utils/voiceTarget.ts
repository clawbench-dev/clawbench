/**
 * Where dictated text goes.
 *
 * A *target* is a live `input` / `textarea` the transcript is written into at
 * the caret. It is deliberately a plain module of DOM primitives (no Vue, no
 * i18n) so the caret arithmetic can be tested on its own — the voice pipeline
 * only has to decide *whether* there is a target, never how to splice text
 * into one.
 *
 * Streaming rewrites the same range on every partial (the recogniser revises
 * earlier words as it hears more), so the target remembers where its own text
 * starts and how long it currently is. Nothing else in the field is touched.
 *
 * Why not `contenteditable`: the app's only rich editors are CodeMirror
 * instances (read-only previews). Writing raw DOM text into them bypasses
 * their model and the next render reverts it, so they are treated as "no
 * target" and fall through to the capsule instead of silently losing text.
 */

export interface VoiceTarget {
  el: HTMLInputElement | HTMLTextAreaElement
  /** Shown to the user ("正在写入「文件搜索」"). */
  label: string
  /** Index in `el.value` where our text starts. */
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

/** Human name for the field, used in the status pill. */
function labelOf(el: HTMLInputElement | HTMLTextAreaElement): string {
  const explicit = el.dataset.voiceLabel || el.getAttribute('aria-label') || ''
  const text = (explicit || el.getAttribute('placeholder') || '').trim()
  return text.length > 24 ? text.slice(0, 24) + '…' : text
}

/**
 * Wraps a field as a target, capturing the caret and any selected text.
 *
 * Returns null for anything that must not receive dictation — callers treat
 * that exactly like "there is no caret here" and use the capsule.
 */
export function createVoiceTarget(el: Element | null): VoiceTarget | null {
  if (!isEditableField(el)) return null
  // Opt-out for widgets that own their keyboard handling (terminal, canvases
  // pretending to be inputs, …).
  if (el.closest('[data-voice-ignore]')) return null

  const start = el.selectionStart ?? el.value.length
  const end = el.selectionEnd ?? start
  return {
    el,
    label: labelOf(el),
    start,
    inserted: 0,
    applied: '',
    replaced: end > start ? el.value.slice(start, end) : '',
    hasWritten: false,
  }
}

/**
 * The field that currently owns the caret, if it can take dictation.
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
  return t.el.value.slice(0, t.start)
}

function dispatchInput(el: HTMLElement): void {
  // Vue's v-model (and every other listener) reads the value on `input`.
  el.dispatchEvent(new Event('input', { bubbles: true }))
}

/**
 * Puts `text` where the target's caret is, replacing whatever we wrote last
 * time (or the original selection on the first call).
 *
 * Re-anchors when the field no longer contains what we wrote: the user may
 * have sent the message (box cleared), undone, or edited around us. In that
 * case the transcript continues from the caret if the field still has focus,
 * otherwise from the end — never from a stale index into text that is gone.
 */
export function writeVoiceText(t: VoiceTarget, text: string): void {
  const el = t.el
  const value = el.value
  const occupied = t.hasWritten ? t.inserted : t.replaced.length
  const expected = t.hasWritten ? t.applied : t.replaced

  if (value.slice(t.start, t.start + occupied) !== expected) {
    const focused = el.ownerDocument.activeElement === el
    const caret = focused ? (el.selectionStart ?? value.length) : value.length
    t.start = Math.max(0, Math.min(caret, value.length))
    t.inserted = 0
    t.applied = ''
    t.replaced = ''
    t.hasWritten = false
  }

  const end = t.hasWritten ? t.start + t.inserted : t.start + t.replaced.length
  try {
    el.setRangeText(text, t.start, end, 'end')
  } catch {
    // A field that refuses the selection API mid-flight (type switched to a
    // non-text input) must not take the recording down with it.
    return
  }
  t.inserted = text.length
  t.applied = text
  t.hasWritten = true
  dispatchInput(el)
}

/**
 * Takes the dictation back out — Esc, or a cancel from the engine.
 *
 * Leaves the field alone if the text we wrote is no longer there: at that
 * point it belongs to the user, and splicing it out would delete their edit.
 */
export function removeVoiceText(t: VoiceTarget): void {
  if (!t.hasWritten) return
  const el = t.el
  if (el.value.slice(t.start, t.start + t.inserted) !== t.applied) return
  try {
    el.setRangeText(t.replaced, t.start, t.start + t.inserted, 'preserve')
    // Put the selection back where it was before dictation started (a caret,
    // usually) — cancelling should leave the field exactly as it was found.
    el.setSelectionRange(t.start, t.start + t.replaced.length)
  } catch {
    return
  }
  t.inserted = 0
  t.applied = ''
  t.hasWritten = false
  dispatchInput(el)
}

/** True when the element is still in the document (a re-rendered field is not). */
export function isTargetAlive(t: VoiceTarget | null): t is VoiceTarget {
  return !!t && t.el.isConnected
}

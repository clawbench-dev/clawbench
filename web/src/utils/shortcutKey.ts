/**
 * Configurable single-shortcut bindings such as `stt.shortcut_key`.
 *
 * A shortcut is written as `+`-joined tokens, the last one being the main key:
 * `F9`, `Ctrl+M`, `Ctrl+Shift+K`, `AltRight`. The main key is matched by
 * `KeyboardEvent.code`, so a sided modifier (`AltLeft` / `AltRight`,
 * `ControlLeft` / `ControlRight`, …) binds only that physical key — `e.altKey`
 * is true for both Alts and cannot tell them apart. The leading modifiers are
 * matched exactly against the event's modifier flags.
 */

export type ModifierKind = 'ctrl' | 'alt' | 'shift' | 'meta'

export interface ParsedShortcut {
  /** `KeyboardEvent.code` values that count as the main key. */
  codes: string[]
  /** Modifiers that must be held together with the main key. */
  modifiers: ModifierKind[]
  /** Normalised text for display, e.g. `Ctrl+M`. */
  label: string
}

const MODIFIER_ORDER: ModifierKind[] = ['ctrl', 'alt', 'shift', 'meta']
const MODIFIER_LABEL: Record<ModifierKind, string> = { ctrl: 'Ctrl', alt: 'Alt', shift: 'Shift', meta: 'Meta' }
const MODIFIER_ALIASES: Record<string, ModifierKind> = {
  ctrl: 'ctrl', control: 'ctrl', alt: 'alt', shift: 'shift', meta: 'meta',
}
const MODIFIER_FLAG: Record<ModifierKind, 'ctrlKey' | 'altKey' | 'shiftKey' | 'metaKey'> = {
  ctrl: 'ctrlKey', alt: 'altKey', shift: 'shiftKey', meta: 'metaKey',
}
const MODIFIER_CODE_BASE: Record<ModifierKind, string> = { ctrl: 'Control', alt: 'Alt', shift: 'Shift', meta: 'Meta' }

/** The modifier a `code` belongs to (`AltRight` → `alt`), or null. */
function modifierOfCode(code: string): ModifierKind | null {
  for (const kind of MODIFIER_ORDER) {
    if (code === `${MODIFIER_CODE_BASE[kind]}Left` || code === `${MODIFIER_CODE_BASE[kind]}Right`) return kind
  }
  return null
}

/** `KeyboardEvent.code` values for one main-key token. */
function codesForToken(token: string): string[] {
  const lower = token.toLowerCase()
  const generic = MODIFIER_ALIASES[lower]
  if (generic) return [`${MODIFIER_CODE_BASE[generic]}Left`, `${MODIFIER_CODE_BASE[generic]}Right`]
  // Sided modifiers: AltRight, CtrlLeft, ControlRight, ShiftLeft, MetaRight, …
  const sided = /^(ctrl|control|alt|shift|meta)(left|right)$/.exec(lower)
  if (sided) {
    const side = sided[2] === 'left' ? 'Left' : 'Right'
    return [`${MODIFIER_CODE_BASE[MODIFIER_ALIASES[sided[1]]]}${side}`]
  }
  if (/^[a-z]$/.test(lower)) return [`Key${lower.toUpperCase()}`]
  if (/^[0-9]$/.test(lower)) return [`Digit${lower}`]
  if (/^f([1-9]|1[0-9]|2[0-4])$/.test(lower)) return [lower.toUpperCase()]
  // Anything else is taken as a literal code (Space, KeyM, Numpad1, Backquote, …).
  return [token]
}

/** Display text for a main-key token: `m` → `M`, `altright` → `AltRight`. */
function labelForToken(token: string, codes: string[]): string {
  const generic = MODIFIER_ALIASES[token.toLowerCase()]
  if (generic) return MODIFIER_LABEL[generic]
  const code = codes[0]
  if (/^Key[A-Z]$/.test(code)) return code.slice(3)
  if (/^Digit[0-9]$/.test(code)) return code.slice(5)
  return code
}

/** Parse a shortcut string; null when it names no key. */
export function parseShortcut(raw: string | null | undefined): ParsedShortcut | null {
  if (typeof raw !== 'string') return null
  const text = raw.trim()
  if (!text) return null
  const parts = text.split('+').map((p) => p.trim())
  if (parts.some((p) => !p)) return null
  const main = parts[parts.length - 1]
  const modifiers: ModifierKind[] = []
  for (const p of parts.slice(0, -1)) {
    const kind = MODIFIER_ALIASES[p.toLowerCase()]
    if (!kind) return null
    if (!modifiers.includes(kind)) modifiers.push(kind)
  }
  modifiers.sort((a, b) => MODIFIER_ORDER.indexOf(a) - MODIFIER_ORDER.indexOf(b))
  const codes = codesForToken(main)
  const label = [...modifiers.map((m) => MODIFIER_LABEL[m]), labelForToken(main, codes)].join('+')
  return { codes, modifiers, label }
}

type KeyEventLike = Pick<KeyboardEvent, 'code' | 'ctrlKey' | 'altKey' | 'shiftKey' | 'metaKey'>

/** Whether a keydown presses the shortcut: main key by code, modifiers exact. */
export function matchesShortcut(e: KeyEventLike, sc: ParsedShortcut | null): boolean {
  if (!sc || !sc.codes.includes(e.code)) return false
  // When the main key is itself a modifier, pressing it sets its own flag.
  const self = modifierOfCode(e.code)
  return MODIFIER_ORDER.every((kind) => kind === self || e[MODIFIER_FLAG[kind]] === sc.modifiers.includes(kind))
}

/** Whether a keyup releases the shortcut: the main key or a required modifier. */
export function releasesShortcut(e: Pick<KeyboardEvent, 'code'>, sc: ParsedShortcut | null): boolean {
  if (!sc) return false
  if (sc.codes.includes(e.code)) return true
  const kind = modifierOfCode(e.code)
  return !!kind && sc.modifiers.includes(kind)
}

/** Display label for a configured shortcut; the raw text when it cannot be parsed. */
export function shortcutLabel(raw: string | null | undefined): string {
  return parseShortcut(raw)?.label ?? (typeof raw === 'string' ? raw.trim() : '')
}

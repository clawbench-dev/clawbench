/**
 * Terminal key definitions catalog.
 * All available keys and symbols with their groups, display labels, and send functions.
 * This is the source of truth for what keys/symbols exist in the system.
 */

export interface KeyDef {
  id: string
  label: string
  group: KeyGroup | SymbolGroup
  sendFn?: string       // function name on useTerminalKeys (for keys)
  char?: string         // character to send (for symbols)
  isModifier?: boolean  // true for ctrl/alt/shift (toggle behavior)
}

export type KeyGroup = 'modifier' | 'function' | 'navigation' | 'arrow' | 'shortcut' | 'editing'
export type SymbolGroup = 'punctuation' | 'math' | 'bracket' | 'quote' | 'shell'

export type ConfigType = 'key' | 'symbol'

export interface KeyGroupInfo {
  key: KeyGroup | SymbolGroup
  label: string  // i18n key
}

// Key groups (Termius-style)
export const KEY_GROUPS: KeyGroupInfo[] = [
  { key: 'modifier', label: 'terminal.keyGroupModifier' },
  { key: 'function', label: 'terminal.keyGroupFunction' },
  { key: 'navigation', label: 'terminal.keyGroupNavigation' },
  { key: 'arrow', label: 'terminal.keyGroupArrow' },
  { key: 'shortcut', label: 'terminal.keyGroupShortcut' },
  { key: 'editing', label: 'terminal.keyGroupEditing' },
]

// Symbol groups (by char type)
export const SYMBOL_GROUPS: KeyGroupInfo[] = [
  { key: 'punctuation', label: 'terminal.symbolGroupPunctuation' },
  { key: 'math', label: 'terminal.symbolGroupMath' },
  { key: 'bracket', label: 'terminal.symbolGroupBracket' },
  { key: 'quote', label: 'terminal.symbolGroupQuote' },
  { key: 'shell', label: 'terminal.symbolGroupShell' },
]

// All available keys
export const ALL_KEYS: KeyDef[] = [
  // Modifiers
  { id: 'esc', label: 'Esc', group: 'modifier', sendFn: 'sendEscape' },
  { id: 'tab', label: 'Tab', group: 'modifier', sendFn: 'sendTab' },
  { id: 'ctrl', label: 'Ctrl', group: 'modifier', isModifier: true },
  { id: 'alt', label: 'Alt', group: 'modifier', isModifier: true },
  { id: 'shift', label: 'Shift', group: 'modifier', isModifier: true },
  { id: 'shift_tab', label: 'Shift Tab', group: 'modifier', sendFn: 'sendShiftTab' },
  // Function keys
  { id: 'f1', label: 'F1', group: 'function', sendFn: 'sendF1' },
  { id: 'f2', label: 'F2', group: 'function', sendFn: 'sendF2' },
  { id: 'f3', label: 'F3', group: 'function', sendFn: 'sendF3' },
  { id: 'f4', label: 'F4', group: 'function', sendFn: 'sendF4' },
  { id: 'f5', label: 'F5', group: 'function', sendFn: 'sendF5' },
  { id: 'f6', label: 'F6', group: 'function', sendFn: 'sendF6' },
  { id: 'f7', label: 'F7', group: 'function', sendFn: 'sendF7' },
  { id: 'f8', label: 'F8', group: 'function', sendFn: 'sendF8' },
  { id: 'f9', label: 'F9', group: 'function', sendFn: 'sendF9' },
  { id: 'f10', label: 'F10', group: 'function', sendFn: 'sendF10' },
  { id: 'f11', label: 'F11', group: 'function', sendFn: 'sendF11' },
  { id: 'f12', label: 'F12', group: 'function', sendFn: 'sendF12' },
  // Navigation
  { id: 'home', label: 'Home', group: 'navigation', sendFn: 'sendHome' },
  { id: 'end', label: 'End', group: 'navigation', sendFn: 'sendEnd' },
  { id: 'pgup', label: 'PgUp', group: 'navigation', sendFn: 'sendPageUp' },
  { id: 'pgdn', label: 'PgDn', group: 'navigation', sendFn: 'sendPageDown' },
  { id: 'insert', label: 'Ins', group: 'navigation', sendFn: 'sendInsert' },
  // Arrows
  { id: 'arrow_up', label: '↑', group: 'arrow', sendFn: 'sendArrowUp' },
  { id: 'arrow_down', label: '↓', group: 'arrow', sendFn: 'sendArrowDown' },
  { id: 'arrow_left', label: '←', group: 'arrow', sendFn: 'sendArrowLeft' },
  { id: 'arrow_right', label: '→', group: 'arrow', sendFn: 'sendArrowRight' },
  // Shortcuts
  { id: 'ctrl_c', label: '⌃C', group: 'shortcut', sendFn: 'sendCtrlC' },
  { id: 'ctrl_z', label: '⌃Z', group: 'shortcut', sendFn: 'sendCtrlZ' },
  { id: 'ctrl_s', label: '⌃S', group: 'shortcut', sendFn: 'sendCtrlS' },
  { id: 'ctrl_d', label: '⌃D', group: 'shortcut', sendFn: 'sendCtrlD' },
  { id: 'ctrl_l', label: '⌃L', group: 'shortcut', sendFn: 'sendCtrlL' },
  { id: 'ctrl_r', label: '⌃R', group: 'shortcut', sendFn: 'sendCtrlR' },
  // Editing
  { id: 'enter', label: 'Enter', group: 'editing', sendFn: 'sendEnter' },
  { id: 'backspace', label: '⌫', group: 'editing', sendFn: 'sendBackspace' },
  { id: 'delete', label: 'Del', group: 'editing', sendFn: 'sendDelete' },
]

// All available symbols
export const ALL_SYMBOLS: KeyDef[] = [
  // Punctuation
  { id: '.', label: '.', group: 'punctuation', char: '.' },
  { id: ',', label: ',', group: 'punctuation', char: ',' },
  { id: ';', label: ';', group: 'punctuation', char: ';' },
  { id: ':', label: ':', group: 'punctuation', char: ':' },
  { id: '!', label: '!', group: 'punctuation', char: '!' },
  { id: '?', label: '?', group: 'punctuation', char: '?' },
  // Math
  { id: '+', label: '+', group: 'math', char: '+' },
  { id: '-', label: '-', group: 'math', char: '-' },
  { id: '*', label: '*', group: 'math', char: '*' },
  { id: '/', label: '/', group: 'math', char: '/' },
  { id: '=', label: '=', group: 'math', char: '=' },
  { id: '%', label: '%', group: 'math', char: '%' },
  { id: '<', label: '<', group: 'math', char: '<' },
  { id: '>', label: '>', group: 'math', char: '>' },
  // Brackets
  { id: '(', label: '(', group: 'bracket', char: '(' },
  { id: ')', label: ')', group: 'bracket', char: ')' },
  { id: '[', label: '[', group: 'bracket', char: '[' },
  { id: ']', label: ']', group: 'bracket', char: ']' },
  { id: '{', label: '{', group: 'bracket', char: '{' },
  { id: '}', label: '}', group: 'bracket', char: '}' },
  // Quotes
  { id: '"', label: '"', group: 'quote', char: '"' },
  { id: "'", label: "'", group: 'quote', char: "'" },
  { id: '`', label: '`', group: 'quote', char: '`' },
  // Shell special
  { id: '|', label: '|', group: 'shell', char: '|' },
  { id: '&', label: '&', group: 'shell', char: '&' },
  { id: '$', label: '$', group: 'shell', char: '$' },
  { id: '~', label: '~', group: 'shell', char: '~' },
  { id: '#', label: '#', group: 'shell', char: '#' },
  { id: '@', label: '@', group: 'shell', char: '@' },
  { id: '\\', label: '\\', group: 'shell', char: '\\' },
  { id: '_', label: '_', group: 'shell', char: '_' },
  { id: '^', label: '^', group: 'shell', char: '^' },
]

// Default selected key IDs (current toolbar keys)
export const DEFAULT_KEY_IDS = [
  'esc', 'tab', 'ctrl', 'alt', 'shift', 'shift_tab',
  'ctrl_c', 'ctrl_z', 'ctrl_s',
  'home', 'end', 'pgup', 'pgdn',
  'arrow_up', 'arrow_down', 'arrow_left', 'arrow_right',
]

// Default selected symbol IDs (fewer)
export const DEFAULT_SYMBOL_IDS = ['.', '/', '-', '$', '&', ';', '|', '=', '>']

// Lookup map for quick access
export const KEY_MAP = new Map(ALL_KEYS.map(k => [k.id, k]))
export const SYMBOL_MAP = new Map(ALL_SYMBOLS.map(s => [s.id, s]))

/** Get KeyDef by type and id */
export function getDef(type: ConfigType, id: string): KeyDef | undefined {
  return type === 'key' ? KEY_MAP.get(id) : SYMBOL_MAP.get(id)
}

/** Get all definitions for a type */
export function getAllDefs(type: ConfigType): KeyDef[] {
  return type === 'key' ? ALL_KEYS : ALL_SYMBOLS
}

/** Get group info for a type */
export function getGroups(type: ConfigType): KeyGroupInfo[] {
  return type === 'key' ? KEY_GROUPS : SYMBOL_GROUPS
}

/** Get default IDs for a type */
export function getDefaultIds(type: ConfigType): string[] {
  return type === 'key' ? [...DEFAULT_KEY_IDS] : [...DEFAULT_SYMBOL_IDS]
}

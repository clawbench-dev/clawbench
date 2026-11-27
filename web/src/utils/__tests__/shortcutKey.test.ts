import { describe, it, expect } from 'vitest'
import { matchesShortcut, parseShortcut, releasesShortcut, shortcutLabel } from '@/utils/shortcutKey'

function ev(code: string, mods: Partial<Record<'ctrlKey' | 'altKey' | 'shiftKey' | 'metaKey', boolean>> = {}) {
  return { code, ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, ...mods }
}

describe('parseShortcut', () => {
  it('parses a bare function key', () => {
    expect(parseShortcut('F9')).toEqual({ codes: ['F9'], modifiers: [], label: 'F9' })
    expect(parseShortcut(' f9 ')?.codes).toEqual(['F9'])
  })

  it('parses combinations into a main key and ordered modifiers', () => {
    expect(parseShortcut('Ctrl+M')).toEqual({ codes: ['KeyM'], modifiers: ['ctrl'], label: 'Ctrl+M' })
    expect(parseShortcut('shift+control+k')).toEqual({ codes: ['KeyK'], modifiers: ['ctrl', 'shift'], label: 'Ctrl+Shift+K' })
    expect(parseShortcut('Alt+1')?.codes).toEqual(['Digit1'])
    expect(parseShortcut('Ctrl+Space')?.codes).toEqual(['Space'])
  })

  it('keeps sided modifiers as distinct keys', () => {
    expect(parseShortcut('AltRight')).toEqual({ codes: ['AltRight'], modifiers: [], label: 'AltRight' })
    expect(parseShortcut('altleft')?.codes).toEqual(['AltLeft'])
    expect(parseShortcut('CtrlRight')?.codes).toEqual(['ControlRight'])
    expect(parseShortcut('Alt')?.codes).toEqual(['AltLeft', 'AltRight'])
  })

  it('rejects empty and malformed values', () => {
    expect(parseShortcut('')).toBeNull()
    expect(parseShortcut(undefined)).toBeNull()
    expect(parseShortcut('Ctrl+')).toBeNull()
    expect(parseShortcut('M+K')).toBeNull()
  })
})

describe('matchesShortcut', () => {
  it('matches F9 only without modifiers', () => {
    const sc = parseShortcut('F9')
    expect(matchesShortcut(ev('F9'), sc)).toBe(true)
    expect(matchesShortcut(ev('F9', { shiftKey: true }), sc)).toBe(false)
    expect(matchesShortcut(ev('F8'), sc)).toBe(false)
  })

  it('matches a combination with exact modifiers', () => {
    const sc = parseShortcut('Ctrl+M')
    expect(matchesShortcut(ev('KeyM', { ctrlKey: true }), sc)).toBe(true)
    expect(matchesShortcut(ev('KeyM'), sc)).toBe(false)
    expect(matchesShortcut(ev('KeyM', { ctrlKey: true, shiftKey: true }), sc)).toBe(false)
    expect(matchesShortcut(ev('F9'), sc)).toBe(false)
  })

  it('tells left and right Alt apart', () => {
    const right = parseShortcut('AltRight')
    expect(matchesShortcut(ev('AltRight', { altKey: true }), right)).toBe(true)
    expect(matchesShortcut(ev('AltLeft', { altKey: true }), right)).toBe(false)
    expect(matchesShortcut(ev('AltRight', { altKey: true, ctrlKey: true }), right)).toBe(false)
    const left = parseShortcut('AltLeft')
    expect(matchesShortcut(ev('AltLeft', { altKey: true }), left)).toBe(true)
    expect(matchesShortcut(ev('AltRight', { altKey: true }), left)).toBe(false)
  })

  it('never matches an unparseable shortcut', () => {
    expect(matchesShortcut(ev('F9'), null)).toBe(false)
  })
})

describe('releasesShortcut', () => {
  it('fires on the main key or a required modifier', () => {
    const sc = parseShortcut('Ctrl+M')
    expect(releasesShortcut(ev('KeyM'), sc)).toBe(true)
    expect(releasesShortcut(ev('ControlLeft'), sc)).toBe(true)
    expect(releasesShortcut(ev('ControlRight'), sc)).toBe(true)
    expect(releasesShortcut(ev('ShiftLeft'), sc)).toBe(false)
    expect(releasesShortcut(ev('KeyK'), sc)).toBe(false)
  })

  it('only the bound side releases a sided key', () => {
    const sc = parseShortcut('AltRight')
    expect(releasesShortcut(ev('AltRight'), sc)).toBe(true)
    expect(releasesShortcut(ev('AltLeft'), sc)).toBe(false)
  })
})

describe('shortcutLabel', () => {
  it('normalises the configured text', () => {
    expect(shortcutLabel('ctrl+m')).toBe('Ctrl+M')
    expect(shortcutLabel('F9')).toBe('F9')
    expect(shortcutLabel('altright')).toBe('AltRight')
  })

  it('falls back to the raw text when unparseable', () => {
    expect(shortcutLabel('M+K')).toBe('M+K')
    expect(shortcutLabel(undefined)).toBe('')
  })
})

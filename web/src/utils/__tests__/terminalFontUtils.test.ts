import { describe, expect, it, vi } from 'vitest'
import {
  loadFontSize,
  applyFontSize,
  shortCwd,
  canReconnect,
  errorDisplayMessage,
  showErrorOverlay,
  DEFAULT_FONT_SIZE,
} from '@/utils/terminalFontUtils'

describe('loadFontSize', () => {
  it('returns default when no saved value', () => {
    expect(loadFontSize(() => null)).toBe(DEFAULT_FONT_SIZE)
  })

  it('returns saved valid value', () => {
    expect(loadFontSize(() => '14')).toBe(14)
  })

  it('returns default for value below minimum', () => {
    expect(loadFontSize(() => '3')).toBe(DEFAULT_FONT_SIZE)
  })

  it('returns default for value above maximum', () => {
    expect(loadFontSize(() => '50')).toBe(DEFAULT_FONT_SIZE)
  })

  it('returns default for non-numeric string', () => {
    expect(loadFontSize(() => 'abc')).toBe(DEFAULT_FONT_SIZE)
  })

  it('returns minimum boundary value', () => {
    expect(loadFontSize(() => '8')).toBe(8)
  })

  it('returns maximum boundary value', () => {
    expect(loadFontSize(() => '28')).toBe(28)
  })

  it('parses integer from float string', () => {
    expect(loadFontSize(() => '14.7')).toBe(14)
  })
})

describe('applyFontSize', () => {
  it('clamps to minimum and persists', () => {
    const stored: string[] = []
    const setItem = (_k: string, v: string) => stored.push(v)
    expect(applyFontSize(3, setItem)).toBe(8)
    expect(stored).toEqual(['8'])
  })

  it('clamps to maximum and persists', () => {
    const stored: string[] = []
    const setItem = (_k: string, v: string) => stored.push(v)
    expect(applyFontSize(100, setItem)).toBe(28)
    expect(stored).toEqual(['28'])
  })

  it('keeps value within range and persists', () => {
    const stored: string[] = []
    const setItem = (_k: string, v: string) => stored.push(v)
    expect(applyFontSize(16, setItem)).toBe(16)
    expect(stored).toEqual(['16'])
  })

  it('preserves boundary values exactly', () => {
    expect(applyFontSize(8, () => {})).toBe(8)
    expect(applyFontSize(28, () => {})).toBe(28)
  })

  it('handles negative input', () => {
    expect(applyFontSize(-5, () => {})).toBe(8)
  })

  it('handles zero', () => {
    expect(applyFontSize(0, () => {})).toBe(8)
  })

  it('stores correct key', () => {
    let storedKey = ''
    const setItem = (k: string, _v: string) => { storedKey = k }
    applyFontSize(14, setItem)
    expect(storedKey).toBe('clawbench-terminal-font-size')
  })
})

describe('shortCwd', () => {
  it('returns empty string for undefined', () => {
    expect(shortCwd(undefined)).toBe('')
  })

  it('returns empty string for null', () => {
    expect(shortCwd(null)).toBe('')
  })

  it('returns empty string for empty string', () => {
    expect(shortCwd('')).toBe('')
  })

  it('returns shallow path unchanged', () => {
    expect(shortCwd('home')).toBe('home')
  })

  it('returns two-level path unchanged', () => {
    expect(shortCwd('home/user')).toBe('home/user')
  })

  it('abbreviates deep path with ellipsis', () => {
    expect(shortCwd('home/user/projects/app')).toBe('.../projects/app')
  })

  it('abbreviates very deep path', () => {
    expect(shortCwd('a/b/c/d/e/f')).toBe('.../e/f')
  })

  it('handles root-level slash path', () => {
    expect(shortCwd('/usr/local/bin')).toBe('.../local/bin')
  })
})

describe('canReconnect', () => {
  it('returns true for undefined', () => {
    expect(canReconnect(undefined)).toBe(true)
  })

  it('returns true for null', () => {
    expect(canReconnect(null)).toBe(true)
  })

  it('returns true for empty string', () => {
    expect(canReconnect('')).toBe(true)
  })

  it('returns false for terminal_disabled', () => {
    expect(canReconnect('terminal_disabled')).toBe(false)
  })

  it('returns false for platform_unsupported', () => {
    expect(canReconnect('platform_unsupported')).toBe(false)
  })

  it('returns true for shell_start_failed', () => {
    expect(canReconnect('shell_start_failed')).toBe(true)
  })

  it('returns true for session_limit', () => {
    expect(canReconnect('session_limit')).toBe(true)
  })

  it('returns true for unknown error codes', () => {
    expect(canReconnect('unknown_error')).toBe(true)
  })
})

describe('errorDisplayMessage', () => {
  const fallback = 'Connection failed'

  it('returns fallback for terminal_disabled', () => {
    expect(errorDisplayMessage('terminal_disabled', 'some message', fallback)).toBe(fallback)
  })

  it('returns fallback for shell_start_failed', () => {
    expect(errorDisplayMessage('shell_start_failed', 'some message', fallback)).toBe(fallback)
  })

  it('returns fallback for platform_unsupported', () => {
    expect(errorDisplayMessage('platform_unsupported', 'some message', fallback)).toBe(fallback)
  })

  it('returns error message when no special code', () => {
    expect(errorDisplayMessage('other_code', 'Actual error', fallback)).toBe('Actual error')
  })

  it('returns fallback when no error message and no special code', () => {
    expect(errorDisplayMessage(null, '', fallback)).toBe(fallback)
  })

  it('returns fallback when no error message and no code', () => {
    expect(errorDisplayMessage(null, null, fallback)).toBe(fallback)
  })

  it('returns fallback when error message is empty string and code is not special', () => {
    expect(errorDisplayMessage('some_code', '', fallback)).toBe(fallback)
  })

  it('prioritizes special code over error message', () => {
    // terminal_disabled should return fallback, not the error message
    expect(errorDisplayMessage('terminal_disabled', 'Should not show', fallback)).toBe(fallback)
  })
})

describe('showErrorOverlay', () => {
  it('returns true for error state', () => {
    expect(showErrorOverlay('error')).toBe(true)
  })

  it('returns true for disconnected state', () => {
    expect(showErrorOverlay('disconnected')).toBe(true)
  })

  it('returns false for connected state', () => {
    expect(showErrorOverlay('connected')).toBe(false)
  })

  it('returns false for connecting state', () => {
    expect(showErrorOverlay('connecting')).toBe(false)
  })

  it('returns false for reconnecting state', () => {
    expect(showErrorOverlay('reconnecting')).toBe(false)
  })
})

describe('constants', () => {
  it('DEFAULT_FONT_SIZE is 12', () => {
    expect(DEFAULT_FONT_SIZE).toBe(12)
  })
})

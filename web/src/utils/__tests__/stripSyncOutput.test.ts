import { describe, it, expect } from 'vitest'
import { stripSyncOutput } from '@/utils/terminalSessionUtils'

describe('stripSyncOutput', () => {
  it('passes through data without sync output sequences unchanged', () => {
    const data = 'hello world\x1b[?1049h\x1b[?25l'
    expect(stripSyncOutput(data)).toBe(data)
  })

  it('strips sync output ON sequence', () => {
    expect(stripSyncOutput('\x1b[?2026h')).toBe('')
  })

  it('strips sync output OFF sequence', () => {
    expect(stripSyncOutput('\x1b[?2026l')).toBe('')
  })

  it('strips both ON and OFF sequences', () => {
    const data = '\x1b[?2026hrendered content\x1b[?2026l'
    expect(stripSyncOutput(data)).toBe('rendered content')
  })

  it('strips multiple sync output pairs', () => {
    const data = '\x1b[?2026hframe1\x1b[?2026l\x1b[?2026hframe2\x1b[?2026l'
    expect(stripSyncOutput(data)).toBe('frame1frame2')
  })

  it('strips unpaired sync output ON (common in Bubble Tea apps)', () => {
    const data = '\x1b[?2026h\x1b[?25l\x1b[1;1H\x1b[38;2;255;255;255mcontent'
    const result = stripSyncOutput(data)
    expect(result).not.toContain('\x1b[?2026')
    expect(result).toContain('\x1b[?25l')
    expect(result).toContain('content')
  })

  it('fast-paths when no sync output sequences present', () => {
    const data = 'normal terminal output without sync sequences'
    // Should return the same string reference (no allocation)
    expect(stripSyncOutput(data)).toBe(data)
  })

  it('preserves other DEC private mode sequences', () => {
    const data = '\x1b[?1049h\x1b[?2026hcontent\x1b[?2026l\x1b[?25l'
    const result = stripSyncOutput(data)
    expect(result).toContain('\x1b[?1049h')
    expect(result).toContain('\x1b[?25l')
    expect(result).not.toContain('\x1b[?2026')
  })

  it('handles empty string', () => {
    expect(stripSyncOutput('')).toBe('')
  })

  it('handles real OpenCode output pattern', () => {
    // Simulated OpenCode output: sync ON + rendering + no sync OFF
    const data = '\x1b[?2026h\x1b[?25l\x1b[1;1H\x1b[38;2;255;255;255m\x1b[48;2;40;44;52m▀▀▀▀'
    const result = stripSyncOutput(data)
    expect(result).not.toContain('\x1b[?2026')
    expect(result).toContain('\x1b[?25l')
    expect(result).toContain('▀▀▀▀')
  })

  it('handles vim output pattern', () => {
    // Simulated vim: alternate screen + sync ON + content + sync OFF
    const data = '\x1b[?1049h\x1b[?2026h\x1b[1;1H~\x1b[2;1H~\x1b[?2026l'
    const result = stripSyncOutput(data)
    expect(result).toContain('\x1b[?1049h')
    expect(result).toContain('~')
    expect(result).not.toContain('\x1b[?2026')
  })
})

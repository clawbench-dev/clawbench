import { describe, it, expect } from 'vitest'
import { useTerminalCommandLine } from '../useTerminalCommandLine'

/** Type a string one keystroke at a time, the way xterm delivers it. */
function type(tracker: ReturnType<typeof useTerminalCommandLine>, text: string) {
  let submitted: string | null = null
  for (const ch of text) {
    const got = tracker.feed(ch)
    if (got) submitted = got
  }
  return submitted
}

describe('useTerminalCommandLine', () => {
  it('submits the line on Enter', () => {
    const tracker = useTerminalCommandLine()
    expect(type(tracker, 'git status')).toBeNull()
    expect(tracker.feed('\r')).toBe('git status')
  })

  it('treats LF as a submit too (pasted multi-line text)', () => {
    const tracker = useTerminalCommandLine()
    expect(tracker.feed('ls\n')).toBe('ls')
  })

  it('applies backspace', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'ls -la')
    tracker.feed('\x7f')
    expect(tracker.feed('\r')).toBe('ls -l')
  })

  it('applies Ctrl-U (clear line)', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'rm -rf /')
    tracker.feed('\x15')
    type(tracker, 'ls')
    expect(tracker.feed('\r')).toBe('ls')
  })

  it('applies Ctrl-W (delete word)', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'git commit -m')
    tracker.feed('\x17')
    expect(tracker.feed('\r')).toBe('git commit')
  })

  it('drops the line on Ctrl-C', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'rm -rf /')
    tracker.feed('\x03')
    expect(tracker.feed('\r')).toBeNull()
  })

  it('does not record a line that was edited with arrow keys', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'ls')
    tracker.feed('\x1b[A') // arrow up recalls a previous command
    expect(tracker.feed('\r')).toBeNull()
  })

  it('does not record a line completed with Tab', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'git ch')
    tracker.feed('\t')
    type(tracker, 'eckout')
    expect(tracker.feed('\r')).toBeNull()
  })

  it('resumes recording after an invalidated line is submitted', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'git ch')
    tracker.feed('\t')
    tracker.feed('\r')
    type(tracker, 'ls')
    expect(tracker.feed('\r')).toBe('ls')
  })

  it('ignores a leading space (HISTCONTROL=ignorespace)', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, ' secret-command')
    expect(tracker.feed('\r')).toBeNull()
  })

  it('submits each line of a pasted block separately', () => {
    const tracker = useTerminalCommandLine()
    const submitted: string[] = []
    for (const ch of 'a\nb\n') {
      const got = tracker.feed(ch)
      if (got) submitted.push(got)
    }
    expect(submitted).toEqual(['a', 'b'])
  })

  it('submits a line seeded with setLine', () => {
    const tracker = useTerminalCommandLine()
    tracker.setLine('git log --oneline')
    expect(tracker.feed('\r')).toBe('git log --oneline')
  })

  it('caps absurdly long lines', () => {
    const tracker = useTerminalCommandLine()
    tracker.feed('a'.repeat(5000))
    const submitted = tracker.feed('\r')
    expect(submitted).not.toBeNull()
    expect(submitted!.length).toBe(4096)
  })

  it('reset forgets the current line', () => {
    const tracker = useTerminalCommandLine()
    type(tracker, 'ls')
    tracker.reset()
    expect(tracker.feed('\r')).toBeNull()
  })
})

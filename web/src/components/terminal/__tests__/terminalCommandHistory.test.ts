import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Command-history wiring inside the terminal.
 *
 * The pieces here are one-line orderings with non-obvious failure modes, and
 * getting them wrong is silent (no error, just a history panel that misses or
 * duplicates entries), so they are pinned at the source level:
 *
 *   1. the `command` frame must reach the backend BEFORE the newline does — the
 *      server checks that the foreground process is still the shell, and once
 *      the shell starts the command it no longer is;
 *   2. `runCommandLine` must NOT seed the tracker, or the Enter it sends would
 *      submit the same command a second time;
 *   3. `insertCommandLine` MUST seed it, or pressing Enter after inserting a
 *      remembered command records nothing.
 */
describe('terminal command history wiring', () => {
  const tabs = readFileSync(resolve(__dirname, '../../../composables/useTerminalTabs.ts'), 'utf8')

  it('reports the command before writing the newline to the PTY', () => {
    const onData = tabs.slice(tabs.indexOf('term.onData((data) =>'), tabs.indexOf('term.onResize'))
    expect(onData).toContain('commandLine.feed(transformed)')
    expect(onData).toContain('session.sendCommand(submitted)')

    const feedAt = onData.indexOf('commandLine.feed(transformed)')
    const sendCommandAt = onData.indexOf('session.sendCommand(submitted)')
    const sendInputAt = onData.indexOf('session.sendInput(transformed)')

    expect(feedAt).toBeGreaterThan(-1)
    expect(sendCommandAt).toBeGreaterThan(feedAt)
    expect(sendInputAt).toBeGreaterThan(sendCommandAt)
  })

  it('does not seed the tracker when it runs a command immediately', () => {
    const fn = tabs.slice(tabs.indexOf('function runCommandLine'), tabs.lastIndexOf('return {'))
    expect(fn).toContain('sendCommand(command)')
    expect(fn).toContain("sendInput(command + '\\r')")
    // Seeding here would make the Enter submit the line again through onData.
    expect(fn).not.toContain('setLine')
  })

  it('seeds the tracker when it only inserts a command', () => {
    const fn = tabs.slice(tabs.indexOf('function insertCommandLine'), tabs.indexOf('function runCommandLine'))
    expect(fn).toContain('setLine(command)')
    expect(fn).toContain('sendInput(command)')
    // Inserting must not execute.
    expect(fn).not.toContain("'\\r'")
  })
})

/**
 * The destructive-command confirmation is an ordering rule with one silent
 * failure mode: if the newline reaches the PTY before the check, the command
 * runs and the dialog appears afterwards, which is worse than useless.
 */
describe('destructive command confirmation wiring', () => {
  const tabs = readFileSync(resolve(__dirname, '../../../composables/useTerminalTabs.ts'), 'utf8')
  const panel = readFileSync(resolve(__dirname, '../TerminalPanelContent.vue'), 'utf8')

  it('checks for danger before the newline is written', () => {
    const onData = tabs.slice(tabs.indexOf('term.onData((data) =>'), tabs.indexOf('term.onResize'))
    const dangerAt = onData.indexOf('opts.detectDanger')
    const sendInputAt = onData.indexOf('session.sendInput(transformed)')
    expect(dangerAt).toBeGreaterThan(-1)
    expect(sendInputAt).toBeGreaterThan(dangerAt)
  })

  it('holds the line back instead of sending it', () => {
    const onData = tabs.slice(tabs.indexOf('term.onData((data) =>'), tabs.indexOf('term.onResize'))
    const dangerBlock = onData.slice(
      onData.indexOf('if (danger)'),
      onData.indexOf('if (opts.shouldRecordHistory'),
    )
    // The tracker is restored so pressing Enter again re-checks the same line.
    expect(dangerBlock).toContain('tab.commandLine.setLine(submitted)')
    // And the handler returns before reaching sendInput.
    expect(dangerBlock).toContain('return')
  })

  it('also checks commands sent from the input drawer', () => {
    const fn = panel.slice(panel.indexOf('function inputToTerminal'), panel.indexOf('function handleModeCycle'))
    expect(fn).toContain('detectDangerousCommand(text)')
    // The drawer path bypasses the keystroke tracker, so it must report that the
    // text has not reached the shell yet.
    expect(fn).toContain('onInputLine: false')
    const guardAt = fn.indexOf('if (danger)')
    const sendAt = fn.indexOf("sendInput(text + '\\r')")
    expect(guardAt).toBeGreaterThan(-1)
    expect(sendAt).toBeGreaterThan(guardAt)
  })

  it('resumes from the right place when confirmed', () => {
    const fn = panel.slice(panel.indexOf('function confirmDanger'), panel.indexOf('function cancelDanger'))
    // Parked on the shell's input line: only the newline is owed, and the
    // tracker must be cleared or the next Enter would run it a second time.
    expect(fn).toContain('tab.commandLine.reset()')
    expect(fn).toContain("sendInput('\\r')")
    // Never sent: the whole command plus the newline.
    expect(fn).toContain("sendInput(pending.command + '\\r')")
  })
})

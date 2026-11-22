import { describe, it, expect } from 'vitest'
import {
  buildCloseDialogOptions,
  interpretCloseDialog,
  normalizeCloseAction,
  type CloseDialogLabels,
} from './closePolicy'

/**
 * The close-behaviour decisions, without Electron.
 *
 * Everything here is the "what should happen" half of the dialog; `closeFlow`
 * only carries it out. The cases that matter are the ones a user can reach by
 * accident: Esc / the dialog's close box (must not close anything), the unticked
 * checkbox (must not overwrite the stored choice), and a corrupted stored value
 * (must fall back to asking rather than to quitting or hiding).
 */

const LABELS: CloseDialogLabels = {
  title: 'Close',
  message: 'What should closing do?',
  detail: 'detail',
  tray: 'Minimize to tray',
  exit: 'Quit',
  exitStop: 'Quit and stop the server',
  cancel: 'Cancel',
  remember: 'Remember',
  trayUnavailable: 'no tray',
  ok: 'OK',
}

describe('normalizeCloseAction', () => {
  it('keeps the three known actions', () => {
    expect(normalizeCloseAction('ask')).toBe('ask')
    expect(normalizeCloseAction('tray')).toBe('tray')
    expect(normalizeCloseAction('exit')).toBe('exit')
  })

  it('falls back to asking for anything else', () => {
    // A hand-edited config, a downgraded build, a stale value: none of them may
    // reach the flow, where 'tray' vs 'exit' is a destructive difference.
    for (const raw of ['minimize', '', 'EXIT', null, undefined, 42, {}, []]) {
      expect(normalizeCloseAction(raw)).toBe('ask')
    }
  })
})

describe('buildCloseDialogOptions', () => {
  const options = buildCloseDialogOptions(LABELS)

  it('offers the three actions in a stable order', () => {
    expect(options.buttons).toEqual(['Minimize to tray', 'Quit', 'Cancel'])
  })

  it('defaults to the non-destructive choice and cancels on Esc', () => {
    // Enter must not quit the app, and Esc / the window's close box must mean
    // "do nothing" — index 2 is the only button that closes nothing.
    expect(options.defaultId).toBe(0)
    expect(options.cancelId).toBe(2)
  })

  it('starts with "remember" unticked', () => {
    // An opt-in, not something a hurried click can silently set: an unnoticed
    // ticked box would turn one dismissal into a permanent behaviour change.
    expect(options.checkboxLabel).toBe('Remember')
    expect(options.checkboxChecked).toBe(false)
  })

  it('adds "quit and stop the server" only when there is a server to stop', () => {
    // Quitting leaves the bundled server running on purpose, so an integrated
    // install gets a third action. A thin client has nothing to stop and must
    // not show a dead option.
    const withServer = buildCloseDialogOptions(LABELS, { canStopServer: true })
    expect(withServer.buttons).toEqual([
      'Minimize to tray',
      'Quit',
      'Quit and stop the server',
      'Cancel',
    ])
    expect(withServer.cancelId).toBe(3)
    expect(options.buttons).not.toContain('Quit and stop the server')
  })
})

describe('interpretCloseDialog', () => {
  it('maps each button to its choice', () => {
    expect(interpretCloseDialog({ response: 0, checkboxChecked: false }, 'ask').choice).toBe('tray')
    expect(interpretCloseDialog({ response: 1, checkboxChecked: false }, 'ask').choice).toBe('exit')
    expect(interpretCloseDialog({ response: 2, checkboxChecked: false }, 'ask').choice).toBeNull()
  })

  it('does not overwrite the stored choice unless the box was ticked', () => {
    for (const response of [0, 1, 2]) {
      expect(interpretCloseDialog({ response, checkboxChecked: false }, 'ask').next).toBe('ask')
      expect(interpretCloseDialog({ response, checkboxChecked: false }, 'tray').next).toBe('tray')
    }
  })

  it('persists the picked action when remembered', () => {
    expect(interpretCloseDialog({ response: 0, checkboxChecked: true }, 'ask').next).toBe('tray')
    expect(interpretCloseDialog({ response: 1, checkboxChecked: true }, 'ask').next).toBe('exit')
  })

  it('cancelling never persists, even with the box ticked', () => {
    // "Remember" plus Cancel must not turn the cancel into a remembered action.
    const { choice, next } = interpretCloseDialog({ response: 2, checkboxChecked: true }, 'ask')
    expect(choice).toBeNull()
    expect(next).toBe('ask')
  })

  it('maps "quit and stop the server" and never remembers it', () => {
    // Stopping the server is a one-off action, not a standing close behaviour:
    // remembering it would turn every later quit into a server shutdown.
    const { choice, next } = interpretCloseDialog(
      { response: 2, checkboxChecked: true },
      'ask',
      { canStopServer: true },
    )
    expect(choice).toBe('exit-stop')
    expect(next).toBe('exit')
  })

  it('treats the third button as cancel when no server is running', () => {
    // The dialog was built without the extra button; a stray index must not
    // quit the app.
    expect(interpretCloseDialog({ response: 2, checkboxChecked: false }, 'ask').choice).toBeNull()
  })
})

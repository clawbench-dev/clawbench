/**
 * Tracks the line currently being typed at the shell prompt, reconstructed from
 * the raw keystroke stream xterm hands us.
 *
 * Why reconstruct it here rather than read the shell's own history: the backend
 * only ever sees the same byte stream, and a shell's history file is written on
 * exit (bash) or with a delay, so a command run ten seconds ago would not be
 * there. The backend still has the final say — it drops anything that does not
 * look like a command, and on Linux it also checks that the foreground process
 * is still the shell.
 *
 * The rule is "prefer missing a command over recording a wrong one". Anything
 * that moves the cursor or inserts text behind our back (arrow keys, Tab
 * completion, Home/End) invalidates the line, because after it our idea of the
 * buffer no longer matches the shell's. A wrong entry in the history panel is
 * worse than a missing one: the user would paste it and run something else.
 */

/** Lines longer than this are pastes of files, not commands worth recalling. */
const MAX_LINE_LENGTH = 4096

export interface CommandLineTracker {
  /** Feed one chunk of terminal input. Returns the submitted command, if any. */
  feed(data: string): string | null
  /** Replace the tracked line (used when a command is inserted programmatically). */
  setLine(text: string): void
  /** Forget everything (new tab, reconnect). */
  reset(): void
}

export function useTerminalCommandLine(): CommandLineTracker {
  let line = ''
  let trusted = true
  // Escape-sequence state: 0 = none, 1 = just saw ESC, 2 = inside CSI/SS3.
  // Sequences arrive in one chunk in practice, but chunk boundaries are not
  // guaranteed, so the state has to survive across calls.
  let escState: 0 | 1 | 2 = 0

  function reset() {
    line = ''
    trusted = true
    escState = 0
  }

  function feed(data: string): string | null {
    let submitted: string | null = null

    for (const ch of data) {
      if (escState !== 0) {
        if (escState === 1) {
          escState = (ch === '[' || ch === 'O') ? 2 : 0
          continue
        }
        // CSI/SS3 ends at a byte in 0x40–0x7e.
        const code = ch.charCodeAt(0)
        if (code >= 0x40 && code <= 0x7e) escState = 0
        continue
      }

      switch (ch) {
        case '\r':
        case '\n': {
          // A leading space is the conventional "do not record this one"
          // marker (HISTCONTROL=ignorespace); honour it.
          const isPrivate = line !== line.trimStart()
          const cmd = trusted && !isPrivate ? line.trim() : ''
          line = ''
          trusted = true
          if (cmd) submitted = cmd
          continue
        }
        case '\x7f':
        case '\b':
          line = line.slice(0, -1)
          continue
        case '\x15': // Ctrl-U
          line = ''
          continue
        case '\x17': // Ctrl-W
          line = line.replace(/\s*\S*$/, '')
          continue
        case '\x03': // Ctrl-C abandons the line
          reset()
          continue
        case '\t':
          // Tab completion inserts text we never saw.
          line = ''
          trusted = false
          continue
        case '\x1b':
          escState = 1
          line = ''
          trusted = false
          continue
        default:
          break
      }

      const code = ch.charCodeAt(0)
      if (code < 0x20) {
        // Some other control character: the buffer state is now unknown.
        trusted = false
        continue
      }
      if (line.length < MAX_LINE_LENGTH) line += ch
    }

    return submitted
  }

  function setLine(text: string) {
    line = text.slice(0, MAX_LINE_LENGTH)
    trusted = true
  }

  return { feed, setLine, reset }
}

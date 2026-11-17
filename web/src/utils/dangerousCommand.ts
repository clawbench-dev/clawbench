/**
 * Flags commands that are destructive enough to ask about before running.
 *
 * This is a speed bump, not a security boundary: the user can always confirm.
 * So the rules are deliberately NARROW. A false positive costs the user a tap
 * on a command they run every day, and after the third one they stop reading
 * the dialog — which is worse than not having it. Things like
 * `rm -rf build/`, `git reset --hard` and `chmod -R 777 .` are intentionally
 * NOT matched: they are routine, and matching them would train the user to
 * dismiss the dialog without looking.
 */

export type DangerKind =
  | 'recursive-delete'
  | 'disk-write'
  | 'mkfs'
  | 'fork-bomb'
  | 'force-push'
  | 'power'
  | 'drop-data'

/** Wrappers that do not change which command actually runs. */
const WRAPPERS = new Set(['sudo', 'doas', 'command', 'nohup', 'time', 'exec'])

/** Options of those wrappers that consume the following word as their value. */
const OPTIONS_WITH_VALUE = new Set([
  '-u', '--user', '-g', '--group', '-C', '--close-from',
  '-h', '--host', '-p', '--prompt', '-r', '--role', '-t', '--type',
  '-U', '--other-user', '-n', '--namespace',
])

/** Splits a command line into the segments that run independently. */
const SEGMENT_SPLIT = /&&|\|\||[;|\n]/

/** Targets whose recursive deletion is unrecoverable. */
const CATASTROPHIC_TARGET = /^(?:\/|~|\*|\.\.?)(?:\/\*|\*)?$/

const RAW_DISK_TARGET = /^\/dev\/(sd|nvme|hd|vd|disk|mmcblk)/

/**
 * Returns the kind of danger in this line, or null when it looks safe.
 * Only the first match is reported — the user needs one reason, not a list.
 */
export function detectDangerousCommand(line: string): DangerKind | null {
  const trimmed = line.trim()
  if (!trimmed) return null

  // Typed inside a database shell, the statement is the whole line.
  if (/^drop\s+(database|table)\b/i.test(trimmed)) return 'drop-data'

  for (const segment of trimmed.split(SEGMENT_SPLIT)) {
    const kind = checkSegment(segment)
    if (kind) return kind
  }
  return null
}

function checkSegment(raw: string): DangerKind | null {
  // A redirect into a raw device is not a command name, so check it on the text.
  if (/(^|\s)>\s*\/dev\/(sd|nvme|hd|vd|disk|mmcblk)/.test(raw)) return 'disk-write'
  // The fork bomb is a function definition of `:`; its shape is the signal.
  if (/:\s*\(\s*\)\s*\{/.test(raw)) return 'fork-bomb'

  const words = tokenize(raw)
  const cmdIndex = commandIndex(words)
  if (cmdIndex < 0) return null

  const cmd = words[cmdIndex]
  const args = words.slice(cmdIndex + 1)

  switch (cmd) {
    case 'rm':
      return checkRm(args)
    case 'dd':
      return checkDd(args)
    case 'git':
      return checkGit(args)
    case 'mysql':
    case 'mariadb':
    case 'psql':
    case 'sqlite3':
      return args.some(a => /\bdrop\s+(database|table)\b/i.test(a)) ? 'drop-data' : null
    case 'init':
      return args[0] === '0' || args[0] === '6' ? 'power' : null
    case 'shutdown':
    case 'reboot':
    case 'halt':
    case 'poweroff':
      return 'power'
    default:
      return cmd.startsWith('mkfs') ? 'mkfs' : null
  }
}

function checkRm(args: string[]): DangerKind | null {
  const flags = args.filter(a => a.startsWith('-'))
  const targets = args.filter(a => !a.startsWith('-'))

  const recursive = flags.some(f => f === '--recursive' || /^-[a-zA-Z]*[rR]/.test(f))
  // `--no-preserve-root` has exactly one purpose; treat it as intent.
  if (flags.includes('--no-preserve-root')) return 'recursive-delete'
  if (!recursive) return null

  return targets.some(t => CATASTROPHIC_TARGET.test(t)) ? 'recursive-delete' : null
}

function checkDd(args: string[]): DangerKind | null {
  const of = args.find(a => a.startsWith('of='))
  if (!of) return null
  return RAW_DISK_TARGET.test(of.slice(3)) ? 'disk-write' : null
}

function checkGit(args: string[]): DangerKind | null {
  if (args[0] !== 'push') return null
  // --force-with-lease is the safe variant and is never flagged.
  if (args.some(a => a.startsWith('--force-with-lease'))) return null
  const forced = args.some(a => a === '--force' || /^-[a-zA-Z]*f/.test(a))
  return forced ? 'force-push' : null
}

/**
 * Index of the word that names the command, skipping wrappers, their option
 * values, and leading `VAR=value` assignments.
 */
function commandIndex(words: string[]): number {
  let i = 0
  while (i < words.length) {
    const w = words[i]
    if (WRAPPERS.has(w)) {
      i++
      continue
    }
    if (/^[A-Za-z_][A-Za-z0-9_]*=/.test(w)) {
      i++
      continue
    }
    if (w.startsWith('-')) {
      i += OPTIONS_WITH_VALUE.has(w) ? 2 : 1
      continue
    }
    return i
  }
  return -1
}

/** Split on unquoted whitespace; quotes are stripped but not interpreted. */
function tokenize(raw: string): string[] {
  const out: string[] = []
  let cur = ''
  let quote = ''

  for (const ch of raw) {
    if (quote) {
      if (ch === quote) quote = ''
      else cur += ch
      continue
    }
    if (ch === '"' || ch === "'") {
      quote = ch
      continue
    }
    if (/\s/.test(ch)) {
      if (cur) {
        out.push(cur)
        cur = ''
      }
      continue
    }
    cur += ch
  }
  if (cur) out.push(cur)
  return out
}

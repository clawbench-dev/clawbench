/**
 * Pick the remembered commands worth offering for the line being typed.
 *
 * Deliberately local and synchronous: this runs on every keystroke, and the
 * candidate list is already in memory. Prefix matches come first because that
 * is what "I typed `git`, show me my git commands" means; substring matches
 * follow for the case where the user is halfway through an argument.
 *
 * Multi-line input (the drawer is a textarea) is matched on the LAST line: that
 * is the command the user is currently writing.
 */

export interface SuggestableCommand {
  command: string
}

export function suggestCommands<T extends SuggestableCommand>(
  commands: readonly T[],
  input: string,
  limit = 5,
): T[] {
  const lines = input.split('\n')
  const query = (lines[lines.length - 1] ?? '').trim()

  if (!query) return commands.slice(0, limit)

  const seen = new Set<string>()
  const prefix: T[] = []
  const contains: T[] = []

  for (const item of commands) {
    // Offering back the exact line being typed is noise.
    if (item.command === query || seen.has(item.command)) continue
    seen.add(item.command)
    if (item.command.startsWith(query)) {
      prefix.push(item)
    } else if (item.command.includes(query)) {
      contains.push(item)
    }
    if (prefix.length >= limit) break
  }

  return [...prefix, ...contains].slice(0, limit)
}

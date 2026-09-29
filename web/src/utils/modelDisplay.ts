/**
 * Formatting for a model's raw wire id when it is shown to the user.
 *
 * Most backends use a plain, human-readable id ("claude-sonnet-4-6"), which is
 * shown as-is. Some ACP agents (DeepSeek Harness) instead identify a model by a
 * JSON-stringified pair of provider and model, e.g.
 * '["deepseek-official","deepseek-v4-pro"]'. That string is the exact value the
 * agent requires on session/set_config_option, so it must be sent back
 * verbatim — but rendering it raw in the UI reads as a JSON blob. Display it in
 * the same "provider/model" form the CLI-discovered backends already use.
 *
 * This is display-only: never feed the result back into an API call.
 */
export function formatModelIdForDisplay(id: string): string {
  if (!id || id[0] !== '[') return id
  try {
    const parsed = JSON.parse(id)
    if (!Array.isArray(parsed) || parsed.length === 0) return id
    if (!parsed.every((part) => typeof part === 'string' && part.length > 0)) return id
    return parsed.join('/')
  } catch {
    // Not valid JSON — treat it as an ordinary opaque id.
    return id
  }
}

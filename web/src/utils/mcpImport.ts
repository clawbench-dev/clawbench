/**
 * Parses MCP server definitions pasted from another tool's config.
 *
 * Every tool that manages MCP writes its own flavour of the same JSON, and the
 * differences are only in the envelope: Claude Desktop / Claude Code / Cursor
 * use `{"mcpServers": {...}}`, some write the bare `{name: {...}}` map, and a
 * single server is often copied on its own. Accepting all three costs one
 * shape check each and removes the "which format does this page want" question
 * from the migration path.
 *
 * Only the fields ClawBench stores are read (name, transport, command, args,
 * env, url, headers). Everything else (`autoApprove`, `disabled`, ...) is
 * dropped: forwarding unknown keys would let a config from another tool decide
 * behaviour this app never validated.
 */

export type McpImportTransport = 'stdio' | 'http' | 'sse'

/** Why a parsed entry cannot be imported as-is. */
export type McpImportIssue =
  | 'empty-name'
  | 'invalid-name'
  | 'bad-transport'
  | 'missing-command'
  | 'missing-url'
  | 'bad-url'

export interface McpImportServer {
  name: string
  transport: McpImportTransport
  command?: string
  args?: string[]
  env?: Record<string, string>
  url?: string
  headers?: Record<string, string>
}

export interface McpImportEntry {
  server: McpImportServer
  /** Set when the entry violates a rule the server would also reject. */
  issue?: McpImportIssue
}

export type McpImportParseResult =
  | {
      ok: true
      entries: McpImportEntry[]
      /** Entries present in the source but deliberately not imported. */
      skipped: number
    }
  | {
      ok: false
      error: 'invalid-json' | 'unsupported-shape'
    }

/** Same character set the server enforces (internal/model/mcp.go). */
const NAME_PATTERN = /^[A-Za-z0-9_-]+$/

/** Envelope keys seen in the wild. `servers` is VS Code's spelling. */
const WRAPPER_KEYS = ['mcpServers', 'mcp_servers', 'servers']

export function isValidMcpName(name: string): boolean {
  return NAME_PATTERN.test(name)
}

export function parseMcpImport(text: string): McpImportParseResult {
  const source = stripCodeFence(text).trim()
  if (!source) return { ok: false, error: 'invalid-json' }

  let raw: unknown
  try {
    raw = JSON.parse(source)
  } catch {
    return { ok: false, error: 'invalid-json' }
  }
  if (!isPlainObject(raw)) return { ok: false, error: 'unsupported-shape' }

  // A single server pasted on its own: it names itself.
  if (typeof raw.name === 'string' || typeof raw.command === 'string' || typeof raw.url === 'string') {
    return fromMap({ [typeof raw.name === 'string' ? raw.name : '']: raw })
  }

  for (const key of WRAPPER_KEYS) {
    const inner = raw[key]
    // A server may itself be called "servers"; then the value under the key is
    // a definition (command/url/type), not a map of definitions.
    if (isPlainObject(inner) && !looksLikeDefinition(inner)) return fromMap(inner)
  }

  // Bare map of name -> definition.
  if (Object.values(raw).some(isPlainObject)) return fromMap(raw)

  return { ok: false, error: 'unsupported-shape' }
}

function fromMap(map: Record<string, unknown>): McpImportParseResult {
  const entries: McpImportEntry[] = []
  let skipped = 0

  for (const [name, value] of Object.entries(map)) {
    if (!isPlainObject(value)) {
      // Not a definition (a stray scalar under the wrapper). Count it so the
      // user is not told "2 entries" and then finds 1.
      skipped++
      continue
    }
    // `disabled: true` is how Cursor / Claude Desktop park a server. Importing
    // it would silently switch on something the user had turned off.
    if (value.disabled === true) {
      skipped++
      continue
    }
    entries.push(buildEntry(name.trim(), value))
  }

  return { ok: true, entries, skipped }
}

function buildEntry(name: string, cfg: Record<string, unknown>): McpImportEntry {
  const transport = normalizeTransport(cfg)
  const server: McpImportServer = {
    name,
    transport: transport ?? 'stdio',
  }

  if (transport === 'stdio') {
    const command = asString(cfg.command)
    if (command) server.command = command
    const args = asArgs(cfg.args)
    if (args) server.args = args
    const env = asStringMap(cfg.env)
    if (env) server.env = env
  } else if (transport) {
    const url = asString(cfg.url)
    if (url) server.url = url
    const headers = asStringMap(cfg.headers)
    if (headers) server.headers = headers
  }

  return { server, issue: entryIssue(name, transport, server) }
}

function entryIssue(
  name: string,
  transport: McpImportTransport | null,
  server: McpImportServer,
): McpImportIssue | undefined {
  if (!name) return 'empty-name'
  if (!isValidMcpName(name)) return 'invalid-name'
  if (!transport) return 'bad-transport'
  if (transport === 'stdio' && !server.command) return 'missing-command'
  if (transport !== 'stdio') {
    if (!server.url) return 'missing-url'
    if (!/^https?:\/\//i.test(server.url)) return 'bad-url'
  }
  return undefined
}

/**
 * Resolves the transport. `type` is the Claude/Cursor spelling, `transport` is
 * used by some others; when neither is present the shape decides (a command
 * means stdio, a URL means remote).
 *
 * Returns null for an explicit but unsupported value — guessing there would
 * hand the server a definition it rejects, with a message pointing at an index
 * instead of the entry the user just pasted.
 */
function normalizeTransport(cfg: Record<string, unknown>): McpImportTransport | null {
  const raw = asString(cfg.type) ?? asString(cfg.transport)
  if (raw) {
    const value = raw.toLowerCase()
    if (value === 'stdio' || value === 'http' || value === 'sse') return value
    if (value === 'streamable-http' || value === 'streamablehttp' || value === 'streamable_http') {
      return 'http'
    }
    return null
  }
  if (asString(cfg.command)) return 'stdio'
  const url = asString(cfg.url)
  if (url) {
    // Some tools omit the type for remote servers. Guessing "http" for an SSE
    // endpoint would pass validation and then never connect, so read the
    // conventional /sse path suffix.
    return url.split(/[?#]/)[0].endsWith('/sse') ? 'sse' : 'http'
  }
  return 'stdio'
}

function asString(value: unknown): string | undefined {
  if (typeof value === 'string') {
    const trimmed = value.trim()
    return trimmed || undefined
  }
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return undefined
}

/**
 * `args` is an array in every spec, but hand-written configs often put the
 * whole command line in one string. Splitting on whitespace matches what the
 * add form does with its "space separated" field.
 */
function asArgs(value: unknown): string[] | undefined {
  if (typeof value === 'string') {
    const parts = value.trim().split(/\s+/).filter(Boolean)
    return parts.length > 0 ? parts : undefined
  }
  if (!Array.isArray(value)) return undefined
  const parts = value
    .filter((item) => typeof item === 'string' || typeof item === 'number')
    .map(String)
    .filter((item) => item !== '')
  return parts.length > 0 ? parts : undefined
}

/** env / headers values must be strings; a bare number or boolean is common. */
function asStringMap(value: unknown): Record<string, string> | undefined {
  if (!isPlainObject(value)) return undefined
  const out: Record<string, string> = {}
  for (const [key, item] of Object.entries(value)) {
    const text = asString(item)
    if (text !== undefined) out[key] = text
  }
  return Object.keys(out).length > 0 ? out : undefined
}

/** Docs and chat messages wrap JSON in ```json fences; pasting one is normal. */
function stripCodeFence(text: string): string {
  const trimmed = text.trim()
  if (!trimmed.startsWith('```')) return trimmed
  return trimmed.replace(/^```[A-Za-z]*\s*\n?/, '').replace(/\n?```$/, '')
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function looksLikeDefinition(value: Record<string, unknown>): boolean {
  return (
    typeof value.command === 'string' ||
    typeof value.url === 'string' ||
    typeof value.type === 'string' ||
    typeof value.transport === 'string' ||
    isPlainObject(value.env) ||
    isPlainObject(value.headers)
  )
}

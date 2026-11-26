#!/usr/bin/env node
/**
 * Decide whether this release has to rebuild the Android APK and the desktop
 * packages, or can reuse the previous release's artifacts.
 *
 * Why this exists
 * ---------------
 * The APK and the desktop shell are thin wrappers: they load the web UI from
 * the server, so a normal release changes neither. Rebuilding them every time
 * costs five jobs (Android + four desktop platforms, macOS building twice) for
 * byte-identical output.
 *
 * What makes reuse SAFE is the client protocol number
 * (`internal/version/client_protocol.txt`): both gates compare it instead of
 * comparing version strings, so a shell built for an older release keeps
 * working against a newer server as long as the contract did not change.
 *
 * What this script decides
 * ------------------------
 *   - `android_changed` / `desktop_changed`: did anything under `android/` or
 *     `desktop/` change since the previous release? Any change rebuilds, even a
 *     cosmetic one — a shell fix that never ships is worse than a slow release.
 *   - a protocol bump forces BOTH to rebuild (the number is compiled into each
 *     shell, so a bump means the old shells are incompatible by definition).
 *   - the FIRST release after the guard was introduced, and any release whose
 *     predecessor predates `client_protocol.txt`, forces both: the previous
 *     artifacts cannot understand the protocol, so shipping them would leave
 *     existing installs stuck in a download loop.
 *
 * It also enforces the protocol-bump discipline: if a file on the shell<->server
 * contract changed in a way a stale shell cannot survive, and the number did
 * not move, the release FAILS. That is deliberate — the alternative is shipping
 * an incompatible pair and finding out from users.
 *
 * The exception is the TypeScript bridge interface (and the desktop preload
 * object implementing it) and the `/api/health` response, which may GROW: an
 * old shell simply does not implement or ask for the new member. Removing a
 * member or making one mandatory still fails (see `bridgeBreakingChanges` and
 * `healthBreakingChanges`).
 *
 * Outputs (written to $GITHUB_OUTPUT when present, always printed):
 *   prev_tag, protocol,
 *   android_changed, desktop_changed,
 *   android_source, desktop_source   (tag whose artifact this release ships)
 */

import { execFileSync } from 'node:child_process'
import { appendFileSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

/** The single source of truth for the compatibility number. */
export const PROTOCOL_FILE = 'internal/version/client_protocol.txt'

/**
 * Files the shell depends on and the server owns. A change here without a
 * protocol bump is a breaking change that nobody declared.
 *
 * Deliberately SMALL: the web UI and the WebSocket event schema are served by
 * the server and are therefore always current — they are not part of the
 * shell's contract. What is listed here is what a stale shell cannot adapt to.
 */
export const CONTRACT_PATHS = [
  'web/src/utils/clawbenchNative.ts',
  'internal/handler/health.go',
  'desktop/src/preload',
  'android/app/src/main/java/com/clawbench/app/BrowserJavascriptInterface.java',
]

/**
 * The `/api/health` response. Like the bridge interface it may GROW: both
 * stale shells decode it field by field and read a missing field as "unknown",
 * so a new field they never ask for is compatible. Removals still fail (see
 * `healthBreakingChanges`), so it is no longer all-or-nothing.
 */
export const HEALTH_CONTRACT_PATH = 'internal/handler/health.go'

/** Paths whose change forces a rebuild of the respective client. */
export const ANDROID_PATHS = ['android']
export const DESKTOP_PATHS = ['desktop']

/**
 * Parse a protocol file's contents.
 *
 * @returns the positive integer, or null when the file is absent/unparseable.
 */
export function parseProtocol(raw) {
  if (typeof raw !== 'string') return null
  const trimmed = raw.trim()
  if (!/^[0-9]+$/.test(trimmed)) return null
  const n = Number(trimmed)
  return n > 0 ? n : null
}

/**
 * Pure decision: what has to be rebuilt for this release?
 *
 * @param {object} input
 * @param {boolean} input.prevHasProtocol  the previous release's client understood the protocol
 * @param {string[]} input.androidChanged  changed paths under android/ (empty = unchanged)
 * @param {string[]} input.desktopChanged  changed paths under desktop/
 * @param {boolean} input.protocolBumped   client_protocol.txt changed since the previous release
 * @param {string} input.tag               the release being cut
 * @param {string} input.prevTag           the previous release, or '' when this is the first
 */
export function decideClientBuild(input) {
  const first = !input.prevTag
  // A previous release that predates the protocol field cannot be reused: its
  // shell still gates on version equality, so it would download this release's
  // artifact, still see a version mismatch, and loop.
  const forced = first || !input.prevHasProtocol || input.protocolBumped

  const androidChanged = forced || input.androidChanged.length > 0
  const desktopChanged = forced || input.desktopChanged.length > 0

  return {
    androidChanged,
    desktopChanged,
    // The tag whose artifact this release ships: the current one when the
    // client is rebuilt, the previous one when it is reused.
    androidSource: androidChanged ? input.tag : input.prevTag,
    desktopSource: desktopChanged ? input.tag : input.prevTag,
    reason: first
      ? 'first release'
      : !input.prevHasProtocol
        ? 'previous release predates the client protocol'
        : input.protocolBumped
          ? 'client protocol bumped'
          : androidChanged || desktopChanged
            ? 'client sources changed'
            : 'clients unchanged',
  }
}

/**
 * The one contract file that can grow without breaking an existing shell: the
 * TypeScript bridge interface. Every member an old shell does not implement is
 * simply absent at runtime, so ADDING an optional method is compatible — the
 * page degrades. Everything else on `CONTRACT_PATHS` stays all-or-nothing.
 */
export const BRIDGE_CONTRACT_PATH = 'web/src/utils/clawbenchNative.ts'

/**
 * The desktop shell's own implementation of that same bridge. Its object has no
 * declared interface to compare, so the members are read off
 * `contextBridge.exposeInMainWorld('ClawBenchNative', { ... })` instead. Rule:
 * removing a member or making one mandatory fails, adding one does not — the
 * exact counterpart of `bridgeBreakingChanges` for the TypeScript declaration.
 */
export const DESKTOP_BRIDGE_IMPLEMENTATION_PATH = 'desktop/src/preload/index.ts'

/**
 * The other two preloads. Each is its own window's whole bridge
 * (`AnnotateHost` for the annotation overlay, `FloatingHost` for the capsule),
 * so it grows the same way: an old shell without the new verb simply returns
 * `undefined` and the page degrades. They used to be judged all-or-nothing
 * along with the rest of `desktop/src/preload`, which made an additive verb —
 * e.g. the overlay's 留存画面 / 录制 buttons — fail the release as "breaking".
 *
 * `index.ts` is never listed here: its bridge is `ClawBenchNative`, judged
 * against the page's interface, and that comparison is the stricter one.
 */
export const AUX_PRELOAD_BRIDGES = [
  { path: 'desktop/src/preload/annotate.ts', global: 'AnnotateHost' },
  { path: 'desktop/src/preload/floating.ts', global: 'FloatingHost' },
]

/**
 * Read the member list of a preload bridge object.
 *
 * The object is the literal handed to
 * `contextBridge.exposeInMainWorld(<global>, { ... })`. Members are found
 * line-based — key at the conventional two-space indent, arrow or method
 * shorthand — the same rule `bridgeMembers` uses for the interface. Brace
 * matching is deliberately NOT used: the bodies contain strings and comments
 * with stray braces (a truncated `'}` in a message is enough), and one mismatch
 * would silently hide every member after it.
 *
 * @returns Map<memberName, {optional: boolean}> — empty when the object cannot
 * be found (callers must treat that as "unknown", not "compatible").
 */
export function preloadBridgeMembers(source, global = 'ClawBenchNative') {
  const members = new Map()
  if (typeof source !== 'string') return members
  const marker = `exposeInMainWorld('${global}', {`
  const open = source.indexOf(marker)
  if (open === -1) return members
  // The literal closes with the first `})` at column 0. A member's own body
  // never sits at column 0, so this cannot cut the object short.
  const close = source.indexOf('\n})', open)
  if (close === -1) return members
  const body = source.slice(open, close)
  // `[ \t]{1,2}` rather than `\s{2}`: `\s` also matches newlines, so a line
  // starting with a single space (or a member at column 0) would sneak in.
  const member = /^[ \t]{1,2}(?:readonly )?([A-Za-z_$][\w$]*)(\??)\s*[:(]/gm
  for (const m of body.matchAll(member)) {
    members.set(m[1], { optional: m[2] === '?' })
  }
  return members
}

/**
 * @returns string[] human-readable reasons; empty means purely additive.
 */
export function preloadBridgeBreakingChanges(prevSource, nextSource, global = 'ClawBenchNative') {
  const before = preloadBridgeMembers(prevSource, global)
  const after = preloadBridgeMembers(nextSource, global)
  // An unreadable object is not evidence of compatibility. Fail closed.
  if (before.size === 0 || after.size === 0) {
    return ['the desktop preload bridge could not be read at one of the two releases']
  }

  const reasons = []
  for (const [name] of before) {
    if (!after.has(name)) reasons.push(`preload: ${name} was removed`)
  }
  // The preload object cannot express optionality (a missing key is just
  // absent), so "became mandatory" has no analogue here.
  return reasons
}

/**
 * Read the member list of `interface ClawBenchNative`.
 *
 * Line-based on purpose: the interface is declared at column 0, closes with a
 * `}` at column 0, and every member starts at exactly two spaces. Multi-line
 * signatures continue at four spaces or start with `)`, and comments start with
 * `/`, so neither can be mistaken for a member. A nested object type is always
 * indented, so its closing brace never ends the block.
 *
 * @returns Map<memberName, {optional: boolean}> — empty when the interface
 * cannot be found (callers must treat that as "unknown", not "compatible").
 */
export function bridgeMembers(source) {
  const members = new Map()
  if (typeof source !== 'string') return members
  const open = source.indexOf('interface ClawBenchNative {')
  if (open === -1) return members
  const end = source.indexOf('\n}', open)
  if (end === -1) return members
  const body = source.slice(open, end)
  const member = /^ {2}(?:readonly )?([A-Za-z_$][\w$]*)(\??)\s*[(:]/gm
  for (const m of body.matchAll(member)) {
    members.set(m[1], { optional: m[2] === '?' })
  }
  return members
}

/**
 * Why an edit to the bridge interface would break a shell built before it.
 *
 * Only the shape that a stale shell actually notices is checked: a member that
 * disappeared, or one that became mandatory (the page would call it on a host
 * that never had it). Return-type widening — `Promise<void>` →
 * `void | Promise<void>` — is compatible and deliberately not flagged; callers
 * `await` either way.
 *
 * @returns string[] human-readable reasons; empty means purely additive.
 */
export function bridgeBreakingChanges(prevSource, nextSource) {
  const before = bridgeMembers(prevSource)
  const after = bridgeMembers(nextSource)
  // An unreadable interface is not evidence of compatibility. Fail closed.
  if (before.size === 0 || after.size === 0) {
    return ['the bridge interface could not be read at one of the two releases']
  }

  const reasons = []
  for (const [name, info] of before) {
    const now = after.get(name)
    if (!now) reasons.push(`${name} was removed`)
    else if (info.optional && !now.optional) reasons.push(`${name} became mandatory`)
  }
  for (const [name, info] of after) {
    if (!before.has(name) && !info.optional) {
      reasons.push(`${name} is new but mandatory (mark it optional)`)
    }
  }
  return reasons
}

/**
 * `GET /api/health` is read by two stale shells — the desktop one (to wait for
 * the server it spawned) and the Android one (to gate on the protocol) — and
 * both decode it field by field. `healthResponse` is a struct whose fields are
 * tagged `json:"<camelCase>"`, so a new field leaves the wire untouched.
 *
 * A field that is only ever tested against a value the stale shell never asks
 * for is therefore additive, exactly like an optional bridge member: a missing
 * `pid` reads as "unknown", never as "wrong". A field removed, renamed, or one
 * that a stale shell has to understand (e.g. a new required handshake) still
 * breaks — the rule is "no member of the previous response disappeared".
 *
 * Line-based on purpose: the struct closes with a `}` at column 0 and every
 * field sits at one tab, so comments and nested braces cannot be mistaken for
 * fields. Go struct field names are not wire names, so compare the JSON tags.
 *
 * @returns Map<wireName, {optional: boolean}> — callers must treat an empty map
 * as "could not be read", not as "compatible".
 */
export function healthFields(source) {
  const fields = new Map()
  if (typeof source !== 'string') return fields
  const open = source.indexOf('type healthResponse struct {')
  if (open === -1) return fields
  const end = source.indexOf('\n}', open)
  if (end === -1) return fields
  const body = source.slice(open, end)
  const field = /^\t[A-Za-z_$][\w$]*\s+[^\n`]*`json:"([^"]+)"`/gm
  for (const m of body.matchAll(field)) {
    // `json:"pid,omitempty"` — only an omitted value is readable as "unknown".
    const [name, opts = ''] = m[1].split(',')
    fields.set(name, { optional: opts.split(',').includes('omitempty') })
  }
  return fields
}

/**
 * @returns string[] human-readable reasons; empty means purely additive.
 */
export function healthBreakingChanges(prevSource, nextSource) {
  const before = healthFields(prevSource)
  const after = healthFields(nextSource)
  // An unreadable struct is not evidence of compatibility. Fail closed.
  if (before.size === 0 || after.size === 0) {
    return ['the /api/health response could not be read at one of the two releases']
  }

  const reasons = []
  for (const [name] of before) {
    if (!after.has(name)) reasons.push(`health: ${name} was removed`)
  }
  // A stale shell only reads the fields it already knows, so a NEW field it
  // never asks for cannot break it — the same rule as bridgeBreakingChanges.
  return reasons
}

/**
 * Whether a contract change forces a protocol bump.
 *
 * `breakingContractPaths` is the subset of changed contract files that a shell
 * built for the previous protocol cannot survive. The caller decides membership:
 * the bridge interface is only breaking when `bridgeBreakingChanges` says so,
 * and `/api/health` only when `healthBreakingChanges` says so.
 *
 * @param {object} input
 * @param {string[]} input.breakingContractPaths
 * @param {number|null} input.prevProtocol
 * @param {number|null} input.protocol
 */
export function contractViolation(input) {
  if (input.breakingContractPaths.length === 0) return false
  // No previous number means the guard is being introduced by this very
  // release; the rebuild is forced instead (see decideClientBuild).
  if (input.prevProtocol === null) return false
  return input.prevProtocol === input.protocol
}

// ── CLI ────────────────────────────────────────────────────────────────

function git(args, { quiet = false } = {}) {
  // `quiet` suppresses git's own stderr: a missing file at the previous tag is
  // an expected answer (the guard is being introduced), not an error worth
  // printing above the decision.
  return execFileSync('git', args, {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', quiet ? 'ignore' : 'inherit'],
  }).trim()
}

/** The most recent release tag before `tag`, using the same ordering the
 * release-notes job uses so both agree on what "previous" means. */
export function previousTag(tags, tag) {
  const releases = tags.filter((t) => /^v[0-9]+\.[0-9]+\.[0-9]+$/.test(t) && t !== tag)
  return releases[0] ?? ''
}

function changedPaths(from, to, paths) {
  const out = git(['diff', '--name-only', `${from}..${to}`, '--', ...paths])
  return out ? out.split('\n').filter(Boolean) : []
}

function fileAt(ref, path) {
  try {
    return git(['show', `${ref}:${path}`], { quiet: true })
  } catch {
    return null
  }
}

/** The protocol number of the checkout being released (CI checks out the tag
 * cleanly, so the working tree IS the release). */
function currentProtocolNumber() {
  try {
    return parseProtocol(readFileSync(PROTOCOL_FILE, 'utf8'))
  } catch {
    return null
  }
}

function main() {
  const tag = process.env.GITHUB_REF_NAME || process.argv[2]
  if (!tag) {
    console.error('client-guard: no release tag (set GITHUB_REF_NAME or pass one)')
    process.exit(1)
  }

  // Only tags reachable from the release being cut: diffing against a tag on a
  // different branch produces a nonsense change set, and "previous release"
  // means the one this tag was built on top of.
  const tags = git(['tag', '--merged', tag, '--sort=-v:refname']).split('\n').filter(Boolean)
  const prevTag = previousTag(tags, tag)

  const currentProtocol = currentProtocolNumber()
  if (currentProtocol === null) {
    console.error(`client-guard: ${PROTOCOL_FILE} is missing or is not a positive integer`)
    process.exit(1)
  }

  const prevProtocolRaw = prevTag ? fileAt(prevTag, PROTOCOL_FILE) : null
  const prevProtocol = parseProtocol(prevProtocolRaw)

  const androidChanged = prevTag ? changedPaths(prevTag, tag, ANDROID_PATHS) : []
  const desktopChanged = prevTag ? changedPaths(prevTag, tag, DESKTOP_PATHS) : []
  const contractChanged = prevTag ? changedPaths(prevTag, tag, CONTRACT_PATHS) : []

  // Split the changed contract files into "a stale shell can still live with
  // this" and "it cannot". The three bridges — the page's interface, the
  // desktop shell's implementation of it, and the overlay/capsule preloads —
  // and the /api/health response only break on removals; the rest stay
  // all-or-nothing.
  const bridgeResult = (path, compare) =>
    contractChanged.includes(path) ? compare(fileAt(prevTag, path), fileAt(tag, path)) : []
  const bridgeReasons = bridgeResult(BRIDGE_CONTRACT_PATH, bridgeBreakingChanges)
  const preloadReasons = bridgeResult(
    DESKTOP_BRIDGE_IMPLEMENTATION_PATH,
    preloadBridgeBreakingChanges,
  )
  const healthReasons = bridgeResult(HEALTH_CONTRACT_PATH, healthBreakingChanges)
  const auxPreloadResults = AUX_PRELOAD_BRIDGES.map(({ path, global }) => ({
    path,
    // The reads are passed in so the bridge's global cannot be forgotten.
    reasons: contractChanged.includes(path)
      ? preloadBridgeBreakingChanges(
          fileAt(prevTag, path),
          fileAt(tag, path),
          global,
        )
      : [],
  }))
  const auxReasons = auxPreloadResults.flatMap((r) => r.reasons)
  // Registering a bridge means its changes are judged by the members rule — an
  // additive verb must NOT stay on the all-or-nothing path. Keying this off
  // "has reasons" instead would leave the additive case exactly as broken as
  // before there were reasons to report (v0.149.0 failed a second time on it).
  const auxPaths = new Set(
    AUX_PRELOAD_BRIDGES.map((b) => b.path).filter((p) => contractChanged.includes(p)),
  )
  const breakingContractPaths = contractChanged.filter(
    (p) =>
      (p !== BRIDGE_CONTRACT_PATH || bridgeReasons.length > 0) &&
      (p !== DESKTOP_BRIDGE_IMPLEMENTATION_PATH || preloadReasons.length > 0) &&
      (p !== HEALTH_CONTRACT_PATH || healthReasons.length > 0) &&
      !auxPaths.has(p),
  )

  const protocolBumped = prevProtocol !== null && prevProtocol !== currentProtocol

  const decision = decideClientBuild({
    prevTag,
    prevHasProtocol: prevProtocol !== null,
    androidChanged,
    desktopChanged,
    protocolBumped,
    tag,
  })

  if (contractViolation({ breakingContractPaths, prevProtocol, protocol: currentProtocol })) {
    console.error(
      [
        '::error::The shell<->server contract changed without a protocol bump.',
        '',
        'Breaking contract files:',
        ...breakingContractPaths.map((p) => `  - ${p}`),
        ...(bridgeReasons.length > 0 ? bridgeReasons.map((r) => `    · ${r}`) : []),
        ...(preloadReasons.length > 0 ? preloadReasons.map((r) => `    · ${r}`) : []),
        ...(auxReasons.length > 0 ? auxReasons.map((r) => `    · ${r}`) : []),
        ...(healthReasons.length > 0 ? healthReasons.map((r) => `    · ${r}`) : []),
        '',
        `The previous release was protocol ${prevProtocol}, this one is ${currentProtocol}.`,
        `Bump ${PROTOCOL_FILE} (to ${currentProtocol + 1}) if the change breaks an existing`,
        'shell, or revert the contract change. Shipping both sides unchanged would leave',
        'old clients talking to a server that no longer understands them.',
      ].join('\n'),
    )
    process.exit(1)
  }

  const outputs = {
    prev_tag: prevTag,
    protocol: String(currentProtocol),
    android_changed: String(decision.androidChanged),
    desktop_changed: String(decision.desktopChanged),
    android_source: decision.androidSource,
    desktop_source: decision.desktopSource,
  }

  if (process.env.GITHUB_OUTPUT) {
    const lines = Object.entries(outputs)
      .map(([k, v]) => `${k}=${v}`)
      .join('\n')
    appendFileSync(process.env.GITHUB_OUTPUT, lines + '\n')
  }

  console.log(`client-guard: ${tag} (previous ${prevTag || 'none'})`)
  console.log(`  reason:          ${decision.reason}`)
  console.log(`  protocol:        ${currentProtocol}${protocolBumped ? ` (bumped from ${prevProtocol})` : ''}`)
  console.log(`  android:         ${decision.androidChanged ? 'build' : `reuse ${decision.androidSource}`}`)
  if (contractChanged.length > 0) {
    const verdict = breakingContractPaths.length > 0 ? 'breaking' : 'additive only'
    console.log(`  contract:        ${verdict} (${contractChanged.join(', ')})`)
  }
  console.log(`  desktop:         ${decision.desktopChanged ? 'build' : `reuse ${decision.desktopSource}`}`)
}

// Only run when executed directly, so the exported helpers stay testable.
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main()
}

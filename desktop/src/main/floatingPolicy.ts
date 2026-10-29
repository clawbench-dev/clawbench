/**
 * Pure decisions for the floating status capsule — no Electron imports, so this
 * is unit-testable with plain vitest (same split as `splashPolicy.ts`).
 *
 * The three counters are derived from `/api/ai/sessions/overview` exactly the
 * way the Android capsule derives them in `FloatingStatusView.countRunning /
 * countPending / countUnread`: pending wins over running, and unread only counts
 * sessions that are neither running nor pending, which keeps the three groups
 * mutually exclusive. Keeping the rules identical is the point — a desktop
 * capsule that disagrees with the phone's is worse than no capsule.
 */

/** The three mutually-exclusive capsule counters. */
export interface FloatingStats {
  running: number
  pending: number
  unread: number
}

/** Palette pushed by the web UI (see the `setTheme` bridge extension). */
export interface FloatingColors {
  bg?: string
  text?: string
  textSecondary?: string
  accent?: string
}

/** A resolved palette: every slot filled, so the capsule never renders undefined. */
export type ResolvedColors = Required<FloatingColors>


/** Capsule design size; the page reports its natural size and we resize to it. */
export const DEFAULT_WIDTH = 200
export const DEFAULT_HEIGHT = 56

/** Gap from the work-area corner for the first-run position (top-right). */
export const EDGE_MARGIN = 16

/** Press/release under this much cursor travel is a click, not a drag. */
export const CLICK_SLOP_PX = 4

/** Counters are display-only; a wild value is a bug, not something to render. */
export const MAX_COUNT = 9999

/**
 * Dark neutral palette, used until the web UI reports the live theme.
 *
 * The status colors (green / amber / blue) are deliberately NOT themeable: the
 * Android capsule keeps them as fixed semantic colors so a session that is
 * running reads the same on every theme. See `docs/plans/
 * 2026-08-28-android-floating-panel.md` ("状态色保持固定语义色").
 */
export const FALLBACK_COLORS: ResolvedColors = {
  bg: 'rgba(24, 26, 31, 0.92)',
  text: '#f2f4f8',
  textSecondary: '#9aa3b2',
  accent: '#4c8dff',
}

/**
 * The only shape accepted from the renderer for a CSS slot.
 *
 * These strings are interpolated into the capsule's stylesheet, so an unchecked
 * value would be a CSS-injection path from a remote page into a native window.
 */
const COLOR_RE = /^#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/i

/** Endpoint the capsule polls, relative to the connected server. */
export const OVERVIEW_PATH = '/api/ai/sessions/overview'

/**
 * Build the overview URL for a connected server, or null when there is none.
 *
 * `serverUrl` is what the main window navigated to, so it may carry a path
 * prefix (a server behind a reverse proxy). The API is rooted at the origin
 * PLUS that prefix, so the whole path is kept and only a trailing slash is
 * dropped — resolving against the origin alone would 404 on a prefixed install.
 *
 * The prefix is the FULL pathname, not its parent directory: `new URL()`'s
 * `pathname` on a navigation URL is a directory, not a filename, so stripping a
 * trailing segment (the usual "dirname" move) would turn `/a/b` into `/a` and
 * 404. Today `buildServerUrl` only ever produces `scheme://host:port`, but a
 * reverse-proxied install is exactly the case this exists for.
 */
export function overviewUrl(serverUrl: string | undefined | null): string | null {
  if (!serverUrl || !/^https?:\/\//i.test(serverUrl)) return null
  let url: URL
  try {
    url = new URL(serverUrl)
  } catch {
    return null
  }
  const base = url.pathname.replace(/\/+$/, '')
  return `${url.protocol}//${url.host}${base}${OVERVIEW_PATH}`
}

/** Every session object across every project group; null-safe on any payload. */
export function sessionsOf(overview: unknown): Record<string, unknown>[] {
  if (!overview || typeof overview !== 'object') return []
  const projects = (overview as { projects?: unknown }).projects
  if (!Array.isArray(projects)) return []
  const out: Record<string, unknown>[] = []
  for (const project of projects) {
    if (!project || typeof project !== 'object') continue
    const sessions = (project as { sessions?: unknown }).sessions
    if (!Array.isArray(sessions)) continue
    for (const session of sessions) {
      if (session && typeof session === 'object') out.push(session as Record<string, unknown>)
    }
  }
  return out
}

function flag(session: Record<string, unknown>, key: string): boolean {
  return session[key] === true
}

function count(session: Record<string, unknown>, key: string): number {
  const raw = Number(session[key])
  return Number.isFinite(raw) && raw > 0 ? raw : 0
}

/** Running and NOT awaiting approval — pending wins, so the groups stay disjoint. */
export function countRunning(overview: unknown): number {
  return sessionsOf(overview).filter((s) => flag(s, 'running') && !flag(s, 'pendingApproval')).length
}

/** Awaiting approval (permission_pending). */
export function countPending(overview: unknown): number {
  return sessionsOf(overview).filter((s) => flag(s, 'pendingApproval')).length
}

/** Finished sessions with unread messages: neither running nor pending. */
export function countUnread(overview: unknown): number {
  return sessionsOf(overview).filter(
    (s) => !flag(s, 'running') && !flag(s, 'pendingApproval') && count(s, 'unreadCount') > 0,
  ).length
}

/** All three counters from one overview payload. */
export function computeStats(overview: unknown): FloatingStats {
  return { running: countRunning(overview), pending: countPending(overview), unread: countUnread(overview) }
}

/** True when there is nothing the capsule could report — drives the idle render. */
export function isIdle(stats: FloatingStats): boolean {
  return stats.running === 0 && stats.pending === 0 && stats.unread === 0
}

/**
 * Tray tooltip: the same three counters, without the dots.
 *
 * Windows truncates tooltips past ~127 chars, so the numbers are all this needs
 * to carry; the capsule itself is the rich surface.
 */
export function trayTooltip(stats: FloatingStats, words: { running: string; pending: string; unread: string; idle: string }): string {
  const parts: string[] = []
  if (stats.running > 0) parts.push(`${words.running} ${stats.running}`)
  if (stats.pending > 0) parts.push(`${words.pending} ${stats.pending}`)
  if (stats.unread > 0) parts.push(`${words.unread} ${stats.unread}`)
  return parts.length > 0 ? parts.join(' · ') : words.idle
}

/** Whether two stat snapshots differ; equal snapshots must not repaint. */
export function statsChanged(a: FloatingStats, b: FloatingStats): boolean {
  return a.running !== b.running || a.pending !== b.pending || a.unread !== b.unread
}

function clampCount(value: unknown): number {
  const n = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(n) || n < 0) return 0
  return Math.min(Math.floor(n), MAX_COUNT)
}

/** Coerce an untrusted payload into displayable counters. */
export function sanitizeStats(raw: unknown): FloatingStats {
  const src = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>
  return {
    running: clampCount(src.running),
    pending: clampCount(src.pending),
    unread: clampCount(src.unread),
  }
}

function pickColor(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return COLOR_RE.test(trimmed) ? trimmed : undefined
}

/**
 * Validate the palette the web UI sends. Returns null when nothing usable came
 * through, so the caller can distinguish "no update" from "update to X".
 */
export function sanitizeColors(raw: unknown): FloatingColors | null {
  const src = (raw && typeof raw === 'object' ? raw : null) as Record<string, unknown> | null
  if (!src) return null
  const out: FloatingColors = {}
  const bg = pickColor(src.bg)
  const text = pickColor(src.text)
  const textSecondary = pickColor(src.textSecondary)
  const accent = pickColor(src.accent)
  if (bg) out.bg = bg
  if (text) out.text = text
  if (textSecondary) out.textSecondary = textSecondary
  if (accent) out.accent = accent
  return Object.keys(out).length > 0 ? out : null
}

/** Stored palette merged over the fallback, so no slot is ever undefined. */
export function resolveColors(stored: FloatingColors | null | undefined): ResolvedColors {
  return { ...FALLBACK_COLORS, ...(sanitizeColors(stored) ?? {}) }
}

/** Whether two palettes differ (equal palettes must not repaint). */
export function colorsChanged(a: ResolvedColors, b: ResolvedColors): boolean {
  return a.bg !== b.bg || a.text !== b.text || a.textSecondary !== b.textSecondary || a.accent !== b.accent
}

/**
 * Geometry helpers live in `geometry.ts` (the main window restores its size with
 * the same rules). They are re-exported here so the capsule's own surface — and
 * its tests — stay where they read best.
 */
export { clampToWorkArea, clampPositionToUnion } from './geometry'
export type { Point, Rect, Size, WorkArea } from './geometry'
import { cornerBounds } from './geometry'
import type { Point, Rect, Size, WorkArea } from './geometry'

/** First-run position: top-right of the given work area. */
export function defaultBounds(area: WorkArea, size: Size = { width: DEFAULT_WIDTH, height: DEFAULT_HEIGHT }): Rect {
  return cornerBounds(area, size, EDGE_MARGIN)
}

/** Cursor travel between press and release, in DIPs. */
export function travel(start: Point, end: Point): number {
  return Math.hypot(end.x - start.x, end.y - start.y)
}

/** A gesture that never moved the cursor is a click: open the app. */
export function isClickGesture(start: Point, end: Point, slop = CLICK_SLOP_PX): boolean {
  return travel(start, end) <= slop
}

/**
 * Whether a press whose release was lost should be resolved.
 *
 * The capsule is small, so the pointer leaves it almost immediately and the
 * renderer can miss the release. A motionless cursor for this long means the
 * release is gone rather than that the user is holding a drag still.
 */
export function releaseLooksLost(now: number, lastCursorMove: number, stillMs: number): boolean {
  return now - lastCursorMove >= stillMs
}

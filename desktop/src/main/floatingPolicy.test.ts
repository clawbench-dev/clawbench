import { describe, it, expect } from 'vitest'
import {
  CLICK_SLOP_PX,
  DEFAULT_HEIGHT,
  DEFAULT_WIDTH,
  EDGE_MARGIN,
  FALLBACK_COLORS,
  MAX_COUNT,
  clampPositionToUnion,
  clampToWorkArea,
  colorsChanged,
  computeStats,
  countPending,
  countRunning,
  countUnread,
  defaultBounds,
  isClickGesture,
  isIdle,
  overviewUrl,
  releaseLooksLost,
  resolveColors,
  sanitizeColors,
  sanitizeStats,
  sessionsOf,
  statsChanged,
  trayTooltip,
} from './floatingPolicy'

/** A one-project overview payload, shaped like the server's response. */
function overview(...sessions: unknown[]): unknown {
  return { projects: [{ name: '/srv/a', sessions }] }
}

describe('countRunning / countPending / countUnread', () => {
  it('keeps the three groups mutually exclusive when a session is both', () => {
    // The server reports `running: true` AND `pendingApproval: true` for a
    // session blocked on a permission prompt. Pending wins (yellow > green), so
    // that session must appear in the pending count ONLY — otherwise the capsule
    // would show the same session twice and the totals would not add up.
    const payload = overview({ id: 's1', running: true, pendingApproval: true })
    expect(countRunning(payload)).toBe(0)
    expect(countPending(payload)).toBe(1)
    expect(countUnread(payload)).toBe(0)
  })

  it('excludes running and pending sessions from the unread group', () => {
    // Unread means "finished and nobody has read it yet". Counting a live
    // session here would put a blue dot on something already shown green.
    const payload = overview(
      { id: 'a', running: true },
      { id: 'b', pendingApproval: true },
      { id: 'c', unreadCount: 2 },
      { id: 'd', unreadCount: 0 },
    )
    expect(countRunning(payload)).toBe(1)
    expect(countPending(payload)).toBe(1)
    expect(countUnread(payload)).toBe(1)
    expect(computeStats(payload)).toEqual({ running: 1, pending: 1, unread: 1 })
  })

  it('aggregates across every project group', () => {
    // The endpoint is cross-project by design; summing only the first group
    // would under-report on any multi-project install.
    const payload = {
      projects: [
        { name: '/srv/a', sessions: [{ id: '1', running: true }] },
        { name: '/srv/b', sessions: [{ id: '2', running: true }, { id: '3', unreadCount: 1 }] },
      ],
      total: 3,
    }
    expect(computeStats(payload)).toEqual({ running: 2, pending: 0, unread: 1 })
  })

  it('treats a malformed payload as no sessions rather than throwing', () => {
    // The capsule polls on a timer: one odd response must not take the shell
    // down, and must not blank the last good numbers either (the caller keeps
    // them when the counters come back all-zero and unchanged).
    for (const bad of [null, undefined, 0, 'x', {}, { projects: 'nope' }, { projects: [null, {}] }]) {
      expect(computeStats(bad)).toEqual({ running: 0, pending: 0, unread: 0 })
    }
    expect(sessionsOf({ projects: [] })).toEqual([])
  })

  it('requires a real boolean for the flags but coerces the unread count', () => {
    // `running: 1` and `running: 'false'` must NOT read as running — Android's
    // optBoolean leaves those at the fallback too, and loose truthiness would
    // count the string "false" as a live session. `unreadCount` is the mirror
    // case: org.json's optInt parses a numeric string, so "3" counts as unread.
    const payload = overview(
      { id: 'a', running: 'false' },
      { id: 'b', running: 1 },
      { id: 'c', unreadCount: '3' },
      { id: 'd', unreadCount: -5 },
    )
    expect(countRunning(payload)).toBe(0)
    expect(countUnread(payload)).toBe(1)
  })
})

describe('isIdle', () => {
  it('is idle only when all three counters are zero', () => {
    expect(isIdle({ running: 0, pending: 0, unread: 0 })).toBe(true)
    expect(isIdle({ running: 0, pending: 1, unread: 0 })).toBe(false)
  })
})

describe('statsChanged', () => {
  it('compares by value so an unchanged poll cannot repaint the capsule', () => {
    expect(statsChanged({ running: 1, pending: 0, unread: 2 }, { running: 1, pending: 0, unread: 2 })).toBe(false)
    expect(statsChanged({ running: 1, pending: 0, unread: 2 }, { running: 1, pending: 0, unread: 3 })).toBe(true)
  })
})

describe('sanitizeStats', () => {
  it('clamps to whole counts within the display ceiling', () => {
    expect(sanitizeStats({ running: -3, pending: 2.7, unread: '9' })).toEqual({
      running: 0,
      pending: 2,
      unread: 9,
    })
    expect(sanitizeStats({ running: 1e9 }).running).toBe(MAX_COUNT)
    expect(sanitizeStats({ running: NaN }).running).toBe(0)
  })

  it('accepts a non-object without throwing', () => {
    // This is the renderer-supplied path; a page can send anything.
    expect(sanitizeStats('nope')).toEqual({ running: 0, pending: 0, unread: 0 })
    expect(sanitizeStats(null)).toEqual({ running: 0, pending: 0, unread: 0 })
  })
})

describe('overviewUrl', () => {
  it('builds the endpoint on the connected origin, port included', () => {
    expect(overviewUrl('http://100.124.54.58:20000')).toBe('http://100.124.54.58:20000/api/ai/sessions/overview')
    expect(overviewUrl('https://bench.example.com/')).toBe('https://bench.example.com/api/ai/sessions/overview')
  })

  it('keeps a reverse-proxy path prefix so a prefixed install still resolves', () => {
    // The main window navigates to the same prefixed URL, so the API lives under
    // it. Dropping the prefix would 404 on exactly those installs.
    expect(overviewUrl('https://example.com/clawbench/')).toBe('https://example.com/clawbench/api/ai/sessions/overview')
  })

  it('keeps a multi-segment prefix whether or not it has a trailing slash', () => {
    // `pathname` on a navigation URL is a directory, not a filename, so the
    // "dirname" move (strip the last segment) is wrong here: it turned
    // `/a/b` into `/a` and 404'd. Both spellings of the same prefix must agree.
    expect(overviewUrl('https://example.com/a/b')).toBe('https://example.com/a/b/api/ai/sessions/overview')
    expect(overviewUrl('https://example.com/a/b/')).toBe('https://example.com/a/b/api/ai/sessions/overview')
  })

  it('drops repeated trailing slashes rather than emitting a double slash', () => {
    expect(overviewUrl('https://example.com/clawbench//')).toBe('https://example.com/clawbench/api/ai/sessions/overview')
  })

  it('returns null for anything that is not a remote server', () => {
    // The first-run login page is a local file: polling it would be a pointless
    // request whose failure the user could not explain.
    expect(overviewUrl('')).toBeNull()
    expect(overviewUrl(undefined)).toBeNull()
    expect(overviewUrl('file:///tmp/login.html')).toBeNull()
    expect(overviewUrl('not a url')).toBeNull()
  })
})

describe('sanitizeColors / resolveColors', () => {
  it('accepts hex literals only', () => {
    expect(sanitizeColors({ bg: '#161b22', text: '#C9D1D9' })).toEqual({ bg: '#161b22', text: '#C9D1D9' })
    expect(sanitizeColors({ accent: '#abc' })).toEqual({ accent: '#abc' })
  })

  it('rejects values that would break out of the CSS slot', () => {
    // These strings are interpolated into the capsule's stylesheet, so an
    // unchecked value is a CSS-injection path from a remote page into a native
    // window — not just a cosmetic bug.
    for (const bad of ['red; } body { display:none', 'url(http://evil/x)', 'javascript:alert(1)', '  ', 42]) {
      expect(sanitizeColors({ bg: bad })).toBeNull()
    }
  })

  it('drops the unusable slots but keeps the good ones', () => {
    expect(sanitizeColors({ bg: '#111111', text: 'x' })).toEqual({ bg: '#111111' })
  })

  it('fills every slot from the fallback so the capsule never renders undefined', () => {
    const resolved = resolveColors({ bg: '#000000' })
    expect(resolved.bg).toBe('#000000')
    expect(resolved.text).toBe(FALLBACK_COLORS.text)
    expect(resolveColors(null)).toEqual(FALLBACK_COLORS)
  })

  it('reports a palette change only when one of the four slots moved', () => {
    const a = resolveColors(null)
    expect(colorsChanged(a, resolveColors(null))).toBe(false)
    expect(colorsChanged(a, resolveColors({ accent: '#ffffff' }))).toBe(true)
  })
})

describe('defaultBounds / clampToWorkArea', () => {
  const area = { x: 0, y: 0, width: 1920, height: 1040 }

  it('places the capsule top-right with the edge margin', () => {
    // Constants, not literals: this assertion is about the placement rule, and a
    // literal here silently locks the design size (it caught exactly that when
    // the capsule was shrunk).
    expect(defaultBounds(area)).toEqual({
      x: 1920 - DEFAULT_WIDTH - EDGE_MARGIN,
      y: EDGE_MARGIN,
      width: DEFAULT_WIDTH,
      height: DEFAULT_HEIGHT,
    })
  })

  it('pulls an off-screen rect back inside (a unplugged monitor case)', () => {
    const lost = { x: 3000, y: 4000, width: DEFAULT_WIDTH, height: 84 }
    const clamped = clampToWorkArea(lost, area)
    expect(clamped.x).toBeLessThanOrEqual(area.width - DEFAULT_WIDTH)
    expect(clamped.y).toBeLessThanOrEqual(area.height - 84)
    expect(clamped.x).toBeGreaterThanOrEqual(0)
    expect(clamped.y).toBeGreaterThanOrEqual(0)
  })

  it('never pushes the rect past the top-left when it is larger than the area', () => {
    // A tiny work area must still produce a non-negative origin; the Math.max
    // guard is what stops the clamp from flipping to negative coordinates.
    const clamped = clampToWorkArea({ x: -500, y: -500, width: 268, height: 84 }, { x: 0, y: 0, width: 300, height: 200 })
    expect(clamped.x).toBe(0)
    expect(clamped.y).toBe(0)
  })
})

describe('clampPositionToUnion', () => {
  const left = { x: 0, y: 0, width: 1920, height: 1040 }
  const right = { x: 1920, y: 0, width: 1280, height: 1024 }

  it('lets a drag cross onto the second display', () => {
    const p = clampPositionToUnion(2400, 100, { width: 268, height: 84 }, [left, right])
    expect(p.x).toBe(2400)
  })

  it('bounds the drag by the union, not by the display it started on', () => {
    const far = clampPositionToUnion(99999, 99999, { width: 268, height: 84 }, [left, right])
    expect(far.x).toBe(1920 + 1280 - 268)
    expect(far.y).toBe(1040 - 84)
  })

  it('passes the point through unchanged when there are no displays to clamp to', () => {
    expect(clampPositionToUnion(10, 20, { width: 268, height: 84 }, [])).toEqual({ x: 10, y: 20 })
  })
})

describe('isClickGesture', () => {
  it('treats sub-threshold travel as a click', () => {
    expect(isClickGesture({ x: 0, y: 0 }, { x: 2, y: 1 })).toBe(true)
  })

  it('treats anything past the slop as a drag', () => {
    // The slop is the whole distinction between "open the app" and "move the
    // capsule"; without it a drag would also fire the click action on release.
    expect(isClickGesture({ x: 0, y: 0 }, { x: CLICK_SLOP_PX + 1, y: 0 })).toBe(false)
  })
})

describe('releaseLooksLost', () => {
  it('resolves a press whose release never arrived', () => {
    expect(releaseLooksLost(10_000, 8_000, 2_000)).toBe(true)
    expect(releaseLooksLost(10_000, 9_500, 2_000)).toBe(false)
  })
})

describe('trayTooltip', () => {
  const words = { running: '执行中', pending: '待审批', unread: '未读', idle: '空闲' }

  it('lists only the non-zero counters', () => {
    // The tray is the fallback surface when the capsule is hidden: it has to say
    // the same thing, in one line, without three zeros of noise.
    expect(trayTooltip({ running: 2, pending: 0, unread: 0 }, words)).toBe('执行中 2')
    expect(trayTooltip({ running: 2, pending: 1, unread: 3 }, words)).toBe('执行中 2 · 待审批 1 · 未读 3')
  })

  it('falls back to the idle word when nothing is active', () => {
    expect(trayTooltip({ running: 0, pending: 0, unread: 0 }, words)).toBe('空闲')
  })

  it('stays well inside the Windows tooltip limit for absurd counts', () => {
    const t = trayTooltip({ running: 9999, pending: 9999, unread: 9999 }, words)
    expect(t.length).toBeLessThan(60)
  })
})

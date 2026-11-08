import { test, expect } from '../fixtures'

/**
 * The share page's "fit to screen" SWITCH.
 *
 * The page reuses the app's components, and every one of them is written in
 * `--dp` (= `1rem / 15`), so they scale with the ROOT FONT SIZE. The reader
 * decides whether that happens, via one icon in the topbar:
 *
 *   off (default) → the design size, exactly like any other web page
 *   on            → fitted to the reader's screen (the app's "auto fit" maths)
 *
 * Asserted in a REAL browser because the whole mechanism is layout: jsdom
 * applies no stylesheets, so a unit test can only check that `--ui-scale` was
 * written, not that the page actually grew and shrank. A `screen` override
 * makes the derivation deterministic — Playwright lets a context report a
 * screen size independently of the window, which is exactly the situation the
 * fit measures.
 */

/** Create a shared conversation and return its path. */
async function shareConversation(page: import('@playwright/test').Page): Promise<string> {
  const shared = await page.evaluate(async () => {
    const created = await fetch('/api/ai/sessions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agentId: 'acp-mock' }),
    })
    if (!created.ok) throw new Error(`create session: ${created.status}`)
    const createdBody = await created.text()
    const sessionId = (JSON.parse(createdBody) as { sessionId?: string }).sessionId
    // A missing id must fail HERE with the real payload: the share call below
    // would otherwise answer a bare 400 ("SessionIdRequired") and hide the
    // actual problem behind a status code.
    if (!sessionId) throw new Error(`create session returned no id: ${createdBody}`)

    const sent = await fetch('/api/ai/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId, message: 'scale_probe_' + Date.now() }),
    })
    if (!sent.ok) throw new Error(`send: ${sent.status}`)

    // The reply (and the persistence of the exchange) lands asynchronously:
    // asking for the share immediately sometimes answers 400
    // ShareSessionNoContent even though the send succeeded. Poll instead of
    // sleeping — the share endpoint IS the readiness signal, so a fixed delay
    // would still be a guess.
    const deadline = Date.now() + 15000
    let lastError = ''
    while (Date.now() < deadline) {
      const share = await fetch('/api/share/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId }),
      })
      if (share.ok) return (await share.json()) as { token: string; path: string }
      lastError = `${share.status} ${await share.text()}`
      if (!/ShareSessionNoContent/.test(lastError)) break // a real error, not a race
      await new Promise((r) => setTimeout(r, 200))
    }
    throw new Error(`share failed: ${lastError} (sessionId=${sessionId})`)
  })
  return shared.path
}

/** The root font size the share page actually rendered at, in px. */
async function rootFontPx(page: import('@playwright/test').Page): Promise<number> {
  return await page.evaluate(() =>
    parseFloat(getComputedStyle(document.documentElement).fontSize),
  )
}

/** A 1440p panel: the fit is 1440/1080 ≈ 1.33, so 15px → 19.95px. */
const HI_DPI_SCREEN = { width: 2560, height: 1440 }
/** The design reference: the fit is exactly 1, so nothing may change. */
const REFERENCE_SCREEN = { width: 1920, height: 1080 }

/**
 * `newContext({ screen })` is a CHROMIUM-ONLY option.
 *
 * Playwright's Firefox and WebKit ignore it and report `screen` as the viewport
 * (measured: a 1200x800 viewport with `screen: 2560x1440` reports 1200x800), so
 * the fit factor computes to 1 and the switch — correctly — does not render.
 * "A large panel gets offered the switch" therefore cannot be staged on those
 * engines at all; asserting it there pins a harness limitation, not the product.
 */
function screenOverrideSupported(browserName: string): boolean {
  return browserName === 'chromium'
}

test.describe('Share page fit-to-screen switch', () => {
  test('opens at the design size until the reader asks', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    // The default matters: a share link is opened by strangers, and silently
    // enlarging their page would be a surprise.
    const path = await shareConversation(page)
    const ctx = await page.context().browser()!.newContext({
      viewport: { width: 1200, height: 800 },
      screen: HI_DPI_SCREEN,
    })
    try {
      const p = await ctx.newPage()
      await p.goto(path)
      await expect(p.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      expect(await rootFontPx(p)).toBeCloseTo(15, 1)
      const btn = p.locator('[data-action="share-fit"]')
      await expect(btn).toBeVisible()
      await expect(btn).toHaveAttribute('aria-pressed', 'false')
    } finally {
      await ctx.close()
    }
  })

  test('one click enlarges the page, a second click restores it', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    const path = await shareConversation(page)
    const ctx = await page.context().browser()!.newContext({
      viewport: { width: 1200, height: 800 },
      screen: HI_DPI_SCREEN,
    })
    try {
      const p = await ctx.newPage()
      await p.goto(path)
      await expect(p.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      const btn = p.locator('[data-action="share-fit"]')
      await btn.click()

      // 15 × 1.33 = 19.95. A RANGE, not the exact factor: this test is about
      // "the page really grew" (the factor is pinned in shareAutoScale.test.ts),
      // so a rounding tweak must not fail it.
      await expect.poll(() => rootFontPx(p)).toBeGreaterThan(18)
      expect(await rootFontPx(p)).toBeLessThan(21)
      await expect(btn).toHaveAttribute('aria-pressed', 'true')

      // The real proof it is a switch and not a one-way enlargement.
      await btn.click()
      await expect.poll(() => rootFontPx(p)).toBeCloseTo(15, 1)
      await expect(btn).toHaveAttribute('aria-pressed', 'false')
    } finally {
      await ctx.close()
    }
  })

  test('the choice survives a reload', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    // A reader who fitted the page expects it to stay fitted when they come
    // back — the switch is a preference, not a one-off action.
    const path = await shareConversation(page)
    const ctx = await page.context().browser()!.newContext({
      viewport: { width: 1200, height: 800 },
      screen: HI_DPI_SCREEN,
    })
    try {
      const p = await ctx.newPage()
      await p.goto(path)
      await expect(p.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })
      await p.locator('[data-action="share-fit"]').click()
      await expect.poll(() => rootFontPx(p)).toBeGreaterThan(18)

      await p.reload()
      await expect(p.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      // Applied before mount, so there is no frame at 15px then a jump — the
      // value is already the fitted one on the first measurement.
      expect(await rootFontPx(p)).toBeGreaterThan(18)
      await expect(p.locator('[data-action="share-fit"]')).toHaveAttribute('aria-pressed', 'true')
    } finally {
      await ctx.close()
    }
  })

  test('is not offered on a screen that already fits', async ({ page }) => {
    // A 1080p panel maps to exactly 100%, so the switch could not change
    // anything. A button that visibly does nothing reads as broken, and in a
    // phone topbar it would also eat the last of the file name's room — so it
    // is absent rather than disabled.
    const path = await shareConversation(page)
    const ctx = await page.context().browser()!.newContext({
      viewport: { width: 1200, height: 800 },
      screen: REFERENCE_SCREEN,
    })
    try {
      const p = await ctx.newPage()
      await p.goto(path)
      await expect(p.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      expect(await rootFontPx(p)).toBeCloseTo(15, 1)
      await expect(p.locator('[data-action="share-fit"]')).toHaveCount(0)
    } finally {
      await ctx.close()
    }
  })

  test('a page loaded with the switch already on can be turned back off', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    // The other direction of the same state: the document loads while fitted
    // (so the outline starts as a closed drawer), then the reader turns fitting
    // off. The rail has to come back — it used to stay hidden because the
    // panel's open/closed state was only decided when the document loaded.
    const shared = await page.evaluate(async () => {
      const resp = await fetch('/api/share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: 'README.md' }),
      })
      if (!resp.ok) throw new Error(`share: ${resp.status}`)
      return (await resp.json()) as { path: string; token: string }
    })

    const ctx = await page.context().browser()!.newContext({
      viewport: { width: 1100, height: 800 },
      screen: HI_DPI_SCREEN,
    })
    try {
      const p = await ctx.newPage()
      await p.addInitScript(() => localStorage.setItem('clawbench-share-fit-to-screen', '1'))
      await p.goto(shared.path)
      await expect(p.locator('.share-topbar')).toBeVisible({ timeout: 15000 })

      // Fitted on load: narrow, and the outline starts closed.
      expect(await rootFontPx(p)).toBeGreaterThan(18)
      await expect(p.locator('.share-body > .share-toc')).toHaveCount(0)
      await expect(p.locator('.share-toc-drawer')).toHaveCount(0)

      await p.locator('[data-action="share-fit"]').click()
      await expect.poll(() => rootFontPx(p)).toBeCloseTo(15, 1)
      await expect(p.locator('.share-body > .share-toc')).toBeVisible()
    } finally {
      await ctx.close()
    }
  })

  test('the fitted page still fits the window', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    // Scaling grows every element without widening the window, so the failure
    // mode is a horizontal overflow: content pushed past the right edge.
    const path = await shareConversation(page)
    const ctx = await page.context().browser()!.newContext({
      viewport: { width: 900, height: 700 },
      screen: HI_DPI_SCREEN,
    })
    try {
      const p = await ctx.newPage()
      await p.goto(path)
      await expect(p.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })
      await p.locator('[data-action="share-fit"]').click()
      await expect.poll(() => rootFontPx(p)).toBeGreaterThan(18)

      const overflow = await p.evaluate(() => {
        const doc = document.documentElement
        return {
          scrollW: doc.scrollWidth,
          clientW: doc.clientWidth,
          // The topbar is the one row whose contents cannot wrap; if the scale
          // is mishandled it is what spills first.
          topbar: (() => {
            const el = document.querySelector('.share-topbar')
            if (!el) return null
            const r = el.getBoundingClientRect()
            return { right: r.right, vw: window.innerWidth }
          })(),
        }
      })

      // A 1px tolerance: scrollbars and sub-pixel rounding.
      expect(overflow.scrollW).toBeLessThanOrEqual(overflow.clientW + 1)
      if (overflow.topbar) {
        expect(overflow.topbar.right).toBeLessThanOrEqual(overflow.topbar.vw + 1)
      }
    } finally {
      await ctx.close()
    }
  })

  test('flipping the switch re-decides the outline layout', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    // The rail/drawer threshold is a DESIGN width multiplied by the factor
    // (see shareNarrowQuery), so turning the switch on moves the threshold
    // ABOVE a window that used to be wide. That decision is cached in a
    // MediaQueryList, which only fires when the WINDOW crosses its threshold —
    // never when the threshold itself moves. The reader would keep the rail on
    // a window too narrow for it, squeezing the text, until they resized.
    const shared = await page.evaluate(async () => {
      const resp = await fetch('/api/share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: 'README.md' }),
      })
      if (!resp.ok) throw new Error(`share: ${resp.status}`)
      return (await resp.json()) as { path: string; token: string }
    })

    // 1100px: wide at 100% (threshold 899), narrow at 133% (threshold ≈1196).
    const ctx = await page.context().browser()!.newContext({
      viewport: { width: 1100, height: 800 },
      screen: HI_DPI_SCREEN,
    })
    try {
      const p = await ctx.newPage()
      await p.goto(shared.path)
      await expect(p.locator('.share-topbar')).toBeVisible({ timeout: 15000 })

      // The document must HAVE an outline, or the test proves nothing.
      await expect(p.locator('.share-body > .share-toc')).toBeVisible()
      await expect(p.locator('.share-toc-drawer')).toHaveCount(0)

      await p.locator('[data-action="share-fit"]').click()
      await expect.poll(() => rootFontPx(p)).toBeGreaterThan(18)

      // Now the same window is too narrow for the scaled rail. The rail goes
      // away and — this is the part that used to be wrong — the drawer does NOT
      // pop open on its own: a drawer that appears because the reader pressed
      // "fit" covers the page they just enlarged, including the switch.
      await expect(p.locator('.share-body > .share-toc')).toHaveCount(0)
      await expect(p.locator('.share-toc-drawer')).toHaveCount(0)

      // The outline is still reachable, on demand.
      await p.locator('[data-action="share-toc"]').click()
      // Asserted on the PANEL, not the `.share-toc-drawer` wrapper: that
      // wrapper has no styles of its own (both children are `position: fixed`),
      // so it has zero size and any visibility check on it reports "hidden"
      // even when the drawer is on screen.
      await expect(p.locator('.share-toc-panel')).toBeVisible()
      await p.locator('.share-toc-backdrop').click()
      await expect(p.locator('.share-toc-panel')).toHaveCount(0)

      // ...and turning it back off restores the rail, so the decision is
      // recomputed in both directions.
      await p.locator('[data-action="share-fit"]').click()
      await expect(p.locator('.share-body > .share-toc')).toBeVisible()
      await expect(p.locator('.share-toc-drawer')).toHaveCount(0)
    } finally {
      await ctx.close()
    }
  })
})

/**
 * The meeting viewer has its own chrome — a nickname gate, a floating control
 * dock, a side panel with a drag divider — and it is the surface a phone user
 * actually reaches through a share link.
 *
 * Its topbar is on the RECORD view (a finished meeting), which is covered by
 * `MeetingShareDetail.test.ts` at the component level: a real record needs a
 * `live_notes` row, and an empty room does not create one, so fabricating that
 * page here would test a fixture rather than the product. What the browser
 * still has to prove is that a fitted page does not break the live shell.
 *
 * The dock is the interesting part: its buttons are 28dp and the pill is
 * allowed to wrap, so at a larger factor it wraps EARLIER. A fixed breakpoint
 * would keep reserving one row for the control pill above it and the pill would
 * end up sitting on the dock's second row.
 */
test.describe('Meeting share under a fitted page', () => {
  async function createRoom(page: import('@playwright/test').Page): Promise<{ token: string; path: string }> {
    return await page.evaluate(async () => {
      const resp = await fetch('/api/live-share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: 'e2e 缩放' }),
      })
      if (!resp.ok) throw new Error(`create room: ${resp.status}`)
      return (await resp.json()) as { token: string; path: string }
    })
  }

  async function endRoom(page: import('@playwright/test').Page, token: string): Promise<void> {
    await page.evaluate(async (tok) => {
      await fetch(`/api/live-share?token=${encodeURIComponent(tok)}`, { method: 'DELETE' })
    }, token)
  }

  /**
   * The LIVE shell has no topbar (it is a full-bleed picture with a floating
   * dock), so the switch is not reachable there — and that is fine: the two
   * surfaces that DO carry the shared topbar (a file/folder/conversation share
   * and the finished meeting record) are covered above. What must hold for the
   * live shell is that the switch's absence does not break it: the page still
   * renders and the dock geometry stays consistent when the factor is on.
   *
   * The record view needs a real `live_notes` row, which an empty room does not
   * create — that page is covered by MeetingShareDetail.test.ts instead of
   * fabricating a meeting here.
   */
  test('the control pill clears the dock even when it wraps', async ({ page }) => {
    // On a narrow window the seven dock buttons wrap to two rows, so the pill
    // above the dock has to reserve the taller height. That height is measured
    // from the dock's own box rather than guessed from a breakpoint, because
    // the wrap point moves with the interface scale.
    const room = await createRoom(page)
    try {
      const ctx = await page.context().browser()!.newContext({
        viewport: { width: 560, height: 800 },
        screen: HI_DPI_SCREEN,
      })
      try {
        const viewer = await ctx.newPage()
        await viewer.goto(room.path)
        await viewer.locator('[data-role="join-nick"]').fill('E2E 观众')
        await viewer.locator('[data-action="join-meeting"]').click()
        await expect(viewer.locator('.live-share-view')).toBeVisible({ timeout: 15000 })

        // Turn the fit on: the dock gets bigger, so this is the case that
        // previously pushed the pill onto the second row.
        // (The switch lives on the record topbar, not in the live shell, so the
        // factor is seeded the same way the entry point reads it.)
        await viewer.evaluate(() => localStorage.setItem('clawbench-share-fit-to-screen', '1'))
        await viewer.reload()
        await viewer.locator('[data-role="join-nick"]').fill('E2E 观众')
        await viewer.locator('[data-action="join-meeting"]').click()
        await expect(viewer.locator('.live-share-view')).toBeVisible({ timeout: 15000 })

        const geometry = await viewer.evaluate(() => {
          const root = document.querySelector('.live-share-view') as HTMLElement | null
          const dock = document.querySelector('.live-dock') as HTMLElement | null
          if (!root || !dock) return null
          const cs = getComputedStyle(root)
          return {
            reserved: parseFloat(cs.getPropertyValue('--live-dock-height')),
            dockHeight: dock.getBoundingClientRect().height,
          }
        })
        expect(geometry).not.toBeNull()
        // Whatever the dock ended up doing, the space reserved above it must be
        // at least as tall as the dock itself.
        expect(geometry!.reserved).toBeGreaterThanOrEqual(geometry!.dockHeight - 2)
      } finally {
        await ctx.close()
      }
    } finally {
      await endRoom(page, room.token)
    }
  })
})

/**
 * The two meeting surfaces that used to be a dead end.
 *
 * A share link can open straight into the nickname gate, and once joined it
 * becomes a full-bleed picture. Neither had a topbar, so a reader on a 1440p or
 * 4K screen had no way to enlarge the page — the gate's field and join button
 * are the only way into the meeting, and the dock is the only way to control it.
 */
test.describe('Meeting pages carry the switch', () => {
  async function createRoom(page: import('@playwright/test').Page): Promise<{ token: string; path: string }> {
    return await page.evaluate(async () => {
      const resp = await fetch('/api/live-share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: 'e2e 缩放' }),
      })
      if (!resp.ok) throw new Error(`create room: ${resp.status}`)
      return (await resp.json()) as { token: string; path: string }
    })
  }

  async function endRoom(page: import('@playwright/test').Page, token: string): Promise<void> {
    await page.evaluate(async (tok) => {
      await fetch(`/api/live-share?token=${encodeURIComponent(tok)}`, { method: 'DELETE' })
    }, token)
  }

  test('the nickname gate has a topbar and grows on click', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    const room = await createRoom(page)
    try {
      const ctx = await page.context().browser()!.newContext({
        viewport: { width: 1100, height: 800 },
        screen: HI_DPI_SCREEN,
      })
      try {
        const viewer = await ctx.newPage()
        await viewer.goto(room.path)
        await expect(viewer.locator('.join-gate')).toBeVisible({ timeout: 15000 })

        // The gate's own topbar, not just any bar on the page.
        await expect(viewer.locator('.join-gate .share-topbar')).toBeVisible()
        const btn = viewer.locator('[data-action="share-fit"]')
        await expect(btn).toBeVisible()
        await expect(btn).toHaveAttribute('aria-pressed', 'false')

        // 44dp at 100% ≈ 44px.
        const before = await viewer.locator('[data-role="join-nick"]').boundingBox()
        expect(before).not.toBeNull()

        await btn.click()
        await expect.poll(() => rootFontPx(viewer)).toBeGreaterThan(18)

        // 44dp × 1.33 ≈ 58.5px — the field the reader has to hit grew with it.
        const after = await viewer.locator('[data-role="join-nick"]').boundingBox()
        expect(after).not.toBeNull()
        expect(after!.height).toBeGreaterThan(before!.height + 8)

        // Nothing spilled: the topbar is the row that would overflow first.
        const overflow = await viewer.evaluate(() => {
          const doc = document.documentElement
          return { scrollW: doc.scrollWidth, clientW: doc.clientWidth }
        })
        expect(overflow.scrollW).toBeLessThanOrEqual(overflow.clientW + 1)
      } finally {
        await ctx.close()
      }
    } finally {
      await endRoom(page, room.token)
    }
  })

  test('the live shell has a topbar, and dock labels follow the measured width', async ({ page, browserName }) => {
    test.skip(!screenOverrideSupported(browserName), 'newContext({screen}) is chromium-only')
    // 600px is wide enough for labels at 100% (threshold 520) and too narrow
    // once the page is fitted (600 / 1.33 ≈ 451 design px). Same window, same
    // dock — only the scale moved. A media query cannot see that.
    const room = await createRoom(page)
    try {
      const ctx = await page.context().browser()!.newContext({
        viewport: { width: 600, height: 800 },
        screen: HI_DPI_SCREEN,
      })
      try {
        const viewer = await ctx.newPage()
        await viewer.goto(room.path)
        await viewer.locator('[data-role="join-nick"]').fill('E2E 观众')
        await viewer.locator('[data-action="join-meeting"]').click()
        await expect(viewer.locator('.live-share-view')).toBeVisible({ timeout: 15000 })

        await expect(viewer.locator('.live-topbar')).toBeVisible()
        const labels = viewer.locator('.live-dock-label').first()
        await expect(labels).toBeVisible()

        await viewer.locator('.live-topbar [data-action="share-fit"]').click()
        await expect.poll(() => rootFontPx(viewer)).toBeGreaterThan(18)

        // Labels are the first thing to drop, and they must drop NOW — not when
        // the reader happens to resize the window past 520 CSS px.
        await expect(labels).toBeHidden()

        // Turning it back off brings them back (the decision is not one-way).
        await viewer.locator('.live-topbar [data-action="share-fit"]').click()
        await expect.poll(() => rootFontPx(viewer)).toBeCloseTo(15, 1)
        await expect(labels).toBeVisible()
      } finally {
        await ctx.close()
      }
    } finally {
      await endRoom(page, room.token)
    }
  })
})

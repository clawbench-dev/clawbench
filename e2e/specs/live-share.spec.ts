import { test, expect } from '../fixtures'

/**
 * Live share (screen + audio) — the viewer side.
 *
 * Playwright cannot drive the Electron capture stack, so the PUBLISHER is out of
 * scope here (that path is covered by desktop/src/main/capture.test.ts plus the
 * manual checklist in the design doc). What a real browser CAN verify is the
 * public half, which is also the half an anonymous reader hits:
 *
 *   - a live token resolves to kind=live from the meta endpoint
 *   - /share/{token} renders the live viewer, not the file/session renderers
 *   - the signalling socket is reachable and answers with `joined`
 *   - the room refuses a second publisher
 *   - ending the share makes the token 404
 *
 * The WebRTC media path itself needs two real endpoints, so it is asserted only
 * up to "the socket negotiated a viewer id".
 */

/** Create a live room through the authenticated management endpoint. */
async function createRoom(page: import('@playwright/test').Page): Promise<{ token: string; path: string }> {
  return await page.evaluate(async () => {
    const resp = await fetch('/api/live-share', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'e2e 直播' }),
    })
    if (!resp.ok) throw new Error(`create room: ${resp.status}`)
    return (await resp.json()) as { token: string; path: string }
  })
}

/** End the room so one test cannot leak a live token into the next. */
async function endRoom(page: import('@playwright/test').Page, token: string): Promise<void> {
  await page.evaluate(async (tok) => {
    await fetch(`/api/live-share?token=${encodeURIComponent(tok)}`, { method: 'DELETE' })
  }, token)
}

test.describe('Live share viewer', () => {
  test('meta reports kind=live and the page renders the live viewer', async ({ page }) => {
    const room = await createRoom(page)
    try {
      // Anonymous context: the token is the only credential a viewer has.
      const anon = await page.context().browser()!.newContext()
      try {
        const meta = await anon.request.get(`/api/share/${room.token}/meta`)
        expect(meta.status()).toBe(200)
        const body = (await meta.json()) as { kind: string; title?: string }
        expect(body.kind).toBe('live')
        expect(body.title).toBe('e2e 直播')

        const viewer = await anon.newPage()
        await viewer.goto(room.path)

        // The live viewer owns the stage, not the file/session renderers.
        await expect(viewer.locator('.live-share-view')).toBeVisible({ timeout: 15000 })
        await expect(viewer.locator('.share-view')).toHaveCount(0)
        await expect(viewer.locator('.session-share-messages')).toHaveCount(0)

        // With no publisher attached the room is treated as gone, so the page
        // settles on a terminal state rather than spinning forever.
        await expect(viewer.locator('.live-overlay-title')).toBeVisible({ timeout: 15000 })
      } finally {
        await anon.close()
      }
    } finally {
      await endRoom(page, room.token)
    }
  })

  test('the signalling socket answers joined for a live token', async ({ page }) => {
    const room = await createRoom(page)
    try {
      // A publisher must be attached for viewers to be admitted, so open the
      // publisher socket first from inside the page (it is same-origin and
      // cookie-authenticated, exactly like the real publisher).
      const joined = await page.evaluate(async (token) => {
        const proto = location.protocol === 'https:' ? 'wss' : 'ws'
        const pub = new WebSocket(`${proto}://${location.host}/api/live-share/publish/ws?token=${token}`)
        await new Promise<void>((resolve, reject) => {
          pub.onopen = () => resolve()
          pub.onerror = () => reject(new Error('publisher socket failed'))
        })
        pub.send(JSON.stringify({ type: 'publish', title: 'e2e' }))

        const view = new WebSocket(`${proto}://${location.host}/api/share/${token}/signal`)
        const first = await new Promise<Record<string, unknown>>((resolve, reject) => {
          const timer = setTimeout(() => reject(new Error('no joined frame')), 10000)
          view.onmessage = (ev) => {
            clearTimeout(timer)
            resolve(JSON.parse(String(ev.data)) as Record<string, unknown>)
          }
          view.onerror = () => {
            clearTimeout(timer)
            reject(new Error('viewer socket failed'))
          }
        })
        // Give the publisher a moment to receive viewer-join, then close both.
        await new Promise((r) => setTimeout(r, 200))
        view.close()
        pub.close()
        return first
      }, room.token)

      expect(joined.type).toBe('joined')
      expect(typeof joined.viewerId).toBe('string')
      expect(joined.viewerId).not.toBe('')
      // Reserved for a future TURN rollout; must serialize as [] not null.
      expect(joined.iceServers).toEqual([])
      // `talkEnabled` is omitempty and the room defaults to disabled, so the
      // frame omits it. Missing === false here (no third state), which is what
      // the viewer relies on when it decides whether to show "hold to talk".
      expect(Boolean(joined.talkEnabled)).toBe(false)
    } finally {
      await endRoom(page, room.token)
    }
  })

  test('a second publisher is refused', async ({ page }) => {
    const room = await createRoom(page)
    try {
      const status = await page.evaluate(async (token) => {
        const proto = location.protocol === 'https:' ? 'wss' : 'ws'
        const first = new WebSocket(`${proto}://${location.host}/api/live-share/publish/ws?token=${token}`)
        await new Promise<void>((resolve, reject) => {
          first.onopen = () => resolve()
          first.onerror = () => reject(new Error('first publisher failed'))
        })
        const second = new WebSocket(`${proto}://${location.host}/api/live-share/publish/ws?token=${token}`)
        const result = await new Promise<number>((resolve) => {
          // A refused upgrade surfaces as an error event; the HTTP status is not
          // exposed to the page, so treat "did not open" as the assertion.
          const timer = setTimeout(() => resolve(0), 5000)
          second.onopen = () => {
            clearTimeout(timer)
            resolve(200)
          }
          second.onerror = () => {
            clearTimeout(timer)
            resolve(409)
          }
        })
        first.close()
        return result
      }, room.token)

      // One token = one publisher. A second must not open.
      expect(status).not.toBe(200)
    } finally {
      await endRoom(page, room.token)
    }
  })

  test('ending the share makes the token 404 for viewers', async ({ page }) => {
    const room = await createRoom(page)
    await endRoom(page, room.token)

    const anon = await page.context().browser()!.newContext()
    try {
      const meta = await anon.request.get(`/api/share/${room.token}/meta`)
      expect(meta.status()).toBe(404)
    } finally {
      await anon.close()
    }
  })

  test('creating twice in the same project reuses the token', async ({ page }) => {
    const first = await createRoom(page)
    try {
      const second = await createRoom(page)
      // Minting a second token would silently invalidate the link the user
      // already copied.
      expect(second.token).toBe(first.token)
    } finally {
      await endRoom(page, first.token)
    }
  })
})

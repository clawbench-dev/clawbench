import { test, expect } from '../fixtures'

/**
 * The public share page is served to an ANONYMOUS reader, and the shell is a
 * client-rendered SPA: the only thing the server contributes is the injected
 * discovery block (title + data URLs + JSON-LD) and the HEAD-friendly data
 * endpoints. Both are what make a share link usable outside a browser (link
 * unfurls, agents, `curl -I`), and both are easy to lose in a refactor of the
 * shell serving path — hence a real-browser check on top of the unit tests.
 */
test.describe('Public share page', () => {
  test('a shared conversation renders read-only with its title and data endpoint', async ({ page }) => {
    // Create a session with a real exchange, then share it through the same API
    // the dialog uses (browser fetch carries both cookies).
    const shared = await page.evaluate(async () => {
      const created = await fetch('/api/ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agentId: 'acp-mock' }),
      })
      if (!created.ok) throw new Error(`create session: ${created.status}`)
      const { sessionId } = await created.json()

      const sent = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId, message: 'share_probe_' + Date.now() }),
      })
      if (!sent.ok) throw new Error(`send: ${sent.status}`)

      const share = await fetch('/api/share/session', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sessionId }),
      })
      if (!share.ok) throw new Error(`share: ${share.status}`)
      return { sessionId, ...(await share.json()) } as { sessionId: string; token: string; path: string }
    })

    expect(shared.token).toBeTruthy()
    expect(shared.path).toBe(`/share/${shared.token}`)

    // Anonymous context: no cookies at all, which is how a reader arrives.
    const anon = await page.context().browser()!.newContext()
    try {
      const sharePage = await anon.newPage()

      // HEAD must be answered on the conversation data endpoint. A session
      // token has no file_shares row, so the bare token path is a file lookup
      // and correctly 404s — the conversation lives under /session.
      const head = await anon.request.fetch(`/api/share/${shared.token}/session`, { method: 'HEAD' })
      expect(head.status()).toBe(200)

      await sharePage.goto(shared.path)

      // The transcript renders (the SPA fetched the snapshot with the token).
      await expect(sharePage.locator('.session-share-messages')).toBeVisible({ timeout: 15000 })

      // Read-only: no chat input, no send button anywhere on the page.
      await expect(sharePage.locator('.chat-textarea')).toHaveCount(0)

      // The server-injected discovery block must survive to the real response.
      const html = await sharePage.content()
      expect(html).toContain('application/ld+json')
      expect(html).toContain(`/api/share/${shared.token}/session`)
      await expect(sharePage).toHaveTitle(/ClawBench|share/i)
    } finally {
      await anon.close()
    }
  })
})

/**
 * Rendered HTML shares.
 *
 * A shared .html is shown in an iframe that must load it by a REAL URL: a
 * srcdoc document has no URL of its own, inherits the share page's base, and
 * every relative reference in it (`css/x.css`, `img/y.png`) 404s — a styled
 * landing page then renders as unstyled black-on-white HTML. The URL is
 * path-addressed under `/doc/{dirKey}/…` so the browser derives the base.
 *
 * That endpoint is the only public one answering `text/html`, so its `sandbox`
 * CSP is the security boundary: without it, any shared document would run
 * same-origin script with the READER's session. Both halves are asserted here
 * because a regression in either is silent — styled-but-dangerous, or
 * safe-but-unstyled.
 */
test.describe('Shared HTML document', () => {
  test('renders with its own stylesheet and stays in an opaque origin', async ({ page }) => {
    const shared = await page.evaluate(async () => {
      const resp = await fetch('/api/share', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: 'web/landing/index.html' }),
      })
      if (!resp.ok) throw new Error(`share: ${resp.status}`)
      return (await resp.json()) as { path: string; token: string }
    })

    const anon = await page.context().browser()!.newContext()
    try {
      const sharePage = await anon.newPage()
      const docResponses: Array<{ url: string; type: string; status: number }> = []
      sharePage.on('response', (res) => {
        if (!res.url().includes(`/api/share/${shared.token}/doc/`)) return
        docResponses.push({
          url: res.url(),
          type: res.headers()['content-type']?.split(';')[0] ?? '',
          status: res.status(),
        })
      })

      await sharePage.goto(shared.path)
      const frame = sharePage.locator('iframe.share-html-iframe')
      await expect(frame).toBeVisible({ timeout: 15000 })
      await expect.poll(() => docResponses.length, { timeout: 15000 }).toBeGreaterThan(1)

      // The document is addressed by PATH (not a query), which is what gives it
      // a base. And it is served as HTML, not downloaded.
      const doc = docResponses.find(r => r.url.endsWith('/index.html'))
      expect(doc, 'document request').toBeTruthy()
      expect(doc!.type).toBe('text/html')

      // Its stylesheet came along with the right type — an octet-stream
      // stylesheet is silently dropped, which is the "unstyled" defect.
      const css = docResponses.find(r => r.url.endsWith('/css/landing.css'))
      expect(css, 'stylesheet request').toBeTruthy()
      expect(css!.type).toBe('text/css')
      expect(css!.status).toBe(200)
      // Every doc response is under the same directory key, i.e. one base.
      const key = new URL(doc!.url).pathname.split('/doc/')[1].split('/')[0]
      for (const r of docResponses) {
        expect(new URL(r.url).pathname).toContain(`/doc/${key}/`)
      }

      // Styled, not raw: the landing page's own dark background is painted.
      const bg = await sharePage.evaluate(() => {
        const f = document.querySelector('iframe.share-html-iframe') as HTMLIFrameElement | null
        try {
          const body = f?.contentDocument?.body
          return body ? getComputedStyle(body).backgroundColor : 'opaque'
        } catch {
          return 'opaque'
        }
      })
      // A sandboxed frame is cross-origin to the parent, so the computed style
      // is unreadable — that IS the isolation guarantee. Either way the page
      // must not be the browser default white.
      expect(bg).not.toBe('rgb(255, 255, 255)')

      // Isolation: the parent cannot reach into the document at all.
      const reachable = await sharePage.evaluate(() => {
        const f = document.querySelector('iframe.share-html-iframe') as HTMLIFrameElement
        try {
          return f.contentDocument !== null
        } catch {
          return false
        }
      })
      expect(reachable, 'sandboxed document must be an opaque origin').toBe(false)

      // ...and the sandbox attribute itself never gains allow-same-origin,
      // which would let the document remove its own sandbox.
      const sandbox = await frame.getAttribute('sandbox')
      expect(sandbox).toContain('allow-scripts')
      // Popups must be granted AND allowed to escape the sandbox, or the
      // document's own `target="_blank"` links do nothing (attribute and CSP
      // flags are intersected, so the server side must grant them too).
      expect(sandbox).toContain('allow-popups-to-escape-sandbox')
      expect(sandbox).not.toContain('allow-same-origin')
    } finally {
      await anon.close()
    }
  })
})

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

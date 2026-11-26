import { test, expect } from '../fixtures'

/**
 * A relayed (LiveKit) meeting: a member who joins late must see every chat line
 * sent before they arrived — the host's AND another member's.
 *
 * Opt-in, because it needs a real relay. The unit tests pin the framing and the
 * attribution rules; this one pins that a member's history request actually
 * crosses a real LiveKit and is answered. Without it, "the chat is empty for a
 * late joiner" would only ever be caught by hand, exactly like the two bugs this
 * spec covers (the data-publish grant, and the packet-identity attribution).
 *
 *   docker run -d --name lk-e2e -p 7880:7880 -p 7881:7881 -p 7882:7882/udp \
 *     livekit/livekit-server:v1.8.4 --dev --bind 0.0.0.0 --node-ip 127.0.0.1
 *   E2E_LIVEKIT=1 npx playwright test -c e2e/playwright.config.ts specs/meeting-relay-chat.spec.ts
 */
const SKIP_REASON = 'set E2E_LIVEKIT=1 with a dev LiveKit on 127.0.0.1:7880 to run'
test.skip(!process.env.E2E_LIVEKIT, SKIP_REASON)

const SFU = { url: 'ws://127.0.0.1:7880', apiKey: 'devkey', apiSecret: 'secret' }

test('relay: late member sees the earlier chat', async ({ page }) => {
  const cfg = await page.request.patch('/api/live-share/sfu', { data: SFU })
  expect(cfg.status(), await cfg.text()).toBe(200)

  const title = `E2E 中继迟到 ${Date.now()}`
  await page.goto('/')
  await page.locator('.dock-btn[data-tab="live"]').filter({ visible: true }).first().click()
  await page.locator('[data-action="meeting-create"]').click()
  await page.locator('[data-role="meeting-title"]').fill(title)
  await page.locator('[data-role="meeting-nick"]').fill('主持人')
  const form = page.locator('.meeting-panel')
  await form.locator('[data-source="__off__"]').click()
  await form.locator('[data-role="device-choice"][data-kind="audio"] [data-device="__off__"]').click()
  await form.locator('[data-role="meeting-relay"]').check()
  await page.locator('[data-action="meeting-start"]').click()
  await expect(page.locator('[data-live-tab="basic"]')).toBeVisible({ timeout: 20000 })

  const token = await page.locator('.live-panel__body').getAttribute('data-quote-meeting-token')
  expect(token, 'meeting token').toBeTruthy()

  // Host sends a line before anybody joins.
  await page.locator('[data-live-tab="chat"]').click()
  const input = page.locator('[data-role="chat-input"]').first()
  await input.fill('主持人：开会前说的')
  await input.press('Enter')

  // Member A joins and speaks. No settling wait: the line they type while the
  // relay is still connecting must be held by the viewer and flushed when the
  // bus opens, not dropped.
  const browser = page.context().browser()!
  const anonA = await browser.newContext()
  try {
    const a = await anonA.newPage()
    await a.goto(`/share/${token}`)
    await expect(a.locator('.join-gate')).toBeVisible({ timeout: 20000 })
    await a.locator('[data-role="join-nick"]').fill('成员A')
    await a.locator('[data-action="join-meeting"]').click()
    await expect(a.locator('.live-share-view')).toBeVisible({ timeout: 20000 })
    await a.locator('[data-action="chat"]').click()
    const aInput = a.locator('[data-role="chat-input"]')
    await expect(aInput).toBeVisible({ timeout: 10000 })
    await aInput.fill('成员A：我先说一句')
    await aInput.press('Enter')
    await expect(a.locator('.chat-row__text').filter({ hasText: '成员A：我先说一句' })).toBeVisible({
      timeout: 15000,
    })

    // The host must receive the member's line over the relay.
    await expect(
      page.locator('.chat-row__text').filter({ hasText: '成员A：我先说一句' }),
    ).toBeVisible({ timeout: 10000 })

    // Close the room's speech switch. That pushes a permission update to every
    // viewer, and the update REPLACES their grant — the old code set
    // `canPublishData: false` there, so from this moment on every member's chat
    // (and their history request) vanished silently.
    await page.locator('[data-live-tab="basic"]').click()
    await page.locator('[data-role="stage-talk"]').uncheck()
    await page.locator('[data-live-tab="chat"]').click()
    await aInput.fill('成员A：关掉发言之后还能聊')
    await aInput.press('Enter')
    await expect(
      page.locator('.chat-row__text').filter({ hasText: '成员A：关掉发言之后还能聊' }),
    ).toBeVisible({ timeout: 10000 })

    // Member B joins later and must see both earlier lines.
    const anonB = await browser.newContext()
    try {
      const b = await anonB.newPage()
      await b.goto(`/share/${token}`)
      await expect(b.locator('.join-gate')).toBeVisible({ timeout: 20000 })
      await b.locator('[data-role="join-nick"]').fill('成员B')
      await b.locator('[data-action="join-meeting"]').click()
      await expect(b.locator('.live-share-view')).toBeVisible({ timeout: 20000 })
      await b.locator('[data-action="chat"]').click()

      await expect(
        b.locator('.chat-row__text').filter({ hasText: '主持人：开会前说的' }),
      ).toBeVisible({ timeout: 10000 })
      await expect(
        b.locator('.chat-row__text').filter({ hasText: '成员A：我先说一句' }),
      ).toBeVisible({ timeout: 10000 })
      await expect(
        b.locator('.chat-row__text').filter({ hasText: '成员A：关掉发言之后还能聊' }),
      ).toBeVisible({ timeout: 10000 })
    } finally {
      await anonB.close()
    }
  } finally {
    await anonA.close()
  }

  // Cleanup.
  await page.locator('[data-action="panel-end"]').click()
  await expect(page.locator('.dlg-box')).toBeVisible({ timeout: 5000 })
  await page.locator('.dlg-ok').click()
  await expect
    .poll(
      async () =>
        (await page.request.delete(`/api/live-share/notes/sessions?token=${token}`)).status(),
      { timeout: 20000, intervals: [500, 1000, 2000] },
    )
    .toBe(200)
})

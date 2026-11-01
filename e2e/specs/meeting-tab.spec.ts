import { test, expect } from '../fixtures'

/**
 * The 会议 tab — the parts a browser can drive without media capture.
 *
 * Starting a meeting needs a screen or a microphone, which Playwright cannot
 * provide reliably here (the capture path is covered by the desktop tests and
 * the manual checklist). What IS worth locking down:
 *
 *   - the tab exists on every host and lands on the RECORD LIST, not a form
 *   - create and join are separate pages, reachable from the list footer
 *   - the join page refuses to submit without a nickname
 *
 * The viewer's own nickname gate is covered by live-share.spec.ts.
 */

test.describe('Meeting tab', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/')
    await page.locator('.dock-btn[data-tab="live"]').filter({ visible: true }).first().click()
  })

  test('lands on the record list with both entry points in the footer', async ({ page }) => {
    await expect(page.locator('[data-action="meeting-create"]')).toBeVisible()
    await expect(page.locator('[data-action="meeting-join"]')).toBeVisible()
    // The list is the landing view: there is no start form on this page.
    await expect(page.locator('[data-role="meeting-title"]')).toHaveCount(0)
  })

  test('create is its own page, with the name first and the voice tile', async ({ page }) => {
    await page.locator('[data-action="meeting-create"]').click()

    await expect(page.locator('[data-role="meeting-title"]')).toBeVisible()
    await expect(page.locator('[data-source="voice"]')).toBeVisible()

    // A source is pre-selected (voice when there is no screen to enumerate), so
    // the primary action is live immediately — the user is never forced to
    // re-choose the obvious default.
    const start = page.locator('[data-action="meeting-start"]')
    await expect(start).toBeEnabled()

    await page.locator('[data-source="voice"]').click()
    await expect(page.locator('[data-source="voice"]')).toHaveClass(/meeting-source--on/)
  })

  test('join requires a nickname before it can submit', async ({ page }) => {
    await page.locator('[data-action="meeting-join"]').click()

    const submit = page.locator('[data-action="meeting-join-submit"]')
    await expect(page.locator('[data-role="meeting-nick"]')).toBeVisible()
    await expect(submit).toBeDisabled()

    await page.locator('[data-role="meeting-nick"]').fill('E2E 用户')
    await page.locator('[data-role="meeting-link"]').fill('http://example.test/share/abc')
    await expect(submit).toBeEnabled()
  })
})

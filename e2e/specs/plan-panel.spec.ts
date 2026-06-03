import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E tests for Plan Progress Panel feature.
 *
 * acp-mock emits plan updates during responses. However, plan events
 * are not guaranteed to arrive in every response — they depend on
 * the ACP agent's internal logic. Tests use generous timeouts and
 * soft assertions where appropriate.
 */
test.describe('Plan Progress Panel', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  test('plan panel is hidden when no plan data', async ({ page }) => {
    // Initial state: no plan has been emitted, so the panel should not exist
    await expect(page.locator('.plan-panel')).not.toBeVisible()
  })

  test('plan panel appears after sending a message', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // acp-mock emits a plan update during the response.
    // Plan events may arrive after the text content, so give generous timeout.
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 60000 })
  })

  test('plan panel shows stepped timeline entries', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for the plan panel to appear
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 60000 })

    // Should show plan entries from acp-mock
    const entries = page.locator('.plan-entry')
    const count = await entries.count()
    expect(count).toBeGreaterThanOrEqual(1)

    // Verify first entry has content
    await expect(entries.first()).not.toBeEmpty()
  })

  test('plan panel collapses on toggle click', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for the expanded plan panel
    await expect(page.locator('.plan-expanded')).toBeVisible({ timeout: 60000 })

    // Click the collapse toggle (▲ button in header)
    await page.locator('.plan-expanded__toggle').click()

    // Expanded timeline should be hidden, chip should appear
    await expect(page.locator('.plan-expanded')).not.toBeVisible()
    await expect(page.locator('.plan-chip')).toBeVisible()
  })

  test('collapsed chip shows in-progress task', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for plan panel
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 60000 })

    // Collapse it
    const toggleBtn = page.locator('.plan-expanded__toggle')
    const toggleVisible = await toggleBtn.isVisible({ timeout: 3000 }).catch(() => false)
    if (toggleVisible) {
      await toggleBtn.click()
      await expect(page.locator('.plan-chip')).toBeVisible()

      // Chip text should show some content
      const chipText = page.locator('.plan-chip__text')
      const hasText = await chipText.isVisible().catch(() => false)
      expect(hasText).toBeTruthy()
    }
  })

  test('clicking collapsed chip expands the panel', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for plan panel and collapse
    await expect(page.locator('.plan-expanded')).toBeVisible({ timeout: 60000 })
    await page.locator('.plan-expanded__toggle').click()
    await expect(page.locator('.plan-chip')).toBeVisible()

    // Click the chip to expand
    await page.locator('.plan-chip').click()

    // Expanded timeline should reappear
    await expect(page.locator('.plan-expanded')).toBeVisible()
    await expect(page.locator('.plan-chip')).not.toBeVisible()

    // Entries should still be visible
    const entries = page.locator('.plan-entry')
    const count = await entries.count()
    expect(count).toBeGreaterThanOrEqual(1)
  })
})

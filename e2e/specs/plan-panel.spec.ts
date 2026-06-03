import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

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

    // acp-mock emits a plan update at the start of each turn
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })
  })

  test('plan panel shows stepped timeline entries', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for the plan panel to appear
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })

    // Should show 3 plan entries from acp-mock
    const entries = page.locator('.plan-entry')
    await expect(entries).toHaveCount(3, { timeout: 10000 })

    // Verify entry content
    await expect(entries.nth(0)).toContainText('Analyze the request')
    await expect(entries.nth(1)).toContainText('Generate response')
    await expect(entries.nth(2)).toContainText('Verify output')

    // Verify status classes
    await expect(entries.nth(0)).toHaveClass(/plan-entry--completed/)
    await expect(entries.nth(1)).toHaveClass(/plan-entry--in_progress/)
    await expect(entries.nth(2)).toHaveClass(/plan-entry--pending/)
  })

  test('plan panel collapses on toggle click', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for the expanded plan panel
    await expect(page.locator('.plan-expanded')).toBeVisible({ timeout: 15000 })

    // Click the collapse toggle (▲ button in header)
    await page.locator('.plan-expanded__toggle').click()

    // Expanded timeline should be hidden, chip should appear
    await expect(page.locator('.plan-expanded')).not.toBeVisible()
    await expect(page.locator('.plan-chip')).toBeVisible()
  })

  test('collapsed chip shows in-progress task', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for plan panel
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 15000 })

    // Collapse it
    await page.locator('.plan-expanded__toggle').click()
    await expect(page.locator('.plan-chip')).toBeVisible()

    // Chip text should show the in-progress entry ("Generate response")
    const chipText = page.locator('.plan-chip__text')
    await expect(chipText).toContainText('Generate response')
  })

  test('clicking collapsed chip expands the panel', async ({ page }) => {
    await chat.sendAndAwaitACPReply('Hello')

    // Wait for plan panel and collapse
    await expect(page.locator('.plan-expanded')).toBeVisible({ timeout: 15000 })
    await page.locator('.plan-expanded__toggle').click()
    await expect(page.locator('.plan-chip')).toBeVisible()

    // Click the chip to expand
    await page.locator('.plan-chip').click()

    // Expanded timeline should reappear
    await expect(page.locator('.plan-expanded')).toBeVisible()
    await expect(page.locator('.plan-chip')).not.toBeVisible()

    // Entries should still be visible
    const entries = page.locator('.plan-entry')
    await expect(entries).toHaveCount(3)
  })
})

import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E tests for Plan Progress Panel feature.
 *
 * Strategy: Use the window.__clawbench E2E bridge to inject plan data
 * directly, rather than relying on plan_update SSE events from acp-mock.
 * This avoids timing issues where the SSE stream closes before plan
 * events arrive or the frontend guard rejects late events.
 *
 * The bridge approach validates:
 * - Vue reactivity (planEntries → PlanPanel rendering)
 * - User interactions (collapse, expand, chip)
 * - CSS class names and structure
 */
test.describe('Plan Progress Panel', () => {
  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
    // Ensure the page is loaded and the bridge is available
    await page.waitForFunction(() => !!(window as any).__clawbench?.updatePlanEntries, undefined, { timeout: 10000 })
  })

  /** Inject plan entries via the E2E bridge and wait for Vue to render */
  async function injectPlanEntries(page: any, entries: Array<{ content: string; priority: string; status: string }>) {
    await page.evaluate((entries) => {
      const bridge = (window as any).__clawbench
      if (bridge?.updatePlanEntries) {
        bridge.updatePlanEntries(entries)
      }
    }, entries)
    // Wait for Vue to render the plan panel
    await page.waitForTimeout(200)
  }

  const sampleEntries = [
    { content: 'Analyze the request', priority: 'high', status: 'completed' },
    { content: 'Generate response', priority: 'high', status: 'in_progress' },
    { content: 'Verify output', priority: 'medium', status: 'pending' },
  ]

  test('plan panel is hidden when no plan data', async ({ page }) => {
    // Initial state: no plan has been emitted, so the panel should not exist
    await expect(page.locator('.plan-panel')).not.toBeVisible()
  })

  test('plan panel appears after injecting plan data', async ({ page }) => {
    await injectPlanEntries(page, sampleEntries)
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 5000 })
  })

  test('plan panel shows stepped timeline entries', async ({ page }) => {
    await injectPlanEntries(page, sampleEntries)

    // Wait for the plan panel to appear
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 5000 })

    // Should show plan entries
    const entries = page.locator('.plan-entry')
    const count = await entries.count()
    expect(count).toBe(3)

    // Verify first entry has content
    await expect(entries.first()).toContainText('Analyze the request')
  })

  test('plan panel collapses on toggle click', async ({ page }) => {
    await injectPlanEntries(page, sampleEntries)

    // Wait for the expanded plan panel
    await expect(page.locator('.plan-expanded')).toBeVisible({ timeout: 5000 })

    // Click the collapse toggle (▲ button in header)
    await page.locator('.plan-expanded__toggle').click()

    // Expanded timeline should be hidden, chip should appear
    await expect(page.locator('.plan-expanded')).not.toBeVisible()
    await expect(page.locator('.plan-chip')).toBeVisible()
  })

  test('collapsed chip shows in-progress task', async ({ page }) => {
    await injectPlanEntries(page, sampleEntries)

    // Wait for plan panel
    await expect(page.locator('.plan-panel')).toBeVisible({ timeout: 5000 })

    // Collapse it
    const toggleBtn = page.locator('.plan-expanded__toggle')
    await toggleBtn.click()
    await expect(page.locator('.plan-chip')).toBeVisible()

    // Chip text should show the in-progress task
    const chipText = page.locator('.plan-chip__text')
    await expect(chipText).toContainText('Generate response')
  })

  test('clicking collapsed chip expands the panel', async ({ page }) => {
    await injectPlanEntries(page, sampleEntries)

    // Wait for plan panel and collapse
    await expect(page.locator('.plan-expanded')).toBeVisible({ timeout: 5000 })
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
    expect(count).toBe(3)
  })
})

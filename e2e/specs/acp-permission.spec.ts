import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import type { Page } from '@playwright/test'

/**
 * E2E tests for ACP permission approval flow.
 *
 * Uses acp-mock agent (real ACP stdio protocol) which:
 * - Requests permissions ONLY when NOT in bypass-permissions mode
 * - Default mode is "Bypass Permissions" (no permission requests)
 * - Switching to Code/Plan mode triggers permission_request on next Prompt
 * - Provides 3 permission options: Allow Once, Allow Always, Deny
 *
 * Key ACP behaviors tested:
 * 1. PermissionApproval card appears in non-bypass mode
 * 2. Clicking allow/deny buttons calls POST /api/ai/permission/respond
 * 3. Permission respond API endpoint works directly
 *
 * SERIAL: Tests must run serially because the ACP mock agent is a single
 * subprocess. Concurrent Prompt requests on the same agent process can
 * cause the JSON-RPC stream to become corrupted.
 */
test.describe.serial('ACP Permission Approval', () => {
  test.setTimeout(120000)

  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  /**
   * Helper: warm up ACP connection (bypass mode by default), then switch
   * to Code mode so that subsequent messages trigger permission requests.
   */
  async function warmUpAndSwitchToCodeMode(page: Page) {
    // First message warms up ACP connection (default mode is bypass-permissions)
    await chat.sendAndAwaitACPReply('hi')

    // Wait for ACP mode state to be available, then switch to Code mode
    await chat.waitForACPModeState()
    await chat.openModeMenu()
    await chat.selectMode('Code')

    // Wait for modal to close (mode selection auto-closes the modal)
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).not.toBeVisible({ timeout: 5000 })
  }

  test('should show permission approval card in non-bypass mode', async ({ page }) => {
    await warmUpAndSwitchToCodeMode(page)

    // Send another message — acp-mock will request permission in Code mode
    const countBefore = await chat.sendMessage('write a file')
    await chat.waitForReply(30000, countBefore)

    // The PermissionApproval tool_use block should appear in the assistant message
    // It renders as a .chat-tool-call with data-category="permission" and auto-expands
    const permissionTool = page.locator('.chat-tool-call[data-category="permission"]')
    await expect(permissionTool.first()).toBeVisible({ timeout: 15000 })

    // The auto-expanded detail area should contain the permission approval view
    const permissionView = page.locator('.permission-approval-view')
    await expect(permissionView.first()).toBeVisible({ timeout: 5000 })

    // Permission header should be visible with warning icon and title
    const permissionHeader = page.locator('.permission-header').first()
    await expect(permissionHeader).toBeVisible()

    // Permission option buttons should be present
    const permissionBtns = page.locator('.permission-btn')
    const btnCount = await permissionBtns.count()
    expect(btnCount).toBeGreaterThanOrEqual(2) // At least Allow + Deny

    // Verify "Allow Once" button is present
    const allowOnceBtn = page.locator('.permission-btn-allow').first()
    await expect(allowOnceBtn).toBeVisible()
  })

  test('should approve permission and continue', async ({ page }) => {
    await warmUpAndSwitchToCodeMode(page)

    // Send a message — acp-mock will request permission
    const countBefore = await chat.sendMessage('do something')
    await chat.waitForReply(30000, countBefore)

    // Wait for permission card to appear
    const permissionView = page.locator('.permission-approval-view').first()
    await expect(permissionView).toBeVisible({ timeout: 15000 })

    // Intercept the permission respond API call
    const respondRequest = page.waitForRequest(
      req => req.url().includes('/api/ai/permission/respond') && req.method() === 'POST'
    )

    // Click the "Allow Once" button
    const allowBtn = page.locator('.permission-btn-allow').first()
    await expect(allowBtn).toBeVisible()
    await allowBtn.click()

    // Verify API request was sent
    const req = await respondRequest
    const body = req.postDataJSON()
    expect(body.toolCallId).toBeTruthy()
    expect(body.cancelled).toBeFalsy()

    // The permission view should show responded state
    await expect(permissionView).toHaveClass(/permission-responded/, { timeout: 5000 })

    // Wait for the full response to complete (permission was approved, agent continues)
    await expect(chat.sendButton).toBeVisible({ timeout: 30000 })
  })

  test('permission respond API should work directly', async ({ page }) => {
    // Warm up ACP connection
    await chat.sendAndAwaitACPReply('hi')

    // Test the API endpoint directly with a non-existent session/toolCallId
    // This should return 404 (permission not found) rather than 500
    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/permission/respond', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sessionId: 'non-existent-session',
          toolCallId: 'non-existent-tool-call',
          optionId: 'allow_once',
          cancelled: false,
        }),
      })
      return { status: resp.status, ok: resp.ok }
    })

    // Should get 404 (session not found or permission not found), not 500
    expect(result.status).toBe(404)

    // Test with missing required fields — should get 400
    const missingFieldsResult = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/permission/respond', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sessionId: '',
          toolCallId: '',
        }),
      })
      return { status: resp.status }
    })

    expect(missingFieldsResult.status).toBe(400)
  })
})

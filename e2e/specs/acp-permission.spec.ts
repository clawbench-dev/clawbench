import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { restoreNonBlockingMode } from '../helpers/agent-mode'
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

  // This spec deliberately switches the shared agent/session into Code mode,
  // where the mock blocks on a permission request. Cancel any parked turn and
  // restore the non-blocking mode so later specs are not affected
  // (see helpers/agent-mode.ts).
  test.afterAll(async () => {
    await restoreNonBlockingMode()
  })

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  /**
   * Helper: warm up ACP connection (bypass mode by default), then switch
   * to Code mode so that subsequent messages trigger permission requests.
   */
  async function warmUpAndSwitchToCodeMode(page: Page) {
    // Start from the non-blocking mode. The run shares one session across
    // specs, so a previous spec may have left it in Code/Plan — in which case
    // the warm-up message would block on a permission request forever.
    // Reload so the frontend re-reads the persisted mode (it sends its own
    // `modeId` on every chat POST).
    await restoreNonBlockingMode()
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    // First message warms up ACP connection (default mode is bypass-permissions)
    await chat.sendAndAwaitACPReply('hi')

    // Wait for ACP mode state to be available, then switch to Code mode
    await chat.waitForACPModeState()
    await chat.openModeMenu()
    await chat.selectMode('Code')

    // Wait for drawer to close (mode selection auto-closes it)
    await expect(chat.sessionSettingDrawer).not.toBeVisible({ timeout: 5000 })
  }

  test('should show permission approval card in non-bypass mode', async ({ page }) => {
    await warmUpAndSwitchToCodeMode(page)

    // Send another message — acp-mock will request permission in Code mode
    const countBefore = await chat.sendMessage('write a file')
    await chat.waitForReply(30000, countBefore)

    // The PermissionApproval block renders as an auto-expanded unified inline
    // card (`.chat-inline-card` with data-category="permission"), not a plain
    // `.chat-tool-call`.
    const permissionView = page.locator('.permission-approval-view')
    await expect(permissionView.first()).toBeVisible({ timeout: 15000 })

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

  /**
   * Cursor omits rawInput from session/request_permission even though it sent
   * it on the preceding tool_call (it encodes the target only in the request's
   * title). The approval card must still show WHAT the agent wants to do, so
   * the server recovers the input from the cached tool_call. The mock
   * reproduces that framing when the prompt carries its marker.
   */
  test('approval card shows the command/file when the permission request omits rawInput', async ({ page }) => {
    // Run this turn in a FRESH session rather than the one the earlier tests in
    // this serial file share. Each of those tests parks a blocked permission
    // turn on the shared session, and the leftover streaming state can swallow
    // this turn's tool frames (they arrive, but resolve to no streaming bubble
    // and are dropped), leaving the card unrendered. A fresh session starts with
    // an empty message list, so the marker turn renders deterministically.
    await chat.createSessionWithAgent('acp-mock')
    await chat.sendAndAwaitACPReply('hi')
    await chat.waitForACPModeState()
    await chat.openModeMenu()
    await chat.selectMode('Code')
    await expect(chat.sessionSettingDrawer).not.toBeVisible({ timeout: 5000 })

    const countBefore = await chat.sendMessage('write a file [mock:omit-perm-rawinput]')
    await chat.waitForReply(30000, countBefore)

    // Scope to the message waitForReply anchored on (index countBefore), NOT
    // `.last()`: a trailing bubble can appear after this one (e.g. a cancelled
    // sibling), and `.last()` would then miss the card. Indexing by the
    // pre-send count still keeps a leftover card from an earlier test in this
    // serial file from satisfying the assertion — that earlier card lives in an
    // earlier message.
    const targetMessage = page.locator('.chat-message.assistant').nth(countBefore)
    const permissionView = targetMessage.locator('.permission-approval-view').first()
    await expect(permissionView).toBeVisible({ timeout: 15000 })

    // The card body must carry the requested target (from the tool_call's
    // rawInput), not just the bare tool name.
    const detail = permissionView.locator('.permission-tool-detail code').first()
    await expect(detail).toBeVisible({ timeout: 5000 })
    await expect(detail).toContainText('main.go')

    // And the strip summary must not degrade to the bare tool name.
    const summary = targetMessage
      .locator('.chat-inline-card[data-tool-name="PermissionApproval"] .tool-summary')
      .first()
    await expect(summary).toContainText('main.go')
  })

  test('permission respond API should work directly', async ({ page }) => {
    // Restore the non-blocking mode: the previous test left the session in Code
    // mode, where the mock blocks on a permission request.
    await restoreNonBlockingMode()
    await page.reload()
    await page.waitForLoadState('domcontentloaded')
    await expect(chat.textarea).toBeVisible({ timeout: 10000 })

    // Warm up ACP connection
    await chat.sendAndAwaitACPReply('hi')

    // Test the API endpoint directly with a non-existent session.
    // The handler validates project ownership BEFORE looking up the permission,
    // and an unknown session resolves to no project — so the response is 403
    // (AccessDenied), not 404. The point of the assertion is that it is a
    // deliberate client error, never a 500.
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

    expect(result.status).toBe(403)

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

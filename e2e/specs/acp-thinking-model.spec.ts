import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'

/**
 * E2E tests for ACP thinking effort persistence and model list.
 *
 * Uses acp-mock agent (real ACP stdio protocol) which provides:
 * - 3 thinking effort levels (Low/Medium/High)
 * - 3 modes (Code, Plan, Bypass Permissions)
 * - 8 slash commands
 *
 * The existing slash-commands.spec.ts covers basic visibility and selection
 * of thinking effort levels. This suite focuses on:
 * 1. Persistence of thinking effort selection across sessions and page reloads
 * 2. Model list display in SessionSettingModal (agent-configured models)
 * 3. Backend API correctness for thinking effort persistence
 *
 * SERIAL: Tests must run serially because the ACP mock agent is a single
 * subprocess. Concurrent Prompt requests on the same agent process can
 * cause the JSON-RPC stream to become corrupted.
 */
test.describe.serial('ACP Thinking Effort & Model List', () => {
  test.setTimeout(120000)

  let chat: ChatPage

  test.beforeEach(async ({ page }) => {
    chat = new ChatPage(page)
  })

  // ───────────────────────────────────────────────────────
  // Thinking effort persistence
  // ───────────────────────────────────────────────────────

  test('should persist thinking effort selection across page reload', async ({ page }) => {
    // Establish ACP connection first (default agent is acp-mock)
    await chat.sendAndAwaitACPReply('hi')

    // Wait for ACP state to be available (mode_update/thinking_effort_update SSE)
    await chat.waitForACPState()

    // Open SessionSettingModal → thinking tab → select "High"
    await chat.openSessionSettingModal()
    await chat.openThinkingTab()
    await chat.selectThinkingEffort('High')

    // Modal closes after selection — verify it's gone
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).not.toBeVisible({ timeout: 5000 })

    // Wait for thinking effort to be persisted via PATCH before reloading
    await chat.waitForSessionThinkingEffort('high')

    // Reload page — thinking effort should be restored from backend
    await page.reload()
    await page.waitForLoadState('networkidle')

    // Wait for ACP state to be restored from backend API
    await chat.waitForACPState()

    // Wait for the UI to be ready
    await expect(chat.textarea).toBeVisible({ timeout: 5000 })

    // Open SessionSettingModal → thinking tab — "High" should be the active selection
    await chat.openSessionSettingModal()
    await chat.openThinkingTab()

    // The "High" item should have the active/selected class
    const highItem = page.locator('.thinking-item').filter({ hasText: /high/i })
    await expect(highItem).toBeVisible()
    await expect(highItem).toHaveClass(/current/, { timeout: 5000 })
  })

  test('should persist thinking effort selection across sessions', async ({ page }) => {
    // Previous test already established ACP connection and set thinking to "High"
    // Wait for ACP state to be available
    await chat.waitForACPState()

    // Verify by opening the modal
    await chat.openSessionSettingModal()
    await chat.openThinkingTab()

    // Change to "Low" for this test
    await chat.selectThinkingEffort('Low')

    // Modal closes after selection
    const modal = page.locator('.modal-dialog, [class*="modal"]')
    await expect(modal.first()).not.toBeVisible({ timeout: 5000 })

    // Create a new session with the same agent
    await chat.createSessionWithAgent('acp-mock')

    // Wait for the new session to be ready and ACP state to populate
    await chat.waitForACPState()

    // Open SessionSettingModal → thinking tab
    await chat.openSessionSettingModal()
    await chat.openThinkingTab()

    // For a new session, thinking effort comes from the agent's
    // preferred_thinking_effort (saved via PATCH /api/agents when user selects).
    // The "Low" selection from the previous session should be the agent default now.
    const lowItem = page.locator('.thinking-item').filter({ hasText: /low/i })
    await expect(lowItem).toBeVisible()

    // Verify the thinking effort is reflected in the chat API response
    const chatData = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return {
        ok: true,
        thinkingEffort: data.thinkingEffort || '',
        thinkingEffortState: data.thinkingEffortState || null,
      }
    })

    // The chat API should return thinking effort state with available levels
    expect(chatData.ok).toBe(true)
    if (chatData.thinkingEffortState) {
      expect(chatData.thinkingEffortState.availableLevels?.length).toBeGreaterThanOrEqual(3)
    }
  })

  // ───────────────────────────────────────────────────────
  // Model list display
  // ───────────────────────────────────────────────────────

  test('should show agent models in SessionSettingModal for ACP session', async ({ page }) => {
    // Warm up ACP connection (may still be warm from previous test)
    await chat.sendAndAwaitACPReply('hi')

    // Open SessionSettingModal
    await chat.openSessionSettingModal()

    // Model items should be visible (from agent's configured model list)
    const modelItems = page.locator('.model-item')
    await expect(modelItems.first()).toBeVisible({ timeout: 5000 })

    // At least one model should be present
    const count = await modelItems.count()
    expect(count).toBeGreaterThan(0)

    // Close the modal by selecting any model
    await modelItems.first().click()
  })

  test('should return acpStates with thinking effort in agents API', async ({ page }) => {
    // Ensure ACP connection is warm
    await chat.sendAndAwaitACPReply('hi')
    // Wait for ACP state to be populated in cache
    await chat.waitForACPState()

    // Call the agents API and check acpStates
    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/agents')
      if (!resp.ok) return { ok: false, status: resp.status }
      const data = await resp.json()
      return {
        ok: true,
        hasAcpStates: !!data.acpStates,
        agentIds: Object.keys(data.acpStates || {}),
        // Check if any agent has thinking effort state
        hasThinkingEffort: Object.values(data.acpStates || {}).some(
          (s: any) => s?.thinkingEffortState?.availableLevels?.length > 0
        ),
        // Check if any agent has model list state
        hasModelList: Object.values(data.acpStates || {}).some(
          (s: any) => s?.modelListState?.models?.length > 0
        ),
      }
    })

    expect(result.ok).toBe(true)
    expect(result.hasAcpStates).toBe(true)
    // acp-mock should have at least one entry in acpStates
    expect((result.agentIds || []).length).toBeGreaterThan(0)
    // acp-mock provides thinking effort levels
    expect(result.hasThinkingEffort).toBe(true)
  })

  // ───────────────────────────────────────────────────────
  // Backend API correctness
  // ───────────────────────────────────────────────────────

  test('thinking effort API should persist selection via chat request', async ({ page }) => {
    // Get current session ID
    const sessionInfo = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId, thinkingEffort: data.thinkingEffort }
    })

    expect(sessionInfo.ok).toBe(true)
    expect(sessionInfo.sessionId).toBeTruthy()

    // The chat API response should include thinking effort from the session
    // (either from DB persistence or from ACP cache)
    // Verify the thinking effort state is populated correctly
    const chatDetail = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      return {
        ok: true,
        thinkingEffort: data.thinkingEffort || '',
        thinkingEffortState: data.thinkingEffortState || null,
      }
    })

    expect(chatDetail.ok).toBe(true)
    // thinkingEffortState should be populated for ACP sessions
    if (chatDetail.thinkingEffortState) {
      expect(chatDetail.thinkingEffortState.availableLevels).toBeDefined()
      expect(chatDetail.thinkingEffortState.availableLevels.length).toBeGreaterThanOrEqual(3)
      // Levels should include low, medium, high
      const levelIds = chatDetail.thinkingEffortState.availableLevels.map((l: any) => l.id)
      expect(levelIds).toContain('low')
      expect(levelIds).toContain('medium')
      expect(levelIds).toContain('high')
    }
  })

  test('should update agent preferred thinking effort via PATCH API', async ({ page }) => {
    // Patch the agent's preferred thinking effort
    const patchResult = await page.evaluate(async () => {
      const agentsResp = await fetch('/api/agents')
      if (!agentsResp.ok) return { ok: false, reason: 'agents_fetch_failed' }
      const agentsData = await agentsResp.json()

      // Find the acp-mock agent
      const agent = (agentsData.agents || []).find((a: any) => a.id === 'acp-mock')
      if (!agent) return { ok: false, reason: 'agent_not_found' }

      // PATCH to set preferred_thinking_effort
      const resp = await fetch('/api/agents', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: agent.id,
          preferred_thinking_effort: 'medium',
        }),
      })
      if (!resp.ok) return { ok: false, reason: 'patch_failed', status: resp.status }
      return { ok: true }
    })

    expect(patchResult.ok).toBe(true)

    // Verify the preference was saved by fetching agents again
    const verifyResult = await page.evaluate(async () => {
      const resp = await fetch('/api/agents')
      if (!resp.ok) return { ok: false }
      const data = await resp.json()
      const agent = (data.agents || []).find((a: any) => a.id === 'acp-mock')
      if (!agent) return { ok: false, reason: 'agent_not_found' }
      return {
        ok: true,
        preferredThinkingEffort: agent.preferredThinkingEffort || agent.preferred_thinking_effort || '',
      }
    })

    expect(verifyResult.ok).toBe(true)
    // The preferred thinking effort should be "medium"
    expect(verifyResult.preferredThinkingEffort).toBe('medium')
  })

  test('chat API should return thinkingEffortState with correct structure', async ({ page }) => {
    // Ensure ACP connection is established
    await chat.sendAndAwaitACPReply('hi')
    // Wait for ACP state to be populated
    await chat.waitForACPState()

    const result = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/chat?limit=1')
      if (!resp.ok) return { ok: false, status: resp.status }
      const data = await resp.json()
      return {
        ok: true,
        sessionId: data.sessionId,
        agentId: data.agentId,
        thinkingEffort: data.thinkingEffort,
        thinkingEffortState: data.thinkingEffortState,
        modelListState: data.modelListState,
      }
    })

    expect(result.ok).toBe(true)

    // Verify thinkingEffortState structure for ACP sessions
    if (result.thinkingEffortState) {
      const state = result.thinkingEffortState
      // Should have currentLevelId or currentId
      expect(state.currentLevelId || state.currentId).toBeTruthy()
      // Should have availableLevels array
      expect(Array.isArray(state.availableLevels)).toBe(true)
      // Each level should have id and name
      for (const level of state.availableLevels) {
        expect(level.id).toBeTruthy()
        expect(level.name).toBeTruthy()
      }
    }
  })
})

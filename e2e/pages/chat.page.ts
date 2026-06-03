import { type Locator, type Page, expect } from '@playwright/test'

/**
 * Page Object Model for the Chat panel.
 *
 * Key selectors (from ChatInputBar.vue, ChatMessageItem.vue, ChatMessageList.vue):
 * - .chat-textarea       → message input textarea
 * - .chat-send-btn        → send button (hidden during loading)
 * - .chat-stop-btn        → stop/cancel button (visible during loading)
 * - .quick-send-title     → quick-send popup title
 * - .model-chip           → model selector chip
 * - .mode-chip            → ACP mode selector chip
 * - .chat-messages        → messages scroll container
 * - .chat-message.user    → user message
 * - .chat-message.assistant → AI assistant message
 * - .chat-action-btn      → session management buttons (sessions, new, delete, speech)
 * - .chat-attach-btn      → file attachment button
 * - .chat-file-attachment → attached file tag
 * - .summary-toggle-btn   → summary toggle button on assistant message
 * - .chat-thinking        → thinking block in assistant message
 * - .thinking-collapsed   → collapsed thinking block
 * - .thinking-header      → thinking block header (clickable to expand)
 */
export class ChatPage {
  readonly page: Page
  readonly textarea: Locator
  readonly sendButton: Locator
  readonly stopButton: Locator
  readonly messagesContainer: Locator
  readonly modelChip: Locator
  readonly modeChip: Locator

  constructor(page: Page) {
    this.page = page
    this.textarea = page.locator('.chat-textarea')
    this.sendButton = page.locator('.chat-send-btn')
    this.stopButton = page.locator('.chat-stop-btn')
    this.messagesContainer = page.locator('.chat-messages')
    this.modelChip = page.locator('.model-chip')
    this.modeChip = page.locator('.mode-chip')
  }

  /** Fill the textarea with text */
  async fillInput(text: string) {
    await this.textarea.fill(text)
  }

  /** Clear the textarea */
  async clearInput() {
    await this.textarea.clear()
  }

  /** Fill the textarea and click send */
  async sendMessage(text: string): Promise<number> {
    // Clear existing content and fill with new text
    await this.textarea.clear()
    await this.textarea.fill(text)
    // Wait for Vue's v-model to sync the filled value (auto-retries, no hardcoded sleep)
    await expect(this.textarea).toHaveValue(text)
    // Record assistant message count *before* sending to avoid stale matches
    const countBefore = await this.page.locator('.chat-message.assistant').count()
    await this.sendButton.click()
    return countBefore
  }

  /** Click send with empty input to open quick-send popup */
  async openQuickSendMenu() {
    await this.sendButton.click()
  }

  /** Wait for an assistant message to fully load (SSE stream reply with content).
   *  Tracks message count before the send so it waits for the *new* message,
   *  avoiding stale matches on pre-existing assistant messages. */
  async waitForReply(timeout = 15000, countBefore?: number): Promise<void> {
    // If caller didn't provide a count, determine it now
    if (countBefore === undefined) {
      countBefore = await this.page.locator('.chat-message.assistant').count()
    }
    // Wait for the new assistant message to appear
    const newMsg = this.page.locator('.chat-message.assistant').nth(countBefore)
    await expect(newMsg).toBeVisible({ timeout })
    // Wait for actual text content (SSE streams content incrementally)
    await expect(newMsg).toContainText(/.+/, { timeout })
  }

  /** Get the last user message element */
  getLastUserMessage(): Locator {
    return this.page.locator('.chat-message.user').last()
  }

  /** Get the last assistant message element */
  getLastAssistantMessage(): Locator {
    return this.page.locator('.chat-message.assistant').last()
  }

  /** Click the new session button (the "+" button in chat action row) */
  async createSession() {
    // The second .chat-action-btn is the "new session" button
    await this.page.locator('.chat-action-btn').nth(1).click()
  }

  /** Click the sessions list button (the first .chat-action-btn) */
  async openSessionList() {
    await this.page.locator('.chat-action-btn').first().click()
  }

  /**
   * Wait for ACP slash commands to be available.
   * Polls the /api/ai/commands endpoint until commands are returned.
   * Uses relative URL so the browser's auth cookies are included.
   */
  async waitForACPCommands(timeout = 20000): Promise<void> {
    const start = Date.now()
    while (Date.now() - start < timeout) {
      try {
        const result = await this.page.evaluate(async () => {
          const resp = await fetch('/api/ai/commands')
          if (!resp.ok) return { ok: false, count: 0 }
          const data = await resp.json()
          return { ok: true, count: data.commands?.length || 0 }
        })
        if (result.count > 0) return
      } catch {
        // Network error — server might not be ready
      }
      await this.page.waitForTimeout(500)
    }
    throw new Error(`ACP commands not available after ${timeout}ms`)
  }

  /**
   * Send a message and wait for the ACP reply.
   * Uses a longer timeout than waitForReply because ACP requires
   * spawning a subprocess and establishing a connection.
   * Also waits for the streaming to complete (send button reappears).
   */
  async sendAndAwaitACPReply(text: string, timeout = 30000): Promise<void> {
    const countBefore = await this.sendMessage(text)
    await this.waitForReply(timeout, countBefore)
    // Wait for streaming to complete — send button reappears when done
    await expect(this.sendButton).toBeVisible({ timeout: 10000 })
  }

  /**
   * Create a new session with a specific agent ID and switch the frontend to it.
   * Uses the window.__clawbench E2E test bridge to call the frontend's own
   * createSession function, which properly handles session switching, state
   * updates, and SSE reconnection — all without a page reload.
   *
   * Falls back to API + reload if the bridge is not available.
   */
  async createSessionWithAgent(agentId: string): Promise<void> {
    // Try the E2E test bridge first (no page reload needed)
    const bridgeAvailable = await this.page.evaluate(() => {
      return !!(window as any).__clawbench?.createSession
    })

    if (bridgeAvailable) {
      await this.page.evaluate(async (agentId) => {
        await (window as any).__clawbench.createSession(agentId)
      }, agentId)
      // Wait for the session switch to complete — textarea becomes ready
      await expect(this.textarea).toBeVisible({ timeout: 5000 })
      return
    }

    // Fallback: create session via API and reload page
    const baseURL = `http://localhost:${process.env.E2E_PORT || 20100}`
    const result = await this.page.evaluate(async ({ url, agentId }) => {
      const resp = await fetch(`${url}/api/ai/sessions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agentId }),
      })
      if (!resp.ok) return { ok: false, status: resp.status }
      const data = await resp.json()
      return { ok: true, sessionId: data.sessionId, backend: data.backend }
    }, { url: baseURL, agentId })

    if (!result.ok) {
      throw new Error(`Failed to create session with agent ${agentId}: ${result.status}`)
    }

    // Reload the page so the frontend picks up the new session
    await this.page.reload()
    await this.page.waitForLoadState('networkidle')

    // Wait for the textarea to be ready
    await expect(this.textarea).toBeVisible({ timeout: 5000 })
  }

  // ───────────────────────────────────────────────────────
  // ModelModal helpers
  // ───────────────────────────────────────────────────────

  /** Open ModelModal by clicking the model chip */
  async openModelModal(): Promise<void> {
    await this.modelChip.click()
    // Wait for modal to appear
    await expect(this.page.locator('.model-tab').first()).toBeVisible({ timeout: 5000 })
  }

  /** Switch to a model by name in ModelModal */
  async switchModel(modelName: string): Promise<void> {
    const item = this.page.locator('.model-item').filter({ hasText: modelName })
    await expect(item).toBeVisible({ timeout: 5000 })
    await item.click()
  }

  /** Search models in ModelModal */
  async searchModel(query: string): Promise<void> {
    const input = this.page.locator('.model-search-input')
    await expect(input).toBeVisible({ timeout: 5000 })
    await input.fill(query)
  }

  /** Switch to the thinking effort tab in ModelModal */
  async openThinkingTab(): Promise<void> {
    const thinkingTab = this.page.locator('.model-tab').filter({ hasText: /thinking|思考/i })
    await expect(thinkingTab).toBeVisible({ timeout: 5000 })
    await thinkingTab.click()
  }

  /** Select a thinking effort level by name */
  async selectThinkingEffort(name: string): Promise<void> {
    const item = this.page.locator('.thinking-item').filter({ hasText: new RegExp(name, 'i') })
    await expect(item).toBeVisible({ timeout: 5000 })
    await item.click()
  }

  /** Click the set-default star button on a thinking item by name */
  async setDefaultThinkingEffort(name: string): Promise<void> {
    const item = this.page.locator('.thinking-item').filter({ hasText: new RegExp(name, 'i') })
    await expect(item).toBeVisible({ timeout: 5000 })
    await item.locator('.set-default-btn').click()
  }

  /** Click the set-default star button on a model item by name */
  async setDefaultModel(modelName: string): Promise<void> {
    const item = this.page.locator('.model-item').filter({ hasText: modelName })
    await expect(item).toBeVisible({ timeout: 5000 })
    await item.locator('.set-default-btn').click()
  }

  // ───────────────────────────────────────────────────────
  // ACP Mode helpers
  // ───────────────────────────────────────────────────────

  /** Open the ACP mode selection menu by clicking the mode chip */
  async openModeMenu(): Promise<void> {
    await this.modeChip.click()
    // Wait for mode menu to appear
    await expect(this.page.locator('.mode-menu-item').first()).toBeVisible({ timeout: 5000 })
  }

  /** Select an ACP mode by name */
  async selectMode(modeName: string): Promise<void> {
    const item = this.page.locator('.mode-menu-item').filter({ hasText: new RegExp(modeName, 'i') })
    await expect(item).toBeVisible({ timeout: 5000 })
    await item.click()
  }
}

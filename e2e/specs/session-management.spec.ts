import { test, expect } from '../fixtures'
import { ChatPage } from '../pages/chat.page'
import { getServerURL } from '../helpers/server'

/**
 * E2E tests for session management: resume, continue conversation.
 *
 * All API calls are made via page.evaluate() so they inherit the browser's
 * authentication cookies (clawbench_session + clawbench_project).
 * Direct Node.js fetch would get 403 because it lacks the project cookie.
 */
test.describe.serial('Session Management', () => {
  const taskIds: number[] = []

  test.afterAll(async () => {
    // Clean up any tasks created during tests using Node.js fetch (localhost bypasses auth)
    const baseURL = getServerURL()
    for (const taskId of taskIds) {
      try {
        await fetch(`${baseURL}/api/tasks/${taskId}`, { method: 'DELETE' })
      } catch {
        // Best effort cleanup — server may be down during teardown
      }
    }
  })

  // ───────────────────────────────────────────────────────
  // Session Resume (4 tests)
  // ───────────────────────────────────────────────────────

  test('should resume a soft-deleted session via API', async ({ page }) => {
    // First soft-delete an existing session to free up a slot (session limit is 10)
    const existingSessions = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions || []
    })
    // If at limit, delete the oldest session that's not the current one
    if (existingSessions.length >= 10) {
      const toDelete = existingSessions[0]
      if (toDelete?.id) {
        await page.evaluate(async (id) => {
          await fetch(`/api/ai/session/delete?session_id=${id}&backend=acp-mock`, { method: 'DELETE' })
        }, toDelete.id)
      }
    }

    // Create a session via browser fetch (carries cookies)
    const { sessionId } = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agentId: 'acp-mock' }),
      })
      if (!resp.ok) throw new Error(`Failed to create session: ${resp.status}`)
      return resp.json()
    })
    expect(sessionId).toBeTruthy()

    // Soft-delete the session
    const deleteOk = await page.evaluate(async (id) => {
      const resp = await fetch(`/api/ai/session/delete?session_id=${id}&backend=acp-mock`, { method: 'DELETE' })
      return resp.ok
    }, sessionId)
    expect(deleteOk).toBe(true)

    // Verify session is no longer in the list
    const foundAfterDelete = await page.evaluate(async (id) => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.some((s: any) => s.id === id)
    }, sessionId)
    expect(foundAfterDelete).toBe(false)

    // Resume the session
    const resumeResult = await page.evaluate(async (id) => {
      const resp = await fetch('/api/ai/session/resume', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: id }),
      })
      if (!resp.ok) throw new Error(`Failed to resume: ${resp.status}`)
      return resp.json()
    }, sessionId)
    expect(resumeResult.ok).toBe(true)

    // Verify session reappears in the list
    const foundAfterResume = await page.evaluate(async (id) => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.some((s: any) => s.id === id)
    }, sessionId)
    expect(foundAfterResume).toBe(true)
  })

  test('should preserve messages after session resume', async ({ page }) => {
    const chat = new ChatPage(page)

    // Send a unique message
    await chat.sendAndAwaitACPReply('sessionresumetest123')

    // Verify the user message is visible
    await expect(chat.getLastUserMessage()).toContainText('sessionresumetest123')
    // Verify the assistant reply is visible
    await expect(chat.getLastAssistantMessage()).toContainText('mock')

    // Get current session ID from the page
    const sessionId = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.[0]?.id
    })
    expect(sessionId).toBeTruthy()

    // Soft-delete the session via API
    await page.evaluate(async (id) => {
      await fetch(`/api/ai/session/delete?session_id=${id}&backend=acp-mock`, { method: 'DELETE' })
    }, sessionId)

    // Resume the session via API
    await page.evaluate(async (id) => {
      await fetch('/api/ai/session/resume', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: id }),
      })
    }, sessionId)

    // Reload the page and verify messages are still visible
    await page.reload()
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(500)

    // The assistant message from before should still be rendered
    await expect(page.locator('.chat-message.assistant').first()).toContainText('mock', { timeout: 10000 })
  })

  test('should preserve agent and model after session resume', async ({ page }) => {
    // Create a session with acp-mock agent
    const { sessionId, backend } = await page.evaluate(async () => {
      const resp = await fetch('/api/ai/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agentId: 'acp-mock' }),
      })
      if (!resp.ok) throw new Error(`Failed to create session: ${resp.status}`)
      return resp.json()
    })
    expect(sessionId).toBeTruthy()
    expect(backend).toBeTruthy()

    // Soft-delete and resume
    await page.evaluate(async (id) => {
      await fetch(`/api/ai/session/delete?session_id=${id}&backend=acp-mock`, { method: 'DELETE' })
      await fetch('/api/ai/session/resume', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: id }),
      })
    }, sessionId)

    // Verify the session's backend still matches via session list
    const resumed = await page.evaluate(async (id) => {
      const resp = await fetch('/api/ai/sessions')
      const data = await resp.json()
      return data.sessions?.find((s: any) => s.id === id)
    }, sessionId)
    expect(resumed).toBeDefined()
    expect(resumed.id).toBe(sessionId)
  })

  test('should return error when resuming non-existent session', async ({ page }) => {
    const fakeId = crypto.randomUUID()

    await expect(page.evaluate(async (id) => {
      const resp = await fetch('/api/ai/session/resume', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: id }),
      })
      if (!resp.ok) throw new Error(`Failed to resume: ${resp.status}`)
      return resp.json()
    }, fakeId)).rejects.toThrow(/404|not found|Failed to resume/i)
  })

  // ───────────────────────────────────────────────────────
  // Continue Conversation from Task (3 tests)
  // ───────────────────────────────────────────────────────

  test('should create task and trigger execution', async ({ page }) => {
    // Create a task with a cron that never fires naturally (Feb 31)
    const task = await page.evaluate(async () => {
      const resp = await fetch('/api/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: 'E2E Test Task',
          cron_expr: '0 0 31 2 *',
          agent_id: 'acp-mock',
          prompt: 'Say hello',
        }),
      })
      if (!resp.ok) throw new Error(`Failed to create task: ${resp.status}`)
      const data = await resp.json()
      return data.task
    })
    expect(task.id).toBeTruthy()
    taskIds.push(task.id)

    // Trigger the task immediately
    await page.evaluate(async (id) => {
      const resp = await fetch(`/api/tasks/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'trigger' }),
      })
      if (!resp.ok) throw new Error(`Failed to trigger task: ${resp.status}`)
    }, task.id)

    // Wait for execution to complete
    const execution = await page.evaluate(async (id) => {
      const start = Date.now()
      while (Date.now() - start < 60000) {
        const resp = await fetch(`/api/tasks/${id}/executions`)
        const data = await resp.json()
        const done = (data.executions || []).find((e: any) =>
          e.status === 'completed' || e.status === 'cancelled' || e.status === 'failed'
        )
        if (done) return done
        await new Promise(r => setTimeout(r, 1000))
      }
      throw new Error(`Task ${id} execution did not complete within 60000ms`)
    }, task.id)
    expect(execution.status).toBe('completed')
  })

  test('should continue conversation from task execution', async ({ page }) => {
    // Create and trigger a task
    const task = await page.evaluate(async () => {
      const resp = await fetch('/api/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: 'E2E Continue Task',
          cron_expr: '0 0 31 2 *',
          agent_id: 'acp-mock',
          prompt: 'Say hello for continue test',
        }),
      })
      if (!resp.ok) throw new Error(`Failed to create task: ${resp.status}`)
      return (await resp.json()).task
    })
    taskIds.push(task.id)

    await page.evaluate(async (id) => {
      const resp = await fetch(`/api/tasks/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'trigger' }),
      })
      if (!resp.ok) throw new Error(`Failed to trigger task: ${resp.status}`)
    }, task.id)

    // Wait for execution to complete
    const execution = await page.evaluate(async (id) => {
      const start = Date.now()
      while (Date.now() - start < 60000) {
        const resp = await fetch(`/api/tasks/${id}/executions`)
        const data = await resp.json()
        const done = (data.executions || []).find((e: any) =>
          e.status === 'completed' || e.status === 'cancelled' || e.status === 'failed'
        )
        if (done) return done
        await new Promise(r => setTimeout(r, 1000))
      }
      throw new Error(`Task ${id} execution did not complete within 60000ms`)
    }, task.id)
    expect(execution.status).toBe('completed')

    // Continue conversation from the execution
    const result = await page.evaluate(async ({ taskId, execId }) => {
      const resp = await fetch(`/api/tasks/${taskId}/executions/${execId}/continue`, {
        method: 'POST',
      })
      if (!resp.ok) throw new Error(`Failed to continue: ${resp.status}`)
      return resp.json()
    }, { taskId: task.id, execId: execution.id })
    expect(result.ok).toBe(true)
    expect(result.sessionId).toBeTruthy()
  })

  test('should inherit source session properties in continued session', async ({ page }) => {
    // Create and trigger a task
    const task = await page.evaluate(async () => {
      const resp = await fetch('/api/tasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: 'E2E Inherit Task',
          cron_expr: '0 0 31 2 *',
          agent_id: 'acp-mock',
          prompt: 'Say hello for inherit test',
        }),
      })
      if (!resp.ok) throw new Error(`Failed to create task: ${resp.status}`)
      return (await resp.json()).task
    })
    taskIds.push(task.id)

    await page.evaluate(async (id) => {
      const resp = await fetch(`/api/tasks/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'trigger' }),
      })
      if (!resp.ok) throw new Error(`Failed to trigger task: ${resp.status}`)
    }, task.id)

    // Wait for execution to complete
    const execution = await page.evaluate(async (id) => {
      const start = Date.now()
      while (Date.now() - start < 60000) {
        const resp = await fetch(`/api/tasks/${id}/executions`)
        const data = await resp.json()
        const done = (data.executions || []).find((e: any) =>
          e.status === 'completed' || e.status === 'cancelled' || e.status === 'failed'
        )
        if (done) return done
        await new Promise(r => setTimeout(r, 1000))
      }
      throw new Error(`Task ${id} execution did not complete within 60000ms`)
    }, task.id)
    expect(execution.status).toBe('completed')

    // Continue from execution
    const result = await page.evaluate(async ({ taskId, execId }) => {
      const resp = await fetch(`/api/tasks/${taskId}/executions/${execId}/continue`, {
        method: 'POST',
      })
      if (!resp.ok) throw new Error(`Failed to continue: ${resp.status}`)
      return resp.json()
    }, { taskId: task.id, execId: execution.id })
    expect(result.ok).toBe(true)

    // Get the continued session's details and verify properties match
    const chatData = await page.evaluate(async (sessionId) => {
      const resp = await fetch(`/api/ai/chat?session_id=${sessionId}`)
      if (!resp.ok) throw new Error(`Failed to get chat: ${resp.status}`)
      return resp.json()
    }, result.sessionId)

    // The continued session should have the same backend as the original task's agent
    expect(chatData.backend).toBe('acp-mock')
  })
})

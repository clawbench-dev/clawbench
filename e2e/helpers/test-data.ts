import { getServerURL } from './server'

/**
 * Test data constants and seeding helpers for E2E tests.
 *
 * The Go server auto-creates an empty database on startup.
 * Data is seeded via API calls in test fixtures or test bodies.
 */

/** Default quick-send items for tests */
export const DEFAULT_QUICK_SEND_ITEMS = [
  { label: '继续', command: '继续' },
  { label: 'Review', command: '请 review 这个文件' },
  { label: 'Commit', command: '请提交当前的改动' },
]

/**
 * Seed quick-send items via API.
 * The server must be running and the user must be authenticated.
 */
export async function seedQuickSendItems(
  baseURL: string,
  items: { label: string; command: string }[] = DEFAULT_QUICK_SEND_ITEMS,
): Promise<void> {
  for (const item of items) {
    const response = await fetch(`${baseURL}/api/chat/quick-send`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(item),
    })
    if (!response.ok) {
      throw new Error(`Failed to seed quick-send item "${item.label}": ${response.status} ${response.statusText}`)
    }
  }
}

/**
 * Get all quick-send items via API.
 */
export async function getQuickSendItems(baseURL: string): Promise<{ id: number; label: string; command: string }[]> {
  const response = await fetch(`${baseURL}/api/chat/quick-send`)
  if (!response.ok) {
    throw new Error(`Failed to get quick-send items: ${response.status}`)
  }
  return response.json()
}

/**
 * Delete all quick-send items via API.
 */
export async function clearQuickSendItems(baseURL: string): Promise<void> {
  const items = await getQuickSendItems(baseURL)
  for (const item of items) {
    await fetch(`${baseURL}/api/chat/quick-send/${item.id}`, { method: 'DELETE' })
  }
}

// ───────────────────────────────────────────────────────
// Task (scheduled task) helpers
// ───────────────────────────────────────────────────────

/**
 * Create a scheduled task via API.
 */
export async function createTask(
  baseURL: string,
  opts: { name: string; cron_expr: string; agent_id: string; prompt: string },
): Promise<{ id: number; name: string }> {
  const resp = await fetch(`${baseURL}/api/tasks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(opts),
  })
  if (!resp.ok) throw new Error(`Failed to create task: ${resp.status}`)
  const data = await resp.json()
  return data.task
}

/**
 * Trigger a scheduled task immediately via API.
 */
export async function triggerTask(baseURL: string, taskId: number): Promise<void> {
  const resp = await fetch(`${baseURL}/api/tasks/${taskId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action: 'trigger' }),
  })
  if (!resp.ok) throw new Error(`Failed to trigger task ${taskId}: ${resp.status}`)
}

/**
 * Delete a scheduled task via API.
 */
export async function deleteTask(baseURL: string, taskId: number): Promise<void> {
  const resp = await fetch(`${baseURL}/api/tasks/${taskId}`, { method: 'DELETE' })
  if (!resp.ok) throw new Error(`Failed to delete task ${taskId}: ${resp.status}`)
}

/**
 * Get task executions via API.
 */
export async function getTaskExecutions(
  baseURL: string,
  taskId: number,
): Promise<Array<{ id: number; status: string; sessionId: string }>> {
  const resp = await fetch(`${baseURL}/api/tasks/${taskId}/executions`)
  if (!resp.ok) throw new Error(`Failed to get executions for task ${taskId}: ${resp.status}`)
  const data = await resp.json()
  return data.executions || []
}

/**
 * Wait for a task execution to reach a completed/cancelled/failed status.
 * Polls the executions endpoint until at least one execution matches.
 * Returns the execution object.
 */
export async function waitForTaskExecution(
  baseURL: string,
  taskId: number,
  timeout = 60000,
): Promise<{ id: number; status: string; sessionId: string }> {
  const start = Date.now()
  while (Date.now() - start < timeout) {
    const execs = await getTaskExecutions(baseURL, taskId)
    const done = execs.find(e => e.status === 'completed' || e.status === 'cancelled' || e.status === 'failed')
    if (done) return done
    await new Promise(r => setTimeout(r, 1000))
  }
  throw new Error(`Task ${taskId} execution did not complete within ${timeout}ms`)
}

/**
 * Continue conversation from a task execution via API.
 * Returns the new session ID.
 */
export async function continueFromExecution(
  baseURL: string,
  taskId: number,
  execId: number,
): Promise<{ ok: boolean; sessionId: string; alreadyExists: boolean }> {
  const resp = await fetch(`${baseURL}/api/tasks/${taskId}/executions/${execId}/continue`, {
    method: 'POST',
  })
  if (!resp.ok) throw new Error(`Failed to continue from execution ${execId}: ${resp.status}`)
  return resp.json()
}

// ───────────────────────────────────────────────────────
// Session helpers
// ───────────────────────────────────────────────────────

/**
 * Create a chat session via API.
 */
export async function createSession(
  baseURL: string,
  opts: { agentId?: string; title?: string } = {},
): Promise<{ sessionId: string; backend: string }> {
  const resp = await fetch(`${baseURL}/api/ai/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(opts),
  })
  if (!resp.ok) throw new Error(`Failed to create session: ${resp.status}`)
  return resp.json()
}

/**
 * Soft-delete a chat session via API.
 */
export async function deleteSession(baseURL: string, sessionId: string, backend = 'acp-mock'): Promise<void> {
  const resp = await fetch(`${baseURL}/api/ai/session/delete?session_id=${sessionId}&backend=${backend}`, {
    method: 'DELETE',
  })
  if (!resp.ok) throw new Error(`Failed to delete session ${sessionId}: ${resp.status}`)
}

/**
 * Resume a soft-deleted chat session via API.
 */
export async function resumeSession(baseURL: string, sessionId: string): Promise<{ ok: boolean }> {
  const resp = await fetch(`${baseURL}/api/ai/session/resume`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId }),
  })
  if (!resp.ok) throw new Error(`Failed to resume session ${sessionId}: ${resp.status}`)
  return resp.json()
}

/**
 * Get session list via API.
 */
export async function getSessions(baseURL: string): Promise<Array<{ id: string; title: string }>> {
  const resp = await fetch(`${baseURL}/api/ai/sessions`)
  if (!resp.ok) throw new Error(`Failed to get sessions: ${resp.status}`)
  const data = await resp.json()
  return data.sessions || []
}

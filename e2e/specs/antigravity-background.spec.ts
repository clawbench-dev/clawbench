import { randomUUID } from 'node:crypto'
import { resolve } from 'node:path'
import { test, expect } from '@playwright/test'
import { projectApiFetch } from '../helpers/auth'

test('Antigravity keeps a late permission and reply in the original turn', async () => {
  const send = (method: string, path: string, body?: unknown) => projectApiFetch(path, {
    method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  })
  const agent = await send('POST', '/api/agents', {
    name: `Async ACP ${randomUUID()}`,
    acp_command: `node ${resolve('e2e/fixtures/antigravity-background.mjs')}`,
  })
  expect(agent.ok).toBeTruthy()
  const { id: agentId } = await agent.json()
  let sessionId: string | undefined
  try {
    const session = await send('POST', '/api/ai/sessions', { agentId })
    expect(session.ok).toBeTruthy()
    const data = await session.json()
    sessionId = data.sessionId || data.id
    expect(sessionId).toBeTruthy()
    const chat = `/api/ai/chat?session_id=${sessionId}`
    expect((await send('POST', chat, { agentId, message: 'Run the background command' })).ok).toBeTruthy()
    await expect.poll(async () => (await send('POST', '/api/ai/permission/respond', {
      sessionId, toolCallId: 'follow-up', optionId: 'once',
    })).status).toBe(200)
    await expect.poll(async () => {
      const { messages } = await (await projectApiFetch(chat)).json()
      return messages.filter((m: { role: string }) => m.role === 'assistant')
        .map((m: { streaming: boolean; content: string }) => ({
          finalized: !m.streaming,
          text: JSON.parse(m.content).blocks.filter((b: { type: string }) => b.type === 'text')
            .map((b: { text: string }) => b.text).join(''),
        }))
    }).toEqual([{ finalized: true, text: expect.stringMatching(/Waiting for the command\.[\s\S]*Received the command result\./) }])
  } finally {
    if (sessionId) await send('DELETE', `/api/ai/session/destroy?session_id=${sessionId}`)
    await send('DELETE', '/api/agents', { id: agentId })
  }
})

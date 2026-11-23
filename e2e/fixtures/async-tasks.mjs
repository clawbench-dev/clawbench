import { createInterface } from 'node:readline'
import { randomUUID } from 'node:crypto'

const send = payload => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...payload }) + '\n')
const sessionId = randomUUID()
const update = update => send({ method: 'session/update', params: { sessionId, update } })
const text = text => update({ sessionUpdate: 'agent_message_chunk', content: { type: 'text', text } })
const start = (asyncTaskId, taskType) => update({ sessionUpdate: 'async_task_spawned', asyncTaskId, taskType })
const finish = asyncTaskId => update({ sessionUpdate: 'async_task_state_update', asyncTaskId, state: 'completed' })
for await (const line of createInterface({ input: process.stdin })) {
  const { id, method, params, result } = JSON.parse(line)
  switch (method) {
    case 'initialize':
      if (!params.clientCapabilities._meta?.jetbrains?.air?.capabilities.includes('asyncTasks')) process.exit(1)
      send({ id, result: { protocolVersion: 1, agentCapabilities: { _meta: { "clawbench/asyncTaskFollowup": true } }, authMethods: [] } })
      break
    case 'session/new':
      send({ id, result: { sessionId } })
      break
    case 'session/prompt':
      start('worker', 'shell')
      text('Task dispatched.')
      send({ id, result: { stopReason: 'end_turn' } })
      // The test controls completion independently of the prompt response.
      send({ id: 'gate', method: 'session/request_permission', params: {
        sessionId, toolCall: { toolCallId: 'worker-gate', title: 'Release worker', kind: 'execute' },
        options: [{ optionId: 'once', kind: 'allow_once', name: 'Release' }],
      } })
      break
    default:
      if (!method && id === 'gate' && result?.outcome?.optionId === 'once') {
        finish('worker')
        start('review', 'turn')
        text('Receipt verified.')
        finish('review')
      } else if (method && id !== undefined) send({ id, result: {} })
  }
}

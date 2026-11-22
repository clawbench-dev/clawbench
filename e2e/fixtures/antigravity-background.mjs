// Replay the native agy ACP sequence. No credentials or shell commands needed.
import { createInterface } from 'node:readline'
import { randomUUID } from 'node:crypto'

const send = msg => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...msg }) + '\n')
let sessionId
const update = update => send({ method: 'session/update', params: { sessionId, update } })
const text = text => update({ sessionUpdate: 'agent_message_chunk', content: { type: 'text', text } })
const usage = () => update({ sessionUpdate: 'usage_update', used: 12, size: 1024 })

for await (const line of createInterface({ input: process.stdin })) {
  const { id, method, params, result } = JSON.parse(line)
  if (method === 'initialize') {
    send({ id, result: { protocolVersion: 1, agentInfo: { name: 'Antigravity ACP Server', version: '1.5.14' }, agentCapabilities: {}, authMethods: [] } })
  } else if (method === 'session/new') {
    sessionId = randomUUID()
    send({ id, result: { sessionId } })
  } else if (method === 'session/prompt') {
    sessionId = params.sessionId
    update({ sessionUpdate: 'tool_call', toolCallId: 'async', title: 'Background command', kind: 'execute', status: 'in_progress', rawInput: { CommandLine: 'echo done' } })
    text('Waiting for the command.')
    usage()
    send({ id, result: { stopReason: 'end_turn' } })
    update({ sessionUpdate: 'tool_call_update', toolCallId: 'async', status: 'completed' })
    send({ id: 'approval', method: 'session/request_permission', params: {
      sessionId, toolCall: { toolCallId: 'follow-up', title: 'Inspect result', kind: 'read' },
      options: [{ optionId: 'once', kind: 'allow_once', name: 'Allow once' }],
    } })
  } else if (!method && id === 'approval') {
    text(result?.outcome?.optionId === 'once' ? 'Received the command result.' : 'Permission cancelled.')
    usage()
  } else if (method && id !== undefined) {
    send({ id, result: {} })
  }
}

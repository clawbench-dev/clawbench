#!/usr/bin/env node
// Minimal MCP stdio server used by TestACP_MCPProbe.
//
// It exposes exactly one tool whose output carries a fixed marker string, so a
// probe can prove the model actually reached the MCP server rather than merely
// that `session/new` accepted the config. Dependencies are deliberately zero:
// the probe must not fail because an npx download broke.
//
// Protocol: newline-delimited JSON-RPC 2.0 on stdin/stdout (MCP stdio
// transport). Only the four messages an agent needs are implemented.

import { createInterface } from 'node:readline'

const MARKER = process.env.MCP_PROBE_MARKER || 'CLAWBENCH_MCP_PROBE_OK'
const TOOL_NAME = process.env.MCP_PROBE_TOOL || 'clawbench_mcp_probe_ping'

function reply(id, result) {
  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id, result }) + '\n')
}

function replyError(id, code, message) {
  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id, error: { code, message } }) + '\n')
}

const rl = createInterface({ input: process.stdin, crlfDelay: Infinity })

rl.on('line', (line) => {
  const trimmed = line.trim()
  if (!trimmed) return

  let msg
  try {
    msg = JSON.parse(trimmed)
  } catch {
    return
  }

  // Notifications carry no id and expect no response.
  if (msg.id === undefined || msg.id === null) return

  switch (msg.method) {
    case 'initialize':
      reply(msg.id, {
        protocolVersion: msg.params?.protocolVersion ?? '2024-11-05',
        capabilities: { tools: {} },
        serverInfo: { name: 'clawbench-mcp-probe', version: '1.0.0' },
      })
      return

    case 'tools/list':
      reply(msg.id, {
        tools: [
          {
            name: TOOL_NAME,
            description:
              'Probe tool. Calling it returns a fixed marker string. ' +
              'It takes no arguments and has no side effects.',
            inputSchema: { type: 'object', properties: {}, additionalProperties: false },
          },
        ],
      })
      return

    case 'tools/call':
      if (msg.params?.name !== TOOL_NAME) {
        replyError(msg.id, -32602, `unknown tool: ${msg.params?.name}`)
        return
      }
      reply(msg.id, {
        content: [{ type: 'text', text: MARKER }],
        isError: false,
      })
      return

    case 'ping':
      reply(msg.id, {})
      return

    default:
      replyError(msg.id, -32601, `method not found: ${msg.method}`)
  }
})

rl.on('close', () => process.exit(0))

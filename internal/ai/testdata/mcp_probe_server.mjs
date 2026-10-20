#!/usr/bin/env node
// Minimal MCP server used by TestACP_MCPProbe.
//
// It exposes exactly one tool whose output carries a fixed marker string, so a
// probe can prove the model actually reached the MCP server rather than merely
// that `session/new` accepted the config. Dependencies are deliberately zero:
// the probe must not fail because an npx download broke.
//
// Two transports, because the probe has to cover both:
//
//   node mcp_probe_server.mjs                  stdio (newline-delimited JSON-RPC)
//   node mcp_probe_server.mjs --http [port]    HTTP on 127.0.0.1, prints
//                                              "PORT=<n>" on stdout
//
// The HTTP mode serves BOTH shapes agents use, because they disagree:
//
//   POST /mcp            Streamable HTTP — JSON-RPC in, JSON out. Also answers
//                        on GET with a short-lived SSE stream.
//   GET  /sse            Legacy HTTP+SSE — opens the event stream and announces
//   POST /messages       the POST endpoint via an `endpoint` event.
//
// Supporting both means a failure is attributable to the agent, not to our
// fixture picking the wrong dialect.

import { createInterface } from 'node:readline'
import { createServer } from 'node:http'

const MARKER = process.env.MCP_PROBE_MARKER || 'CLAWBENCH_MCP_PROBE_OK'
const TOOL_NAME = process.env.MCP_PROBE_TOOL || 'clawbench_mcp_probe_ping'

function result(id, payload) {
  return { jsonrpc: '2.0', id, result: payload }
}

function errorResult(id, code, message) {
  return { jsonrpc: '2.0', id, error: { code, message } }
}

// handle returns the response object, or null for notifications (no id).
function handle(msg) {
  if (!msg || msg.id === undefined || msg.id === null) return null

  switch (msg.method) {
    case 'initialize':
      return result(msg.id, {
        protocolVersion: msg.params?.protocolVersion ?? '2024-11-05',
        capabilities: { tools: {} },
        serverInfo: { name: 'clawbench-mcp-probe', version: '1.0.0' },
      })

    case 'tools/list':
      return result(msg.id, {
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

    case 'tools/call':
      if (msg.params?.name !== TOOL_NAME) {
        return errorResult(msg.id, -32602, `unknown tool: ${msg.params?.name}`)
      }
      return result(msg.id, {
        content: [{ type: 'text', text: MARKER }],
        isError: false,
      })

    case 'ping':
      return result(msg.id, {})

    default:
      return errorResult(msg.id, -32601, `method not found: ${msg.method}`)
  }
}

// ── stdio transport ────────────────────────────────────────────────────────

function runStdio() {
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
    const resp = handle(msg)
    if (resp) process.stdout.write(JSON.stringify(resp) + '\n')
  })
  rl.on('close', () => process.exit(0))
}

// ── HTTP transports ────────────────────────────────────────────────────────

// sessionId -> SSE response object (legacy HTTP+SSE transport)
const sseSessions = new Map()
let nextSession = 1

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')))
    req.on('error', reject)
  })
}

function writeJSON(res, payload) {
  const body = JSON.stringify(payload)
  res.writeHead(200, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(body),
  })
  res.end(body)
}

async function runHTTP(port) {
  const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://127.0.0.1')

    // Streamable HTTP: POST /mcp → single JSON response.
    if (req.method === 'POST' && (url.pathname === '/mcp' || url.pathname === '/')) {
      let msg
      try {
        msg = JSON.parse(await readBody(req))
      } catch {
        res.writeHead(400).end('bad json')
        return
      }
      const resp = handle(msg)
      if (!resp) {
        res.writeHead(202).end()
        return
      }
      writeJSON(res, resp)
      return
    }

    // Legacy HTTP+SSE: GET /sse opens the stream.
    if (req.method === 'GET' && url.pathname === '/sse') {
      const sessionId = String(nextSession++)
      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      })
      sseSessions.set(sessionId, res)
      // The client must POST JSON-RPC to this endpoint.
      res.write(`event: endpoint\ndata: /messages?sessionId=${sessionId}\n\n`)
      req.on('close', () => sseSessions.delete(sessionId))
      return
    }

    // Legacy HTTP+SSE: POST /messages, answer on the open stream.
    if (req.method === 'POST' && url.pathname === '/messages') {
      const sessionId = url.searchParams.get('sessionId')
      const stream = sseSessions.get(sessionId)
      if (!stream) {
        res.writeHead(404).end('unknown session')
        return
      }
      let msg
      try {
        msg = JSON.parse(await readBody(req))
      } catch {
        res.writeHead(400).end('bad json')
        return
      }
      const resp = handle(msg)
      if (resp) stream.write(`event: message\ndata: ${JSON.stringify(resp)}\n\n`)
      res.writeHead(202).end()
      return
    }

    // Streamable HTTP GET: a stream that only carries server-initiated messages.
    // This probe never initiates, so an empty stream is correct; it exists so a
    // client that insists on opening it does not treat 405 as a failure.
    if (req.method === 'GET' && url.pathname === '/mcp') {
      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      })
      res.write(': keep-alive\n\n')
      return
    }

    res.writeHead(404).end('not found')
  })

  server.listen(port, '127.0.0.1', () => {
    // The probe reads this line to learn the actual port (port 0 = ephemeral).
    process.stdout.write(`PORT=${server.address().port}\n`)
  })
}

if (process.argv.includes('--http')) {
  const idx = process.argv.indexOf('--http')
  const raw = process.argv[idx + 1]
  const port = raw && /^\d+$/.test(raw) ? Number(raw) : 0
  runHTTP(port)
} else {
  runStdio()
}

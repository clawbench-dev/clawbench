import { describe, expect, it } from 'vitest'
import { isValidMcpName, parseMcpImport } from '@/utils/mcpImport'

function entriesOf(text: string) {
  const result = parseMcpImport(text)
  if (!result.ok) throw new Error(`expected a successful parse, got ${result.error}`)
  return result
}

describe('parseMcpImport', () => {
  it('reads the mcpServers wrapper used by Claude and Cursor', () => {
    const { entries, skipped } = entriesOf(JSON.stringify({
      mcpServers: {
        github: {
          command: 'npx',
          args: ['-y', '@modelcontextprotocol/server-github'],
          env: { GITHUB_TOKEN: 'abc' },
        },
        remote: {
          type: 'sse',
          url: 'https://example.com/sse',
          headers: { Authorization: 'Bearer x' },
        },
      },
    }))

    expect(skipped).toBe(0)
    expect(entries).toHaveLength(2)
    expect(entries[0].server).toEqual({
      name: 'github',
      transport: 'stdio',
      command: 'npx',
      args: ['-y', '@modelcontextprotocol/server-github'],
      env: { GITHUB_TOKEN: 'abc' },
    })
    expect(entries[1].server).toEqual({
      name: 'remote',
      transport: 'sse',
      url: 'https://example.com/sse',
      headers: { Authorization: 'Bearer x' },
    })
    expect(entries.every((entry) => entry.issue === undefined)).toBe(true)
  })

  it('reads a bare map of servers and a single server object', () => {
    const map = entriesOf('{"time": {"command": "uvx", "args": ["mcp-server-time"]}}')
    expect(map.entries).toHaveLength(1)
    expect(map.entries[0].server.name).toBe('time')

    const single = entriesOf('{"name": "time", "command": "uvx"}')
    expect(single.entries).toHaveLength(1)
    expect(single.entries[0].server.name).toBe('time')
  })

  it('strips a markdown code fence around the pasted JSON', () => {
    const { entries } = entriesOf('```json\n{"mcpServers": {"a": {"command": "node"}}}\n```')
    expect(entries).toHaveLength(1)
    expect(entries[0].server.name).toBe('a')
  })

  it('keeps a server literally named "servers" as a server', () => {
    const { entries } = entriesOf('{"servers": {"command": "node"}}')
    expect(entries).toHaveLength(1)
    expect(entries[0].server.name).toBe('servers')
    expect(entries[0].server.command).toBe('node')
  })

  it('skips entries disabled in the source config', () => {
    const { entries, skipped } = entriesOf(JSON.stringify({
      mcpServers: {
        on: { command: 'node' },
        off: { command: 'node', disabled: true },
        offNumeric: { command: 'node', disabled: 1 },
        offEnabled: { command: 'node', enabled: false },
      },
    }))
    expect(entries.map((entry) => entry.server.name)).toEqual(['on'])
    expect(skipped).toBe(3)
  })

  it('counts values that are not definitions instead of importing them', () => {
    const { entries, skipped } = entriesOf('{"mcpServers": {"a": {"command": "node"}, "b": "oops"}}')
    expect(entries).toHaveLength(1)
    expect(skipped).toBe(1)
  })

  it('infers the transport from the shape when no type is given', () => {
    const { entries } = entriesOf(JSON.stringify({
      mcpServers: {
        cmd: { command: 'node' },
        web: { url: 'https://example.com/mcp' },
        legacy: { url: 'https://example.com/sse' },
      },
    }))
    expect(entries[0].server.transport).toBe('stdio')
    expect(entries[1].server.transport).toBe('http')
    // Guessing http for an SSE endpoint would save fine and never connect.
    expect(entries[2].server.transport).toBe('sse')
  })

  it('maps streamable-http onto http and rejects unknown transports', () => {
    const ok = entriesOf('{"a": {"type": "streamable-http", "url": "https://x.test/mcp"}}')
    expect(ok.entries[0].server.transport).toBe('http')

    const bad = entriesOf('{"a": {"type": "carrier-pigeon", "url": "https://x.test/mcp"}}')
    expect(bad.entries[0].issue).toBe('bad-transport')
  })

  it('splits a string args value and stringifies env values', () => {
    const { entries } = entriesOf('{"a": {"command": "npx", "args": "-y  some-server", "env": {"PORT": 8080, "DEBUG": true}}}')
    expect(entries[0].server.args).toEqual(['-y', 'some-server'])
    expect(entries[0].server.env).toEqual({ PORT: '8080', DEBUG: 'true' })
  })

  it('drops fields it does not store', () => {
    const { entries } = entriesOf('{"a": {"command": "node", "autoApprove": ["search"], "timeout": 30}}')
    expect(entries[0].server).toEqual({ name: 'a', transport: 'stdio', command: 'node' })
  })

  it('flags entries the server would reject', () => {
    const { entries } = entriesOf(JSON.stringify({
      mcpServers: {
        'bad name': { command: 'node' },
        'no-command': {},
        'no-url': { type: 'http' },
        'ftp-url': { url: 'ftp://example.com/x' },
      },
    }))
    expect(entries.map((entry) => entry.issue)).toEqual([
      'invalid-name',
      'missing-command',
      'missing-url',
      'bad-url',
    ])
  })

  it('reports a single server object without a name', () => {
    const { entries } = entriesOf('{"command": "node"}')
    expect(entries[0].issue).toBe('empty-name')
  })

  it('accepts an empty wrapper as zero entries', () => {
    const { entries, skipped } = entriesOf('{"mcpServers": {}}')
    expect(entries).toEqual([])
    expect(skipped).toBe(0)
  })

  it('rejects text that is not JSON or not an MCP config', () => {
    expect(parseMcpImport('')).toEqual({ ok: false, error: 'invalid-json' })
    expect(parseMcpImport('not json')).toEqual({ ok: false, error: 'invalid-json' })
    expect(parseMcpImport('[1, 2]')).toEqual({ ok: false, error: 'unsupported-shape' })
    expect(parseMcpImport('{"foo": 1}')).toEqual({ ok: false, error: 'unsupported-shape' })
  })
})

describe('isValidMcpName', () => {
  it('matches the server-side character set', () => {
    expect(isValidMcpName('my_server-1')).toBe(true)
    expect(isValidMcpName('my.server')).toBe(false)
    expect(isValidMcpName('my server')).toBe(false)
  })
})

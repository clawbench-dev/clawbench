import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import http from 'node:http'
import type { AddressInfo } from 'node:net'

// vi.mock factories are hoisted; keep their inputs in vi.hoisted.
const h = vi.hoisted(() => ({
  userDataDir: '',
  serverUrl: '',
  cookies: [] as Array<{ name: string; value: string }>,
}))

vi.mock('electron', () => ({
  app: { getPath: () => h.userDataDir },
  session: {
    defaultSession: {
      cookies: { get: async () => h.cookies },
    },
  },
}))

vi.mock('./store', () => ({
  getStore: () => ({ get: (k: string) => (k === 'serverUrl' ? h.serverUrl : undefined) }),
}))

import {
  record,
  recordError,
  startClientLog,
  stopClientLog,
  flushOnShutdown,
  _resetForTesting,
  _bufferForTesting,
  _flushLocalForTesting,
} from './clientLog'

/** Start a throwaway HTTP server that records every /api/client-log request. */
function startServer(): Promise<{ url: string; requests: Array<{ cookie?: string; body: string }>; close: () => Promise<void> }> {
  const requests: Array<{ cookie?: string; body: string }> = []
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      let body = ''
      req.on('data', (c) => { body += c })
      req.on('end', () => {
        requests.push({ cookie: req.headers.cookie, body })
        res.writeHead(200, { 'Content-Type': 'application/json' })
        res.end('{}')
      })
    })
    srv.listen(0, '127.0.0.1', () => {
      const port = (srv.address() as AddressInfo).port
      resolve({
        url: `http://127.0.0.1:${port}`,
        requests,
        close: () => new Promise<void>((r) => srv.close(() => r())),
      })
    })
  })
}

function readLocalLog(): string {
  const p = path.join(h.userDataDir, 'desktop.log')
  return fs.existsSync(p) ? fs.readFileSync(p, 'utf8') : ''
}

/** Minimal WebContents stand-in that lets a test fire console-message. */
function fakeWebContents() {
  const listeners: Array<(e: unknown, level: number, message: string) => void> = []
  const wc = {
    on: (_ev: string, cb: (e: unknown, level: number, message: string) => void) => { listeners.push(cb) },
    removeListener: () => { listeners.length = 0 },
  } as unknown as Electron.WebContents
  return { listeners, wc }
}

describe('clientLog (Electron main process)', () => {
  beforeEach(() => {
    h.userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'cb-clientlog-'))
    h.serverUrl = ''
    h.cookies = []
    _resetForTesting()
  })

  afterEach(() => {
    stopClientLog(null)
    fs.rmSync(h.userDataDir, { recursive: true, force: true })
  })

  it('writes to desktop.log once capture is armed', async () => {
    // desktop.log is gated by logCapture, matching Android (capture off => only
    // the platform's own sink) and the JS HTTP relay. Capture on is the state
    // in which the shell is expected to leave a local record.
    startClientLog(null)
    record('I', 'Main', 'hello')
    await _flushLocalForTesting()
    expect(readLocalLog()).toContain('hello')
    expect(readLocalLog()).toContain('[I] Main:')
  })

  it('writes nothing locally while capture is off', async () => {
    // Guards the gate itself: an unarmed relay must not quietly accumulate a
    // file the user never asked for.
    record('I', 'Main', 'hello')
    await _flushLocalForTesting()
    expect(readLocalLog()).toBe('')
  })

  it('records the local file with a real ISO timestamp and level letter', async () => {
    startClientLog(null)
    record('W', 'Tunnel', 'ssh dropped')
    await _flushLocalForTesting()
    const line = readLocalLog().trim()
    expect(line).toMatch(/^\d{4}-\d{2}-\d{2}T[\d:.]+Z \[W\] Tunnel: ssh dropped$/)
  })

  it('flattens newlines so one entry cannot forge extra lines', async () => {
    // Mirrors the server's escaping rationale: a raw \n in Msg would split the
    // record and let a logged object inject additional log lines.
    startClientLog(null)
    record('I', 'Main', 'first\n2020-01-01T00:00:00.000 [E] Fake: injected')
    await _flushLocalForTesting()
    const content = readLocalLog()
    expect(content.trim().split('\n')).toHaveLength(1)
    expect(content).toContain('first\\n')
  })

  it('serializes an Error via recordError using its stack', async () => {
    startClientLog(null)
    recordError('Main', new Error('boom'))
    await _flushLocalForTesting()
    const content = readLocalLog()
    expect(content).toContain('boom')
    expect(content).toContain('[E] Main:')
  })

  it('serializes non-Error throwables from recordError', async () => {
    startClientLog(null)
    recordError('Main', { code: 'ECONNRESET' })
    await _flushLocalForTesting()
    expect(readLocalLog()).toContain('ECONNRESET')
  })

  it('buffers entries while disarmed instead of posting them', async () => {
    const srv = await startServer()
    h.serverUrl = srv.url
    h.cookies = [{ name: 'clawbench_session', value: 'tok' }]

    record('I', 'Main', 'before arming')
    await flushOnShutdown() // explicit flush attempt while still disarmed
    expect(srv.requests).toHaveLength(0)

    await srv.close()
  })

  it('POSTs to /api/client-log with source=electron once armed', async () => {
    const srv = await startServer()
    h.serverUrl = srv.url
    h.cookies = [{ name: 'clawbench_session', value: 'tok' }]

    startClientLog(null)
    record('E', 'Main', 'shell error')
    await flushOnShutdown()

    expect(srv.requests).toHaveLength(1)
    const body = JSON.parse(srv.requests[0].body)
    expect(body.entries).toHaveLength(1)
    expect(body.entries[0]).toMatchObject({ level: 'E', tag: 'Main', msg: 'shell error', source: 'electron' })

    await srv.close()
  })

  it('authenticates with the session cookie', async () => {
    const srv = await startServer()
    h.serverUrl = srv.url
    h.cookies = [{ name: 'clawbench_session', value: 'secret-token' }]

    startClientLog(null)
    record('I', 'Main', 'x')
    await flushOnShutdown()

    expect(srv.requests[0].cookie).toBe('clawbench_session=secret-token')

    await srv.close()
  })

  it('finds the port-scoped cookie name', async () => {
    // The server names the cookie cb<port>_clawbench_session unless the port is
    // 20000, so matching only the bare name would drop auth on any other port.
    const srv = await startServer()
    h.serverUrl = srv.url
    h.cookies = [{ name: 'cb20300_clawbench_session', value: 'scoped' }]

    startClientLog(null)
    record('I', 'Main', 'x')
    await flushOnShutdown()

    expect(srv.requests[0].cookie).toBe('cb20300_clawbench_session=scoped')

    await srv.close()
  })

  it('does not post when there is no session cookie (not logged in)', async () => {
    const srv = await startServer()
    h.serverUrl = srv.url
    h.cookies = [] // not logged in yet

    startClientLog(null)
    record('I', 'Main', 'x')
    await flushOnShutdown()

    expect(srv.requests).toHaveLength(0)

    await srv.close()
  })

  it('does not post when no server is configured', async () => {
    h.serverUrl = ''
    startClientLog(null)
    record('I', 'Main', 'x')
    // Must not throw despite having nowhere to send.
    await expect(flushOnShutdown()).resolves.toBeUndefined()
  })

  it('survives an unreachable server without throwing', async () => {
    // Port 1 is never listening; the relay must swallow the failure.
    h.serverUrl = 'http://127.0.0.1:1'
    h.cookies = [{ name: 'clawbench_session', value: 'tok' }]

    startClientLog(null)
    record('I', 'Main', 'x')
    await expect(flushOnShutdown()).resolves.toBeUndefined()
  })

  it('survives a malformed server URL', async () => {
    h.serverUrl = 'not a url'
    h.cookies = [{ name: 'clawbench_session', value: 'tok' }]
    startClientLog(null)
    record('I', 'Main', 'x')
    await expect(flushOnShutdown()).resolves.toBeUndefined()
  })

  it('caps the buffer so a long disarmed stretch cannot grow unbounded', () => {
    startClientLog(null)
    for (let i = 0; i < 500; i++) record('I', 'Main', `line ${i}`)
    const buf = _bufferForTesting()
    expect(buf.length).toBeLessThanOrEqual(200)
    // Keeps the NEWEST entries — the ones nearest a failure.
    expect(buf[buf.length - 1].msg).toBe('line 499')
  })

  it('drops the buffer when capture is stopped, without posting it', async () => {
    const srv = await startServer()
    h.serverUrl = srv.url
    h.cookies = [{ name: 'clawbench_session', value: 'tok' }]

    startClientLog(null)
    record('I', 'Main', 'pending')
    stopClientLog(null)
    await flushOnShutdown()

    expect(srv.requests).toHaveLength(0)

    await srv.close()
  })

  it('mirrors renderer console output into desktop.log while armed', async () => {
    // Regression: this listener is the reason desktop.log used to stay empty —
    // appLog skipped console on all native hosts, so console-message never
    // fired. Now desktop keeps console on (see appLog.emit).
    const { listeners, wc } = fakeWebContents()

    startClientLog(wc)
    expect(listeners).toHaveLength(1)

    listeners[0](null, 3, '[ChatStream] something failed')
    await _flushLocalForTesting()
    const content = readLocalLog()
    expect(content).toContain('[E] Renderer: [ChatStream] something failed')
  })

  it('maps console levels onto the appLog letters', async () => {
    const { listeners, wc } = fakeWebContents()

    startClientLog(wc)
    listeners[0](null, 0, 'd')
    listeners[0](null, 1, 'i')
    listeners[0](null, 2, 'w')
    listeners[0](null, 3, 'e')
    await _flushLocalForTesting()

    const lines = readLocalLog().trim().split('\n')
    expect(lines[0]).toContain('[D] Renderer: d')
    expect(lines[1]).toContain('[I] Renderer: i')
    expect(lines[2]).toContain('[W] Renderer: w')
    expect(lines[3]).toContain('[E] Renderer: e')
  })
})

import net from 'node:net'
import http from 'node:http'
import https from 'node:https'
import { Client } from 'ssh2'
import { getPassword } from './secrets'
import { getStore } from './store'

export type TunnelErrorType = 'auth' | 'network' | 'hostkey' | 'unknown' | ''

export interface TunnelState {
  connected: boolean
  error: string
  errorType: TunnelErrorType
  forwarded: Map<number, { targetPort: number; host: string }>
}

const state: TunnelState = { connected: false, error: '', errorType: '', forwarded: new Map() }
let client: Client | null = null

export function isTunnelConnected(): boolean { return state.connected }
export function getTunnelError(): string { return state.error }
export function getTunnelErrorType(): TunnelErrorType { return state.errorType }
export function getForwardedPorts(): Array<{ port: number; host: string }> {
  return [...state.forwarded.entries()].map(([localPort, v]) => ({ port: localPort, host: v.host }))
}

function classifyError(err: Error & { level?: string; code?: string }): TunnelErrorType {
  if (err.level === 'client-authentication') return 'auth'
  if (err.code === 'ENOTFOUND' || err.code === 'ECONNREFUSED' || err.code === 'ETIMEDOUT') return 'network'
  if (err.level === 'client-timeout') return 'hostkey'
  return 'unknown'
}

export function connectTunnel(host: string, port: number, username: string): Promise<boolean> {
  return new Promise((resolve) => {
    disconnectTunnel()
    state.error = ''
    state.errorType = ''
    client = new Client()
    client
      .on('ready', () => {
        state.connected = true
        state.error = ''
        state.errorType = ''
        resolve(true)
      })
      .on('error', (err: Error) => {
        state.connected = false
        state.error = err.message
        state.errorType = classifyError(err)
        resolve(false)
      })
      .on('close', () => {
        state.connected = false
        client = null
      })
      .connect({ host, port, username, password: getPassword() })
  })
}

export function disconnectTunnel(): void {
  if (client) {
    try { client.end() } catch { /* ignore */ }
    client = null
  }
  state.connected = false
  state.forwarded.clear()
}

/** Add a local port forward: localhost:localPort → host:targetPort via the SSH channel. */
export async function addForwardedPort(localPort: number, targetPort: number, host: string): Promise<boolean> {
  if (!state.connected) {
    const ok = await ensureTunnel()
    if (!ok) return false
  }
  return new Promise((resolve) => {
    if (!client || !state.connected) { resolve(false); return }
    const server = net.createServer((socket) => {
      if (!client) { socket.destroy(); return }
      client.forwardOut('127.0.0.1', 0, host || 'localhost', targetPort, (err, stream) => {
        if (err) { socket.destroy(); return }
        socket.pipe(stream).pipe(socket)
      })
    })
    server.listen(localPort, '127.0.0.1', () => {
      state.forwarded.set(localPort, { targetPort, host })
      resolve(true)
    })
    server.on('error', () => resolve(false))
  })
}

export function removeForwardedPort(localPort: number): void {
  state.forwarded.delete(localPort)
}

export function testPortReachable(localPort: number): Promise<boolean> {
  return new Promise((resolve) => {
    const sock = net.connect({ host: '127.0.0.1', port: localPort })
    sock.on('connect', () => { sock.destroy(); resolve(true) })
    sock.on('error', () => resolve(false))
    setTimeout(() => { sock.destroy(); resolve(false) }, 500)
  })
}

const DEFAULT_SSH_USER = 'clawbench'

interface SshInfo {
  enabled: boolean
  port: number
  username: string
}

/** Fetch SSH connection info from the server's public /api/ssh/info endpoint. */
function fetchSshInfo(serverUrl: string): Promise<SshInfo | null> {
  return new Promise((resolve) => {
    try {
      const url = new URL(serverUrl)
      const scheme = url.protocol.replace(':', '')
      const httpPort = url.port || (scheme === 'https' ? '443' : '80')
      const infoUrl = `${scheme}://${url.hostname}:${httpPort}/api/ssh/info`
      const lib = scheme === 'https' ? https : http
      const req = lib.get(infoUrl, { rejectUnauthorized: false }, (res) => {
        let data = ''
        res.on('data', c => { data += c })
        res.on('end', () => {
          try {
            const j = JSON.parse(data)
            resolve({ enabled: !!j.enabled, port: j.port || -1, username: j.username || DEFAULT_SSH_USER })
          } catch { resolve(null) }
        })
      })
      req.on('error', () => resolve(null))
    } catch { resolve(null) }
  })
}

/** Establish the SSH tunnel if not already connected. Returns true when connected. */
export async function ensureTunnel(): Promise<boolean> {
  if (state.connected && client) return true
  const serverUrl = getStore().get('serverUrl')
  if (!serverUrl) return false
  let url: URL
  try { url = new URL(serverUrl) } catch { return false }

  const info = await fetchSshInfo(serverUrl)
  const sshPort = info && info.enabled && info.port > 0 ? info.port : Number(url.port || 80) + 1
  const username = info?.username || DEFAULT_SSH_USER
  return connectTunnel(url.hostname, sshPort, username)
}

export function reconnectTunnel(): Promise<boolean> {
  disconnectTunnel()
  return ensureTunnel()
}

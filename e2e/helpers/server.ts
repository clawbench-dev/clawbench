import { spawn, type ChildProcess } from 'child_process'
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync, chmodSync, cpSync } from 'fs'
import { join } from 'path'
import { tmpdir } from 'os'

const E2E_PORT = parseInt(process.env.E2E_PORT || '20100')
const E2E_PASSWORD = process.env.E2E_PASSWORD || 'e2e-test-password'

/** Path to the shared state file written by globalSetup and read by globalTeardown */
const STATE_FILE = join(tmpdir(), 'clawbench-e2e-state.json')

/** State persisted between globalSetup and globalTeardown */
export interface ServerState {
  pid: number
  tempDir: string
  port: number
}

/**
 * Wait for the Go server to become ready by polling /api/me.
 */
export async function waitForServer(port: number, timeoutMs: number): Promise<void> {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    try {
      const response = await fetch(`http://localhost:${port}/api/me`)
      if (response.ok || response.status === 401) return
    } catch {
      // Server not ready yet
    }
    await new Promise(r => setTimeout(r, 500))
  }
  throw new Error(`Server did not start within ${timeoutMs}ms on port ${port}`)
}

/**
 * Get the base URL for the E2E server.
 */
export function getServerURL(): string {
  return `http://localhost:${E2E_PORT}`
}

/**
 * Start the Go backend with ACP mock agent for E2E testing.
 *
 * Creates an isolated temp directory with:
 * - config/config.yaml (test configuration with known password)
 * - config/agents/acp-mock.yaml (ACP mock agent using real ACP acp-stdio protocol)
 * - .clawbench/ (database directory)
 *
 * The server is started from the temp directory so it picks up our config.
 */
export async function startServer(): Promise<ServerState> {
  const projectRoot = process.cwd()

  // 1. Create isolated temp directory for this test run
  const tempDir = mkdtempSync(join(tmpdir(), 'clawbench-e2e-'))
  const port = E2E_PORT
  const password = E2E_PASSWORD

  // 2. Write minimal test config
  const configDir = join(tempDir, 'config')
  mkdirSync(configDir, { recursive: true })
  writeFileSync(join(configDir, 'config.yaml'), `port: ${port}
password: "${password}"
log_level: warn
default_agent: acp-mock
chat:
  initial_messages: 20
  page_size: 20
session:
  max_count: 0
terminal:
  enabled: true
  idle_timeout: 1h
port_forward:
  enabled: false
rag:
  enabled: false
`)

  // 3. Create .clawbench dir so DB is created in our temp dir
  mkdirSync(join(tempDir, '.clawbench'), { recursive: true })

  // 4. Write agent config
  const agentsDir = join(tempDir, 'config', 'agents')
  mkdirSync(agentsDir, { recursive: true })

  // ACP mock agent (uses real ACP acp-stdio protocol with slash commands, modes)
  // acp_command points to the acp-mock binary we'll copy to the temp dir
  writeFileSync(join(agentsDir, 'acp-mock.yaml'), `backend: acp-mock
icon: "\\U0001F916"
id: acp-mock
name: ACP Mock Agent
specialty: E2E Testing (ACP)
transport: acp-stdio
acp_command: ./acp-mock
preferred_model: mock-pro
models:
  - id: mock-pro
    name: Mock Pro
    default: true
  - id: mock-fast
    name: Mock Fast
system_prompt: |
    You are a mock ACP agent for E2E testing.
`)

  // 5. Copy the pre-built Go binary to temp dir
  // The binary is built before E2E tests run (by CI or developer)
  const binPath = join(projectRoot, 'clawbench')
  const tempBinPath = join(tempDir, 'clawbench')
  writeFileSync(tempBinPath, readFileSync(binPath))
  chmodSync(tempBinPath, 0o755) // Make binary executable

  // 5a. Copy the ACP mock agent binary (used by acp-mock agent with acp-stdio transport)
  const acpMockBinPath = join(projectRoot, 'acp-mock')
  const tempAcpMockBinPath = join(tempDir, 'acp-mock')
  try {
    writeFileSync(tempAcpMockBinPath, readFileSync(acpMockBinPath))
    chmodSync(tempAcpMockBinPath, 0o755)
  } catch {
    console.warn('[E2E] Warning: acp-mock binary not found, ACP agent tests will fail')
  }

  // 5b. Copy frontend build artifacts (public/ directory) to temp dir
  // The Go server serves static files from <BinDir>/public/
  const publicDir = join(projectRoot, 'public')
  try {
    cpSync(publicDir, join(tempDir, 'public'), { recursive: true })
  } catch {
    console.warn('[E2E] Warning: public/ directory not found, frontend may not be served')
  }

  // 6. Start server from temp dir so it picks up our config
  const serverProcess = spawn(tempBinPath, [`--port`, String(port)], {
    cwd: tempDir,
    env: {
      ...process.env,
      // Prepend tempDir to PATH so that any child process invoking "clawbench"
      // uses our copied binary instead of a potentially different version in
      // the system PATH. This ensures test isolation.
      PATH: `${tempDir}:${process.env.PATH}`,
    },
    stdio: ['pipe', 'pipe', 'pipe'],
  })

  // Log server output for debugging
  serverProcess.stdout?.on('data', (data: Buffer) => {
    process.stdout.write(`[E2E Server stdout] ${data.toString()}`)
  })
  serverProcess.stderr?.on('data', (data: Buffer) => {
    process.stderr.write(`[E2E Server stderr] ${data.toString()}`)
  })

  // 7. Wait for server to be ready (gse dictionary loading can take ~15s)
  await waitForServer(port, 60000)

  const state: ServerState = {
    pid: serverProcess.pid!,
    tempDir,
    port,
  }

  // 8. Persist state for globalTeardown
  writeFileSync(STATE_FILE, JSON.stringify(state))

  return state
}

/**
 * Stop the Go backend server.
 */
export async function stopServer(): Promise<void> {
  let state: ServerState | undefined
  try {
    const data = readFileSync(STATE_FILE, 'utf-8')
    state = JSON.parse(data)
  } catch {
    // No state file — nothing to stop
    return
  }

  // Kill the server process
  try {
    process.kill(state.pid, 'SIGTERM')
  } catch {
    // Process may already be dead
  }

  // Wait for process to exit, then force kill if needed
  await new Promise<void>(resolve => {
    const timeout = setTimeout(() => {
      try {
        process.kill(state!.pid, 'SIGKILL')
      } catch {
        // Already dead
      }
      resolve()
    }, 5000)

    // Try to detect process exit
    const checkInterval = setInterval(() => {
      try {
        process.kill(state!.pid, 0) // Signal 0 = check if process exists
      } catch {
        // Process is dead
        clearTimeout(timeout)
        clearInterval(checkInterval)
        resolve()
      }
    }, 200)
  })

  // Clean up temp directory
  try {
    rmSync(state.tempDir, { recursive: true, force: true })
  } catch {
    // Best effort cleanup
  }

  // Remove state file
  try {
    rmSync(STATE_FILE)
  } catch {
    // Ignore
  }
}

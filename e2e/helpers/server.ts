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
 * Authenticate against the E2E server and return the raw `Set-Cookie` pairs.
 *
 * POST /api/project is auth-gated, so the setup step that selects the default
 * project has to log in first. Kept local to this module (rather than reusing
 * helpers/auth.ts) because globalSetup runs before any test fixture and must
 * not depend on the memoized per-worker cookie cache.
 */
async function loginForSetup(port: number): Promise<string> {
  const resp = await fetch(`http://localhost:${port}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: E2E_PASSWORD }),
  })
  if (!resp.ok) {
    throw new Error(`E2E setup login failed: ${resp.status} ${resp.statusText}`)
  }
  const cookies = (resp.headers.getSetCookie?.() ?? [])
    .map((c) => c.split(';')[0].trim())
    .filter((c) => c.length > 0)
    .join('; ')
  if (!cookies) {
    throw new Error('E2E setup login succeeded but returned no session cookie')
  }
  return cookies
}

/**
 * Select the default project for the test server.
 *
 * Fails loudly: if this does not take effect, file tests silently skip and
 * ACP sessions run in the wrong working directory, which is far harder to
 * diagnose than a setup error.
 */
async function setDefaultProject(port: number, projectPath: string): Promise<void> {
  const cookie = await loginForSetup(port)
  const resp = await fetch(`http://localhost:${port}/api/project`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Cookie: cookie },
    body: JSON.stringify({ path: projectPath }),
  })
  if (!resp.ok) {
    throw new Error(`E2E setup: POST /api/project failed: ${resp.status} ${resp.statusText}`)
  }
  const data = (await resp.json()) as { path?: string }
  if (data.path !== projectPath) {
    throw new Error(`E2E setup: project resolved to ${data.path}, expected ${projectPath}`)
  }
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

  // ACP mock agent (uses real ACP acp-stdio protocol with slash commands, modes).
  //
  // acp_command MUST be an absolute path. ACP spawns the agent with
  // cmd.Dir = the session's project root, so a relative "./acp-mock" resolves
  // against that directory, not the server's cwd (nor PATH — Go only consults
  // PATH for names without a separator). With the temp-dir project root this
  // silently produced `fork/exec ./acp-mock: no such file or directory`, which
  // disabled every ACP-backed test.
  const tempAcpMockBinPath = join(tempDir, 'acp-mock')
  writeFileSync(join(agentsDir, 'acp-mock.yaml'), `backend: acp-mock
icon: "\\U0001F916"
id: acp-mock
name: ACP Mock Agent
specialty: E2E Testing (ACP)
transport: acp-stdio
acp_command: ${tempAcpMockBinPath}
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

  // A SECOND acp-mock agent under a distinct id. A group cannot contain the same
  // agent twice: AddGroupMember dedups by agent id, so adding "acp-mock" to a
  // group whose host is also "acp-mock" just returns the HOST row and the group
  // stays single-member. Specs that need a real host + member pair (e.g. the
  // group permission approval path, where only the member must block) use this
  // second id for the member. Same binary, different id ⇒ independent ACP
  // connection and independent per-session mode.
  writeFileSync(join(agentsDir, 'acp-mock-b.yaml'), `backend: acp-mock
icon: "\\U0001F916"
id: acp-mock-b
name: ACP Mock Agent B
specialty: E2E Testing (ACP, member)
transport: acp-stdio
acp_command: ${tempAcpMockBinPath}
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
  // The binary is built before E2E tests run (by CI or developer).
  // E2E_SERVER_BIN overrides the binary path (e.g. a `-tags integration` build
  // that registers the acp-mock backend).
  const binPath = process.env.E2E_SERVER_BIN || join(projectRoot, 'clawbench')
  const tempBinPath = join(tempDir, 'clawbench')
  writeFileSync(tempBinPath, readFileSync(binPath))
  chmodSync(tempBinPath, 0o755) // Make binary executable

  // 5a. Copy the ACP mock agent binary (used by acp-mock agent with acp-stdio
  // transport). tempAcpMockBinPath was resolved above so the agent YAML could
  // reference the same absolute path.
  const acpMockBinPath = join(projectRoot, 'acp-mock')
  try {
    writeFileSync(tempAcpMockBinPath, readFileSync(acpMockBinPath))
    chmodSync(tempAcpMockBinPath, 0o755)
  } catch {
    console.warn('[E2E] Warning: acp-mock binary not found, ACP agent tests will fail')
  }

  // 5b. Copy frontend build artifacts (.clawbench-web/ directory) to temp dir
  // The Go server serves static files from <CWD>/.clawbench-web/
  const buildDir = join(projectRoot, '.clawbench-web')
  try {
    cpSync(buildDir, join(tempDir, '.clawbench-web'), { recursive: true })
  } catch {
    console.warn('[E2E] Warning: .clawbench-web/ directory not found, frontend may not be served')
  }

  // 6. Start server from temp dir so it picks up our config.
  // The temp .clawbench dir is passed via --data-dir so the test server NEVER
  // touches the developer's real database (~/.clawbench).
  const serverProcess = spawn(tempBinPath, [`--port`, String(port), `--data-dir`, join(tempDir, '.clawbench')], {
    cwd: tempDir,
    env: {
      ...process.env,
      // Prepend tempDir to PATH so that any child process invoking "clawbench"
      // uses our copied binary instead of a potentially different version in
      // the system PATH. This ensures test isolation.
      PATH: `${tempDir}:${process.env.PATH}`,
      // Make the mock agent's reply take long enough that a test can enqueue
      // more messages while the first turn is still streaming. Without this the
      // reply is near-instant and the queued-messages spec races: messages 2/3
      // arrive after turn 1 already finished, so nothing ever queues.
      // The delay is applied before the first word (see cmd/acp-mock/main.go).
      ACP_MOCK_REPLY_DELAY_MS: process.env.ACP_MOCK_REPLY_DELAY_MS || '1200',
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

  // 7a. Point the default project at the repository root.
  //
  // The temp DB has no projects, so GET /api/project falls back to the user's
  // home directory. Two things break under that fallback:
  //   - file tests that open a known repo file (`go.mod`) find nothing and
  //     silently `test.skip()`.
  //   - ACP sessions inherit the project root as their cwd; the repo root is
  //     the directory the fixtures were written against.
  // POST /api/project persists the choice as `is_default`, so every browser
  // context's subsequent GET /api/project resolves to it.
  await setDefaultProject(port, projectRoot)

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

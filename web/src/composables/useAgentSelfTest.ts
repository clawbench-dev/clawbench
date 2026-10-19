import { ref } from 'vue'
import { apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'

/** One assertion returned by POST /api/agents/{id}/self-test. */
export interface AgentSelfTestCheck {
  key: string
  /** "ok" | "warn" | "fail" */
  status: 'ok' | 'warn' | 'fail'
  detail?: string
}

/** Result of the opt-in DEEP run (one real turn). */
export interface AgentSelfTestDeepResult {
  reply?: string
  transport?: string
  stopReason?: string
  durationMs?: number
}

export interface AgentSelfTestDeepResponse {
  ok: boolean
  error?: string
  deep?: AgentSelfTestDeepResult
  static?: AgentSelfTestResult
}

export interface AgentSelfTestResult {
  agentId: string
  ok: boolean
  passed: number
  failed: number
  warned: number
  checks: AgentSelfTestCheck[]
}

/**
 * Runs the backend's agent integration self-test.
 *
 * The backend inspects persisted state, the backend registry and the filesystem
 * — it does NOT send a prompt, so this is safe to run on every agent and costs
 * no tokens. A prompt-based check would spawn a process and spend credit just to
 * answer "is this configured correctly".
 */
export function useAgentSelfTest() {
  const testing = ref(false)
  const result = ref<AgentSelfTestResult | null>(null)
  const deepResult = ref<AgentSelfTestDeepResult | null>(null)
  const error = ref('')

  async function run(agentId: string): Promise<void> {
    testing.value = true
    error.value = ''
    result.value = null
    deepResult.value = null
    try {
      result.value = await apiPost<AgentSelfTestResult>(
        `/api/agents/${encodeURIComponent(agentId)}/self-test`,
        {},
        { timeoutMs: 30_000 },
      )
    } catch (err: unknown) {
      // The failure reason matters to the user (e.g. 404 after a concurrent
      // delete), so surface it instead of a generic "test failed".
      error.value = err instanceof Error ? err.message : String(err)
      appLog.e('AgentSelfTest', 'self-test request failed', err)
    } finally {
      testing.value = false
    }
  }

  /**
   * Run one REAL turn against the agent.
   *
   * Separate from `run` because it costs tokens and can take up to ~90s, so it
   * must never happen implicitly. The backend re-runs the static checks first
   * and refuses if any of them fail — spending a full timeout to learn that a
   * command is missing would be pointless.
   */
  async function runDeep(agentId: string): Promise<void> {
    testing.value = true
    error.value = ''
    result.value = null
    deepResult.value = null
    try {
      const res = await apiPost<AgentSelfTestDeepResponse>(
        `/api/agents/${encodeURIComponent(agentId)}/self-test-deep`,
        {},
        // Generous: the backend's own turn timeout is 90s.
        { timeoutMs: 120_000 },
      )
      // The static checks ride along, so the UI can show both without a second
      // request — and a refusal is reported the same way as a failed run.
      if (res.static) result.value = res.static
      if (res.ok) {
        deepResult.value = res.deep ?? null
      } else {
        error.value = res.error ?? ''
      }
    } catch (err: unknown) {
      error.value = err instanceof Error ? err.message : String(err)
      appLog.e('AgentSelfTest', 'deep self-test request failed', err)
    } finally {
      testing.value = false
    }
  }

  function clear(): void {
    result.value = null
    deepResult.value = null
    error.value = ''
  }

  return { testing, result, deepResult, error, run, runDeep, clear }
}

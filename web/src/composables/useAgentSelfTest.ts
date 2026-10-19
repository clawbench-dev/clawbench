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
  const error = ref('')

  async function run(agentId: string): Promise<void> {
    testing.value = true
    error.value = ''
    result.value = null
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

  function clear(): void {
    result.value = null
    error.value = ''
  }

  return { testing, result, error, run, clear }
}

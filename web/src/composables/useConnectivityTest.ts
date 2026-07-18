import { ref } from 'vue'
import { apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'

export interface ConnectivityTestResult {
  success: boolean
  message: string
}

export function useConnectivityTest() {
  const testing = ref(false)
  const testResults = ref<ConnectivityTestResult[]>([])

  async function runTests(tests: Array<{ category: string; values: Record<string, unknown> }>): Promise<void> {
    testing.value = true
    testResults.value = []
    try {
      const results = await Promise.allSettled(
        tests.map(t =>
          apiPost<ConnectivityTestResult>('/api/config/test', {
            category: t.category,
            values: t.values,
          }, { timeoutMs: 15_000 })
        )
      )
      testResults.value = results.map(r => {
        if (r.status === 'fulfilled') return r.value
        return { success: false, message: r.reason instanceof Error ? r.reason.message : 'Test failed' }
      })
    } catch (err: unknown) {
      appLog.e('ConnectivityTest', 'Unexpected error in runTests', err)
    } finally {
      testing.value = false
    }
  }

  function clearResults() {
    testResults.value = []
  }

  return { testing, testResults, runTests, clearResults }
}

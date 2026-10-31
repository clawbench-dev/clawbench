import { ref } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'

/** One provider as the settings pages need it (no secret ever crosses the wire). */
export interface ProviderOption {
  id: number
  name: string
  protocol: string
  baseUrl: string
  hasKey: boolean
}

// Module-level so the provider page and the AI-summary panel share one fetch.
// Deliberately not a global store: only these two screens consume providers, and
// a stale cache is worse than a refetch when the user just edited one.
const providers = ref<ProviderOption[]>([])
const modelsByProvider = ref<Record<number, string[]>>({})
const modelErrors = ref<Record<number, string>>({})
let inFlight: Promise<void> | null = null

export function useProviders() {
  async function loadProviders(force = false): Promise<void> {
    if (!force && providers.value.length > 0) return
    if (inFlight) return inFlight
    inFlight = (async () => {
      try {
        const data = await apiGet<{ providers: ProviderOption[] }>('/api/providers')
        providers.value = data.providers ?? []
      } catch (err) {
        appLog.w('useProviders', 'load providers failed', err)
        providers.value = []
      } finally {
        inFlight = null
      }
    })()
    return inFlight
  }

  /**
   * Fetch a provider's model catalogue. A gateway that does not expose /models
   * yields an empty list and records the reason; callers fall back to free text.
   */
  async function loadModels(providerId: number, force = false): Promise<string[]> {
    if (!force && modelsByProvider.value[providerId]) return modelsByProvider.value[providerId]
    try {
      const data = await apiGet<{ models?: string[]; error?: string }>(`/api/providers/${providerId}/models`)
      const models = data.models ?? []
      modelsByProvider.value = { ...modelsByProvider.value, [providerId]: models }
      const nextErrors = { ...modelErrors.value }
      if (data.error) nextErrors[providerId] = data.error
      else delete nextErrors[providerId]
      modelErrors.value = nextErrors
      return models
    } catch (err) {
      modelsByProvider.value = { ...modelsByProvider.value, [providerId]: [] }
      modelErrors.value = { ...modelErrors.value, [providerId]: err instanceof Error ? err.message : String(err) }
      return []
    }
  }

  /** Providers the summarizer can talk to (it speaks OpenAI and Anthropic only). */
  function summaryProviders(): ProviderOption[] {
    return providers.value.filter((p) => p.protocol === 'anthropic' || p.protocol === 'openai')
  }

  return { providers, modelsByProvider, modelErrors, loadProviders, loadModels, summaryProviders }
}

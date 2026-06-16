/**
 * useSetup — composable for the setup wizard
 *
 * Manages all API interactions for the 5-step agent setup wizard:
 * 1. Check status (needs_setup, embedded_agent)
 * 2. Get providers
 * 3. Scan models
 * 4. Verify configuration
 * 5. Complete setup
 */
import { ref, readonly } from 'vue'
import { apiGet, apiPost } from '@/utils/api'

// ── Types ──

export interface SetupStatus {
    needs_setup: boolean
    embedded_agent: boolean
    agent_version: string
}

export interface Provider {
    id: string
    name: string
    envVar: string
    apiFormat: string
}

export interface ModelItem {
    id: string
    name: string
    created: number
    context_length?: number
    supports_thinking?: boolean
    cost_tier?: string
}

export interface ModelsResponse {
    models: ModelItem[]
    summarize_model_hint: string
    error?: string
}

export interface VerifyResponse {
    success: boolean
    message: string
    model?: string
}

export interface BackendInfo {
    id: string
    name: string
    icon: string
    specialty: string
    default_cmd: string
    thinking_effort_levels?: string[]
}

export interface CompleteResponse {
    success: boolean
    agent?: { id: string; name: string; [key: string]: unknown }
    default_agent_id?: string
}

export interface SetupCompleteRequest {
    provider: string
    custom_url: string
    api_format: string
    api_key: string
    model: string
    summarize_model: string
    agent_name: string
    agent_id: string
}

// ── Provider → Agent name mapping ──

export const providerAgentNames: Record<string, { name: string; id: string }> = {
    'anthropic':              { name: 'Anthropic Claude',   id: 'anthropic-claude' },
    'openai':                 { name: 'OpenAI',             id: 'openai' },
    'deepseek':               { name: 'DeepSeek',           id: 'deepseek' },
    'minimax':                { name: 'MiniMax',            id: 'minimax' },
    'minimax-cn':             { name: 'MiniMax (中国)',      id: 'minimax-cn' },
    'groq':                   { name: 'Groq',               id: 'groq' },
    'openrouter':             { name: 'OpenRouter',         id: 'openrouter' },
    'mistral':                { name: 'Mistral',            id: 'mistral' },
    'xai':                    { name: 'xAI Grok',           id: 'xai-grok' },
    'cerebras':               { name: 'Cerebras',           id: 'cerebras' },
    'fireworks':              { name: 'Fireworks',          id: 'fireworks' },
    'moonshotai':             { name: 'Moonshot AI',        id: 'moonshot-ai' },
    'moonshotai-cn':          { name: 'Moonshot AI (中国)', id: 'moonshot-ai-cn' },
    'opencode':               { name: 'OpenCode Zen',       id: 'opencode-zen' },
    'kimi-coding':            { name: 'Kimi For Coding',    id: 'kimi-coding' },
    'zai':                    { name: 'ZAI',                id: 'zai' },
    'huggingface':            { name: 'Hugging Face',       id: 'huggingface' },
    'vercel-ai-gateway':      { name: 'Vercel AI GW',       id: 'vercel-ai-gw' },
    'xiaomi':                 { name: 'Xiaomi MiMo',        id: 'xiaomi-mimo' },
    'xiaomi-token-plan-cn':   { name: 'Xiaomi MiMo (CN)',   id: 'xiaomi-mimo-cn' },
    'xiaomi-token-plan-ams':  { name: 'Xiaomi MiMo (AMS)',  id: 'xiaomi-mimo-ams' },
    'xiaomi-token-plan-sgp':  { name: 'Xiaomi MiMo (SGP)',  id: 'xiaomi-mimo-sgp' },
    '_custom':                { name: '自定义智能体',        id: 'custom-agent' },
}

// ── Singleton state ──

const status = ref<SetupStatus | null>(null)
const providers = ref<Provider[]>([])
const models = ref<ModelItem[]>([])
const summarizeModelHint = ref('')
const modelsError = ref('')
const loading = ref(false)
const completed = ref(false)

// ── API calls ──

async function checkStatus(): Promise<SetupStatus> {
    const data = await apiGet<SetupStatus>('/api/setup/status')
    status.value = data
    return data
}

async function getProviders(): Promise<Provider[]> {
    const data = await apiGet<{ providers: Provider[]; custom_url_supported: boolean }>('/api/setup/providers')
    providers.value = data.providers || []
    return providers.value
}

async function getBackends(): Promise<BackendInfo[]> {
    const data = await apiGet<{ backends: BackendInfo[] }>('/api/setup/backends')
    return data.backends || []
}

async function scanModels(provider: string, customUrl: string, apiKey: string, apiFormat: string): Promise<ModelsResponse> {
    loading.value = true
    modelsError.value = ''
    try {
        const data = await apiPost<ModelsResponse>('/api/setup/models', {
            provider,
            custom_url: customUrl,
            api_key: apiKey,
            api_format: apiFormat,
        }, { signal: AbortSignal.timeout(30_000) })
        models.value = data.models || []
        summarizeModelHint.value = data.summarize_model_hint || ''
        if (data.error) modelsError.value = data.error
        return data
    } catch (err) {
        modelsError.value = err instanceof Error ? err.message : String(err)
        models.value = []
        summarizeModelHint.value = ''
        return { models: [], summarize_model_hint: '', error: modelsError.value }
    } finally {
        loading.value = false
    }
}

async function verify(provider: string, customUrl: string, apiKey: string, model: string, apiFormat: string): Promise<VerifyResponse> {
    loading.value = true
    try {
        const data = await apiPost<VerifyResponse>('/api/setup/verify', {
            provider,
            custom_url: customUrl,
            api_key: apiKey,
            model,
            api_format: apiFormat,
        }, { signal: AbortSignal.timeout(35_000) })
        return data
    } catch (err) {
        return {
            success: false,
            message: err instanceof Error ? err.message : String(err),
        }
    } finally {
        loading.value = false
    }
}

async function complete(config: SetupCompleteRequest): Promise<CompleteResponse> {
    loading.value = true
    try {
        const data = await apiPost<CompleteResponse>('/api/setup/complete', config)
        if (data.success) completed.value = true
        return data
    } catch (err) {
        return {
            success: false,
        }
    } finally {
        loading.value = false
    }
}

// ── Exported composable ──

export function useSetup() {
    return {
        // State (readonly for consumers)
        status: readonly(status),
        providers: readonly(providers),
        models: readonly(models),
        summarizeModelHint: readonly(summarizeModelHint),
        modelsError: readonly(modelsError),
        loading: readonly(loading),
        completed: readonly(completed),

        // API methods
        checkStatus,
        getProviders,
        getBackends,
        scanModels,
        verify,
        complete,
    }
}

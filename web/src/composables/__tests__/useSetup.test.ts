import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useSetup, providerAgentNames } from '@/composables/useSetup'

// Mock the api module
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}))

import { apiGet, apiPost } from '@/utils/api'

describe('useSetup', () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset()
    vi.mocked(apiPost).mockReset()
  })

  it('returns all expected properties and methods', () => {
    const setup = useSetup()
    expect(setup.status).toBeDefined()
    expect(setup.providers).toBeDefined()
    expect(setup.models).toBeDefined()
    expect(setup.summarizeModelHint).toBeDefined()
    expect(setup.modelsError).toBeDefined()
    expect(setup.loading).toBeDefined()
    expect(setup.completed).toBeDefined()
    expect(typeof setup.checkStatus).toBe('function')
    expect(typeof setup.getProviders).toBe('function')
    expect(typeof setup.getBackends).toBe('function')
    expect(typeof setup.scanModels).toBe('function')
    expect(typeof setup.verify).toBe('function')
    expect(typeof setup.complete).toBe('function')
  })

  describe('checkStatus', () => {
    it('calls GET /api/setup/status and returns data', async () => {
      vi.mocked(apiGet).mockResolvedValueOnce({
        needs_setup: true,
        embedded_agent: true,
        agent_version: '0.78.0',
      })
      const setup = useSetup()
      const result = await setup.checkStatus()
      expect(apiGet).toHaveBeenCalledWith('/api/setup/status')
      expect(result.needs_setup).toBe(true)
      expect(result.embedded_agent).toBe(true)
      expect(result.agent_version).toBe('0.78.0')
    })
  })

  describe('getProviders', () => {
    it('calls GET /api/setup/providers and returns provider list', async () => {
      vi.mocked(apiGet).mockResolvedValueOnce({
        providers: [
          { id: 'openai', name: 'OpenAI', envVar: 'OPENAI_API_KEY' },
          { id: 'anthropic', name: 'Anthropic', envVar: 'ANTHROPIC_API_KEY' },
        ],
        custom_url_supported: true,
      })
      const setup = useSetup()
      const result = await setup.getProviders()
      expect(apiGet).toHaveBeenCalledWith('/api/setup/providers')
      expect(result).toHaveLength(2)
      expect(result[0].id).toBe('openai')
    })
  })

  describe('getBackends', () => {
    it('calls GET /api/setup/backends and returns backend list', async () => {
      vi.mocked(apiGet).mockResolvedValueOnce({
        backends: [
          { id: 'pi', name: 'Pi', icon: '🥧', specialty: '极简编程智能体', default_cmd: 'pi' },
          { id: 'codebuddy', name: 'CodeBuddy', icon: '🤖', specialty: 'AI pair programmer', default_cmd: 'codebuddy' },
        ],
      })
      const setup = useSetup()
      const result = await setup.getBackends()
      expect(apiGet).toHaveBeenCalledWith('/api/setup/backends')
      expect(result).toHaveLength(2)
      expect(result[0].id).toBe('pi')
      expect(result[1].name).toBe('CodeBuddy')
    })

    it('returns empty array when backends response is empty', async () => {
      vi.mocked(apiGet).mockResolvedValueOnce({})
      const setup = useSetup()
      const result = await setup.getBackends()
      expect(result).toEqual([])
    })
  })

  describe('scanModels', () => {
    it('calls POST /api/setup/models and returns models', async () => {
      vi.mocked(apiPost).mockResolvedValueOnce({
        models: [
          { id: 'gpt-4o', name: 'gpt-4o', created: 1700000000 },
          { id: 'gpt-4o-mini', name: 'gpt-4o-mini', created: 1699999999 },
        ],
        summarize_model_hint: 'gpt-4o-mini',
      })
      const setup = useSetup()
      const result = await setup.scanModels('openai', '', 'sk-test', '')
      expect(apiPost).toHaveBeenCalledWith('/api/setup/models', {
        provider: 'openai',
        custom_url: '',
        api_key: 'sk-test',
        api_format: '',
      }, expect.any(Object))
      expect(result.models).toHaveLength(2)
      expect(result.summarize_model_hint).toBe('gpt-4o-mini')
    })

    it('handles errors gracefully', async () => {
      vi.mocked(apiPost).mockRejectedValueOnce(new Error('Network error'))
      const setup = useSetup()
      const result = await setup.scanModels('openai', '', 'sk-test', '')
      expect(result.models).toHaveLength(0)
      expect(result.error).toBe('Network error')
    })

    it('passes api_format for custom URL mode', async () => {
      vi.mocked(apiPost).mockResolvedValueOnce({
        models: [],
        summarize_model_hint: '',
      })
      const setup = useSetup()
      await setup.scanModels('_custom', 'https://api.example.com/v1/chat/completions', 'sk-test', 'openai')
      expect(apiPost).toHaveBeenCalledWith('/api/setup/models', {
        provider: '_custom',
        custom_url: 'https://api.example.com/v1/chat/completions',
        api_key: 'sk-test',
        api_format: 'openai',
      }, expect.any(Object))
    })
  })

  describe('verify', () => {
    it('calls POST /api/setup/verify and returns result', async () => {
      vi.mocked(apiPost).mockResolvedValueOnce({
        success: true,
        message: '配置验证成功！',
        model: 'gpt-4o',
      })
      const setup = useSetup()
      const result = await setup.verify('openai', '', 'sk-test', 'gpt-4o', '')
      expect(apiPost).toHaveBeenCalledWith('/api/setup/verify', {
        provider: 'openai',
        custom_url: '',
        api_key: 'sk-test',
        model: 'gpt-4o',
        api_format: '',
      }, expect.any(Object))
      expect(result.success).toBe(true)
    })

    it('handles verify failure gracefully', async () => {
      vi.mocked(apiPost).mockRejectedValueOnce(new Error('Timeout'))
      const setup = useSetup()
      const result = await setup.verify('openai', '', 'sk-test', 'gpt-4o', '')
      expect(result.success).toBe(false)
      expect(result.message).toBe('Timeout')
    })

    it('passes api_format for custom URL verify', async () => {
      vi.mocked(apiPost).mockResolvedValueOnce({
        success: true,
        message: 'OK',
      })
      const setup = useSetup()
      await setup.verify('_custom', 'https://api.deepseek.com/v1/chat/completions', 'sk-test', 'deepseek-chat', 'openai')
      expect(apiPost).toHaveBeenCalledWith('/api/setup/verify', {
        provider: '_custom',
        custom_url: 'https://api.deepseek.com/v1/chat/completions',
        api_key: 'sk-test',
        model: 'deepseek-chat',
        api_format: 'openai',
      }, expect.any(Object))
    })
  })

  describe('complete', () => {
    it('calls POST /api/setup/complete and returns result', async () => {
      vi.mocked(apiPost).mockResolvedValueOnce({
        success: true,
        agent: { id: 'openai', name: 'OpenAI' },
        default_agent_id: 'openai',
      })
      const setup = useSetup()
      const result = await setup.complete({
        provider: 'openai',
        custom_url: '',
        api_format: '',
        api_key: 'sk-test',
        model: 'gpt-4o',
        summarize_model: 'gpt-4o-mini',
        agent_name: 'OpenAI',
        agent_id: 'openai',
      })
      expect(apiPost).toHaveBeenCalledWith('/api/setup/complete', {
        provider: 'openai',
        custom_url: '',
        api_format: '',
        api_key: 'sk-test',
        model: 'gpt-4o',
        summarize_model: 'gpt-4o-mini',
        agent_name: 'OpenAI',
        agent_id: 'openai',
      })
      expect(result.success).toBe(true)
    })

    it('handles complete failure gracefully', async () => {
      vi.mocked(apiPost).mockRejectedValueOnce(new Error('Server error'))
      const setup = useSetup()
      const result = await setup.complete({
        provider: 'openai',
        custom_url: '',
        api_format: '',
        api_key: 'sk-test',
        model: 'gpt-4o',
        summarize_model: 'gpt-4o-mini',
        agent_name: 'OpenAI',
        agent_id: 'openai',
      })
      expect(result.success).toBe(false)
    })

    it('sends api_format for custom URL complete', async () => {
      vi.mocked(apiPost).mockResolvedValueOnce({
        success: true,
        agent: { id: 'custom-agent', name: 'Custom' },
      })
      const setup = useSetup()
      await setup.complete({
        provider: '_custom',
        custom_url: 'https://api.deepseek.com/v1/chat/completions',
        api_format: 'openai',
        api_key: 'sk-test',
        model: 'deepseek-chat',
        summarize_model: 'deepseek-chat',
        agent_name: 'Custom DeepSeek',
        agent_id: 'custom-deepseek',
      })
      expect(apiPost).toHaveBeenCalledWith('/api/setup/complete', {
        provider: '_custom',
        custom_url: 'https://api.deepseek.com/v1/chat/completions',
        api_format: 'openai',
        api_key: 'sk-test',
        model: 'deepseek-chat',
        summarize_model: 'deepseek-chat',
        agent_name: 'Custom DeepSeek',
        agent_id: 'custom-deepseek',
      })
    })
  })
})

describe('providerAgentNames', () => {
  it('contains entries for major providers', () => {
    expect(providerAgentNames['openai']).toBeDefined()
    expect(providerAgentNames['anthropic']).toBeDefined()
    expect(providerAgentNames['minimax']).toBeDefined()
    expect(providerAgentNames['deepseek']).toBeDefined()
  })

  it('each entry has name and id', () => {
    for (const [, value] of Object.entries(providerAgentNames)) {
      expect(value.name).toBeTruthy()
      expect(value.id).toBeTruthy()
      // ID should be lowercase with hyphens only
      expect(value.id).toMatch(/^[a-z0-9-]+$/)
    }
  })

  it('includes _custom entry', () => {
    expect(providerAgentNames['_custom']).toBeDefined()
    expect(providerAgentNames['_custom'].id).toBe('custom-agent')
  })
})

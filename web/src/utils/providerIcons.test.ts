import { describe, it, expect } from 'vitest'
import { getModelProvider } from '@/utils/providerIcons'

describe('getModelProvider', () => {
    // Plain model names (no slash)
    it('matches claude family', () => {
        expect(getModelProvider('claude-sonnet-4-6')).toBe('claude')
        expect(getModelProvider('claude-3-opus')).toBe('claude')
    })

    it('matches openai family', () => {
        expect(getModelProvider('gpt-4o')).toBe('openai')
        expect(getModelProvider('o3-mini')).toBe('openai')
        expect(getModelProvider('chatgpt-4o')).toBe('openai')
    })

    it('matches gemini family', () => {
        expect(getModelProvider('gemini-2.0-flash')).toBe('gemini')
        expect(getModelProvider('gemma-2b')).toBe('google')
    })

    it('matches deepseek', () => {
        expect(getModelProvider('deepseek-chat')).toBe('deepseek')
    })

    it('matches mistral family', () => {
        expect(getModelProvider('mistral-large')).toBe('mistral')
        expect(getModelProvider('codestral-2405')).toBe('mistral')
    })

    it('matches meta family', () => {
        expect(getModelProvider('llama-3-70b')).toBe('meta')
        expect(getModelProvider('meta-llama-3')).toBe('meta')
    })

    it('matches other providers', () => {
        expect(getModelProvider('qwen-max')).toBe('qwen')
        expect(getModelProvider('doubao-pro-32k')).toBe('doubao')
        expect(getModelProvider('hunyuan-lite')).toBe('hunyuan')
        expect(getModelProvider('minimax-01')).toBe('minimax')
        expect(getModelProvider('kimi-latest')).toBe('kimi')
        expect(getModelProvider('mimo-7b')).toBe('xiaomi')
        expect(getModelProvider('glm-4')).toBe('zhipu')
        expect(getModelProvider('grok-2')).toBe('xai')
        expect(getModelProvider('groq-llama')).toBe('groq')
    })

    it('returns null for unknown model', () => {
        expect(getModelProvider('unknown-model')).toBeNull()
    })

    it('returns null for empty string', () => {
        expect(getModelProvider('')).toBeNull()
    })

    // Slash format: "provider/model-name" — model name part matched first
    it('strips provider prefix and matches model name', () => {
        // azure/gpt-4o should match "openai" (from model part "gpt-4o"), not "azure"
        expect(getModelProvider('azure/gpt-4o')).toBe('openai')
        // bedrock/claude-3-opus should match "claude", not "bedrock"
        expect(getModelProvider('bedrock/claude-3-opus')).toBe('claude')
        // fireworks/deepseek-chat should match "deepseek"
        expect(getModelProvider('fireworks/deepseek-chat')).toBe('deepseek')
        // openrouter/gpt-4o should match "openai"
        expect(getModelProvider('openrouter/gpt-4o')).toBe('openai')
    })

    it('falls back to provider prefix when model name is unknown', () => {
        // openrouter/auto — "auto" has no pattern match, fall back to "openrouter"
        expect(getModelProvider('openrouter/auto')).toBe('openrouter')
        // azure/unknown-model — "unknown-model" no match, fall back to "azure"
        expect(getModelProvider('azure/unknown-model')).toBe('azure')
    })

    it('provider prefix matches same provider as model name', () => {
        // anthropic/claude-sonnet-4-6 — model part "claude-sonnet-4-6" matches "claude"
        expect(getModelProvider('anthropic/claude-sonnet-4-6')).toBe('claude')
        // deepseek/deepseek-chat — model part "deepseek-chat" matches "deepseek"
        expect(getModelProvider('deepseek/deepseek-chat')).toBe('deepseek')
    })

    it('handles case insensitivity with slash format', () => {
        expect(getModelProvider('Azure/GPT-4o')).toBe('openai')
        expect(getModelProvider('Anthropic/Claude-3-Opus')).toBe('claude')
    })

    it('falls back to provider prefix when no model pattern and no icon map entry', () => {
        // If provider prefix is not in icon map either, still return null
        expect(getModelProvider('unknown-provider/unknown-model')).toBeNull()
    })
})

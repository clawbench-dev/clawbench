import { describe, expect, it } from 'vitest'
import { getModelProvider, getProviderIcon, extractSvgInner, extractViewBox, getProviderSvgHtml, getProviderViewBox } from '@/utils/providerIcons'

describe('providerIcons', () => {
    describe('getModelProvider', () => {
        // Anthropic / Claude family
        it('detects claude models', () => {
            expect(getModelProvider('claude-sonnet-4-6')).toBe('claude')
            expect(getModelProvider('claude-3-opus')).toBe('claude')
            expect(getModelProvider('Claude-3.5-Sonnet')).toBe('claude')
        })

        it('detects anthropic prefix', () => {
            expect(getModelProvider('anthropic/claude-sonnet-4-6')).toBe('anthropic')
        })

        // OpenAI family
        it('detects OpenAI models', () => {
            expect(getModelProvider('gpt-4o')).toBe('openai')
            expect(getModelProvider('gpt-3.5-turbo')).toBe('openai')
            expect(getModelProvider('chatgpt-4o')).toBe('openai')
        })

        it('detects OpenAI reasoning models', () => {
            expect(getModelProvider('o1-preview')).toBe('openai')
            expect(getModelProvider('o3-mini')).toBe('openai')
            expect(getModelProvider('o4-mini')).toBe('openai')
        })

        it('detects OpenAI provider prefix', () => {
            expect(getModelProvider('openai/gpt-4o')).toBe('openai')
        })

        // Google / Gemini family
        it('detects Gemini models', () => {
            expect(getModelProvider('gemini-2.0-flash')).toBe('gemini')
            expect(getModelProvider('gemini-pro')).toBe('gemini')
        })

        it('detects Gemma models as Google', () => {
            expect(getModelProvider('gemma-2b')).toBe('google')
        })

        // DeepSeek
        it('detects DeepSeek models', () => {
            expect(getModelProvider('deepseek-chat')).toBe('deepseek')
            expect(getModelProvider('deepseek-reasoner')).toBe('deepseek')
        })

        // Mistral family
        it('detects Mistral models', () => {
            expect(getModelProvider('mistral-large')).toBe('mistral')
            expect(getModelProvider('codestral-2405')).toBe('mistral')
            expect(getModelProvider('pixtral-large')).toBe('mistral')
        })

        // Meta / Llama
        it('detects Llama models as Meta', () => {
            expect(getModelProvider('llama-3.1-405b')).toBe('meta')
            expect(getModelProvider('meta-llama-3.1-8b')).toBe('meta')
        })

        // Qwen
        it('detects Qwen models', () => {
            expect(getModelProvider('qwen-max')).toBe('qwen')
            expect(getModelProvider('qwen-plus')).toBe('qwen')
        })

        // Doubao
        it('detects Doubao models', () => {
            expect(getModelProvider('doubao-pro-4k')).toBe('doubao')
        })

        // Hunyuan
        it('detects Hunyuan models', () => {
            expect(getModelProvider('hunyuan-lite')).toBe('hunyuan')
        })

        // MiniMax
        it('detects MiniMax models', () => {
            expect(getModelProvider('minimax-01')).toBe('minimax')
            expect(getModelProvider('abab-6.5s-chat')).toBe('minimax')
        })

        // Kimi / Moonshot
        it('detects Kimi/Moonshot models', () => {
            expect(getModelProvider('moonshot-v1-8k')).toBe('kimi')
            expect(getModelProvider('kimi-latest')).toBe('kimi')
        })

        // Xiaomi / MiMo
        it('detects MiMo models as Xiaomi', () => {
            expect(getModelProvider('mimo-7b')).toBe('xiaomi')
        })

        // Zhipu / GLM / ChatGLM
        it('detects GLM models as Zhipu', () => {
            expect(getModelProvider('glm-4')).toBe('zhipu')
            expect(getModelProvider('glm-4-flash')).toBe('zhipu')
        })

        it('detects ChatGLM models', () => {
            expect(getModelProvider('chatglm3-turbo')).toBe('chatglm')
        })

        it('detects zhipu prefix', () => {
            expect(getModelProvider('zhipu/glm-4')).toBe('zhipu')
        })

        // xAI / Grok
        it('detects Grok models', () => {
            expect(getModelProvider('grok-2')).toBe('xai')
        })

        // Groq
        it('detects Groq models', () => {
            expect(getModelProvider('groq-llama-3.1')).toBe('groq')
        })

        // Provider prefixes (Pi model IDs)
        it('detects provider prefixes with slash', () => {
            expect(getModelProvider('google/gemini-2.0-flash')).toBe('gemini')
            expect(getModelProvider('deepseek/deepseek-chat')).toBe('deepseek')
            expect(getModelProvider('mistral/mistral-large')).toBe('mistral')
            expect(getModelProvider('meta/llama-3.1')).toBe('meta')
            expect(getModelProvider('xai/grok-2')).toBe('xai')
            expect(getModelProvider('qwen/qwen-max')).toBe('qwen')
            expect(getModelProvider('cerebras/llama-3.1')).toBe('cerebras')
            expect(getModelProvider('openrouter/auto')).toBe('openrouter')
            expect(getModelProvider('fireworks/llama-3.1')).toBe('fireworks')
        })

        // Unknown / edge cases
        it('returns null for unknown models', () => {
            expect(getModelProvider('some-custom-model')).toBeNull()
            expect(getModelProvider('')).toBeNull()
        })

        it('is case-insensitive', () => {
            expect(getModelProvider('CLAUDE-SONNET-4-6')).toBe('claude')
            expect(getModelProvider('GPT-4O')).toBe('openai')
            expect(getModelProvider('DeepSeek-Chat')).toBe('deepseek')
        })
    })

    describe('getProviderIcon', () => {
        it('returns icon data for known providers', () => {
            const providers = ['openai', 'anthropic', 'claude', 'gemini', 'deepseek', 'mistral', 'meta', 'qwen', 'kimi', 'xiaomi', 'groq', 'xai', 'copilot', 'codex', 'codebuddy', 'zhipu', 'chatglm', 'glm']
            for (const id of providers) {
                const data = getProviderIcon(id)
                expect(data, `provider "${id}" should have icon data`).not.toBeNull()
                expect(data!.raw.length, `provider "${id}" raw SVG should not be empty`).toBeGreaterThan(0)
            }
        })

        it('returns null for unknown providers', () => {
            expect(getProviderIcon('unknown')).toBeNull()
        })

        it('vecli uses inline fallback SVG', () => {
            const data = getProviderIcon('vecli')
            expect(data).not.toBeNull()
            expect(data!.raw).toContain('<svg')
        })
    })

    describe('extractSvgInner', () => {
        it('extracts inner content from a full SVG', () => {
            const svg = '<svg viewBox="0 0 24 24"><title>Test</title><path d="M0 0"/></svg>'
            const inner = extractSvgInner(svg)
            expect(inner).toContain('<path')
            expect(inner).not.toContain('<svg')
        })

        it('preserves currentColor fills on child elements (color via CSS)', () => {
            const svg = '<svg viewBox="0 0 24 24"><path fill="currentColor" d="M0 0"/></svg>'
            const inner = extractSvgInner(svg)
            expect(inner).toContain('fill="currentColor"')
        })

        it('propagates currentColor from <svg> tag to child elements without fill', () => {
            const svg = '<svg fill="currentColor" viewBox="0 0 24 24"><path d="M0 0"/><circle cx="5" cy="5" r="3"/></svg>'
            const inner = extractSvgInner(svg)
            // <path> and <circle> should gain fill="currentColor" since they had none
            expect(inner).toContain('<path fill="currentColor"')
            expect(inner).toContain('<circle fill="currentColor"')
        })

        it('does not add currentColor to elements that already have fill', () => {
            const svg = '<svg fill="currentColor" viewBox="0 0 24 24"><path fill="#412991" d="M0 0"/></svg>'
            const inner = extractSvgInner(svg)
            expect(inner).toContain('fill="#412991"')
            expect(inner).not.toContain('fill="currentColor"')
        })

        it('propagates currentColor to <g> and <defs> container elements', () => {
            const svg = '<svg fill="currentColor" viewBox="0 0 24 24"><g id="layer1"><path d="M0 0"/></g></svg>'
            const inner = extractSvgInner(svg)
            expect(inner).toContain('<g fill="currentColor"')
        })

        it('removes <title> elements to avoid aria-label conflict', () => {
            const svg = '<svg viewBox="0 0 24 24"><title>OpenAI</title><path d="M0 0"/></svg>'
            const inner = extractSvgInner(svg)
            expect(inner).not.toContain('<title>')
            expect(inner).not.toContain('OpenAI')
            expect(inner).toContain('<path')
        })

        it('does not modify explicit fills when no currentColor on svg tag', () => {
            const svg = '<svg viewBox="0 0 24 24"><path fill="#412991" d="M0 0"/></svg>'
            const inner = extractSvgInner(svg)
            expect(inner).toContain('fill="#412991"')
        })
    })

    describe('extractViewBox', () => {
        it('extracts viewBox from SVG string', () => {
            expect(extractViewBox('<svg viewBox="0 0 24 24"><path/></svg>')).toBe('0 0 24 24')
        })

        it('returns default viewBox when not found', () => {
            expect(extractViewBox('<svg><path/></svg>')).toBe('0 0 24 24')
        })
    })

    describe('getProviderSvgHtml', () => {
        it('returns processed SVG inner HTML for known provider', () => {
            const html = getProviderSvgHtml('claude')
            expect(html).not.toBeNull()
            expect(html!.length).toBeGreaterThan(0)
            expect(html!).not.toContain('<svg')
        })

        it('returns null for unknown provider', () => {
            expect(getProviderSvgHtml('unknown')).toBeNull()
        })

        it('preserves currentColor for monochrome providers (CSS controls color)', () => {
            const html = getProviderSvgHtml('openai')
            expect(html).not.toBeNull()
            // OpenAI icon keeps fill="currentColor" — CSS class `mono-openai` controls the color
            expect(html!).toContain('fill="currentColor"')
        })
    })

    describe('getProviderViewBox', () => {
        it('returns viewBox for known provider', () => {
            expect(getProviderViewBox('claude')).toBeTruthy()
        })

        it('returns default viewBox for unknown provider', () => {
            expect(getProviderViewBox('unknown')).toBe('0 0 24 24')
        })
    })
})

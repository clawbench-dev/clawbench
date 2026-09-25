import { describe, it, expect, vi, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const holder = vi.hoisted(() => ({
  serverConfig: null as any,
  toastShow: vi.fn(),
  generateSessionTitle: vi.fn(),
}))

vi.mock('@/composables/useLocale', () => ({ gt: (key: string) => key }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: holder.toastShow }) }))
vi.mock('@/composables/useSessionIdentity', () => ({ generateSessionTitle: holder.generateSessionTitle }))
vi.mock('@/composables/useSettingsConfig', async () => {
  const { ref } = await import('vue')
  holder.serverConfig = ref<Record<string, unknown>>({})
  return { useSettingsConfig: () => ({ serverConfig: holder.serverConfig }) }
})

import { buildRenameGenerateOptions } from '@/utils/sessionRename'

describe('buildRenameGenerateOptions', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    holder.serverConfig.value = {}
  })

  it('returns no options when the summary model is not configured', () => {
    expect(buildRenameGenerateOptions('s1')).toEqual({})
  })

  it('returns no options when ai_summary exists but has no api block', () => {
    holder.serverConfig.value = { ai_summary: { model: 'gpt-4o-mini' } }
    expect(buildRenameGenerateOptions('s1')).toEqual({})
  })

  it('returns no options when the base_url is an empty string', () => {
    holder.serverConfig.value = { ai_summary: { api: { base_url: '' } } }
    expect(buildRenameGenerateOptions('s1')).toEqual({})
  })

  it('offers the button when the base_url is configured', () => {
    holder.serverConfig.value = { ai_summary: { api: { base_url: 'https://summary.example.com' } } }
    const opts = buildRenameGenerateOptions('s1')
    expect(opts.generateText).toBe('chat.sessionRename.generate')
    expect(typeof opts.onGenerate).toBe('function')
  })

  it('resolves the generated title for the requested session', async () => {
    holder.serverConfig.value = { ai_summary: { api: { base_url: 'https://summary.example.com' } } }
    holder.generateSessionTitle.mockResolvedValue('Generated Title')
    const opts = buildRenameGenerateOptions('s1')

    await expect(opts.onGenerate()).resolves.toBe('Generated Title')
    expect(holder.generateSessionTitle).toHaveBeenCalledWith('s1')
    expect(holder.toastShow).not.toHaveBeenCalled()
  })

  it('toasts and resolves null when generation yields nothing', async () => {
    holder.serverConfig.value = { ai_summary: { api: { base_url: 'https://summary.example.com' } } }
    holder.generateSessionTitle.mockResolvedValue(null)
    const opts = buildRenameGenerateOptions('s1')

    await expect(opts.onGenerate()).resolves.toBeNull()
    expect(holder.toastShow).toHaveBeenCalledWith('chat.sessionRename.generateFailed', expect.anything())
  })
})

// Both rename entry points are the SAME action reached two ways, so a second
// inline copy of this logic would drift silently (a changed config path or key
// would apply to only one). Behavior tests cannot catch that — each entry point
// would still work on its own — so this asserts the shared builder is used.
describe('rename generate options are shared, not duplicated', () => {
  const files = {
    'App.vue': resolve(process.cwd(), 'src/App.vue'),
    'SessionList.vue': resolve(process.cwd(), 'src/components/session/SessionList.vue'),
  }

  it('both entry points call the shared builder', () => {
    for (const [name, path] of Object.entries(files)) {
      const src = readFileSync(path, 'utf8')
      expect(src, `${name} must use the shared builder`).toContain('buildRenameGenerateOptions(')
    }
  })

  it('neither entry point inlines the gating or the generate payload', () => {
    for (const [name, path] of Object.entries(files)) {
      const src = readFileSync(path, 'utf8')
      // The config path and the onGenerate producer must live only in the builder.
      expect(src, `${name} must not re-implement the summary-model gate`).not.toContain('ai_summary')
      expect(src, `${name} must not re-implement the generate payload`).not.toContain('generateSessionTitle(')
    }
  })
})

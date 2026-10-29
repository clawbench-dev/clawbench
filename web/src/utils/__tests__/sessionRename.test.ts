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
  // The frontend lives in web/, but the two ways to run the suite disagree on
  // cwd (bare `vitest` from web/ vs `npm test` → scripts/vitest-run.sh from the
  // repo root). Probe both so this guard cannot pass locally and ENOENT in CI.
  function webFile(relPath: string): string {
    for (const base of [process.cwd(), resolve(process.cwd(), 'web'), resolve(process.cwd(), '../web')]) {
      try {
        return readFileSync(resolve(base, relPath), 'utf8')
      } catch {
        // try the next candidate root
      }
    }
    throw new Error(`${relPath} not found from cwd: ${process.cwd()}`)
  }

  const files = {
    'App.vue': 'src/App.vue',
    'SessionList.vue': 'src/components/session/SessionList.vue',
  }

  it('both entry points call the shared builder', () => {
    for (const [name, relPath] of Object.entries(files)) {
      const src = webFile(relPath)
      expect(src, `${name} must use the shared builder`).toContain('buildRenameGenerateOptions(')
    }
  })

  it('neither entry point inlines the gating or the generate payload', () => {
    for (const [name, relPath] of Object.entries(files)) {
      const src = webFile(relPath)
      // The config path and the onGenerate producer must live only in the builder.
      expect(src, `${name} must not re-implement the summary-model gate`).not.toContain('ai_summary')
      expect(src, `${name} must not re-implement the generate payload`).not.toContain('generateSessionTitle(')
    }
  })
})

// The COMMIT half must be shared too, not just the generate button.
//
// The list used to PATCH on its own and update only its own row. Renaming the
// session that was already open therefore left the chat header (which renders
// `currentSessionTitle` and nothing else) showing the old title until the
// session was reopened — while renaming from the header worked, because that
// path went through the composable. Both are the same action on the same
// session, so the persistence + state convergence lives in one place now.
describe('rename commit path is shared, not duplicated', () => {
  function webFile(relPath: string): string {
    for (const base of [process.cwd(), resolve(process.cwd(), 'web'), resolve(process.cwd(), '../web')]) {
      try {
        return readFileSync(resolve(base, relPath), 'utf8')
      } catch {
        // try the next candidate root
      }
    }
    throw new Error(`${relPath} not found from cwd: ${process.cwd()}`)
  }

  it('the session list commits through the shared helper', () => {
    const src = webFile('src/components/session/SessionList.vue')
    expect(src).toContain('renameSessionById')
    // A title PATCH of its own is what drifted; pinning still uses apiPatch and
    // is fine, so this looks for the title payload specifically.
    expect(src).not.toMatch(/apiPatch\([^)]*session\/update[^)]*\{\s*title:/)
  })

  it('the chat header commits through the same composable entry point', () => {
    const src = webFile('src/App.vue')
    expect(src).toContain('sessionIdentity.renameSession(')
  })

  it('the shared helper is what writes the open session title', () => {
    const src = webFile('src/composables/useSessionIdentity.ts')
    const start = src.indexOf('export async function renameSessionById')
    expect(start, 'renameSessionById must exist').toBeGreaterThan(-1)
    const body = src.slice(start, src.indexOf('\n}\n', start))
    // The header has no other source for the title, so a successful rename of
    // the OPEN session must write it here.
    expect(body).toContain('currentSessionTitle.value = trimmed')
    expect(body).toContain('sessionListVersion++')
  })
})

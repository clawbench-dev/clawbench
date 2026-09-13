import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockFetchBinding = vi.fn()

vi.mock('@/utils/forgeApi', () => ({
  fetchForgeBinding: (...a: unknown[]) => mockFetchBinding(...a),
}))

import { useForgeBinding, setForgeBindingState, forgeDockIconKind } from '@/composables/useForgeBinding'

describe('useForgeBinding', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset the module singleton between tests.
    setForgeBindingState(null)
  })

  it('refresh exposes the bound platform and host', async () => {
    mockFetchBinding.mockResolvedValue({
      binding: { platform: 'gitlab', host: 'git.acme.internal', owner: 'a', repo: 'b' },
    })
    const { platform, host, refresh } = useForgeBinding()
    await refresh()
    expect(platform.value).toBe('gitlab')
    expect(host.value).toBe('git.acme.internal')
  })

  it('an unbound project clears the platform', async () => {
    setForgeBindingState({ platform: 'github', host: 'github.com' })
    mockFetchBinding.mockResolvedValue({ binding: null })
    const { platform, refresh } = useForgeBinding()
    await refresh()
    expect(platform.value).toBe('')
  })

  it('a failed refresh clears rather than keeping a stale platform', async () => {
    setForgeBindingState({ platform: 'github', host: 'github.com' })
    mockFetchBinding.mockRejectedValue(new Error('network'))
    const { platform, refresh } = useForgeBinding()
    await refresh()
    // A stale platform would show the wrong brand icon indefinitely.
    expect(platform.value).toBe('')
  })

  it('setForgeBindingState is the single source of truth for the dock icon', () => {
    const { platform } = useForgeBinding()
    setForgeBindingState({ platform: 'gitlab' })
    expect(platform.value).toBe('gitlab')
    setForgeBindingState(null)
    expect(platform.value).toBe('')
  })
})

describe('forgeDockIconKind', () => {
  it('defaults to GitHub when nothing is bound', () => {
    expect(forgeDockIconKind('')).toBe('github')
  })

  it('uses GitHub for a bound GitHub repository', () => {
    expect(forgeDockIconKind('github')).toBe('github')
  })

  it('switches to GitLab only for a bound GitLab repository', () => {
    expect(forgeDockIconKind('gitlab')).toBe('gitlab')
  })

  it('treats an unrecognized platform as GitHub rather than blank', () => {
    expect(forgeDockIconKind('bitbucket')).toBe('github')
  })
})

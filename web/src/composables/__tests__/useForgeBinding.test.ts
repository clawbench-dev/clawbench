import { describe, expect, it, vi, beforeEach } from 'vitest'

const mockFetchBinding = vi.fn()

vi.mock('@/utils/forgeApi', () => ({
  fetchForgeBinding: (...a: unknown[]) => mockFetchBinding(...a),
}))

import { useForgeBinding, setForgeBindingState, resetForgeBindingState, forgeDockIconKind } from '@/composables/useForgeBinding'

describe('useForgeBinding', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset the module singleton between tests.
    resetForgeBindingState()
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
    await refresh(true)
    expect(platform.value).toBe('')
  })

  it('a failed refresh clears rather than keeping a stale platform', async () => {
    setForgeBindingState({ platform: 'github', host: 'github.com' })
    mockFetchBinding.mockRejectedValue(new Error('network'))
    const { platform, refresh } = useForgeBinding()
    await refresh(true)
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

  it('exposes owner/repo as slug, empty when unbound', async () => {
    mockFetchBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets' },
    })
    const { slug, refresh } = useForgeBinding()
    await refresh()
    expect(slug.value).toBe('acme/widgets')

    mockFetchBinding.mockResolvedValue({ binding: null })
    await refresh(true)
    expect(slug.value).toBe('')
  })

  // The distinction the task views rely on: an unresolved lookup must not be
  // presented as "confirmed unbound", or the UI flashes a false warning.
  it('reports resolved=false until the first lookup completes', async () => {
    const { resolved, refresh } = useForgeBinding()
    expect(resolved.value).toBe(false)

    mockFetchBinding.mockResolvedValue({ binding: null })
    await refresh()
    expect(resolved.value).toBe(true)
  })

  it('resetForgeBindingState returns to the unresolved state', async () => {
    mockFetchBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'a', repo: 'b' },
    })
    const { resolved, slug, refresh } = useForgeBinding()
    await refresh()
    expect(resolved.value).toBe(true)

    resetForgeBindingState()
    expect(resolved.value).toBe(false)
    expect(slug.value).toBe('')
  })
})

// A single navigation used to fire 3-5 identical binding requests (dock icon,
// task list, task form, event card, forge panel). They queued behind each
// other on the server's 2-connection read pool, so the last one could take
// hundreds of milliseconds for a 1ms handler.
describe('useForgeBinding request coalescing', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetForgeBindingState()
  })

  it('shares one in-flight request between concurrent callers', async () => {
    let release: (v: unknown) => void = () => {}
    mockFetchBinding.mockReturnValue(new Promise(resolve => { release = resolve }))

    const a = useForgeBinding()
    const b = useForgeBinding()
    const c = useForgeBinding()
    const p1 = a.refresh()
    const p2 = b.refresh()
    const p3 = c.refresh()

    // All three joined the same request instead of issuing their own.
    expect(mockFetchBinding).toHaveBeenCalledTimes(1)

    release({ binding: { platform: 'github', host: 'github.com', owner: 'a', repo: 'b' } })
    await Promise.all([p1, p2, p3])

    expect(mockFetchBinding).toHaveBeenCalledTimes(1)
    expect(a.slug.value).toBe('a/b')
    expect(b.slug.value).toBe('a/b')
    expect(c.slug.value).toBe('a/b')
  })

  it('serves a repeat lookup from cache within the TTL', async () => {
    mockFetchBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'a', repo: 'b' },
    })
    const { refresh } = useForgeBinding()
    await refresh()
    await refresh()
    await refresh()
    // Only the first call hit the network.
    expect(mockFetchBinding).toHaveBeenCalledTimes(1)
  })

  it('force bypasses the cache after a write', async () => {
    mockFetchBinding.mockResolvedValue({
      binding: { platform: 'github', host: 'github.com', owner: 'a', repo: 'b' },
    })
    const { refresh } = useForgeBinding()
    await refresh()
    await refresh(true)
    expect(mockFetchBinding).toHaveBeenCalledTimes(2)
  })

  // A force refresh follows a write, so an older in-flight lookup — started
  // before that write — carries the pre-write answer. Adopting it would show
  // the repository the user just switched away from.
  it('force supersedes an in-flight lookup instead of joining it', async () => {
    const releases: Array<(v: unknown) => void> = []
    mockFetchBinding.mockImplementation(() => new Promise(resolve => { releases.push(resolve) }))

    const { refresh, slug } = useForgeBinding()
    const stale = refresh()          // in flight, will answer with the old repo
    const fresh = refresh(true)      // issued after the write

    expect(mockFetchBinding).toHaveBeenCalledTimes(2)

    // The stale request answers last, and must not win.
    releases[1]({ binding: { platform: 'github', host: 'github.com', owner: 'new', repo: 'repo' } })
    await fresh
    releases[0]({ binding: { platform: 'github', host: 'github.com', owner: 'old', repo: 'repo' } })
    await stale

    expect(slug.value).toBe('new/repo')
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

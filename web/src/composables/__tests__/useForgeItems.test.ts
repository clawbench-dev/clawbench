import { describe, expect, it, vi, beforeEach } from 'vitest'

// The composable resolves through the shared forge API client; mock it so the
// tests assert request shaping and state handling rather than HTTP.
const mockFetchForgeItems = vi.fn()

vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<typeof import('@/utils/forgeApi')>('@/utils/forgeApi')
  return {
    ...actual,
    fetchForgeItems: (...a: unknown[]) => mockFetchForgeItems(...a),
  }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useForgeItems } from '@/composables/useForge'
import { setForgeBindingState } from '@/composables/useForgeBinding'

const binding = {
  platform: 'github', host: 'github.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets',
}

function page(overrides: Record<string, unknown> = {}) {
  return { items: [], hasMore: false, nextPage: 2, binding, ...overrides }
}

/** The state filter sent on the most recent request. */
function lastState(): string {
  const calls = mockFetchForgeItems.mock.calls
  return (calls[calls.length - 1][0] as { state: string }).state
}

describe('useForgeItems state filter', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetchForgeItems.mockResolvedValue(page())
    setForgeBindingState(binding)
  })

  it('defaults to open', () => {
    const items = useForgeItems(() => '/proj')
    expect(items.state.value).toBe('open')
  })

  it('forwards merged as a state filter', async () => {
    const items = useForgeItems(() => '/proj')
    items.setType('pr')
    await Promise.resolve()
    mockFetchForgeItems.mockClear()

    items.setState('merged')
    await Promise.resolve()

    // The filter is the whole point: GitLab reports merged as its own MR state,
    // and GitHub needs a search qualifier for it. Neither can serve it if the
    // value never leaves the client.
    expect(items.state.value).toBe('merged')
    expect(lastState()).toBe('merged')
  })

  it('resets a merged filter when switching back to the issues tab', async () => {
    // Issues have no merged lifecycle on either platform, so leaving the filter
    // selected would query an impossible state and show a permanently empty
    // list with no visible cause.
    const items = useForgeItems(() => '/proj')
    items.setType('pr')
    await Promise.resolve()
    items.setState('merged')
    await Promise.resolve()
    expect(items.state.value).toBe('merged')

    mockFetchForgeItems.mockClear()
    items.setType('issue')
    await Promise.resolve()

    expect(items.state.value).toBe('open')
    expect(lastState()).toBe('open')
  })

  it('keeps a non-merged filter when switching tabs', async () => {
    // Only the impossible filter is reset; a valid one must survive the switch
    // or the user's selection would be silently discarded.
    const items = useForgeItems(() => '/proj')
    items.setType('pr')
    await Promise.resolve()
    items.setState('closed')
    await Promise.resolve()

    mockFetchForgeItems.mockClear()
    items.setType('issue')
    await Promise.resolve()

    expect(items.state.value).toBe('closed')
    expect(lastState()).toBe('closed')
  })
})

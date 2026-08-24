import { describe, it, expect, vi, afterEach } from 'vitest'
import { getIconUrl } from '@/utils/materialIcons'

/**
 * Tests for getIconUrl() caching and dedup behavior against the real module.
 *
 * Icons are now static assets resolved by URL (no import.meta.glob), so we
 * stub the global fetch used internally by checkIconExists().
 */

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('getIconUrl caching and dedup logic', () => {
  it('should load and cache icon URL', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))
    const url = await getIconUrl('go')
    expect(url).toBe('/material-icons/go.svg')
  })

  it('should return cached URL on subsequent calls', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const first = await getIconUrl('typescript')
    const second = await getIconUrl('typescript')
    expect(first).toBe(second)
    expect(fetchMock).toHaveBeenCalledTimes(1) // Only checked once
  })

  it('should dedup concurrent loads for the same icon', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const [a, b, c] = await Promise.all([
      getIconUrl('folder'),
      getIconUrl('folder'),
      getIconUrl('folder'),
    ])

    expect(a).toBe(b)
    expect(b).toBe(c)
    expect(fetchMock).toHaveBeenCalledTimes(1) // dedup via iconUrlPending
  })

  it('should return undefined for unknown icon names', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))
    const url = await getIconUrl('nonexistent-icon')
    expect(url).toBeUndefined()
  })

  it('should handle different icons independently', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const [goUrl, tsUrl] = await Promise.all([
      getIconUrl('swift'),
      getIconUrl('kotlin'),
    ])
    expect(goUrl).toBe('/material-icons/swift.svg')
    expect(tsUrl).toBe('/material-icons/kotlin.svg')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('should handle fetch failure gracefully', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network down')))
    const url = await getIconUrl('broken')
    expect(url).toBeUndefined()
  })
})

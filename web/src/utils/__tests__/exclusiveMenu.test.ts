import { describe, expect, it, vi, beforeEach } from 'vitest'
import { claimOpenMenu, releaseOpenMenu, resetOpenMenuForTest } from '@/utils/exclusiveMenu'

/**
 * The exclusive-open slot is what stops two teleported menus from being on
 * screen at once. It lives in a real module (not a `<script setup>` top-level
 * binding, which would be per-instance and coordinate nothing).
 */
describe('exclusiveMenu', () => {
  beforeEach(() => resetOpenMenuForTest())

  it('closes the previous holder when a new menu claims the slot', () => {
    const first = { close: vi.fn() }
    const second = { close: vi.fn() }

    claimOpenMenu(first)
    claimOpenMenu(second)

    expect(first.close).toHaveBeenCalledTimes(1)
    expect(second.close).not.toHaveBeenCalled()
  })

  it('does not close the holder when it re-claims its own slot', () => {
    const only = { close: vi.fn() }
    claimOpenMenu(only)
    claimOpenMenu(only)
    expect(only.close).not.toHaveBeenCalled()
  })

  it('releases the slot so the next claim does not close anyone', () => {
    const first = { close: vi.fn() }
    const second = { close: vi.fn() }

    claimOpenMenu(first)
    releaseOpenMenu(first)
    claimOpenMenu(second)

    expect(first.close).not.toHaveBeenCalled()
  })

  it('ignores a stale release from a superseded instance', () => {
    // The sequence that would otherwise re-open the door to two menus at once:
    // A opens, B opens (closing A), then A's own close handler runs late and
    // must NOT clear B's slot.
    const first = { close: vi.fn() }
    const second = { close: vi.fn() }

    claimOpenMenu(first)
    claimOpenMenu(second) // closes first
    releaseOpenMenu(first) // stale — must be ignored

    const third = { close: vi.fn() }
    claimOpenMenu(third)
    expect(second.close, 'the real holder must still be closed by the next claim').toHaveBeenCalledTimes(1)
  })
})

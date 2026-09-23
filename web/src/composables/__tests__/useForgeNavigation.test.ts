import { describe, expect, it, beforeEach } from 'vitest'
import {
  pendingForgeTarget,
  setPendingForgeTarget,
  consumePendingForgeTarget,
  clearPendingForgeTarget,
  type ForgeTarget,
} from '@/composables/useForgeNavigation'

function target(overrides: Partial<ForgeTarget> = {}): ForgeTarget {
  return { type: 'pr', number: 42, runId: 0, itemKey: 'pr/42', ...overrides }
}

describe('useForgeNavigation', () => {
  beforeEach(() => {
    clearPendingForgeTarget()
  })

  it('starts with nothing pending', () => {
    expect(pendingForgeTarget.value).toBeNull()
    expect(consumePendingForgeTarget()).toBeNull()
  })

  it('publishes the target on the reactive ref, so a mounted panel can watch it', () => {
    // The panel may already be active when the target arrives, in which case
    // switchTab('forge') is a no-op and the ref change is the ONLY signal.
    const t = target()
    setPendingForgeTarget(t)
    expect(pendingForgeTarget.value).toEqual(t)
  })

  it('consume returns the target once and clears it', () => {
    setPendingForgeTarget(target())
    expect(consumePendingForgeTarget()).toEqual(target())
    // A target that cannot be opened must not fire again on the next activation.
    expect(consumePendingForgeTarget()).toBeNull()
    expect(pendingForgeTarget.value).toBeNull()
  })

  it('carries the opaque itemKey verbatim, including a pipeline run key', () => {
    // A pipeline's identity is the run id; its number is always 0, so rebuilding
    // the key from (type, number) would produce "pipeline/0" and match nothing.
    const t = target({ type: 'pipeline', number: 0, runId: 555, itemKey: 'pipeline/run:555' })
    setPendingForgeTarget(t)
    expect(consumePendingForgeTarget()?.itemKey).toBe('pipeline/run:555')
  })

  it('keeps the destination project so a panel can gate on it', () => {
    setPendingForgeTarget(target({ projectPath: '/home/u/proj-b' }))
    expect(pendingForgeTarget.value?.projectPath).toBe('/home/u/proj-b')
  })

  it('clear drops the target without returning it', () => {
    // Used when a cross-project navigation is abandoned: leaving it armed would
    // make the panel open a stale item on some later matching activation.
    setPendingForgeTarget(target({ projectPath: '/gone' }))
    clearPendingForgeTarget()
    expect(pendingForgeTarget.value).toBeNull()
    expect(consumePendingForgeTarget()).toBeNull()
  })

  it('a later target replaces an earlier one', () => {
    setPendingForgeTarget(target({ itemKey: 'pr/1', number: 1 }))
    setPendingForgeTarget(target({ itemKey: 'pr/2', number: 2 }))
    expect(consumePendingForgeTarget()?.itemKey).toBe('pr/2')
  })
})

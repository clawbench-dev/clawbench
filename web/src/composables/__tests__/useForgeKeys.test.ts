import { describe, expect, it } from 'vitest'
import { forgeItemKey, forgePipelineItemKey, forgeTargetItemKey } from '@/composables/useForge'

/**
 * These keys are the wire contract with the Go side's forge.ItemKey /
 * ItemKeyForNumber / PipelineItemKey. A drift here is silent: the read endpoint
 * simply matches no rows and the badge stays lit with nothing the user can
 * clear. The expected literals below are copied from forge/events.go.
 */
describe('forge item keys match the Go side', () => {
  it('builds "<type>/<number>" for an issue', () => {
    expect(forgeItemKey({ type: 'issue', number: 12 })).toBe('issue/12')
  })

  it('builds "<type>/<number>" for a change request', () => {
    expect(forgeItemKey({ type: 'pr', number: 7 })).toBe('pr/7')
  })

  it('builds "pipeline/run:<id>" for a run', () => {
    expect(forgePipelineItemKey(555)).toBe('pipeline/run:555')
  })

  describe('forgeTargetItemKey', () => {
    it('uses the number for an issue', () => {
      expect(forgeTargetItemKey('issue', 12, 0)).toBe('issue/12')
    })

    it('uses the number for a change request', () => {
      expect(forgeTargetItemKey('pr', 7, 0)).toBe('pr/7')
    })

    it('uses the RUN ID for a pipeline, never its number', () => {
      // The regression this guards: a pipeline stores number 0 for every run,
      // so a key built from (type, number) would be "pipeline/0" — a key that
      // matches nothing and would leave the run permanently unread.
      expect(forgeTargetItemKey('pipeline', 0, 555)).toBe('pipeline/run:555')
      expect(forgeTargetItemKey('pipeline', 0, 555)).not.toBe('pipeline/0')
    })
  })
})

import { describe, expect, it, vi } from 'vitest'

// The label helper resolves through vue-i18n. This mock interpolates the
// {runId} placeholder (the shared test mock only echoes the key, which would
// hide a broken interpolation) so the pipeline case is asserted for real.
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string, params?: Record<string, unknown>) => {
        if (params && 'runId' in params) return `${key}:${params.runId}`
        return key
      },
      locale: { value: 'en' },
    },
  },
}))

import { forgeOverviewLabel, unreadReasonLabel } from '@/utils/forgeEventLabels'

describe('unreadReasonLabel', () => {
  it('maps each stored event type to its own reason key', () => {
    expect(unreadReasonLabel('commented')).toBe('forge.overview.reason.commented')
    expect(unreadReasonLabel('closed')).toBe('forge.overview.reason.closed')
    expect(unreadReasonLabel('pipeline_done')).toBe('forge.overview.reason.pipeline_done')
  })

  it('falls back to the raw token for an unknown event type', () => {
    // A new backend event type must degrade to something readable, never to an
    // empty string (which would render a row with no explanation at all).
    expect(unreadReasonLabel('some_future_event')).toBe('some_future_event')
    expect(unreadReasonLabel('')).toBe('')
  })
})

describe('forgeOverviewLabel', () => {
  it('labels an issue with its type, number and reason', () => {
    const label = forgeOverviewLabel({
      type: 'issue', number: 42, runId: 0, eventType: 'commented', slug: 'acme/widgets',
    })
    expect(label).toBe('acme/widgets issue#42 · forge.overview.reason.commented')
  })

  it('labels a pull request', () => {
    const label = forgeOverviewLabel({
      type: 'pr', number: 455, runId: 0, eventType: 'closed', slug: 'clawbench-dev/clawbench',
    })
    expect(label).toBe('clawbench-dev/clawbench pr#455 · forge.overview.reason.closed')
  })

  it('identifies a pipeline by its run id, never by #0', () => {
    // A pipeline stores number 0 for every run, so rendering "#0" would look
    // like a broken PR reference and would not distinguish two runs.
    const label = forgeOverviewLabel({
      type: 'pipeline', number: 0, runId: 555, eventType: 'pipeline_done', slug: 'acme/widgets',
    })
    expect(label).not.toContain('#0')
    expect(label).toContain('555')
    expect(label).toBe('acme/widgets forge.overview.pipelineRef:555 · forge.overview.reason.pipeline_done')
  })

  it('always includes the repository slug', () => {
    const label = forgeOverviewLabel({
      type: 'pr', number: 1, runId: 0, eventType: 'opened', slug: 'owner/repo',
    })
    expect(label.startsWith('owner/repo ')).toBe(true)
  })
})

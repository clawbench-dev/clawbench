import { describe, expect, it, vi } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

// The registry resolves labels through the app i18n singleton, which a bare
// vue-i18n mock leaves undefined (importing @/i18n would throw). The mock
// returns the key so assertions can name a variable by its i18n key.
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
}))

import {
  EVENT_CONTEXT_VARS,
  eventContextSampleRows,
  eventContextTemplateText,
  eventVarVisible,
  isRepoTargetedOnly,
  visibleEventContextVars,
} from '../forgeEventContextVars'

const ctx = (transitions: string[]) => {
  const set = new Set(transitions)
  return { transitions: set, repoTargetedOnly: isRepoTargetedOnly(set) }
}

/** The placeholders a subscription yields, in order. */
const placeholders = (transitions: string[]) =>
  visibleEventContextVars(ctx(transitions)).map(v => v.placeholder)

describe('forgeEventContextVars', () => {
  it('lists every variable the backend renders', () => {
    // A subscription that can yield everything: every kind plus the comment and
    // pipeline transitions. A variable missing here is one the UI would
    // silently stop advertising.
    const all = placeholders(['opened', 'closed', 'merged', 'reopened', 'commented', 'pipeline_done'])
    for (const p of [
      'EVENT_TYPE', 'REPO', 'ITEM_TYPE #ITEM_NUMBER', 'TITLE', 'URL', 'AUTHOR', 'STATE',
      'PREV_STATE', 'BODY', 'LABELS', 'ASSIGNEES', 'DRAFT', 'SOURCE_BRANCH', 'MERGED_AT',
      'CREATED_AT', 'UPDATED_AT', 'COMMENT_COUNT', 'COMMENT_BODY', 'COMMENT_ID',
      'PIPELINE_STATUS', 'PIPELINE_URL', 'PIPELINE_NUMBER', 'PIPELINE_REF', 'PIPELINE_SHA',
      'PIPELINE_TRIGGER', 'PIPELINE_DURATION', 'PIPELINE_LINKED_PRS', 'ACTOR_IS_SELF',
    ]) {
      expect(all, `missing ${p}`).toContain(p)
    }
  })

  // With no event selected the task has no trigger, so the backend injects no
  // context block. Listing variables then would document a payload that can
  // never arrive.
  it('yields nothing for an empty subscription', () => {
    expect(visibleEventContextVars(ctx([]))).toEqual([])
    expect(eventContextTemplateText(ctx([]))).toBe('')
    expect(eventContextSampleRows(ctx([]))).toEqual([])
  })

  it('has no duplicate placeholders', () => {
    const all = EVENT_CONTEXT_VARS.map(v => v.placeholder)
    expect(new Set(all).size).toBe(all.length)
  })

  // The preview must read in the same order as the real prompt, which is the
  // backend's eventContextVars slice. The two lists live in different languages,
  // so nothing else can catch a reordering.
  it('matches the backend variable order', () => {
    // The Go source is the authority; read it rather than duplicating the list.
    // readWebFile probes both cwds the suite may run under (web/ locally, the
    // repo root in CI), so this does not depend on how the runner was invoked.
    const go = readWebFile('../internal/service/forge_event_context.go')
    const backendOrder = [...go.matchAll(/placeholder:\s+"([^"]+)"/g)].map(m => m[1])
    expect(backendOrder.length, 'backend variables not found — did the source move?').toBeGreaterThan(0)
    expect(EVENT_CONTEXT_VARS.map(v => v.placeholder)).toEqual(backendOrder)
  })

  // The regression this registry exists to prevent: subscriptions are stored
  // kind-scoped ("pr.commented") while a variable's scope names a bare
  // transition, so a caller that forgets to normalize matches nothing.
  it('shows the comment variables for a kind-scoped commented subscription', () => {
    const vars = placeholders(['commented'])
    expect(vars).toContain('COMMENT_BODY')
    expect(vars).toContain('COMMENT_ID')
    expect(vars).not.toContain('PIPELINE_STATUS')
  })

  it('shows the pipeline variables for a pipeline subscription', () => {
    const vars = placeholders(['pipeline_done'])
    expect(vars).toContain('PIPELINE_STATUS')
    expect(vars).toContain('PIPELINE_REF')
    expect(vars).toContain('PIPELINE_SHA')
    expect(vars).not.toContain('COMMENT_BODY')
  })

  it('shows PREV_STATE for a state transition', () => {
    expect(placeholders(['opened'])).toContain('PREV_STATE')
    expect(placeholders(['merged'])).toContain('PREV_STATE')
  })

  // `commented` carries a previous state equal to the current one, so rendering
  // it would emit the noise "状态：open / 前一状态：open".
  it('hides PREV_STATE for a commented-only subscription', () => {
    expect(placeholders(['commented'])).not.toContain('PREV_STATE')
  })

  // A pipeline run has no item identity, so item-scoped variables can never
  // arrive and must not be advertised.
  it('hides every item-scoped variable for a pipeline-only subscription', () => {
    const vars = placeholders(['pipeline_done'])
    for (const hidden of [
      'ITEM_TYPE #ITEM_NUMBER', 'BODY', 'LABELS', 'ASSIGNEES', 'DRAFT',
      'SOURCE_BRANCH', 'MERGED_AT', 'COMMENT_COUNT',
    ]) {
      expect(vars, `${hidden} must be hidden`).not.toContain(hidden)
    }
  })

  it('keeps the item variables when a pipeline is combined with a PR', () => {
    const vars = placeholders(['pipeline_done', 'pr.opened'])
    expect(vars).toContain('ITEM_TYPE #ITEM_NUMBER')
    expect(vars).toContain('BODY')
    expect(vars).toContain('PIPELINE_STATUS')
  })

  it('detects a repository-targeted-only subscription', () => {
    expect(isRepoTargetedOnly(['pipeline_done'])).toBe(true)
    expect(isRepoTargetedOnly(['pipeline_done', 'pr.opened'])).toBe(false)
    // An empty subscription is not repo-targeted: it is "not configured yet".
    expect(isRepoTargetedOnly([])).toBe(false)
  })

  it('eventVarVisible agrees with the filtered list', () => {
    // A single source of truth: the predicate and the filter cannot disagree,
    // or a caller using one would render a different block than the other.
    const c = ctx(['pr.commented'])
    for (const v of EVENT_CONTEXT_VARS) {
      expect(eventVarVisible(v, c)).toBe(visibleEventContextVars(c).includes(v))
    }
  })

  describe('eventContextTemplateText', () => {
    it('renders a placeholder block in prompt order', () => {
      const out = eventContextTemplateText(ctx(['pr.opened']))
      expect(out).toContain('{{EVENT_TYPE}}')
      expect(out).toContain('{{REPO}}')
      expect(out).toContain('{{BODY}}')
      // Order matters: the preview must read like the real prompt.
      expect(out.indexOf('{{EVENT_TYPE}}')).toBeLessThan(out.indexOf('{{BODY}}'))
    })

    it('marks a two-value variable as one line', () => {
      const out = eventContextTemplateText(ctx(['pr.opened']))
      expect(out).toContain('{{ITEM_TYPE #ITEM_NUMBER}}')
      expect(out).not.toContain('{{ITEM_TYPE}}')
    })

    it('omits variables the subscription cannot yield', () => {
      const out = eventContextTemplateText(ctx(['pr.opened']))
      expect(out).not.toContain('{{PIPELINE_')
      expect(out).not.toContain('{{COMMENT_BODY}}')
    })
  })

  describe('eventContextSampleRows', () => {
    it('fills every visible row with a value', () => {
      // A blank sample would render an empty row and read as a broken preview.
      const rows = eventContextSampleRows(
        ctx(['opened', 'commented', 'pipeline_done']),
      )
      expect(rows.length).toBeGreaterThan(0)
      for (const row of rows) {
        expect(row.value, `${row.placeholder} needs a sample`).not.toBe('')
      }
    })

    it('applies caller overrides by placeholder token', () => {
      const rows = eventContextSampleRows(ctx(['pr.opened']), {
        REPO: 'acme/widgets',
        'ITEM_TYPE #ITEM_NUMBER': 'issue #7',
      })
      const byPlaceholder = new Map(rows.map(r => [r.placeholder, r.value]))
      expect(byPlaceholder.get('REPO')).toBe('acme/widgets')
      expect(byPlaceholder.get('ITEM_TYPE #ITEM_NUMBER')).toBe('issue #7')
    })

    it('excludes the item row for a pipeline-only subscription', () => {
      const rows = eventContextSampleRows(ctx(['pipeline_done']))
      expect(rows.map(r => r.placeholder)).not.toContain('ITEM_TYPE #ITEM_NUMBER')
    })
  })
})

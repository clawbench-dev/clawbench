import { describe, expect, it, vi } from 'vitest'

// The label helpers resolve through vue-i18n; the mock echoes the key so the
// tests assert the mapping rather than the translated string.
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string) => key,
      locale: { value: 'en' },
    },
  },
}))

import {
  splitEventTypes,
  expandStoredEventTypes,
  offeredEventValues,
  parseEventKey,
  eventChips,
  eventTypesSummary,
  parseEventUrl,
  eventSourceLabel,
  eventKindLabel,
  FORGE_EVENT_TRANSITIONS,
  FORGE_REPO_TARGETED_TRANSITIONS,
} from '@/utils/forgeEventLabels'

describe('eventKindLabel', () => {
  // The i18n mock echoes keys, so this asserts the MAPPING (which key a wire
  // value resolves to). A missing entry is the failure mode that matters: the
  // helper falls back to the raw token, so an unmapped "pipeline" rendered an
  // untranslated English word inside a Chinese notification title.
  it('maps every item type the backend can put on the wire', () => {
    expect(eventKindLabel('issue')).toBe('task.form.eventKindIssue')
    expect(eventKindLabel('pr')).toBe('task.form.eventKindPr')
    // forge.ItemTypePipeline = "pipeline" — the real wire value for a CI event.
    expect(eventKindLabel('pipeline')).toBe('task.form.eventKindRepo')
    // The form's pseudo-kind, still used for subscription grouping.
    expect(eventKindLabel('repo')).toBe('task.form.eventKindRepo')
  })

  it('falls back to the raw token for an unrecognized kind', () => {
    // Deliberate: a future backend type degrades to something readable rather
    // than an empty string. It must NOT silently resolve to a real label.
    expect(eventKindLabel('bogus')).toBe('bogus')
  })
})

describe('splitEventTypes', () => {
  it('splits, trims and drops empties', () => {
    expect(splitEventTypes('a,, b ,c')).toEqual(['a', 'b', 'c'])
  })

  it('returns an empty array for empty input', () => {
    expect(splitEventTypes('')).toEqual([])
    expect(splitEventTypes(undefined)).toEqual([])
    expect(splitEventTypes(null)).toEqual([])
  })
})

describe('expandStoredEventTypes', () => {
  it('keeps kind-scoped keys as-is', () => {
    expect(expandStoredEventTypes('issue.opened,pr.merged')).toEqual(['issue.opened', 'pr.merged'])
  })

  it('expands a bare key to every kind that supports the transition', () => {
    // `opened` exists for both kinds; `merged` is PR-only.
    expect(expandStoredEventTypes('opened').sort()).toEqual(['issue.opened', 'pr.opened'])
    expect(expandStoredEventTypes('merged')).toEqual(['pr.merged'])
  })

  it('keeps an unrecognized key verbatim rather than dropping it', () => {
    expect(expandStoredEventTypes('bogus')).toEqual(['bogus'])
  })

  it('keeps a repository-targeted transition BARE', () => {
    // pipeline_done is offered without a kind prefix, and the backend matches it
    // that way. Expanding it to "pipeline.pipeline_done" would produce a key the
    // backend never matches, so the checkbox would silently stop working.
    expect(expandStoredEventTypes('pipeline_done')).toEqual(['pipeline_done'])
  })

  it('does not expand a repository-targeted transition into a kind-scoped key', () => {
    // Guard against a regression where pipeline_done is added to
    // FORGE_EVENT_TRANSITIONS as a pseudo-kind.
    const expanded = expandStoredEventTypes('pipeline_done')
    expect(expanded).not.toContain('pipeline.pipeline_done')
    expect(expanded).not.toContain('repo.pipeline_done')
  })
})

describe('offeredEventValues', () => {
  it('contains every kind-scoped transition plus the bare repository events', () => {
    const offered = offeredEventValues()
    const expected = [
      ...Object.entries(FORGE_EVENT_TRANSITIONS)
        .flatMap(([kind, transitions]) => transitions.map(tr => `${kind}.${tr}`)),
      ...FORGE_REPO_TARGETED_TRANSITIONS,
    ]
    expect([...offered].sort()).toEqual(expected.sort())
    expect(offered.has('issue.merged')).toBe(false)
    expect(offered.has('pr.merged')).toBe(true)
    // Offered bare, because that is how the backend matches it.
    expect(offered.has('pipeline_done')).toBe(true)
    expect(offered.has('pipeline.pipeline_done')).toBe(false)
  })
})

describe('parseEventKey', () => {
  it('splits a kind-scoped key', () => {
    expect(parseEventKey('pr.merged')).toEqual({ kind: 'pr', transition: 'merged' })
  })

  it('treats a bare key as transition-only', () => {
    expect(parseEventKey('opened')).toEqual({ kind: '', transition: 'opened' })
  })
})

describe('eventChips', () => {
  it('renders a legacy bare key as one chip per matching kind', () => {
    const chips = eventChips('opened')
    expect(chips.map(c => c.key).sort()).toEqual(['issue.opened', 'pr.opened'])
    // Labels come from i18n (mocked to the key).
    expect(chips[0].label).toBe('task.form.eventOpened')
  })

  it('returns no chips for an empty subscription', () => {
    expect(eventChips('')).toEqual([])
  })

  it('labels a repository-targeted transition with the repo pseudo-kind', () => {
    // The stored key stays bare (that is what the backend matches), but the
    // chip is labelled so the UI can group it under "Repository pipelines"
    // instead of showing an unlabelled entry.
    const chips = eventChips('pipeline_done')
    expect(chips).toHaveLength(1)
    expect(chips[0].key).toBe('pipeline_done')
    expect(chips[0].kind).toBe('repo')
    expect(chips[0].transition).toBe('pipeline_done')
    expect(chips[0].label).toBe('task.form.eventPipeline')
  })
})

describe('eventTypesSummary', () => {
  it('prefixes each kind-scoped transition with its kind label', () => {
    expect(eventTypesSummary('pr.merged')).toBe('task.form.eventKindPr · task.form.eventMerged')
  })

  it('joins multiple subscriptions', () => {
    expect(eventTypesSummary('issue.opened,pr.merged'))
      .toBe('task.form.eventKindIssue · task.form.eventOpened · task.form.eventKindPr · task.form.eventMerged')
  })

  it('returns an empty string when nothing is subscribed', () => {
    expect(eventTypesSummary('')).toBe('')
  })
})

describe('parseEventUrl', () => {
  it('parses a GitHub pull request URL', () => {
    expect(parseEventUrl('https://github.com/acme/widgets/pull/123'))
      .toEqual({ slug: 'acme/widgets', number: '123', kind: 'pr' })
  })

  it('parses a GitHub issue URL', () => {
    expect(parseEventUrl('https://github.com/acme/widgets/issues/42'))
      .toEqual({ slug: 'acme/widgets', number: '42', kind: 'issue' })
  })

  it('parses a GitLab merge request URL with the /-/ separator', () => {
    expect(parseEventUrl('https://gitlab.com/acme/widgets/-/merge_requests/7'))
      .toEqual({ slug: 'acme/widgets', number: '7', kind: 'pr' })
  })

  it('parses a nested GitLab group path', () => {
    expect(parseEventUrl('https://gitlab.com/group/sub/widgets/-/issues/9'))
      .toEqual({ slug: 'group/sub/widgets', number: '9', kind: 'issue' })
  })

  it('rejects non-issue/PR URLs', () => {
    expect(parseEventUrl('https://github.com/acme/widgets')).toBeNull()
    expect(parseEventUrl('https://github.com/acme/widgets/releases/1')).toBeNull()
  })

  it('rejects a malformed URL instead of throwing', () => {
    expect(parseEventUrl('not a url')).toBeNull()
    expect(parseEventUrl('')).toBeNull()
    expect(parseEventUrl(undefined)).toBeNull()
  })
})

describe('eventSourceLabel', () => {
  it('renders "slug PR #n" for a pull request', () => {
    expect(eventSourceLabel('https://github.com/acme/widgets/pull/123'))
      .toBe('acme/widgets PR #123')
  })

  it('renders the localized kind for an issue', () => {
    expect(eventSourceLabel('https://github.com/acme/widgets/issues/5'))
      .toBe('acme/widgets task.form.eventKindIssue #5')
  })

  it('returns an empty string for an unrecognized URL', () => {
    expect(eventSourceLabel('https://example.com/nope')).toBe('')
    expect(eventSourceLabel(undefined)).toBe('')
  })
})

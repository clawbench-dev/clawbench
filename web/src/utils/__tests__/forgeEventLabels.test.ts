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
  FORGE_EVENT_TRANSITIONS,
} from '@/utils/forgeEventLabels'

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

  it('does not invent keys for a retired transition on a kind that lacks it', () => {
    // pipeline_done is not in FORGE_EVENT_TRANSITIONS at all.
    expect(expandStoredEventTypes('pipeline_done')).toEqual(['pipeline_done'])
  })
})

describe('offeredEventValues', () => {
  it('contains every kind-scoped transition and nothing else', () => {
    const offered = offeredEventValues()
    const expected = Object.entries(FORGE_EVENT_TRANSITIONS)
      .flatMap(([kind, transitions]) => transitions.map(tr => `${kind}.${tr}`))
    expect([...offered].sort()).toEqual(expected.sort())
    expect(offered.has('issue.merged')).toBe(false)
    expect(offered.has('pr.merged')).toBe(true)
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

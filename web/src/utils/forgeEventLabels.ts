// Shared rendering of a forge event subscription for the task UI.
//
// The subscription is stored as a comma-separated list of kind-scoped keys
// ("issue.opened", "pr.merged"). Three surfaces need to turn that into human
// text — the task list, the task overview and the task form — so the mapping
// lives here rather than being duplicated per component.
//
// A bare key ("opened") is the pre-split legacy spelling and matches either
// kind; it renders without a kind prefix.
import i18n from '@/i18n'

/** Canonical transitions offered per item kind, in display order. */
export const FORGE_EVENT_TRANSITIONS: Record<string, string[]> = {
    issue: ['opened', 'closed', 'reopened', 'commented'],
    pr: ['opened', 'closed', 'merged', 'reopened', 'commented'],
}

/** Transitions that are still accepted but no longer offered (nothing derives them). */
export const FORGE_RETIRED_TRANSITIONS = ['pipeline_done']

const TRANSITION_LABEL_KEYS: Record<string, string> = {
    opened: 'task.form.eventOpened',
    closed: 'task.form.eventClosed',
    merged: 'task.form.eventMerged',
    reopened: 'task.form.eventReopened',
    commented: 'task.form.eventCommented',
    pipeline_done: 'task.form.eventPipeline',
}

const KIND_LABEL_KEYS: Record<string, string> = {
    issue: 'task.form.eventKindIssue',
    pr: 'task.form.eventKindPr',
}

/** Split a stored subscription into clean, non-empty keys. */
export function splitEventTypes(raw: string | undefined | null): string[] {
    if (!raw) return []
    return raw.split(',').map(s => s.trim()).filter(Boolean)
}

/**
 * Expand a stored subscription into the kind-scoped keys the checkboxes
 * represent.
 *
 * A bare key is the pre-split spelling. The backend treats it as "either kind",
 * so it maps to EVERY kind that supports that transition (bare `opened` means
 * both issue.opened and pr.opened; bare `merged` means pr.merged only). An
 * unrecognized key is kept verbatim rather than dropped, so a retired
 * subscription survives an edit untouched.
 */
export function expandStoredEventTypes(raw: string | undefined | null): string[] {
    const out: string[] = []
    for (const key of splitEventTypes(raw)) {
        if (key.includes('.')) {
            out.push(key)
            continue
        }
        const kinds = Object.entries(FORGE_EVENT_TRANSITIONS)
            .filter(([, transitions]) => transitions.includes(key))
            .map(([kind]) => kind)
        if (kinds.length === 0) out.push(key)
        else for (const kind of kinds) out.push(`${kind}.${key}`)
    }
    return out
}

/** Every kind-scoped value the form checkboxes can represent. */
export function offeredEventValues(): Set<string> {
    return new Set(
        Object.entries(FORGE_EVENT_TRANSITIONS)
            .flatMap(([kind, transitions]) => transitions.map(tr => `${kind}.${tr}`)),
    )
}

/**
 * Split a kind-scoped key into its parts for display. A bare legacy key has no
 * kind; it is returned as a transition with an empty kind.
 */
export function parseEventKey(key: string): { kind: string; transition: string } {
    const dot = key.indexOf('.')
    if (dot <= 0) return { kind: '', transition: key }
    return { kind: key.slice(0, dot), transition: key.slice(dot + 1) }
}

/** Localized label for one transition ("新建" / "Opened"). */
export function eventTransitionLabel(transition: string): string {
    const key = TRANSITION_LABEL_KEYS[transition]
    return key ? i18n.global.t(key) : transition
}

/** Localized label for one item kind ("议题" / "Issues"). */
export function eventKindLabel(kind: string): string {
    const key = KIND_LABEL_KEYS[kind]
    return key ? i18n.global.t(key) : kind
}

export interface EventChip {
    /** Raw stored key, e.g. "pr.merged". */
    key: string
    /** Item kind ("issue" / "pr"), empty for a bare legacy key. */
    kind: string
    transition: string
    /** Localized transition label. */
    label: string
}

/**
 * Turn a stored subscription into display chips. Legacy bare keys produce a
 * chip per matching kind so "opened" shows both "Issues · Opened" and
 * "Pull requests · Opened" — which is what the backend will actually match.
 */
export function eventChips(raw: string | undefined | null): EventChip[] {
    return expandStoredEventTypes(raw).map(key => {
        const { kind, transition } = parseEventKey(key)
        return { key, kind, transition, label: eventTransitionLabel(transition) }
    })
}

/** Compact single-line rendering of a subscription, e.g. "Issues · Opened · PR · Merged". */
export function eventTypesSummary(raw: string | undefined | null): string {
    const chips = eventChips(raw)
    if (chips.length === 0) return ''
    return chips.map(c => (c.kind ? `${eventKindLabel(c.kind)} · ${c.label}` : c.label)).join(' · ')
}

export interface EventSourceRef {
    /** owner/repo */
    slug: string
    number: string
    kind: 'issue' | 'pr' | ''
}

/**
 * Derive a compact "which item" reference from a stored event URL.
 *
 * The execution record only stores the URL, not the parsed identity, and the
 * rendered event summary uses hard-coded backend labels that are unsafe to
 * parse. The URL is stable across platforms and encodes both parts:
 *
 *   GitHub  https://github.com/owner/repo/pull/123   /issues/123
 *   GitLab  https://gitlab.com/owner/repo/-/merge_requests/123  /-/issues/123
 *
 * Returns null when the URL does not look like an issue/PR link.
 */
export function parseEventUrl(url: string | undefined | null): EventSourceRef | null {
    if (!url) return null
    let parsed: URL
    try {
        parsed = new URL(url)
    } catch {
        return null
    }
    const segments = parsed.pathname.split('/').filter(Boolean)
    if (segments.length < 4) return null

    // GitLab inserts a "/-/" separator before the resource segment.
    const dashIdx = segments.indexOf('-')
    const resourceIdx = dashIdx >= 0 ? dashIdx + 1 : 2
    const resource = segments[resourceIdx]
    const number = segments[resourceIdx + 1]
    if (!resource || !number || !/^\d+$/.test(number)) return null

    const kind: EventSourceRef['kind'] =
        resource === 'pull' || resource === 'merge_requests' || resource === 'pulls'
            ? 'pr'
            : resource === 'issues'
                ? 'issue'
                : ''
    if (!kind) return null

    const slug = segments.slice(0, dashIdx >= 0 ? dashIdx : resourceIdx).join('/')
    return { slug, number, kind }
}

/** One-line source label for a triggered run, e.g. "acme/widgets PR #123". */
export function eventSourceLabel(url: string | undefined | null): string {
    const ref = parseEventUrl(url)
    if (!ref) return ''
    const kind = ref.kind === 'pr' ? 'PR' : eventKindLabel(ref.kind)
    return `${ref.slug} ${kind} #${ref.number}`
}

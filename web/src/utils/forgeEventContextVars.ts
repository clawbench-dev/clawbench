// The event-context variable registry for the task UI.
//
// An event-triggered task's prompt is prefixed with a fixed, read-only block of
// `- 标签：值` lines (rendered by the backend's RenderEventContext). Two surfaces
// must show the user what that block will contain:
//
//   - the task form, as a placeholder template ({{BODY}}, ...);
//   - the task overview card, with sample values so the shape is concrete.
//
// The block has ~27 variables gated three different ways, so hand-copying the
// list into each component invites drift — and it already drifted once: the
// backend's own template compared a kind-scoped subscription key ("pr.commented")
// against a bare scoped transition ("commented"), so the comment and pipeline
// variables were hidden for every kind-scoped subscription.
//
// Keeping the registry here means the gating rules are stated once and both
// surfaces derive from it. The ORDER here must match the backend's
// eventContextVars slice so the preview reads in the same order as the real
// prompt.
import i18n from '@/i18n'
import { FORGE_REPO_TARGETED_TRANSITIONS } from '@/utils/forgeEventLabels'

/**
 * Which events a variable belongs to.
 *
 *   - `all`            — every event type.
 *   - `stateChanges`   — the lifecycle transitions, i.e. the events that have a
 *                        meaningful previous state. `commented` is excluded: its
 *                        previous state equals the current one, so rendering it
 *                        would emit the noise "状态：open / 前一状态：open".
 *   - `commented` / `pipeline` — a single event type.
 */
export type EventVarScope = 'all' | 'stateChanges' | 'commented' | 'pipeline'

export interface EventContextVar {
    /** i18n key under `task.form.` for the line's label. */
    labelKey: string
    /** The backend's placeholder token, shown verbatim in the form template. */
    placeholder: string
    scope: EventVarScope
    /**
     * True when the variable only makes sense for an event attached to an issue
     * or PR. A pipeline run has no item, so these are hidden from a
     * pipeline-only subscription rather than promising a value that can never
     * arrive (and would render as "pipeline #0").
     */
    requiresItem?: boolean
    /** Sample value for the overview card. Absent for a yes/no flag. */
    sample?: string
}

/**
 * Transitions for which a previous state is meaningful. Mirrors the backend's
 * stateTransitionEvents; keep the two in sync.
 */
const STATE_CHANGE_TRANSITIONS = ['opened', 'closed', 'merged', 'reopened']

/** Ordered exactly like the backend's eventContextVars. */
export const EVENT_CONTEXT_VARS: EventContextVar[] = [
    { labelKey: 'task.form.varEventType', placeholder: 'EVENT_TYPE', scope: 'all', sample: 'pr.opened' },
    { labelKey: 'task.form.varRepo', placeholder: 'REPO', scope: 'all', sample: 'owner/repo' },
    {
        labelKey: 'task.form.varItem',
        placeholder: 'ITEM_TYPE #ITEM_NUMBER',
        scope: 'all',
        requiresItem: true,
        sample: 'pr #123',
    },
    { labelKey: 'task.form.varTitle', placeholder: 'TITLE', scope: 'all', sample: 'Fix the nil deref' },
    { labelKey: 'task.form.varUrl', placeholder: 'URL', scope: 'all', sample: 'https://owner/repo/pull/123' },
    { labelKey: 'task.form.varAuthor', placeholder: 'AUTHOR', scope: 'all', sample: 'octocat' },
    { labelKey: 'task.form.varState', placeholder: 'STATE', scope: 'all', sample: 'open' },
    {
        labelKey: 'task.form.varPrevState',
        placeholder: 'PREV_STATE',
        scope: 'stateChanges',
        sample: 'open',
    },
    {
        labelKey: 'task.form.varBody',
        placeholder: 'BODY',
        scope: 'all',
        requiresItem: true,
        sample: 'The handler panics when the body is empty.',
    },
    { labelKey: 'task.form.varLabels', placeholder: 'LABELS', scope: 'all', requiresItem: true, sample: 'bug, urgent' },
    {
        labelKey: 'task.form.varAssignees',
        placeholder: 'ASSIGNEES',
        scope: 'all',
        requiresItem: true,
        sample: 'alice, bob',
    },
    { labelKey: 'task.form.varDraft', placeholder: 'DRAFT', scope: 'all', requiresItem: true, sample: '是' },
    {
        labelKey: 'task.form.varSourceBranch',
        placeholder: 'SOURCE_BRANCH',
        scope: 'all',
        requiresItem: true,
        sample: 'fix/nil-deref',
    },
    {
        labelKey: 'task.form.varMergedAt',
        placeholder: 'MERGED_AT',
        scope: 'all',
        requiresItem: true,
        sample: '2026-09-10T08:30:00Z',
    },
    {
        labelKey: 'task.form.varCreatedAt',
        placeholder: 'CREATED_AT',
        scope: 'all',
        sample: '2026-09-01T10:00:00Z',
    },
    {
        labelKey: 'task.form.varUpdatedAt',
        placeholder: 'UPDATED_AT',
        scope: 'all',
        sample: '2026-09-10T09:00:00Z',
    },
    {
        labelKey: 'task.form.varCommentCount',
        placeholder: 'COMMENT_COUNT',
        scope: 'all',
        requiresItem: true,
        sample: '3',
    },
    {
        labelKey: 'task.form.varCommentBody',
        placeholder: 'COMMENT_BODY',
        scope: 'commented',
        sample: 'please rebase onto main',
    },
    { labelKey: 'task.form.varCommentId', placeholder: 'COMMENT_ID', scope: 'commented', sample: '987' },
    {
        labelKey: 'task.form.varPipelineStatus',
        placeholder: 'PIPELINE_STATUS',
        scope: 'pipeline',
        sample: 'failure',
    },
    {
        labelKey: 'task.form.varPipelineUrl',
        placeholder: 'PIPELINE_URL',
        scope: 'pipeline',
        sample: 'https://ci.example.com/run/42',
    },
    { labelKey: 'task.form.varPipelineNumber', placeholder: 'PIPELINE_NUMBER', scope: 'pipeline', sample: '17' },
    { labelKey: 'task.form.varPipelineRef', placeholder: 'PIPELINE_REF', scope: 'pipeline', sample: 'main' },
    {
        labelKey: 'task.form.varPipelineSha',
        placeholder: 'PIPELINE_SHA',
        scope: 'pipeline',
        sample: 'abc123def456',
    },
    {
        labelKey: 'task.form.varPipelineTrigger',
        placeholder: 'PIPELINE_TRIGGER',
        scope: 'pipeline',
        sample: 'push',
    },
    {
        labelKey: 'task.form.varPipelineDuration',
        placeholder: 'PIPELINE_DURATION',
        scope: 'pipeline',
        sample: '3m12s',
    },
    {
        labelKey: 'task.form.varPipelineLinkedPrs',
        placeholder: 'PIPELINE_LINKED_PRS',
        scope: 'pipeline',
        sample: '#42 Fix the nil deref',
    },
    {
        labelKey: 'task.form.varActorIsSelf',
        placeholder: 'ACTOR_IS_SELF',
        scope: 'pipeline',
        // The value is the backend's literal rendering (是 / 否), not a
        // translated string: the sample mirrors exactly what will be injected.
        sample: '否',
    },
]

/** The gating inputs both surfaces compute from a stored subscription. */
export interface EventVarContext {
    /** Bare transitions the task subscribes to (e.g. "commented", "pipeline_done"). */
    transitions: Set<string>
    /** True when the subscription is made up ONLY of repository-targeted events. */
    repoTargetedOnly: boolean
}

/**
 * Whether a variable is in scope for the given subscription.
 *
 * An empty subscription means "not yet configured", which shows everything so
 * the user can see the full payload before choosing.
 */
export function eventVarVisible(v: EventContextVar, ctx: EventVarContext): boolean {
    const showAll = ctx.transitions.size === 0
    if (ctx.repoTargetedOnly && v.requiresItem) return false
    if (showAll || v.scope === 'all') return true
    if (v.scope === 'stateChanges') {
        return STATE_CHANGE_TRANSITIONS.some(tr => ctx.transitions.has(tr))
    }
    if (v.scope === 'commented') return ctx.transitions.has('commented')
    return ctx.transitions.has('pipeline_done')
}

/** The visible variables for a subscription, in prompt order. */
export function visibleEventContextVars(ctx: EventVarContext): EventContextVar[] {
    return EVENT_CONTEXT_VARS.filter(v => eventVarVisible(v, ctx))
}

/**
 * The read-only placeholder block for the task form.
 *
 * Mirrors the backend's EventPromptTemplate: only variables the subscription can
 * actually yield are listed, so the user sees exactly what will be injected.
 */
export function eventContextTemplateText(ctx: EventVarContext): string {
    const lines = visibleEventContextVars(ctx).map(
        v => `- ${i18n.global.t(v.labelKey)}：{{${v.placeholder}}}`,
    )
    return `## ${i18n.global.t('task.form.eventContextHeader')}\n${lines.join('\n')}`
}

/** One rendered row for the overview card's sample block. */
export interface EventContextSampleRow {
    label: string
    placeholder: string
    value: string
}

/**
 * The sample block for the task overview card.
 *
 * `overrides` lets a caller substitute context-specific samples (the real bound
 * repository slug, an item path derived from the subscribed kind) without
 * duplicating the registry.
 */
export function eventContextSampleRows(
    ctx: EventVarContext,
    overrides: Partial<Record<string, string>> = {},
): EventContextSampleRow[] {
    return visibleEventContextVars(ctx).map(v => ({
        label: i18n.global.t(v.labelKey),
        placeholder: v.placeholder,
        value: overrides[v.placeholder] ?? v.sample ?? '',
    }))
}

/** True when a subscription is made up only of repository-targeted events. */
export function isRepoTargetedOnly(transitions: Iterable<string>): boolean {
    const all = [...transitions]
    return all.length > 0 && all.every(tr => FORGE_REPO_TARGETED_TRANSITIONS.includes(tr))
}

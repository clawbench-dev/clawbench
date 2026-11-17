// The environment variables a gating script can read, mirrored from the Go
// registry in internal/service/gate_script_env.go.
//
// The task form renders this list so the user can see what is available without
// reading the docs. Order and names must match the backend exactly — the
// backend sets these names on the child process, and a parity test
// (internal/service/gate_script_env_parity_test.go) fails if the two drift.
//
// Scope decides which trigger kinds see a variable:
//   - common — cron, email and event runs alike
//   - mail   — only an email-triggered run
//   - event  — only a forge (GitHub / GitLab) event run
export type GateScriptScope = 'common' | 'mail' | 'event'

export interface GateScriptVar {
    /** The literal environment variable name, e.g. CLAWBENCH_MAIL_SUBJECT. */
    name: string
    /** i18n key under `task.form.` describing the value. */
    labelKey: string
    scope: GateScriptScope
}

export const GATE_SCRIPT_VARS: GateScriptVar[] = [
    { name: 'CLAWBENCH_TRIGGER', labelKey: 'task.form.scriptVarTrigger', scope: 'common' },
    { name: 'CLAWBENCH_TASK_ID', labelKey: 'task.form.scriptVarTaskId', scope: 'common' },
    { name: 'CLAWBENCH_TASK_NAME', labelKey: 'task.form.scriptVarTaskName', scope: 'common' },
    { name: 'CLAWBENCH_PROJECT_PATH', labelKey: 'task.form.scriptVarProjectPath', scope: 'common' },
    { name: 'CLAWBENCH_MAIL_ACCOUNT', labelKey: 'task.form.scriptVarMailAccount', scope: 'mail' },
    { name: 'CLAWBENCH_MAIL_FOLDER', labelKey: 'task.form.scriptVarMailFolder', scope: 'mail' },
    { name: 'CLAWBENCH_MAIL_FROM', labelKey: 'task.form.scriptVarMailFrom', scope: 'mail' },
    { name: 'CLAWBENCH_MAIL_TO', labelKey: 'task.form.scriptVarMailTo', scope: 'mail' },
    { name: 'CLAWBENCH_MAIL_SUBJECT', labelKey: 'task.form.scriptVarMailSubject', scope: 'mail' },
    { name: 'CLAWBENCH_MAIL_DATE', labelKey: 'task.form.scriptVarMailDate', scope: 'mail' },
    { name: 'CLAWBENCH_MAIL_HAS_ATTACHMENT', labelKey: 'task.form.scriptVarMailHasAttachment', scope: 'mail' },
    { name: 'CLAWBENCH_MAIL_MESSAGE_ID', labelKey: 'task.form.scriptVarMailMessageId', scope: 'mail' },
    { name: 'CLAWBENCH_EVENT_TYPE', labelKey: 'task.form.scriptVarEventType', scope: 'event' },
    { name: 'CLAWBENCH_REPO', labelKey: 'task.form.scriptVarRepo', scope: 'event' },
    { name: 'CLAWBENCH_ITEM_TYPE', labelKey: 'task.form.scriptVarItemType', scope: 'event' },
    { name: 'CLAWBENCH_ITEM_NUMBER', labelKey: 'task.form.scriptVarItemNumber', scope: 'event' },
    { name: 'CLAWBENCH_TITLE', labelKey: 'task.form.scriptVarTitle', scope: 'event' },
    { name: 'CLAWBENCH_URL', labelKey: 'task.form.scriptVarUrl', scope: 'event' },
    { name: 'CLAWBENCH_AUTHOR', labelKey: 'task.form.scriptVarAuthor', scope: 'event' },
    { name: 'CLAWBENCH_STATE', labelKey: 'task.form.scriptVarState', scope: 'event' },
    { name: 'CLAWBENCH_PREV_STATE', labelKey: 'task.form.scriptVarPrevState', scope: 'event' },
    { name: 'CLAWBENCH_LABELS', labelKey: 'task.form.scriptVarLabels', scope: 'event' },
    { name: 'CLAWBENCH_ASSIGNEES', labelKey: 'task.form.scriptVarAssignees', scope: 'event' },
    { name: 'CLAWBENCH_SOURCE_BRANCH', labelKey: 'task.form.scriptVarSourceBranch', scope: 'event' },
    { name: 'CLAWBENCH_DRAFT', labelKey: 'task.form.scriptVarDraft', scope: 'event' },
    { name: 'CLAWBENCH_ACTOR_IS_SELF', labelKey: 'task.form.scriptVarActorIsSelf', scope: 'event' },
    { name: 'CLAWBENCH_PIPELINE_STATUS', labelKey: 'task.form.scriptVarPipelineStatus', scope: 'event' },
    { name: 'CLAWBENCH_PIPELINE_URL', labelKey: 'task.form.scriptVarPipelineUrl', scope: 'event' },
    { name: 'CLAWBENCH_PIPELINE_NUMBER', labelKey: 'task.form.scriptVarPipelineNumber', scope: 'event' },
    { name: 'CLAWBENCH_PIPELINE_REF', labelKey: 'task.form.scriptVarPipelineRef', scope: 'event' },
    { name: 'CLAWBENCH_PIPELINE_SHA', labelKey: 'task.form.scriptVarPipelineSha', scope: 'event' },
]

/**
 * The variables in scope for a trigger mode.
 *
 * The backend decides from the task's stored trigger mode, so an unrecognised
 * value falls back to the cron set (the only one that is always valid).
 */
export function gateScriptVarsFor(triggerMode: string | undefined): GateScriptVar[] {
    const scope: GateScriptScope = triggerMode === 'email' ? 'mail' : triggerMode === 'event' ? 'event' : 'common'
    return GATE_SCRIPT_VARS.filter((v) => v.scope === 'common' || v.scope === scope)
}

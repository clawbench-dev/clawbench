import type { Component } from 'vue'
import { Bot, Check, Clock, Gauge, GitMerge, Hand, Mail, ShieldAlert, X } from 'lucide-vue-next'
import { MEETING_ICON } from '@/utils/meetingIcon'
import type { FeatureFlagKey } from '@/composables/useFeatureFlags'
import type { NotificationItem, NotificationKind } from '@/utils/notificationsApi'

/**
 * The notification center's ONE per-kind table.
 *
 * Everything the panel renders per kind lives here: the glyph, the kind name,
 * the event chip (label / tone / icon), the feature switch that must be on for
 * the destination to exist, and the destination itself. The panel only looks
 * entries up, so adding a kind is "one table entry + i18n keys" instead of
 * touching seven parallel switches.
 *
 * `satisfies Record<NotificationKind, …>` is load-bearing: adding a kind to
 * the wire type fails the build until it is declared here. The server has the
 * matching registry (internal/service/notification_subjects.go) for the
 * "does the source still exist" probe.
 *
 * A row whose kind is not in this table (a newer server, or a typo) is rendered
 * as "other" and refuses to navigate with a hint. It must never fall back to a
 * concrete kind: showing a plugin's notification as "session" is a wrong label,
 * not a harmless default.
 */

/** Dispatch a row jump. Injected by the panel so every jump carries its origin. */
export type NotificationJump = (event: string, detail: Record<string, unknown>) => void

export interface NotificationKindSpec {
  id: NotificationKind
  /** Glyph on the timeline node and in the narrow card header. */
  icon: Component
  /** i18n key for the kind name; also the fallback title. */
  labelKey: string
  /**
   * Master switch that must be on for this kind's destination to exist. When
   * off, the row stays readable (the log is a record) but the click explains
   * instead of dispatching a dead deep link.
   */
  feature?: FeatureFlagKey
  /**
   * True for a row that is not tied to a project, so the click must skip the
   * cross-project confirmation. Usage is instance-wide; mail is per-person.
   */
  instanceWide?: boolean
  /** Chip label per event type; an unknown event type falls back to its raw name. */
  eventLabels: Record<string, string>
  /** Default chip tone. */
  tone: string
  /** Per-event tone overrides. */
  toneByEvent?: Record<string, string>
  /** Default chip glyph. */
  eventIcon: Component
  /** Per-event glyph overrides. */
  eventIconByEvent?: Record<string, Component>
  /** Optional title fallback before the kind name (forge uses its item key). */
  fallbackTitle?: (n: NotificationItem) => string
  /**
   * Where a row goes. Returns false when the row cannot name a destination
   * (a malformed row); the panel then shows a hint rather than swallowing the
   * press. Omitted means the kind has no destination at all — also a hint.
   */
  open?: (n: NotificationItem, jump: NotificationJump) => boolean
}

/**
 * Rebuild a forge deep-link target from the stored opaque key.
 *
 * The key is "issue/12" / "pr/34" / "pipeline/run:56". A pipeline's number is
 * always 0, so the run id is its identity — matching the writer's format.
 */
function forgeTargetFromItemKey(key: string) {
  if (key.startsWith('pipeline/run:')) {
    const runId = Number(key.slice('pipeline/run:'.length)) || 0
    return { type: 'pipeline', number: 0, runId, itemKey: key }
  }
  const [type, numStr] = key.split('/')
  const number = Number(numStr) || 0
  return { type, number, runId: 0, itemKey: key }
}

/** Session and task share their chip vocabulary; only the i18n keys differ. */
function runChipLabels(completedKey: string, cancelledKey: string): Record<string, string> {
  return {
    completed: completedKey,
    cancelled: cancelledKey,
    permission_pending: 'notifications.event.permissionPending',
    running: 'notifications.event.taskRunning',
    failed: 'notifications.event.taskFailed',
  }
}

const RUN_TONE_BY_EVENT = {
  failed: 'danger',
  cancelled: 'danger',
  permission_pending: 'warning',
  running: 'info',
}

const RUN_ICON_BY_EVENT = {
  failed: X,
  cancelled: X,
  permission_pending: ShieldAlert,
}

export const NOTIFICATION_SPECS = {
  session: {
    id: 'session',
    icon: Bot,
    labelKey: 'notifications.kind.session',
    eventLabels: runChipLabels(
      'notifications.event.sessionCompleted',
      'notifications.event.sessionCancelled',
    ),
    tone: 'success',
    toneByEvent: RUN_TONE_BY_EVENT,
    eventIcon: Check,
    eventIconByEvent: RUN_ICON_BY_EVENT,
    open: (n, jump) => {
      if (!n.subjectId) return false
      jump('clawbench-open-session', {
        sessionId: n.subjectId,
        projectPath: n.projectPath,
      })
      return true
    },
  },
  task: {
    id: 'task',
    icon: Clock,
    labelKey: 'notifications.kind.task',
    feature: 'tasks.enabled',
    eventLabels: runChipLabels(
      'notifications.event.taskCompleted',
      'notifications.event.taskCancelled',
    ),
    tone: 'success',
    toneByEvent: RUN_TONE_BY_EVENT,
    eventIcon: Check,
    eventIconByEvent: RUN_ICON_BY_EVENT,
    open: (n, jump) => {
      if (!n.subjectId) return false
      jump('clawbench-open-task', {
        taskId: n.subjectId,
        executionId: n.executionId,
        projectPath: n.projectPath,
      })
      return true
    },
  },
  forge: {
    id: 'forge',
    icon: GitMerge,
    labelKey: 'notifications.kind.forge',
    feature: 'forge.enabled',
    eventLabels: {
      opened: 'notifications.event.forgeOpened',
      closed: 'notifications.event.forgeClosed',
      merged: 'notifications.event.forgeMerged',
      reopened: 'notifications.event.forgeReopened',
      commented: 'notifications.event.forgeCommented',
      pipeline_done: 'notifications.event.forgePipelineDone',
    },
    tone: 'purple',
    toneByEvent: { failed: 'danger', cancelled: 'danger' },
    eventIcon: GitMerge,
    fallbackTitle: (n) => n.subjectId || '',
    open: (n, jump) => {
      if (!n.subjectId) return false
      jump('clawbench-open-forge', {
        projectPath: n.projectPath,
        target: forgeTargetFromItemKey(n.subjectId),
      })
      return true
    },
  },
  usage: {
    id: 'usage',
    icon: Gauge,
    labelKey: 'notifications.kind.usage',
    // The meter aggregates ALL projects, so a usage row is never "some other
    // project" and must not pop the switch-project confirmation.
    instanceWide: true,
    eventLabels: {
      over: 'notifications.event.usageOver',
      full: 'notifications.event.usageFull',
    },
    tone: 'warning',
    toneByEvent: { full: 'danger' },
    eventIcon: Gauge,
    open: (_n, jump) => {
      jump('clawbench-open-usage-meter', {})
      return true
    },
  },
  meeting: {
    id: 'meeting',
    icon: MEETING_ICON,
    labelKey: 'notifications.kind.meeting',
    eventLabels: {
      control_request: 'notifications.event.meetingControlRequest',
      viewer_joined: 'notifications.event.meetingViewerJoined',
      screen_share: 'notifications.event.meetingScreenShare',
      minutes_ready: 'notifications.event.meetingMinutesReady',
      minutes_failed: 'notifications.event.meetingMinutesFailed',
      transcript_ready: 'notifications.event.transcriptReady',
      transcript_failed: 'notifications.event.transcriptFailed',
      revised_ready: 'notifications.event.revisedReady',
      revised_failed: 'notifications.event.revisedFailed',
    },
    tone: 'success',
    toneByEvent: {
      // A remote-control request is the one meeting event that needs an
      // answer, so it reads like permission_pending rather than a completion.
      control_request: 'warning',
      minutes_failed: 'danger',
      transcript_failed: 'danger',
      revised_failed: 'danger',
    },
    eventIcon: MEETING_ICON,
    eventIconByEvent: {
      control_request: Hand,
      minutes_ready: Check,
      minutes_failed: X,
      transcript_ready: Check,
      transcript_failed: X,
      revised_ready: Check,
      revised_failed: X,
    },
    open: (n, jump) => {
      // The share token IS the meeting's identity; the record it opens lives in
      // the row's project, so the path travels with it.
      if (!n.subjectId) return false
      jump('clawbench-open-meeting', {
        token: n.subjectId,
        projectPath: n.projectPath,
      })
      return true
    },
  },
  mail: {
    id: 'mail',
    icon: Mail,
    labelKey: 'notifications.kind.mail',
    feature: 'mail.enabled',
    // A mailbox belongs to the person, not to a repository: no project switch.
    instanceWide: true,
    eventLabels: { received: 'notifications.event.mailReceived' },
    tone: 'info',
    eventIcon: Mail,
    open: (n, jump) => {
      // The reader fetches by id, so the message need not be on the folder's
      // loaded page.
      const messageId = Number(n.subjectId)
      if (!messageId) return false
      jump('clawbench-open-mail', { messageId })
      return true
    },
  },
} satisfies Record<NotificationKind, NotificationKindSpec>

/** The table in declaration order — the filter dropdown's order. */
export const NOTIFICATION_KINDS: readonly NotificationKindSpec[] = Object.values(NOTIFICATION_SPECS)

/**
 * Look up a kind, including one the wire type does not know yet.
 *
 * `NotificationItem.kind` is typed as the known union, but the server is the
 * authority and a newer build can send something else; the string signature is
 * what lets the panel degrade instead of trusting the cast.
 */
export function notificationKindSpec(kind: string): NotificationKindSpec | undefined {
  return (NOTIFICATION_SPECS as Record<string, NotificationKindSpec>)[kind]
}

import type { Ref } from 'vue'
import { useLiveShare } from '@/composables/useLiveShare'
import { setMeetingCreateReturn } from '@/composables/useMeetingViewState'
import { appLog } from '@/utils/appLog'
import { MEETING_ICON } from '@/utils/meetingIcon'
import type { DockPlugin } from '@/composables/dockPluginTypes'

// Lazy: useLiveShare is a large composable and the dock only needs one state
// ref. Calling it here on first badge read avoids pulling it into module load
// (App.vue already imports it, so this is about initialization order, not size).
const TAG = 'MeetingDeepLink'

let state: Ref<string> | null = null
function liveState(): Ref<string> {
  if (!state) state = useLiveShare().state
  return state
}

/**
 * 视频会议: the meeting records, and the meeting itself — screen/camera or
 * voice. It used to be two tabs (听记 and 桌面共享) even though both drove the
 * same recorder, the same endpoints and the same artifact folder; a record can
 * be either kind, so one tab is the honest shape.
 *
 * The icon is a monitor with an outgoing arrow — the picture half of a meeting.
 * See utils/meetingIcon.ts: the list header, the quote chip and this tab share
 * one constant so they cannot drift apart.
 */
export default {
  id: 'live',
  titleKey: 'nav.meeting',
  descriptionKey: 'settings.dockPlugins.desc.live',
  group: 'personal',
  closable: true,
  settingsCategory: 'meeting',
  icon: MEETING_ICON,
  // The settings row uses the SAME glyph as the dock tab: a second, unrelated
  // meeting icon made the page and the tab it configures look like two
  // different features (see utils/meetingIcon.ts).
  settingsPage: {
    group: 'dockModules',
    icon: MEETING_ICON,
    kind: 'group',
  },
  badge: () => ({ count: 0, live: liveState().value !== 'idle' }),
  // The dot is the share's appearance on the tab; the badge above is its data.
  // Both answer the same question, but the button reads the dot declaration —
  // the host never special-cases 会议.
  appearance: {
    liveDot: () => liveState().value !== 'idle',
  },
  deeplinks: [
    {
      event: 'clawbench-open-meeting',
      /**
       * A meeting notification was tapped, or a quote jumped to the meeting it
       * was taken from.
       *
       * The meeting panel listens to the SAME event and sets its own view (it
       * owns the list/detail/stage state). This handler does the two things the
       * panel cannot: switch to the meeting tab, and — for a row from another
       * project — rebuild that project first. A meeting record is project-scoped,
       * so opening it against the old project would 404 and land on an empty
       * detail page.
       */
      handler: (raw, ctx) => {
        const detail = raw as { token?: string; projectPath?: string } | undefined
        if (!detail?.token) {
          appLog.w(TAG, 'clawbench-open-meeting: no token in detail, ignoring')
          return
        }
        const { token, projectPath } = detail
        const crossProject = !!projectPath && projectPath !== ctx.currentProject()
        if (!crossProject) ctx.recordNotificationOrigin(detail)
        if (crossProject) {
          // The panel already handled the original event, against the OLD
          // project. Re-dispatch once the switch has landed: the second pass is
          // the one whose detail fetch can find the record. (Same-project jumps
          // need no second dispatch — the first one is already correct.)
          ctx
            .hotSwitchProject(projectPath)
            .then(() => {
              ctx.switchTab('live')
              window.dispatchEvent(new CustomEvent('clawbench-open-meeting', { detail: { token } }))
            })
            .catch(() => {
              appLog.w(TAG, 'clawbench-open-meeting: project switch failed, opening the current project')
              ctx.switchTab('live')
            })
          return
        }
        ctx.switchTab('live')
      },
    },
  ],
  panel: {
    load: () => import('@/components/live/MeetingPanel.vue'),
    on: (host, extras) => ({
      quote: extras.meeting.quote,
      // MeetingPanel declares the camelCase emit; v-on object keys are runtime
      // strings, so use the exact name (the SFC template's @relay-settings is
      // compiler-normalized, an object key is not).
      // The create form is dropped on re-entry unless the jump parks it, so
      // the plugin owns that request; the host only navigates.
      relaySettings: () => {
        setMeetingCreateReturn()
        host.openSettingsCategory('meeting')
      },
    }),
  },
} satisfies DockPlugin

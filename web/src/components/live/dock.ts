import type { Ref } from 'vue'
import { useLiveShare } from '@/composables/useLiveShare'
import type { DockPluginRuntime } from '@/composables/dockPluginTypes'

// Lazy: useLiveShare is a large composable and the dock only needs one state
// ref. Calling it here on first badge read avoids pulling it into module load
// (App.vue already imports it, so this is about initialization order, not size).
let state: Ref<string> | null = null
function liveState(): Ref<string> {
  if (!state) state = useLiveShare().state
  return state
}

/** Meeting runtime: a dot while a share is running, no count, plus the panel. */
export const liveDock: DockPluginRuntime = {
  badge: () => ({ count: 0, live: liveState().value !== 'idle' }),
  panel: {
    load: () => import('./MeetingPanel.vue'),
    on: (host) => ({
      quote: host.quoteMeeting,
      // MeetingPanel declares the camelCase emit; v-on object keys are runtime
      // strings, so use the exact name (the SFC template's @relay-settings is
      // compiler-normalized, an object key is not).
      relaySettings: host.openMeetingRelaySettings,
    }),
  },
}

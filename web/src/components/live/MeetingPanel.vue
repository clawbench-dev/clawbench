<template>
  <div class="meeting-shell">
    <MeetingList
      v-if="view === 'list'"
      :active="active"
      :live-token="liveToken"
      @create="view = 'create'"
      @join="view = 'join'"
      @join-live="joinRunning"
      @open="openDetail"
      @resume="view = 'meeting'"
    />
    <MeetingCreate
      v-else-if="view === 'create'"
      :active="active"
      @back="view = 'list'"
      @started="view = 'meeting'"
      @relay-settings="emit('relaySettings')"
    />
    <MeetingJoin v-else-if="view === 'join'" @back="view = 'list'" />
    <MeetingDetail
      v-else-if="view === 'detail'"
      :key="detailToken"
      :token="detailToken"
      :initial-tab="detailTab"
      @back="view = 'list'"
      @quote="(token: string) => emit('quote', token)"
      @tab-change="detailTab = $event"
    />
    <!-- `v-show`, NOT `v-if`: leaving the stage for the record list must not
         destroy it. Its chat lines, pending control requests and the viewer it
         granted control to are component state, and its subscription to the
         viewers' control channel is torn down on unmount — a member's message
         sent while the host was reading the list would then be neither shown
         nor archived. The meeting keeps running either way (the room lives in
         useLiveShare), so the instance has to as well. -->
    <MeetingStage
      v-show="view === 'meeting'"
      :active="view === 'meeting'"
      @ended="onEnded"
      @back="view = 'list'"
    />
  </div>
</template>

<script setup lang="ts">
/**
 * The 会议 dock tab.
 *
 * Four views, one state machine:
 *
 *   list ──创建──▶ create ──开始──▶ meeting ──结束──▶ detail
 *    │                                ▲                 │
 *    └──点一条记录────────────────────────────────────▶│
 *    └──加入──▶ join                                  └──返回/删除──▶ list
 *
 * Ending a meeting lands on the SAME detail view a list row opens: there is no
 * separate "post-meeting" screen, so the minutes, the transcript, the chat and
 * the delete button live in exactly one place.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import MeetingList from './MeetingList.vue'
import MeetingCreate from './MeetingCreate.vue'
import MeetingJoin from './MeetingJoin.vue'
import MeetingStage from './MeetingStage.vue'
import MeetingDetail from './MeetingDetail.vue'
import { useLiveShare } from '@/composables/useLiveShare'
import { useWakeLock } from '@/composables/useWakeLock'
import { useToast } from '@/composables/useToast'
import { setHostedMeetingToken, setVisibleMeetingToken } from '@/utils/meetingVisibility'
import { getNative } from '@/utils/clawbenchNative'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { takeMeetingReturn, takeMeetingCreateReturn, clearMeetingView, type MeetingDetailTab } from '@/composables/useMeetingViewState'

const props = defineProps<{ active?: boolean }>()
const emit = defineEmits<{ quote: [token: string]; relaySettings: [] }>()

type View = 'list' | 'create' | 'join' | 'meeting' | 'detail'
const view = ref<View>('list')
const detailToken = ref('')
const detailTab = ref<MeetingDetailTab>('resources')
const { t } = useI18n()
const toast = useToast()
const share = useLiveShare()
const wakeLock = useWakeLock()
/** The meeting this client publishes, or '' — the list's "back to meeting" chip. */
const liveToken = computed(() => (share.state.value === 'publishing' ? share.token.value : ''))

/**
 * Keep the screen on for as long as a meeting runs.
 *
 * On a phone, letting the screen time out is not a cosmetic problem: the app
 * goes to the background, the WebView is paused and the publisher socket closes
 * — which `useLiveShare` treats as "the share is over" and stops for good. So
 * the lock is keyed on the share state rather than on the stage being visible:
 * the meeting keeps running while the host reads the record list or works in
 * another tab, and the screen must stay on for that too.
 *
 * `immediate` covers mounting the panel while a meeting is already publishing
 * (a reload, or the panel being re-created); at 'idle' it is a harmless no-op.
 */
watch(
  () => share.state.value,
  (s) => {
    if (s === 'publishing' || s === 'capturing') void wakeLock.acquire('meeting')
    else wakeLock.release('meeting')
  },
  { immediate: true },
)

function openDetail(token: string): void {
  if (!token) return
  // A running meeting's "record" IS the meeting: opening it returns to the
  // stage, not to a detail page that has no minutes and no transcript yet.
  if (share.state.value === 'publishing' && token === share.token.value) {
    view.value = 'meeting'
    return
  }
  detailToken.value = token
  // A record opened from the list starts at its resources (the folder, the
  // meeting link), not at whatever tab the previous record happened to be on.
  detailTab.value = 'resources'
  view.value = 'detail'
}

function onEnded(token: string): void {
  // The record exists from the moment the meeting started, so the token is all
  // the detail needs — no "which folder is this" lookup.
  if (token) openDetail(token)
  else view.value = 'list'
}

/**
 * One-click entry into the meeting the list says is running but this client
 * is not publishing (another device hosts it). The record's token IS the
 * share link — the room is per project, so there is nothing to look up — and
 * the opening mechanism is the same one the 加入 page performs with a pasted
 * link: the native bridge opens the member view inside the app shell, a
 * browser opens it in a new tab. No nickname edit here: there is no field to
 * type into, and the member view's own gate asks for a name when none is
 * remembered.
 */
async function joinRunning(token: string): Promise<void> {
  if (!token) return
  const link = `${window.location.origin}/share/${encodeURIComponent(token)}`
  const bridge = getNative()
  if (bridge?.openShareLink) {
    const ok = await bridge.openShareLink(link)
    if (!ok) toast.show(t('liveShare.joinInvalid'), { type: 'error', icon: 'warning' })
    return
  }
  window.open(link, '_blank', 'noopener')
}

// A meeting started from anywhere (the create page, a restored session) brings
// the tab to the stage.
let lastToken = ''
watch(
  () => share.token.value,
  (token) => {
    if (token) lastToken = token
  },
  { immediate: true },
)

watch(
  () => share.state.value,
  (next, prev) => {
    if (next === 'publishing') {
      if (view.value !== 'meeting') view.value = 'meeting'
      return
    }
    // The share can stop WITHOUT the stage's stop button: the publisher socket
    // closed, the capture ended, or the Android projection was revoked. The
    // stage then has no way back (its stop button only renders while
    // publishing), so the shell has to move — to the record if we know which
    // meeting it was, otherwise to the list.
    if (prev === 'publishing' && view.value === 'meeting') {
      if (lastToken) openDetail(lastToken)
      else view.value = 'list'
    }
  },
)

// The panel is kept alive by TabPanel, so `active` is the only "entered the tab"
// signal. Leaving the detail (back to the list) drops the view the coordinator
// would otherwise use as a return target for a later file jump.
watch(view, (next) => {
  if (next === 'list') clearMeetingView()
})

watch(
  () => props.active,
  (active) => {
    if (!active) return
    // Returning from a file/resource opened inside a meeting: restore the
    // record and the tab. The coordinator parks this because the origin itself
    // is consumed synchronously, before this watcher runs.
    const pending = takeMeetingReturn()
    if (pending?.token) {
      detailToken.value = pending.token
      detailTab.value = pending.tab
      view.value = 'detail'
      return
    }
    // Returning from the relay configuration, which is reached FROM the create
    // form: the form is still mounted and half filled, so it is exactly what
    // the user must land on. (The refresh of the relay state itself is the
    // form's own job — it watches `active`.)
    if (takeMeetingCreateReturn()) {
      view.value = 'create'
      return
    }
    // Re-entering the tab keeps where the user left off. A running meeting
    // used to force the stage here, which yanked a host back mid-read after
    // they had deliberately gone to the list to look at another record. The
    // stage is one tap away again (its own back control, then the list's live
    // chip), so only the transient pages fall back — a half-filled 创建/加入
    // form is not a place to come back to.
    if (view.value === 'create' || view.value === 'join') {
      view.value = share.state.value === 'publishing' ? 'meeting' : 'list'
    }
  },
)

/**
 * Back inside the tab (edge swipe or the Android hardware button): every
 * subpage returns to the list.
 *
 * Gated on `active` like every other panel — an inactive tab must never
 * intercept a back press, or the gesture would be swallowed by a panel the
 * user cannot even see.
 *
 * The stage is included. It reads as a session rather than a page, but leaving
 * it costs nothing: the room lives in `useLiveShare`, not in this component, so
 * the meeting keeps running and its row (see `openDetail`) leads straight back.
 * What back must NOT do is END the meeting — that is destructive, and the stage
 * keeps it behind an explicit 结束会议 button.
 */
const canGoBack = computed(() => !!props.active && view.value !== 'list')

useFeatureBackHandler(
  'meeting',
  () => canGoBack.value,
  () => {
    view.value = 'list'
  },
  PRIORITY_PAGE,
)

/**
 * Opening a meeting from a quote's "jump to source".
 *
 * App switches the tab, then calls this; the token comes from the quote, so the
 * record does not have to be on the current page of the list.
 */
function handleOpenMeeting(event: Event): void {
  const detail = (event as CustomEvent<{ token?: string }>).detail
  if (detail?.token) openDetail(detail.token)
}

onMounted(() => {
  window.addEventListener('clawbench-open-meeting', handleOpenMeeting)
  if (share.state.value === 'publishing') view.value = 'meeting'
})
onBeforeUnmount(() => {
  window.removeEventListener('clawbench-open-meeting', handleOpenMeeting)
  // A held wake lock outlives this component (it lives in the composable), so
  // leaving it behind would keep the screen on for good.
  wakeLock.release('meeting')
  // The panel is the only writer of these two facts; leaving them behind would
  // keep suppressing (or worse, keep raising) notifications for a meeting this
  // client no longer shows.
  setHostedMeetingToken('')
  setVisibleMeetingToken('')
})

// Two facts the global event handler needs (see utils/meetingVisibility):
// which meeting this client publishes, and whether its stage is what the user
// is looking at. Derived HERE because this is the only place that knows the
// tab's active state and the panel's own view at the same time — the stage
// component alone cannot tell whether the tab is on screen (panels are kept
// mounted), and the composable alone cannot tell which view is rendered.
watchEffect(() => {
  setHostedMeetingToken(share.state.value === 'publishing' ? share.token.value : '')
})
watchEffect(() => {
  setVisibleMeetingToken(props.active && view.value === 'meeting' ? share.token.value : '')
})

defineExpose({ openDetail })
</script>

<style scoped>
.meeting-shell {
  display: flex;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
</style>

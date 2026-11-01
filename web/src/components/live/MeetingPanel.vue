<template>
  <div class="meeting-shell">
    <MeetingList
      v-if="view === 'list'"
      :active="active"
      @create="view = 'create'"
      @join="view = 'join'"
      @open="openDetail"
    />
    <MeetingCreate v-else-if="view === 'create'" @back="view = 'list'" @started="view = 'meeting'" />
    <MeetingJoin v-else-if="view === 'join'" @back="view = 'list'" />
    <MeetingStage v-else-if="view === 'meeting'" @ended="onEnded" />
    <MeetingDetail
      v-else
      :key="detailToken"
      :token="detailToken"
      @back="view = 'list'"
      @quote="(token: string) => emit('quote', token)"
      @open-file="(path: string) => emit('open-file', path)"
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
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import MeetingList from './MeetingList.vue'
import MeetingCreate from './MeetingCreate.vue'
import MeetingJoin from './MeetingJoin.vue'
import MeetingStage from './MeetingStage.vue'
import MeetingDetail from './MeetingDetail.vue'
import { useLiveShare } from '@/composables/useLiveShare'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'

const props = defineProps<{ active?: boolean }>()
const emit = defineEmits<{ quote: [token: string]; 'open-file': [path: string] }>()

type View = 'list' | 'create' | 'join' | 'meeting' | 'detail'
const view = ref<View>('list')
const detailToken = ref('')
const share = useLiveShare()

function openDetail(token: string): void {
  if (!token) return
  // A running meeting's "record" IS the meeting: opening it returns to the
  // stage, not to a detail page that has no minutes and no transcript yet.
  if (share.state.value === 'publishing' && token === share.token.value) {
    view.value = 'meeting'
    return
  }
  detailToken.value = token
  view.value = 'detail'
}

function onEnded(token: string): void {
  // The record exists from the moment the meeting started, so the token is all
  // the detail needs — no "which folder is this" lookup.
  if (token) openDetail(token)
  else view.value = 'list'
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
// signal: a meeting in progress returns to the stage, anything else to the list.
watch(
  () => props.active,
  (active) => {
    if (!active) return
    view.value = share.state.value === 'publishing' ? 'meeting' : 'list'
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

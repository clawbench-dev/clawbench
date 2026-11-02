<template>
  <!-- The member's shared screen, inside the SAME layout the member's own page
       uses: picture on the left, 聊天 / 听记 panel on the right, draggable
       boundary. The host used to get a floating 280×320 chat box while the
       member had a full-height panel, so one conversation had two shapes
       depending on which end you were on.
       Rendered twice — full screen over the app, and inside the Document PiP
       window — and it is deliberately the SAME component: the host moved the
       picture, not the controls. -->
  <LiveStageLayout
    class="member-screen"
    :data-variant="variant"
    :side-open="chatOpen"
    :active-tab="tab"
    :notes-available="notesAvailable"
    @update:active-tab="tab = $event"
    @close="emit('close')"
  >
    <video ref="videoRef" class="member-screen__video" autoplay playsinline muted />

    <!-- Same pill and buttons as the member's own share page (LiveDock /
         LiveDockButton), because the host is looking at the same call from the
         other end. Only the actions differ: no microphone or screen capture
         here, but a way to move the picture into its own window and to stop it.
    -->
    <LiveDock brand live :title="sharer">
      <LiveDockButton
        :live="tab === 'chat' && chatOpen"
        :badge="unread"
        :aria-pressed="tab === 'chat' && chatOpen"
        :aria-label="t('liveShare.chat')"
        :title="t('liveShare.chat')"
        data-action="screen-chat"
        @click="onChatButton"
      >
        <template #icon><MessageSquare :size="16" /></template>
        {{ t('liveShare.chat') }}
      </LiveDockButton>
      <LiveDockButton
        v-if="notesAvailable"
        :live="tab === 'notes' && chatOpen"
        :aria-pressed="tab === 'notes' && chatOpen"
        :aria-label="t('liveShare.notes')"
        :title="t('liveShare.notesTitle')"
        data-action="screen-notes"
        @click="onNotesButton"
      >
        <template #icon><FileText :size="16" /></template>
        {{ t('liveShare.notes') }}
      </LiveDockButton>
      <LiveDockButton
        v-if="canSwitch"
        :aria-label="switchLabel"
        :title="switchLabel"
        data-action="screen-switch"
        @click="emit('switch')"
      >
        <template #icon>
          <Minimize v-if="variant === 'window'" :size="16" />
          <Maximize v-else :size="16" />
        </template>
        {{ switchLabel }}
      </LiveDockButton>
      <LiveDockButton
        live
        :aria-label="t('liveShare.stopScreenShare')"
        :title="t('liveShare.stopScreenShare')"
        data-action="screen-stop"
        @click="emit('stop')"
      >
        <template #icon><ScreenShareOff :size="16" /></template>
        {{ t('liveShare.stopScreenShare') }}
      </LiveDockButton>
      <LiveDockButton
        :aria-label="t('common.close')"
        :title="t('common.close')"
        data-action="screen-close"
        @click="emit('close')"
      >
        <template #icon><X :size="16" /></template>
        {{ t('common.close') }}
      </LiveDockButton>
    </LiveDock>

    <template #chat>
      <LiveChatPanel :messages="messages" @send="(text: string) => emit('send-chat', text)" />
    </template>

    <template #notes>
      <!-- Same three-way honesty as the member's own panel: waiting while the
           recorder runs, "nothing was recorded" once it stopped. -->
      <MeetingEmpty
        v-if="noteBlocks.length === 0"
        :icon="Captions"
        :title="t(notesEnabled ? 'share.live.notesWaiting' : 'share.live.notesOffTitle')"
        :desc="t(notesEnabled ? 'share.live.notesWaitingDesc' : 'share.live.notesOffDesc')"
        data-role="notes-empty"
      />
      <TranscriptList v-else :blocks="noteBlocks" fill />
    </template>
  </LiveStageLayout>
</template>

<script setup lang="ts">
/**
 * The member's shared screen, with its dock and the shared side panel.
 *
 * The picture is a live MediaStream, so it can only be attached here, in the
 * document that owns the track. That is also why the second copy is a Document
 * PiP window rather than a real one: a separate JS context cannot see the
 * stream.
 */
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Captions, FileText, Maximize, MessageSquare, Minimize, ScreenShareOff, X } from 'lucide-vue-next'
import LiveChatPanel, { type ChatMessage } from '@/share/LiveChatPanel.vue'
import LiveDock from '@/share/LiveDock.vue'
import LiveDockButton from '@/share/LiveDockButton.vue'
import LiveStageLayout from '@/share/LiveStageLayout.vue'
import MeetingEmpty from '@/components/live/MeetingEmpty.vue'
import TranscriptList from '@/components/live/TranscriptList.vue'
import type { NoteBlock } from '@/composables/liveNotes'

const props = defineProps<{
  stream: MediaStream | null
  sharer: string
  unread: number
  /** Panel open/closed — owned by the caller (it also drives the unread count). */
  chatOpen: boolean
  messages: ChatMessage[]
  /** The host's own 听记, the same rows its 转写 tab draws. */
  noteBlocks: NoteBlock[]
  /** Whether the recorder is running right now (the tab outlives it). */
  notesEnabled: boolean
  /** Which frame is holding the picture; only changes the dock's switch label. */
  variant: 'fullscreen' | 'window'
  /** False on hosts that cannot open a window at all (Android WebView). */
  canSwitch: boolean
}>()

const emit = defineEmits<{
  switch: []
  stop: []
  close: []
  'toggle-chat': []
  'open-panel': []
  'send-chat': [text: string]
}>()

const { t } = useI18n()
const videoRef = ref<HTMLVideoElement | null>(null)

/** Which pane the panel shows; the panel keeps both mounted. */
const tab = ref<'chat' | 'notes'>('chat')

/** The 听记 tab outlives the recorder: a finished transcript stays readable. */
const notesAvailable = computed(() => props.notesEnabled || props.noteBlocks.length > 0)

/**
 * The dock's chat button toggles the panel (as it always did) but also selects
 * the chat pane: with the panel open on 听记, pressing 聊天 used to do nothing.
 */
function onChatButton(): void {
  if (props.chatOpen && tab.value === 'chat') {
    emit('toggle-chat')
    return
  }
  tab.value = 'chat'
  if (!props.chatOpen) emit('open-panel')
}

/** 听记 always opens the panel on its own pane; the X is how it closes. */
function onNotesButton(): void {
  tab.value = 'notes'
  if (!props.chatOpen) emit('open-panel')
}

/** Where the switch button would put the picture — named, not "toggle". */
const switchLabel = computed(() =>
  props.variant === 'fullscreen' ? t('liveShare.openInWindow') : t('liveShare.fullscreen'),
)

// The element appears after the stream does (the whole surface is behind a
// v-if), so watching both is the only way to catch either order.
watch(
  [() => props.stream, videoRef],
  async ([stream]) => {
    await nextTick()
    const el = videoRef.value
    if (el && el.srcObject !== stream) el.srcObject = stream
  },
  { immediate: true },
)
</script>

<style scoped>
/* The layout's root, restyled for a picture-only surface. Scoped styles reach
   a child component's root, so this lands on `.live-shell`.
   The background rule names BOTH classes on purpose: `.live-shell` already sets
   one, and two single-class rules would be decided by bundle order. */
.member-screen {
  flex: 1;
  min-width: 0;
  min-height: 0;
}

.member-screen.live-shell {
  background: #000;
}

/* In a Document PiP window the surface is teleported straight into <body>,
   which is neither positioned nor a flex container — pin it to the window. */
.member-screen[data-variant='window'] {
  position: fixed;
  inset: 0;
}

.member-screen__video {
  flex: 1;
  min-height: 0;
  width: 100%;
  object-fit: contain;
  background: #000;
}
</style>

<template>
  <div class="member-screen" :data-variant="variant">
    <video ref="videoRef" class="member-screen__video" autoplay playsinline muted />
    <!-- Same pill and buttons as the member's own share page (LiveDock /
         LiveDockButton), because the host is looking at the same call from the
         other end. Only the actions differ: no microphone or screen capture
         here, but a way to move the picture into its own window and to stop it.
    -->
    <LiveDock brand live :title="sharer">
      <LiveDockButton
        :live="chatOpen"
        :badge="unread"
        :aria-pressed="chatOpen"
        :aria-label="t('liveShare.chat')"
        :title="t('liveShare.chat')"
        data-action="screen-chat"
        @click="emit('toggle-chat')"
      >
        <template #icon><MessageSquare :size="16" /></template>
        {{ t('liveShare.chat') }}
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
    <div v-if="chatOpen" class="member-screen__chat">
      <LiveChatPanel :messages="messages" @send="(text: string) => emit('send-chat', text)" />
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * The member's shared screen, with its dock.
 *
 * Rendered twice — full screen over the app, and inside the Document PiP
 * window — and it is deliberately the SAME component: the host moved the
 * picture, not the controls, so a second implementation would drift (a button
 * that exists in one and not the other, or two copies of the same dock CSS).
 * The dock itself is the viewer page's `LiveDock`, so both ends of a share are
 * the same object on screen.
 *
 * The picture is a live MediaStream, so it can only be attached here, in the
 * document that owns the track. That is also why the window is a PiP window
 * rather than a real one: a separate JS context cannot see the stream.
 */
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Maximize, MessageSquare, Minimize, ScreenShareOff, X } from 'lucide-vue-next'
import LiveChatPanel, { type ChatMessage } from '@/share/LiveChatPanel.vue'
import LiveDock from '@/share/LiveDock.vue'
import LiveDockButton from '@/share/LiveDockButton.vue'

const props = defineProps<{
  stream: MediaStream | null
  sharer: string
  unread: number
  chatOpen: boolean
  messages: ChatMessage[]
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
  'send-chat': [text: string]
}>()

const { t } = useI18n()
const videoRef = ref<HTMLVideoElement | null>(null)

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
.member-screen {
  position: relative;
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
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

.member-screen__chat {
  position: absolute;
  right: calc(14 * var(--dp));
  bottom: calc(60 * var(--dp));
  width: calc(280 * var(--dp));
  height: calc(320 * var(--dp));
  border-radius: var(--radius-md, 6px);
  overflow: hidden;
  background: var(--bg-primary, #fff);
  box-shadow: 0 calc(8 * var(--dp)) calc(24 * var(--dp)) color-mix(in srgb, #000 45%, transparent);
}
</style>

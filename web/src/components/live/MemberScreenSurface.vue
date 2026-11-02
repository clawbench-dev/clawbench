<template>
  <div class="member-screen" :data-variant="variant">
    <video ref="videoRef" class="member-screen__video" autoplay playsinline muted />
    <!-- One dock for both surfaces: the picture is the same picture, and the
         actions that belong next to it are the same actions. Only the frame
         differs — the whole screen, or a window of its own. -->
    <div class="member-screen__dock">
      <span class="member-screen__who" :title="sharer">
        {{ t('liveShare.memberSharing', { id: sharer }) }}
      </span>
      <button class="member-screen__btn" type="button" data-action="screen-chat" @click="emit('toggle-chat')">
        {{ t('liveShare.chat') }}
        <span v-if="unread > 0" class="live-count">{{ unread > 9 ? '9+' : unread }}</span>
      </button>
      <button
        v-if="canSwitch"
        class="member-screen__btn"
        type="button"
        data-action="screen-switch"
        @click="emit('switch')"
      >
        {{ t(variant === 'fullscreen' ? 'liveShare.openInWindow' : 'liveShare.fullscreen') }}
      </button>
      <button
        class="member-screen__btn member-screen__btn--danger"
        type="button"
        data-action="screen-stop"
        @click="emit('stop')"
      >
        {{ t('liveShare.stopScreenShare') }}
      </button>
      <button
        class="member-screen__btn member-screen__btn--icon"
        type="button"
        data-action="screen-close"
        :aria-label="t('common.close')"
        @click="emit('close')"
      >
        <X :size="14" />
      </button>
    </div>
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
 *
 * The picture is a live MediaStream, so it can only be attached here, in the
 * document that owns the track. That is also why the window is a PiP window
 * rather than a real one: a separate JS context cannot see the stream.
 */
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { X } from 'lucide-vue-next'
import LiveChatPanel, { type ChatMessage } from '@/share/LiveChatPanel.vue'

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

/* Floating dock, mirroring the audience page: it must not eat picture area. */
.member-screen__dock {
  position: absolute;
  left: 50%;
  bottom: calc(14 * var(--dp));
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: calc(6 * var(--dp));
  padding: calc(5 * var(--dp)) calc(8 * var(--dp));
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, #000 62%, transparent);
  border: 1px solid color-mix(in srgb, #fff 16%, transparent);
  box-shadow: 0 calc(6 * var(--dp)) calc(20 * var(--dp)) color-mix(in srgb, #000 45%, transparent);
}

.member-screen__who {
  max-width: calc(220 * var(--dp));
  padding: 0 calc(6 * var(--dp));
  color: #fff;
  font-size: var(--font-size-xs, 11px);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.member-screen__btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: calc(4 * var(--dp)) calc(10 * var(--dp));
  border: none;
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, #fff 14%, transparent);
  color: #fff;
  font-size: var(--font-size-xs, 11px);
  cursor: pointer;
}

.member-screen__btn:hover {
  background: color-mix(in srgb, #fff 24%, transparent);
}

.member-screen__btn--danger {
  background: color-mix(in srgb, var(--color-red, #cf222e) 80%, transparent);
}

.member-screen__btn--icon {
  padding: calc(4 * var(--dp));
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

<template>
  <Teleport to="body">
    <!-- ── 语音胶囊 ──
         录音中在这里滚动显示识别到的文字；说完才写进输入框（有光标时）
         或留在这里等处理（没有光标时）。 -->
    <Transition name="voice-slide">
      <div
        v-if="capsuleVisible"
        class="voice-capsule"
        :style="capsuleStyle"
        role="status"
        aria-live="polite"
      >
        <span v-if="voiceState === 'recording'" class="voice-dot" aria-hidden="true"></span>
        <span v-else-if="voiceState === 'transcribing'" class="voice-wave" aria-hidden="true">
          <i></i><i></i><i></i><i></i><i></i>
        </span>
        <Check v-else-if="justWrote" :size="15" class="voice-capsule-mic" />
        <Mic v-else :size="15" class="voice-capsule-mic" />

        <div class="voice-capsule-body">
          <div
            ref="capTextEl"
            class="voice-capsule-text"
            :class="{ 'is-empty': !voiceText.trim() && !justWrote }"
          >{{ justWrote ? t('chat.voice.written') : (voiceText.trim() ? voiceText : t('chat.voice.listening')) }}</div>
          <div class="voice-capsule-sub">
            <template v-if="voiceState === 'recording'">
              {{ elapsedText }} · {{ t('chat.voice.releaseToStop') }} · {{ t('chat.voice.escToCancel') }}
            </template>
            <template v-else-if="voiceState === 'transcribing'">{{ t('chat.voice.transcribing') }}</template>
            <template v-else-if="justWrote" />
            <template v-else>{{ t('chat.voice.capsuleDoneHint') }}</template>
          </div>
        </div>

        <div v-if="capsuleDone" class="voice-capsule-actions">
          <button class="voice-cap-btn" @click="copyDraft">{{ t('chat.voice.copy') }}</button>
          <button class="voice-cap-btn primary" @click="draftToChat">{{ t('chat.voice.toChat') }}</button>
          <button class="voice-cap-x" :title="t('chat.voice.discard')" @click="discardDraft">
            <X :size="14" />
          </button>
        </div>
      </div>
    </Transition>

    <!-- ── 手机：输入框聚焦时的小圆钮 ──
         A div, not a <button>: a real button is focusable, so tapping it moves
         the caret out of the field and the keyboard closes (then reopens when
         the transcript is written — a visible flicker). A non-focusable
         element plus preventDefault on every "press" event keeps the field
         focused for the whole dictation, so the keyboard never moves. -->
    <Transition name="voice-pop">
      <div
        v-if="fabVisible"
        ref="fabEl"
        class="voice-fab"
        role="button"
        :style="fabStyle"
        :title="t('chat.voice.holdToTalk')"
        :aria-label="t('chat.voice.holdToTalk')"
        @pointerup="onFabUp"
        @pointercancel="onFabCancel"
      >
        <Mic :size="18" />
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
/**
 * VoiceOverlay — the global surface of voice input.
 *
 * Mounted once (App.vue). One rule: *wherever the caret is*.
 *
 *   - a field owns the caret → the capsule shows the live transcript while you
 *     speak; on release the whole text is written into that field at the caret
 *     (useVoiceInput's `field` mode). Nothing is spliced in mid-utterance, so a
 *     take that gets cancelled leaves no half sentence behind.
 *   - nothing writable owns the caret → the capsule keeps the transcript and
 *     offers copy / send to chat / discard (the `buffer` mode).
 *   - touch devices → a mic button floats next to the focused field, because
 *     there is no F9 to hold. Sliding off it cancels the take.
 *
 * The F9 shortcut lives here rather than in the chat composer: it has to work
 * with focus anywhere in the app, and only one place may own it.
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Mic, X } from 'lucide-vue-next'
import { useVoiceInput } from '@/composables/useVoiceInput'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { useToast } from '@/composables/useToast'
import { createVoiceTarget, writeVoiceText } from '@/utils/voiceTarget'

const { t } = useI18n()
const toast = useToast()
const { isTouchPrimary } = usePlatformDetect()

const {
  state: voiceState,
  inputText: voiceText,
  draftPending,
  justWrote,
  configured,
  startGlobal,
  startAt,
  stop,
  cancel,
  discardDraft: discardVoiceDraft,
  shortcutKey,
} = useVoiceInput()

/* ── elapsed timer (only while recording) ─────────────────────────── */
const elapsed = ref(0)
let startedAt = 0
let ticker: ReturnType<typeof setInterval> | null = null

function startTicker() {
  stopTicker()
  startedAt = Date.now()
  elapsed.value = 0
  ticker = setInterval(() => { elapsed.value = Date.now() - startedAt }, 250)
}
function stopTicker() {
  if (ticker) { clearInterval(ticker); ticker = null }
}
const elapsedText = computed(() => {
  const s = Math.floor(elapsed.value / 1000)
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
})

watch(voiceState, (s) => {
  if (s === 'recording') startTicker()
  else stopTicker()
})

/* ── capsule ──────────────────────────────────────────────────────── */
const capTextEl = ref<HTMLElement | null>(null)
const capsuleVisible = computed(() =>
  voiceState.value !== 'idle' || draftPending.value || justWrote.value,
)
const capsuleDone = computed(() => draftPending.value && voiceState.value === 'idle')
const capsuleBottom = ref(16)

/**
 * Follow the newest words while speaking: the transcript can be longer than
 * the two visible lines, and the end is the part the user is waiting for.
 * Once it is done, jump back to the top so it reads from the beginning.
 */
watch([voiceText, voiceState], () => {
  const el = capTextEl.value
  if (!el) return
  void nextTick(() => {
    el.scrollTop = voiceState.value === 'recording' ? el.scrollHeight : 0
  })
})

/**
 * Sits above whatever is pinned to the bottom of the current screen: the
 * bottom dock on a phone, the chat composer when that tab is open. Computed
 * from live layout rather than a fixed offset, because both of those are
 * conditional.
 */
function placeCapsule() {
  const vv = window.visualViewport
  const keyboard = vv ? Math.max(0, window.innerHeight - (vv.offsetTop + vv.height)) : 0
  const dock = document.querySelector('.bottom-dock-wrapper')?.getBoundingClientRect().height ?? 0
  const composer = document.querySelector('.chat-input-wrapper')?.getBoundingClientRect().height ?? 0
  capsuleBottom.value = keyboard + dock + composer + 16
}
const capsuleStyle = computed(() => ({ bottom: `${capsuleBottom.value}px` }))

/* ── actions on the finished transcript ───────────────────────────── */
async function copyDraft() {
  const text = voiceText.value
  try {
    await navigator.clipboard.writeText(text)
    toast.show(t('chat.voice.copied'), { icon: 'success', type: 'success', duration: 2000 })
  } catch {
    toast.show(t('chat.voice.copyFailed'), { icon: 'warning', type: 'error', duration: 4000 })
  }
}

/**
 * Drops the transcript into the chat composer at its caret.
 *
 * The composer is the only field we address by selector: it is the app's
 * primary writing surface, and "放到聊天框" must work even though focus is
 * nowhere near it. Every other field is reached through the caret itself.
 */
function draftToChat() {
  const el = document.querySelector<HTMLTextAreaElement>('[data-voice-chat-input]')
  const target = createVoiceTarget(el)
  if (!target) {
    void copyDraft()
    return
  }
  writeVoiceText(target, voiceText.value)
  el?.focus()
  discardVoiceDraft()
  toast.show(t('chat.voice.toChatDone'), { icon: 'success', type: 'success', duration: 2500 })
}

function discardDraft() {
  discardVoiceDraft()
  toast.show(t('chat.voice.discarded'), { icon: 'delete', type: 'info', duration: 2000 })
}

/* ── touch: floating mic next to the focused field ────────────────── */
const focusEl = ref<HTMLElement | null>(null)
const fabEl = ref<HTMLElement | null>(null)
const fabPos = ref({ top: 0, right: 12 })
// Where the button was when the finger landed. The element unmounts as soon as
// recording starts, so the bounds are kept here for the slide-off check.
let fabRect: { left: number; right: number; top: number; bottom: number } | null = null

const fabVisible = computed(() => {
  // No recognition engine → no button. Pressing it could only ever produce
  // "go configure it"; F9 still gives that hint for discoverability.
  if (!configured.value) return false
  if (!isTouchPrimary.value || voiceState.value !== 'idle') return false
  const el = focusEl.value
  if (!el) return false
  return !!createVoiceTarget(el)
})
const fabStyle = computed(() => ({ top: `${fabPos.value.top}px`, right: `${fabPos.value.right}px` }))

/**
 * Sits just above the focused field, the same rule as the status pill.
 *
 * Deliberately NOT pinned to the keyboard's top edge: on the chat tab that
 * strip is the composer, and the button would land on the send button — a
 * tap meant for dictation would send the message instead.
 */
function placeFab() {
  const el = focusEl.value
  if (!el) return
  const vv = window.visualViewport
  const visibleBottom = vv ? vv.offsetTop + vv.height : window.innerHeight
  const r = (el.closest('.chat-input-wrapper') ?? el).getBoundingClientRect()
  let top = r.top - 50
  // A field near the top of the screen has no room above it; keep the button
  // inside the visible area rather than off-screen.
  top = Math.max(8, Math.min(top, visibleBottom - 50))
  fabPos.value = { top, right: 12 }
}

function onFocusIn(e: FocusEvent) {
  focusEl.value = e.target as HTMLElement
  placeFab()
}
function onFocusOut() {
  // activeElement is already the next element by the time this fires, but give
  // the browser a tick for the case where focus lands on a freshly mounted node.
  window.setTimeout(() => {
    focusEl.value = (document.activeElement as HTMLElement | null) ?? null
    placeFab()
  }, 0)
}

function onFabDown() {
  // Focus is kept by the window-capture handler below (preventDefault before
  // the browser can move the caret); starting here would race it.
  fabRect = fabEl.value?.getBoundingClientRect() ?? null
  void startAt(focusEl.value)
  // The button unmounts the moment recording starts, so its own pointerup
  // never fires. Watch the window instead — otherwise lifting the finger
  // leaves the recorder running and nothing is ever written.
  window.addEventListener('pointerup', onFabUp, { once: true })
  window.addEventListener('pointercancel', onFabCancel, { once: true })
  window.addEventListener('pointermove', onFabMove, true)
}

/**
 * Sliding the finger off the button cancels the take.
 *
 * Touch pointers are implicitly captured by the element that received the
 * press, so `pointerleave` never fires; the position has to be compared by
 * hand. A small tolerance keeps a shaky finger from cancelling by accident.
 */
function onFabMove(e: PointerEvent) {
  if (!fabRect || voiceState.value !== 'recording') return
  const pad = 12
  const outside =
    e.clientX < fabRect.left - pad ||
    e.clientX > fabRect.right + pad ||
    e.clientY < fabRect.top - pad ||
    e.clientY > fabRect.bottom + pad
  if (!outside) return
  cleanupFabPointer()
  cancel()
}

function cleanupFabPointer() {
  window.removeEventListener('pointerup', onFabUp)
  window.removeEventListener('pointercancel', onFabCancel)
  window.removeEventListener('pointermove', onFabMove, true)
  fabRect = null
}

function onFabUp() {
  cleanupFabPointer()
  if (voiceState.value === 'recording') void stop()
}
function onFabCancel() {
  cleanupFabPointer()
  cancel()
}

/* ── global shortcut ──────────────────────────────────────────────── */
function inTerminal(): boolean {
  const el = document.activeElement as HTMLElement | null
  return !!el?.closest?.('.xterm')
}

function onKeyDown(e: KeyboardEvent) {
  if (e.key === 'Escape' && (voiceState.value === 'recording' || voiceState.value === 'transcribing')) {
    // Capture phase: cancelling a dictation must not also close the dialog the
    // user happens to be typing in.
    e.preventDefault()
    e.stopPropagation()
    cancel()
    return
  }
  if (e.code !== 'F9' || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
  // Only the F9 binding is supported (see useVoiceInput.shortcutKey).
  if (shortcutKey() !== 'F9') return
  // The terminal consumes function keys itself; F9 there belongs to the shell.
  if (inTerminal()) return
  e.preventDefault()
  if (e.repeat) return
  void startGlobal()
}

function onKeyUp(e: KeyboardEvent) {
  if (e.code !== 'F9' || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
  if (shortcutKey() !== 'F9' || inTerminal()) return
  e.preventDefault()
  void stop()
}

function onWindowBlur() {
  // keyup never arrives when focus leaves while the key is held.
  if (voiceState.value === 'recording') void stop()
}

/* ── positioning plumbing ─────────────────────────────────────────── */
let raf = 0
function reposition() {
  if (raf) return
  raf = requestAnimationFrame(() => {
    raf = 0
    placeCapsule()
    placeFab()
  })
}

/**
 * Our surfaces are chrome, not page content. Any popup/drawer that dismisses
 * on "click outside" (they listen on document, usually in the capture phase)
 * must not treat a press on the mic button or the capsule as outside — the
 * quote bar would vanish the moment dictation starts over its input.
 *
 * The listener runs on `window` capture, which is ahead of every document
 * listener. For the FAB the press is handled HERE (stopPropagation would
 * otherwise also swallow the element's own listener); for the capsule it only
 * hides the press — its buttons act on `click`, which is unaffected.
 */
function onWindowPointerDown(e: PointerEvent) {
  const el = e.target as HTMLElement | null
  if (!el || typeof el.closest !== 'function') return
  if (el.closest('.voice-fab')) {
    e.preventDefault()
    e.stopPropagation()
    onFabDown()
    return
  }
  if (el.closest('.voice-capsule')) e.stopPropagation()
}

function onWindowTouchStart(e: TouchEvent) {
  const el = e.target as HTMLElement | null
  // Belt and braces for WebViews that move the caret on the touch itself
  // rather than on the compatibility mouse events.
  if (el?.closest?.('.voice-fab')) e.preventDefault()
}

onMounted(() => {
  window.addEventListener('pointerdown', onWindowPointerDown, true)
  window.addEventListener('touchstart', onWindowTouchStart, { capture: true, passive: false })
  window.addEventListener('keydown', onKeyDown, true)
  window.addEventListener('keyup', onKeyUp, true)
  window.addEventListener('blur', onWindowBlur)
  window.addEventListener('resize', reposition)
  window.addEventListener('scroll', reposition, true)
  document.addEventListener('focusin', onFocusIn, true)
  document.addEventListener('focusout', onFocusOut, true)
  window.visualViewport?.addEventListener('resize', reposition)
  window.visualViewport?.addEventListener('scroll', reposition)
  placeCapsule()
})

onBeforeUnmount(() => {
  window.removeEventListener('pointerdown', onWindowPointerDown, true)
  window.removeEventListener('touchstart', onWindowTouchStart, true)
  window.removeEventListener('keydown', onKeyDown, true)
  window.removeEventListener('keyup', onKeyUp, true)
  window.removeEventListener('blur', onWindowBlur)
  window.removeEventListener('resize', reposition)
  window.removeEventListener('scroll', reposition, true)
  document.removeEventListener('focusin', onFocusIn, true)
  document.removeEventListener('focusout', onFocusOut, true)
  window.visualViewport?.removeEventListener('resize', reposition)
  window.visualViewport?.removeEventListener('scroll', reposition)
  cleanupFabPointer()
  stopTicker()
  if (raf) cancelAnimationFrame(raf)
  // A dictation must not outlive the overlay (logout / project switch).
  cancel()
})
</script>

<style scoped>
/* The two surfaces share one elevated look: card background, hairline border,
   soft shadow. They sit above modals (3000) and below toasts (--z-toast), so a
   result toast is never hidden by them. */
.voice-capsule,
.voice-fab {
  position: fixed;
  z-index: calc(var(--z-toast) - 50);
  box-sizing: border-box;
  /* Lift the surface off the page in EVERY theme. A fixed background step
     (secondary → elevated) is invisible in the ~15 themes where those two are
     nearly the same colour, so the surface is mixed from the text colour
     instead: it darkens in light themes and lightens in dark ones, always
     moving away from whatever is behind it. */
  background: color-mix(in srgb, var(--text-primary) 8%, var(--bg-elevated, var(--bg-secondary)));
  border: 1px solid color-mix(in srgb, var(--text-primary) 18%, var(--border-color));
  box-shadow:
    0 1px 2px rgba(0, 0, 0, 0.16),
    0 10px 28px rgba(0, 0, 0, 0.22);
}

/* ── capsule ── */
.voice-capsule {
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: calc(10 * var(--dp));
  width: min(calc(600 * var(--dp)), calc(100vw - 24 * var(--dp)));
  padding: calc(9 * var(--dp)) calc(10 * var(--dp)) calc(9 * var(--dp)) calc(13 * var(--dp));
  border-radius: var(--radius-lg);
}
.voice-capsule-body { flex: 1; min-width: 0; }
.voice-capsule-text {
  font-size: var(--font-size-md);
  line-height: var(--line-height-snug);
  color: var(--text-primary);
  /* Two lines tall; longer transcripts scroll inside while speaking. */
  max-height: calc(var(--font-size-md) * 2.9);
  overflow-y: auto;
  overflow-wrap: break-word;
  scrollbar-width: none;
}
.voice-capsule-text::-webkit-scrollbar { display: none; }
.voice-capsule-text.is-empty { color: var(--text-muted); }
.voice-capsule-sub {
  margin-top: calc(2 * var(--dp));
  font-size: var(--font-size-2xs);
  color: var(--text-hint);
  font-variant-numeric: tabular-nums;
}
.voice-capsule-mic { color: var(--accent-color); flex: none; }
.voice-capsule-actions { display: flex; align-items: center; gap: calc(4 * var(--dp)); flex: none; }
.voice-cap-btn {
  height: calc(28 * var(--dp));
  padding: 0 calc(10 * var(--dp));
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color);
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  white-space: nowrap;
}
.voice-cap-btn:hover { color: var(--text-primary); border-color: var(--border-strong, var(--text-hint)); }
.voice-cap-btn.primary {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}
.voice-cap-btn.primary:hover { background: var(--accent-hover, var(--accent-color)); }
.voice-cap-x {
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  display: grid;
  place-items: center;
  /* A <button> keeps the UA's default face (a light rounded chip) unless it is
     cleared — in dark themes that reads as a broken white square. */
  background: transparent;
  border: none;
  padding: 0;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
}
.voice-cap-x:hover { background: var(--bg-hover); color: var(--text-primary); }

/* ── touch mic ── */
.voice-fab {
  width: calc(42 * var(--dp));
  height: calc(42 * var(--dp));
  display: grid;
  place-items: center;
  border-radius: 50%;
  /* Accent-tinted so it reads as a control, not as another panel: the mic is
     the one thing here the user is meant to press. */
  background: color-mix(in srgb, var(--accent-color) 16%, var(--bg-elevated, var(--bg-secondary)));
  border-color: color-mix(in srgb, var(--accent-color) 42%, var(--border-color));
  box-shadow:
    0 2px 4px rgba(0, 0, 0, 0.18),
    0 8px 22px rgba(0, 0, 0, 0.24),
    0 0 0 calc(3 * var(--dp)) color-mix(in srgb, var(--accent-color) 14%, transparent);
  color: var(--accent-color);
  /* The button is pressed and held; the browser must not treat that as a
     scroll gesture or synthesize a click on release. */
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
}
.voice-fab:active { background: var(--bg-active); }

/* ── recording dot / wave ── */
.voice-dot {
  position: relative;
  width: calc(8 * var(--dp));
  height: calc(8 * var(--dp));
  border-radius: 50%;
  background: var(--color-red, #e5484d);
  flex: none;
}
.voice-dot::after {
  content: '';
  position: absolute;
  inset: calc(-4 * var(--dp));
  border-radius: 50%;
  background: var(--color-red, #e5484d);
  opacity: 0.25;
  animation: voice-pulse 1.4s ease-out infinite;
}
@keyframes voice-pulse {
  0% { transform: scale(0.6); opacity: 0.35; }
  70% { transform: scale(1.6); opacity: 0; }
  100% { opacity: 0; }
}

.voice-wave {
  display: flex;
  align-items: center;
  gap: calc(2.5 * var(--dp));
  height: calc(18 * var(--dp));
  flex: none;
}
.voice-wave i {
  width: calc(3 * var(--dp));
  height: calc(6 * var(--dp));
  border-radius: calc(2 * var(--dp));
  background: var(--accent-color);
  animation: voice-wave 1s ease-in-out infinite;
}
.voice-wave i:nth-child(1) { animation-delay: 0s; }
.voice-wave i:nth-child(2) { animation-delay: 0.12s; }
.voice-wave i:nth-child(3) { animation-delay: 0.24s; }
.voice-wave i:nth-child(4) { animation-delay: 0.36s; }
.voice-wave i:nth-child(5) { animation-delay: 0.18s; }
@keyframes voice-wave {
  0%, 100% { height: calc(5 * var(--dp)); opacity: 0.55; }
  50% { height: calc(16 * var(--dp)); opacity: 1; }
}

/* ── transitions ── */
.voice-pop-enter-active,
.voice-pop-leave-active { transition: opacity var(--duration-fast), transform var(--duration-fast); }
.voice-pop-enter-from,
.voice-pop-leave-to { opacity: 0; transform: translateY(calc(4 * var(--dp))); }

.voice-slide-enter-active,
.voice-slide-leave-active { transition: opacity var(--duration-base), transform var(--duration-base); }
.voice-slide-enter-from,
.voice-slide-leave-to {
  opacity: 0;
  /* Keep the centring translate — only the lift animates. */
  transform: translateX(-50%) translateY(calc(10 * var(--dp)));
}

@media (prefers-reduced-motion: reduce) {
  /* The wave and the pulsing dot are the only signal that audio is being
     captured, so they keep animating (same call as BusyBar). Only the
     entrance transforms are dropped. */
  .voice-pop-enter-from,
  .voice-pop-leave-to,
  .voice-slide-enter-from,
  .voice-slide-leave-to { transform: translateX(-50%); }
  .voice-pop-enter-from,
  .voice-pop-leave-to { transform: none; }
}
</style>

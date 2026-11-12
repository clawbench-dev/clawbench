<template>
  <Teleport to="body">
    <!-- ── 光标模式：贴在输入框上方的迷你胶囊 ── -->
    <Transition name="voice-pop">
      <div
        v-if="pillVisible"
        ref="pillEl"
        class="voice-pill"
        :style="pillStyle"
        role="status"
      >
        <span v-if="voiceState === 'recording'" class="voice-dot" aria-hidden="true"></span>
        <span v-if="voiceState === 'recording'" class="voice-wave" aria-hidden="true">
          <i></i><i></i><i></i><i></i><i></i>
        </span>
        <LoadingIndicator v-else-if="voiceState === 'transcribing'" size="sm" inline />
        <Check v-else :size="13" />
        <!-- While recording: just the timer. Where the text is going is already
             on screen (it is being written into the field under this pill), so
             naming the field was pure noise. -->
        <span v-if="voiceState === 'recording'" class="voice-pill-hint">{{ elapsedText }}</span>
        <span v-else-if="voiceState === 'transcribing'" class="voice-pill-text">{{ t('chat.voice.transcribing') }}</span>
        <span v-else class="voice-pill-text">{{ t('chat.voice.written') }}</span>
      </div>
    </Transition>

    <!-- ── 没有可写光标：底部胶囊横幅 ── -->
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
        <Mic v-else :size="15" class="voice-capsule-mic" />

        <div class="voice-capsule-body">
          <div class="voice-capsule-text" :class="{ 'is-empty': !voiceText.trim() }">
            {{ voiceText.trim() ? voiceText : t('chat.voice.listening') }}
          </div>
          <div class="voice-capsule-sub">
            <template v-if="voiceState === 'recording'">
              {{ elapsedText }} · {{ t('chat.voice.releaseToStop') }} · {{ t('chat.voice.escToCancel') }}
            </template>
            <template v-else-if="voiceState === 'transcribing'">{{ t('chat.voice.transcribing') }}</template>
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

    <!-- ── 手机：输入框聚焦时的小圆钮 ── -->
    <Transition name="voice-pop">
      <button
        v-if="fabVisible"
        class="voice-fab"
        :style="fabStyle"
        :title="t('chat.voice.holdToTalk')"
        :aria-label="t('chat.voice.holdToTalk')"
        @pointerdown.prevent="onFabDown"
        @pointerup="onFabUp"
        @pointercancel="onFabUp"
      >
        <Mic :size="18" />
      </button>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
/**
 * VoiceOverlay — the global surface of voice input.
 *
 * Mounted once (App.vue). Three pieces, one rule: *wherever the caret is*.
 *
 *   - a field owns the caret → a small pill sits above that field, the
 *     transcript is written into it (useVoiceInput's `field` mode).
 *   - nothing writable owns the caret → the capsule banner collects the text
 *     and offers copy / send to chat / discard (the `buffer` mode).
 *   - touch devices → a mic button floats next to the focused field, because
 *     there is no F9 to hold.
 *
 * The F9 shortcut lives here rather than in the chat composer: it has to work
 * with focus anywhere in the app, and only one place may own it.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Mic, X } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { useVoiceInput } from '@/composables/useVoiceInput'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { useToast } from '@/composables/useToast'
import { createVoiceTarget, writeVoiceText } from '@/utils/voiceTarget'

const { t } = useI18n()
const toast = useToast()
const { isTouchPrimary } = usePlatformDetect()

const {
  state: voiceState,
  mode: voiceMode,
  inputText: voiceText,
  targetEl,
  draftPending,
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

/* ── mini pill (caret mode) ───────────────────────────────────────── */
const pillEl = ref<HTMLElement | null>(null)
const pillPos = ref({ top: 0, left: 0 })

const pillVisible = computed(() => !!targetEl.value && (voiceMode.value === 'field' || voiceState.value !== 'idle'))
const pillStyle = computed(() => ({ top: `${pillPos.value.top}px`, left: `${pillPos.value.left}px` }))

/**
 * Sits just above the field — never over it. The user is watching the words
 * land in that box; a badge on top of the line being written defeats the
 * point. Falls below only when there is no room above.
 */
function placePill() {
  const el = targetEl.value
  if (!el || !el.isConnected) return
  // Always just above the field itself — for the chat composer that means it
  // floats over the toolbar row, which is fine: it is the closest spot to the
  // line being written, and the pill never takes clicks.
  const r = el.getBoundingClientRect()
  const width = pillEl.value?.offsetWidth || 120
  const height = pillEl.value?.offsetHeight || 34
  const gap = 6
  let top = r.top - height - gap
  if (top < 8) top = r.bottom + gap
  const left = Math.max(8, Math.min(r.left, window.innerWidth - width - 8))
  pillPos.value = { top, left }
}

/* ── capsule (no caret) ───────────────────────────────────────────── */
const capsuleVisible = computed(() => voiceMode.value === 'buffer' && (voiceState.value !== 'idle' || draftPending.value))
const capsuleDone = computed(() => draftPending.value && voiceState.value === 'idle')
const capsuleBottom = ref(16)

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
const fabPos = ref({ top: 0, right: 12 })

const fabVisible = computed(() => {
  if (!isTouchPrimary.value || voiceState.value !== 'idle' || targetEl.value) return false
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
  // Same anchor rule as the pill: clear the composer's toolbar, not just the field.
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
  // `.prevent` on pointerdown keeps the field focused — losing it would drop
  // the caret we are about to write into.
  void startAt(focusEl.value)
  // The button unmounts the moment recording starts (the status pill replaces
  // it), so its own pointerup never fires. Watch the window instead — otherwise
  // lifting the finger leaves the recorder running and nothing is ever written.
  window.addEventListener('pointerup', onFabUp, { once: true })
  window.addEventListener('pointercancel', onFabUp, { once: true })
}
function onFabUp() {
  window.removeEventListener('pointerup', onFabUp)
  window.removeEventListener('pointercancel', onFabUp)
  if (voiceState.value === 'recording') void stop()
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
    placePill()
    placeCapsule()
    placeFab()
  })
}

let followTimer: ReturnType<typeof setInterval> | null = null

watch(pillVisible, (on) => {
  if (on) {
    placePill()
    if (!followTimer) followTimer = setInterval(placePill, 300)
  } else if (followTimer) {
    clearInterval(followTimer)
    followTimer = null
  }
})
watch([capsuleVisible, capsuleDone], () => { placeCapsule() })
watch(fabVisible, (on) => { if (on) placeFab() })

onMounted(() => {
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
  window.removeEventListener('keydown', onKeyDown, true)
  window.removeEventListener('keyup', onKeyUp, true)
  window.removeEventListener('blur', onWindowBlur)
  window.removeEventListener('resize', reposition)
  window.removeEventListener('scroll', reposition, true)
  document.removeEventListener('focusin', onFocusIn, true)
  document.removeEventListener('focusout', onFocusOut, true)
  window.visualViewport?.removeEventListener('resize', reposition)
  window.visualViewport?.removeEventListener('scroll', reposition)
  stopTicker()
  if (followTimer) { clearInterval(followTimer); followTimer = null }
  if (raf) cancelAnimationFrame(raf)
  // A dictation must not outlive the overlay (logout / project switch).
  cancel()
})

// Keep the "is there a target" check honest when the mode changes without a
// focus event (e.g. the field was removed mid-recording).
watch(voiceMode, () => { reposition() })
</script>

<style scoped>
/* The three surfaces share one elevated look: card background, hairline
   border, soft shadow. They sit above modals (3000) and below toasts
   (--z-toast), so a result toast is never hidden by them. */
.voice-pill,
.voice-capsule,
.voice-fab {
  position: fixed;
  z-index: calc(var(--z-toast) - 50);
  box-sizing: border-box;
}

/* ── mini pill ── */
.voice-pill {
  display: flex;
  align-items: center;
  gap: calc(7 * var(--dp));
  max-width: min(calc(320 * var(--dp)), calc(100vw - 16 * var(--dp)));
  height: calc(34 * var(--dp));
  padding: 0 calc(11 * var(--dp));
  background: var(--bg-elevated, var(--bg-secondary));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
  box-shadow: var(--shadow-md);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  pointer-events: none;
  white-space: nowrap;
}
.voice-pill-text {
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text-primary);
}
.voice-pill-hint {
  color: var(--text-hint);
  font-variant-numeric: tabular-nums;
}

/* ── capsule banner ── */
.voice-capsule {
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: calc(10 * var(--dp));
  width: min(calc(600 * var(--dp)), calc(100vw - 24 * var(--dp)));
  padding: calc(9 * var(--dp)) calc(10 * var(--dp)) calc(9 * var(--dp)) calc(13 * var(--dp));
  background: var(--bg-elevated, var(--bg-secondary));
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-lg, var(--shadow-md));
}
.voice-capsule-body { flex: 1; min-width: 0; }
.voice-capsule-text {
  font-size: var(--font-size-md);
  line-height: var(--line-height-snug);
  color: var(--text-primary);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  overflow-wrap: break-word;
}
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
  background: var(--bg-elevated, var(--bg-secondary));
  border: 1px solid var(--border-color);
  box-shadow: var(--shadow-md);
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

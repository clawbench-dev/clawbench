<template>
  <Transition name="quote-bar">
    <div
      v-if="visible && (quoteData || composerMode)"
      ref="barRef"
      class="quote-question-bar"
      :class="{ 'quote-question-bar--anchored': anchored }"
      :style="barStyle"
    >

      <!-- Collapsed: the floating selection toolbar (issue #544). It follows
           the selected text instead of parking at the top of the page, so a
           selection made mid-document is acted on where it sits.
           Every action carries a LABEL, not just an icon: copy / comment /
           quote are not self-evident from a glyph, and this toolbar is now the
           only entry point (the collapsed quote preview is gone).
           `pointerdown` pins before the click lands: the global selection
           handler re-evaluates on pointerup and would otherwise hide the bar —
           taking the button being pressed with it — out from under the click. -->
      <div v-if="showCollapsed" class="quote-bar-toolbar" @pointerdown="onRowPointerDown">
        <CopyButton
          :text="quoteData?.text || ''"
          class="quote-bar-action"
          :label="t('common.copy')"
          @click="onCopyAction"
        />
        <button
          class="quote-bar-action"
          @click.stop="expand()"
          :title="t('quoteBar.comment')"
          :aria-label="t('quoteBar.comment')"
        >
          <MessageSquare :size="14" />
          <span>{{ t('quoteBar.comment') }}</span>
        </button>
        <button
          class="quote-bar-action"
          @click.stop="onAddClick"
          @touchstart.passive="startHold('add')"
          @touchend="endHold"
          @touchcancel="cancelHold"
          @mousedown.stop="startHold('add')"
          @mouseup.stop="endHold"
          @mouseleave="cancelHold"
          :title="t('quoteBar.quoteSelectionHint')"
          :aria-label="t('quoteBar.quoteSelection')"
        >
          <Quote :size="14" />
          <span>{{ t('quoteBar.quoteSelection') }}</span>
        </button>
      </div>

      <!-- Expanded: quoted snippet (full, when there is one) + input. -->
      <div v-else class="quote-bar-expanded">
        <!-- Destination row (issue #560 follow-up): WHICH conversation this
             comment will land in, shown before the content so it reads as the
             addressee of the message ("To: …", then the body).

             One line, click to change: opening the picker here only RECORDS a
             destination (intent `choose`) — it must not commit anything, since
             the user is still composing. -->
        <button
          class="qq-target"
          type="button"
          :title="t('quoteBar.changeTarget')"
          :aria-label="`${t('quoteBar.sendTo')} ${targetLabel}${t('quoteBar.targetSeparator')}${t('quoteBar.changeTarget')}`"
          @click.stop="onChangeTarget"
        >
          <AgentIcon
            v-if="targetAgentId"
            :backend="targetBackend"
            :name="targetAgentName"
            :avatar="targetAgentAvatar"
            size="sm"
            class="qq-target-icon"
          />
          <span v-else class="qq-target-icon qq-target-icon--empty" aria-hidden="true">
            <Plus :size="9" />
          </span>
          <span class="qq-target-label">{{ t('quoteBar.sendTo') }}</span>
          <span class="qq-target-name" :class="{ 'qq-target-name--empty': !targetLabel }">{{ targetLabel }}</span>
          <ChevronRight :size="12" class="qq-target-chevron" />
        </button>

        <!-- Quoted snippet — fully shown when expanded. Absent when the user has
             not selected anything yet (composer mode): the bar then asks for a
             message directly. -->
        <div v-if="quoteData" class="qq-quoted-snippet">
          <span class="qq-quoted-text qq-quoted-text--expanded">{{ displayQuoteText }}</span>
          <CopyButton :text="quoteData?.text || ''" class="qq-copy-btn" @click.stop />
        </div>

        <!-- Pending quote target (composer mode only): the file or issue/PR the
             bar will quote when committed. Read-only — it is not in the chat
             context yet, so there is nothing to remove here; closing the bar
             discards it. On commit it becomes a quote card (same card as a text
             selection quote), not a separate attachment. -->
        <div v-if="composerAttachment" class="qq-pending-attachment">
          <Link v-if="composerAttachment.kind === 'url'" :size="13" class="qq-pending-icon" />
          <FileIcon v-else :path="composerAttachment.path || composerAttachment.label" :size="13" class="qq-pending-icon" />
          <span class="qq-pending-label">{{ composerAttachment.label }}</span>
        </div>

        <!-- Input -->
        <div class="qq-input-container">
          <div class="qq-input-row">
            <textarea
              ref="inputRef"
              v-model="inputText"
              class="qq-textarea"
              rows="1"
              :placeholder="t('quoteBar.placeholder')"
              @keydown.enter.exact.prevent="onEnterKey"
              @input="autoResizeTextarea"
            />
            <button
              class="qq-add-btn"
              @click="onAddClick"
              @touchstart.passive="startHold('add')"
              @touchend="endHold"
              @touchcancel="cancelHold"
              @mousedown.stop="startHold('add')"
              @mouseup.stop="endHold"
              @mouseleave="cancelHold"
              :title="t('quoteBar.addToChatHint')"
              :aria-label="t('quoteBar.addToChat')"
            >
              <Plus :size="13" />
            </button>
            <button
              class="qq-send-btn"
              :class="{ disabled: !canSend }"
              @click="onSendClick"
              @touchstart.passive="startHold('send')"
              @touchend="endHold"
              @touchcancel="cancelHold"
              @mousedown.stop="startHold('send')"
              @mouseup.stop="endHold"
              @mouseleave="cancelHold"
              :title="t('quoteBar.sendHint')"
            >
              <Send :size="13" />
            </button>
          </div>
        </div>
      </div>

    </div>
  </Transition>
</template>

<script setup>
import { Plus, Send, Link, MessageSquare, Quote, ChevronRight } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { canSendInput } from '@/utils/quoteQuestionUtils'
import { computeSelectionToolbarStyle } from '@/utils/selectionToolbarPosition'
import { useVisualViewportInset } from '@/composables/useVisualViewportInset'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { useAgents } from '@/composables/useAgents'
import { useChosenTarget, chooseTarget, clearChosenTarget } from '@/composables/useConversationTarget'
import FileIcon from '@/components/common/FileIcon.vue'
import CopyButton from '@/components/common/CopyButton.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'

const { t } = useI18n()

// ── Destination row ────────────────────────────────────────────
// The bar shows WHICH conversation the comment will land in, so "send" is never
// a guess. Default = the active session (the common case, zero clicks); picking
// another one is a single click on the row.
const identity = useSessionIdentity()
const { getAgentBackend, getAgentName, getAgentAvatar } = useAgents()
const chosenTarget = useChosenTarget()

const targetLabel = computed(() => {
  const picked = chosenTarget.value
  if (picked) return picked.title || t('session.unnamed')
  const id = identity.currentSessionId.value
  if (!id) return t('quoteBar.chooseSession')
  return identity.currentSessionTitle.value || t('session.unnamed')
})

/**
 * Agent of the destination, for the row's icon.
 *
 * When a target was picked, the picker recorded its agent. Otherwise this is
 * the active session's agent, which the identity store already tracks.
 */
const targetAgentId = computed(() => {
  const picked = chosenTarget.value
  return picked ? picked.agentId : identity.currentAgentId.value || ''
})
const targetBackend = computed(() => getAgentBackend(targetAgentId.value))
const targetAgentName = computed(() => getAgentName(targetAgentId.value))
const targetAgentAvatar = computed(() => getAgentAvatar(targetAgentId.value))

function onChangeTarget() {
  chooseTarget()
}

const props = defineProps({
  visible: Boolean,
  quoteData: Object,
  // Rect of the text selection this bar is about, used to float the toolbar
  // next to it (issue #544). Null when there is no DOM selection behind the
  // bar (entry-point composers, terminal/CodeMirror selections, the OpenAPI
  // preview's iframe) — the bar then keeps its pinned top position.
  anchor: Object,
  // Opened from an entry point (e.g. the issue/PR detail header) rather than by
  // a text selection. The bar is then useful with no quote at all, so it skips
  // the collapsed preview and goes straight to the input.
  composerMode: Boolean,
  // The attachment the composer will add on commit, previewed as a chip. Null in
  // the normal selection flow, which has no attachment of its own.
  composerAttachment: Object,
  // Counter bumped by the Android native selection menu's 评论 action. The
  // component watches it and opens the input (see the watcher below).
  expandRequest: Number,
})
const emit = defineEmits(['add', 'send', 'close', 'pin', 'unpin'])

const expanded = ref(false)
const inputText = ref('')
const inputRef = ref(null)
const barRef = ref(null)

// Floating position (issue #544). Measured after paint because the bar's size
// decides how far it can sit from the viewport edges. The fallbacks cover jsdom
// (offsetWidth is always 0) and the first frame before measurement — without
// them the computed style would be NaN and the bar would jump on mount.
const measuredWidth = ref(0)
const measuredHeight = ref(0)
const FALLBACK_BAR_WIDTH = 220
const FALLBACK_BAR_HEIGHT = 32

const anchored = computed(() => !!props.anchor)

// How much of the layout viewport the soft keyboard hides. Fixed offsets are
// measured from the LAYOUT viewport, so without this the bar would stay pinned
// to a bottom the user cannot see once the keyboard opens (mobile browsers
// shrink only the VISUAL viewport). Android's adjustResize shrinks the layout
// viewport too, so the inset stays 0 there and nothing changes — correct.
const { bottomInset, topInset, viewportVersion } = useVisualViewportInset()

const barStyle = computed(() => {
  if (!props.anchor) return {}
  // `viewportVersion` is read purely to establish the dependency. The position
  // math calls getZoomedViewport(), which reads window.innerHeight DIRECTLY and
  // is therefore not reactive — so without this, the Android adjustResize case
  // (insets stay 0 while the layout viewport shrinks) would never recompute and
  // a `bottom`-anchored bar would keep an offset from the old, taller viewport.
  void viewportVersion.value
  return computeSelectionToolbarStyle(props.anchor, {
    barWidth: measuredWidth.value || FALLBACK_BAR_WIDTH,
    barHeight: measuredHeight.value || FALLBACK_BAR_HEIGHT,
    bottomInset: bottomInset.value,
    topInset: topInset.value,
  })
})

function measureBar() {
  const el = barRef.value
  if (!el) return
  measuredWidth.value = el.offsetWidth
  measuredHeight.value = el.offsetHeight
}

// Re-measure whenever the bar appears or the anchor moves: the collapsed
// toolbar and the expanded input have different sizes, and the flip decision
// depends on the current height.
//
// The viewport insets are deliberately NOT watched here. `barStyle` is a
// computed that reads them directly, so a keyboard opening already recomputes
// the position (covered by the component tests).
watch(
  () => [props.visible, props.anchor, expanded.value],
  async () => {
    await nextTick()
    measureBar()
  },
  { immediate: true },
)

/**
 * Re-measure on the bar's OWN size changes.
 *
 * The textarea auto-grows as the user types (autoResizeTextarea), and the
 * keyboard clamp uses `measuredHeight` to decide whether the bar fits the
 * visible band. A stale, too-small height would let the real bar's bottom cross
 * the visible edge — the input row dipping behind the keyboard in exactly the
 * scenario this feature exists to prevent. A watcher cannot cover it: the growth
 * comes from a DOM style write, not from a reactive value.
 *
 * No feedback loop: the only emitted styles that could resize the element are
 * `maxWidth` (derived from the viewport, not from the measurement) and the
 * position offsets (which do not affect size). So a resize never re-triggers a
 * different measurement.
 */
let barResizeObserver = null

watch(barRef, (el) => {
  barResizeObserver?.disconnect()
  barResizeObserver = null
  if (!el) return
  barResizeObserver = new ResizeObserver(() => measureBar())
  barResizeObserver.observe(el)
})

onUnmounted(() => {
  barResizeObserver?.disconnect()
  barResizeObserver = null
})

// Quote text shown in the EXPANDED snippet. The collapsed toolbar no longer
// previews the selection (issue #544 made it an icon+label toolbar), so there
// is nothing to truncate for a single-line preview any more — the expanded
// block is the only consumer and it always shows the full text.
const displayQuoteText = computed(() => props.quoteData?.text || '')

const canSend = computed(() => canSendInput(inputText.value))

/**
 * The bar has exactly two render states, and this is the one that decides
 * between them:
 *
 *   collapsed → the floating selection toolbar (only meaningful WITH a quote)
 *   expanded  → quoted block (if any) + the input
 *
 * The collapsed toolbar is deliberately unreachable without a quote: there is
 * no selection to copy/comment/quote. The outer `v-if` already refuses to
 * render the bar at all unless there is a quote or composer mode; composer mode
 * additionally forces expanded. Naming the rule keeps a future `quoteData =
 * null` from falling through both branches and leaving an empty shell.
 */
const showCollapsed = computed(() => !expanded.value && !!props.quoteData)

// Both PC and mobile show the collapsed toolbar on selection: it has no input,
// so it never grabs focus and doesn't disturb the active selection. Choosing
// "comment" expands it and focuses the input. Reset back to collapsed on hide.
function onVisibleChange(val) {
  if (!val) {
    expanded.value = false
    inputText.value = ''
    // A destination picked for THIS comment must not silently redirect the
    // next one: the row is a per-composition choice, not a sticky setting.
    clearChosenTarget()
  }
}

watch(() => props.visible, onVisibleChange)

// Composer mode opens with no quote, so there is no snippet to preview and
// nothing to click to expand. Go straight to the input and focus it, so the
// user can type their message immediately. Runs on open and whenever a quote is
// (re)captured while the bar stays open, keeping the input visible throughout.
watch(
  () => [props.visible, props.composerMode],
  async ([vis, composer]) => {
    if (!vis || !composer) return
    emit('pin')
    expanded.value = true
    await nextTick()
    focusInput()
  },
  { immediate: true },
)

// Click outside to close
function onPointerDown(e) {
  if (!props.visible) return
  if (!barRef.value) return
  // Don't close if clicking inside the bar
  if (barRef.value.contains(e.target)) return
  // Don't close if clicking inside a BottomSheet (bs-overlay/bs-panel) or ModalDialog
  if (e.target.closest('.bs-overlay, .bs-panel, .modal-dialog')) return
  emit('close')
}

// Escape closes the bar; Enter (while the bar is COLLAPSED and focus is not in
// an editable field) expands it and focuses the quote input — same action as
// clicking the collapsed row, so keyboard users can start a quote reply without
// reaching for the mouse.
function onKeyDown(e) {
  if (!props.visible) return
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
    return
  }
  if (e.key === 'Enter') {
    // Already expanded → the textarea handles Enter itself (send).
    if (expanded.value) return
    // Don't hijack Enter while typing in a genuinely editable field / on
    // interactive elements (they confirm via their own handlers).
    //
    // `isContentEditable` alone is too broad. CodeMirror keeps
    // `contenteditable="true"` on its `.cm-content` even in read-only browse
    // mode — the viewer only sets `EditorState.readOnly`, not the `editable`
    // view facet — and mirrors that state via `aria-readonly="true"`. Treating
    // it as "the user is typing" made Enter a no-op for a code-selection quote
    // (the bar showed but never expanded), while the markdown preview — whose
    // selection lives on plain DOM — worked. A read-only region has no
    // meaningful Enter action, so let the bar expand there.
    const t = e.target
    const tag = t?.tagName
    // `closest` covers a target that is a descendant of the read-only host
    // (CodeMirror's caret can sit in a `.cm-line` inside `.cm-content`).
    const inEditable = t?.isContentEditable === true && !t?.closest?.('[aria-readonly="true"]')
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || inEditable) return
    if (t?.closest?.('button, a, [role="button"]')) return
    e.preventDefault()
    e.stopImmediatePropagation()
    expand()
  }
}

// Focus the input. Uses the ref when available, otherwise falls back to a DOM
// lookup (the ref is unreliable on elements inside <Transition>).
function focusInput() {
  const el = inputRef.value || document.querySelector('.qq-textarea')
  el?.focus()
}

onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown, true)
  document.addEventListener('keydown', onKeyDown, true)
})

onUnmounted(() => {
  document.removeEventListener('pointerdown', onPointerDown, true)
  document.removeEventListener('keydown', onKeyDown, true)
})

async function expand() {
  emit('pin')
  expanded.value = true
  await nextTick()
  focusInput()
}

// Pin on pointerdown so the global selection handler's pointerup re-evaluation
// (which runs before the row's click/expand and can clear the selection) cannot
// hide the bar before expand() runs.
function onRowPointerDown() {
  emit('pin')
}

// Copy: the clipboard write is owned by CopyButton (it has the text), so this
// only closes the bar. Same "commit and dismiss" contract as the quote action —
// both actions finish their job and get out of the way, rather than leaving a
// toolbar floating over text the user is done with.
function onCopyAction() {
  emit('close')
}

function autoResizeTextarea() {
  const el = inputRef.value
  if (!el) return
  el.style.height = 'auto'
  const computed = getComputedStyle(el)
  // Line-height resolves to px (--input-line-height is a px value); fall back to
  // that token's own 18px so the cap stays a whole number.
  const lineHeight = parseFloat(computed.lineHeight) || 18
  const paddingTop = parseFloat(computed.paddingTop) || 0
  const paddingBottom = parseFloat(computed.paddingBottom) || 0
  const maxContentHeight = lineHeight * 3
  const maxHeight = maxContentHeight + paddingTop + paddingBottom
  el.style.height = Math.min(el.scrollHeight, maxHeight) + 'px'
}

// Watch inputText changes to ensure textarea height stays in sync with content.
watch(inputText, () => nextTick(() => autoResizeTextarea()))

// The Android native selection menu asks for the comment input by bumping
// expandRequest (it cannot reach `expanded`, which is local to this component).
// A counter rather than a boolean so two consecutive requests both fire.
watch(() => props.expandRequest, (n, prev) => {
  if (n === undefined || n === (prev ?? 0)) return
  expand()
})

function handleSend(pickTarget = false) {
  if (!canSend.value) return
  emit('send', inputText.value, { pickTarget })
  expanded.value = false
  inputText.value = ''
}

function handleAdd(pickTarget = false) {
  emit('add', inputText.value, { pickTarget })
  expanded.value = false
  inputText.value = ''
}

/**
 * Enter in the input = "send to the active conversation".
 *
 * It MUST go through a wrapper, not `handleSend` directly: Vue passes the
 * KeyboardEvent as the first argument, which would land in `pickTarget` and
 * make every Enter press open the session picker — the exact friction this
 * feature removes.
 */
function onEnterKey() {
  handleSend(false)
}

// Long-press (touch) / press-and-hold (mouse) on the commit buttons = "send
// this to a DIFFERENT conversation". The plain tap is the primary path and
// commits straight to the active session (issue #560); cross-session delivery
// is the rare case, so it moved to a secondary gesture instead of a mandatory
// dialog.
//
// Three details keep one gesture from turning into two actions:
//  - the commit happens on RELEASE, not when the timer fires, so the pressed
//    button is still mounted while the gesture is in flight;
//  - the commit is then deferred by a task, so the click that follows the
//    release is dispatched (and swallowed) before the bar collapses;
//  - a short suppression window ignores the compatibility mouse events a hybrid
//    device synthesizes after a touch.
const HOLD_MS = 500
/** Window in which the tail events of a completed hold are ignored. */
const HOLD_TAIL_MS = 1000
let holdTimer = null
/** Set when the hold threshold was reached; cleared by the release. */
let holdArmed = false
/** One-shot: swallow the click that belongs to a completed hold. */
let suppressNextClick = false
/** Compatibility mouse events before this timestamp belong to a touch hold. */
let holdSuppressedUntil = 0
/** Which commit the current hold will fire: the add card or the send. */
let holdAction = 'add'

/** Pointer down: start counting. Nothing is emitted yet. */
function startHold(action) {
  // A compatibility mousedown right after a touch hold must not re-arm.
  if (Date.now() < holdSuppressedUntil) return
  holdAction = action
  holdArmed = false
  if (holdTimer) clearTimeout(holdTimer)
  holdTimer = setTimeout(() => {
    holdTimer = null
    holdArmed = true
  }, HOLD_MS)
}

/** Pointer up: a short tap does nothing here (the click handles it); a hold commits. */
function endHold(e) {
  if (holdTimer) {
    // Released before the threshold: an ordinary tap, whose click does the work.
    clearTimeout(holdTimer)
    holdTimer = null
    holdArmed = false
    return
  }
  if (!holdArmed) return
  // Consume the arm FIRST: a hybrid device can deliver touchend and mouseup for
  // the same gesture, and only the first may commit.
  holdArmed = false
  const action = holdAction
  suppressNextClick = true
  holdSuppressedUntil = Date.now() + HOLD_TAIL_MS
  // Cancel the synthetic click where we can (touch). The deferred commit below
  // is the second line of defence: it runs AFTER any click the browser still
  // dispatches, so the click hits a live button (and is swallowed) instead of
  // whatever the collapsed bar renders underneath.
  e?.preventDefault?.()
  setTimeout(() => {
    if (action === 'add') handleAdd(true)
    else handleSend(true)
    // A click that never arrived (the touch cancel worked) must not leave the
    // latch armed for the NEXT interaction. Both timers are queued in order, so
    // this runs after the commit above and after any click from the release.
    setTimeout(() => { suppressNextClick = false }, 0)
  }, 0)
}

// Pointer left the button mid-hold: cancel, so the next click is a normal
// commit rather than being swallowed as "the release of a hold".
function cancelHold() {
  if (holdTimer) clearTimeout(holdTimer)
  holdTimer = null
  holdArmed = false
}

function onAddClick() {
  if (suppressNextClick) {
    suppressNextClick = false
    return
  }
  handleAdd(false)
}

function onSendClick() {
  if (suppressNextClick) {
    suppressNextClick = false
    return
  }
  handleSend(false)
}

defineExpose({ expanded, showCollapsed, expand, displayQuoteText, onVisibleChange, inputRef, inputText })
</script>

<style scoped>
/* ── One geometry in this component ──
   Both states use the bar's hard-edged language: the container is
   --radius-xs and the controls inside it are on that same scale. The
   COLLAPSED toolbar (issue #544) used to be a pill on the theory that a
   compact floating surface wants a round shape; in practice the pill buttons
   read as pasted-on next to the bar's square frame, so they now share the
   expanded panel's geometry. The guard in quoteQuestionBar.test.ts pins the
   sharp scale for both. */
.quote-question-bar {
  /* The expanded panel's target width, in one place. Two rules need it — the
     root's `max-width` cap and the panel's own `width` — and they must agree:
     the panel relies on the root's clamp to shrink (`max-width: 100%`), so a
     drift between them would either clip the panel or leave it narrower than
     intended. 60% of the previous 600px, per review feedback that the popup
     read as too wide for an inline reply box. */
  --qq-expanded-width: 360px;
  position: fixed;
  top: calc(var(--header-height) + 8px + var(--header-safe-area-top));
  left: 8px;
  right: 8px;
  background: color-mix(in srgb, var(--bg-tertiary) 88%, var(--bg-elevated, var(--bg-tertiary)));
  border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
  border-radius: var(--radius-xs);
  box-shadow: var(--shadow-lg);
  z-index: var(--z-quote-bar);
  max-width: var(--qq-expanded-width);
  margin: 0 auto;
  overflow: hidden;
}

/* Anchored: the bar floats next to the selection, so it must shrink to its
   content and drop the full-width centring of the pinned top position. The
   inline left/top (or bottom) come from computeSelectionToolbarStyle; `right`
   and the auto margins must be cleared or they would fight the inline `left`.

   `top: auto` is load-bearing: when the bar sits ABOVE the selection the
   computed style anchors it with `bottom` only, and without this the base
   rule's `top` would still apply — leaving the element with both a top and a
   bottom and stretching it down the whole viewport. An inline `top` (the
   flipped-below case) overrides this as usual. */
.quote-question-bar--anchored {
  top: auto;
  right: auto;
  margin: 0;
  max-width: none;
  width: max-content;
}

/* The collapsed pill sizes to its content (`max-content` above). The expanded
   input panel must NOT: it holds a textarea whose intrinsic width is huge, so
   `max-content` would stretch it across the screen.

   Size it to the shared target width (--qq-expanded-width, declared on the root
   rule above) and let it shrink with the room actually available:

     width: var(--qq-expanded-width) → the target width on a roomy screen
     max-width: 100%    → resolves against the root's used width, which the
                          root's INLINE max-width (from
                          computeSelectionToolbarStyle, = viewport − 2·margin,
                          in pre-zoom px) has already clamped. So the panel
                          narrows with the viewport/zoom without any `vw` unit
                          (which would not track the app's `html { zoom }`).

   Without `max-width: 100%` a narrow screen would leave the root clamped while
   the child stayed at the full target width and got silently clipped by the
   root's `overflow: hidden`. */
.quote-question-bar--anchored .quote-bar-expanded {
  width: var(--qq-expanded-width);
  max-width: 100%;
}

/* ===== Collapsed: floating selection toolbar ===== */
.quote-bar-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2);
}

/* One action = icon + label. The label is not optional: copy / comment / quote
   are not readable from a glyph alone, and this toolbar is now the only entry
   point for all three. */
.quote-bar-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  height: 28px;
  padding: 0 var(--space-6);
  cursor: pointer;
  flex-shrink: 0;
  white-space: nowrap;
  font: inherit;
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  background: transparent;
  border: none;
  /* Square-ish, matching the bar's frame and the expanded panel's buttons —
     a pill here read as a different surface pasted onto the toolbar. */
  border-radius: var(--radius-xs);
  transition: color var(--duration-base), background var(--duration-base);
  /* These are controls, not selectable text — and the quote action is also a
     long-press target ("quote into another conversation").
     Without this the Android WebView recognises the hold as its own text
     selection gesture, takes over the touch stream and fires touchcancel,
     which kills our 500ms timer: the long-press would simply never work on
     Android. `touch-callout` is the iOS counterpart. Same pair the quick-send
     rows and the file-manager rows carry (both are long-press targets too). */
  user-select: none;
  -webkit-user-select: none;
  -webkit-touch-callout: none;
}

@media (hover: hover) {
  .quote-bar-action:hover {
    color: var(--text-primary);
    background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  }
}

/* No action is tinted at rest. The quote action used to carry the accent
   colour to mark it as primary, but on a toolbar where every item is a peer
   (copy / comment / quote) the lone coloured label read as a selected state
   rather than a hierarchy cue. Hover already communicates interactivity. */

/* ===== Expanded panel ===== */
.quote-bar-expanded {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
}

/* ── Destination row ("To: <session>") ──
   Sits at the TOP of the expanded panel and is separated by a rule, so it reads
   as the addressee of the whole block (like a mail composer's To: line) rather
   than as another item between the quote and the input.

   22px tall, one line, no background at rest: this is a label the user can
   change, not a button competing with 发送. The chevron is the only "this is
   clickable" cue, which is why it never shrinks — a long session name
   ellipsises instead of pushing it out. */
.qq-target {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  min-height: 22px;
  padding: 0 0 var(--space-2);
  margin: 0;
  border: none;
  border-bottom: 1px solid var(--border-color);
  border-radius: 0;
  background: transparent;
  font: inherit;
  text-align: left;
  cursor: pointer;
  color: var(--text-muted);
  transition: color var(--duration-base);
  /* A long-press here must not summon the WebView's selection menu (same
     reason as the commit buttons below). */
  user-select: none;
  -webkit-user-select: none;
  -webkit-touch-callout: none;
}

.qq-target:hover { color: var(--text-secondary); }
.qq-target:active { opacity: var(--opacity-hover); }

/* The agent icon is an <img>/<svg>/initial span from AgentIcon; at `sm` it is
   14px, which matches the row's 11px text block. */
.qq-target-icon {
  flex-shrink: 0;
}

/* No agent to show (no active session yet): a small dashed square with a plus,
   matching the icon's footprint so the row does not jump when a session loads. */
.qq-target-icon--empty {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border: 1px dashed color-mix(in srgb, var(--text-muted) 60%, transparent);
  border-radius: 3px;
  color: var(--text-muted);
}

.qq-target-label {
  flex-shrink: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.qq-target-name {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* No session yet: the name is an instruction ("选择会话…"), not a title. */
.qq-target-name--empty { color: var(--text-muted); }

.qq-target-chevron {
  flex-shrink: 0;
  opacity: 0.55;
  transition: opacity var(--duration-base);
}

.qq-target:hover .qq-target-chevron { opacity: 0.9; }

/* Quoted snippet block — relative so the floating copy button anchors here.
   Square (radius 0) with a 2px accent rule on the left: the hard edge is what
   reads as a quote in this sharp-corner language, so do not round it. */
.qq-quoted-snippet {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: var(--space-1);
  padding: var(--space-2) var(--space-3);
  background: color-mix(in srgb, var(--accent-color) 10%, var(--bg-tertiary));
  border-left: 2px solid var(--accent-color);
  border-radius: 0;
  margin: 0;
  flex: 1;
  min-width: 0;
}

/* Copy button of the EXPANDED snippet — floats at its top-right, overlaying the
   text. `position:absolute` keeps it out of the text flow (see
   .qq-quoted-snippet). Fixed 22px square: the copied state swaps the icon for a
   Check rather than a text label, so the footprint never changes. A label would
   grow it LEFTWARD (it is pinned to `right`), sliding it over the quote text.
   Square corners, like the rest of the expanded panel. (The collapsed toolbar's
   copy action is a separate pill — see .quote-bar-action.) */
.qq-copy-btn {
  position: absolute;
  top: 2px;
  right: 2px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: color var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .qq-copy-btn:hover {
    color: var(--text-primary);
    background: transparent;
  }
}

/* Copied feedback: the glyph swap comes from CopyButton; the tint is the
   shared `.copy-btn.is-copied` rule in css/copy-button.css. Nothing to add
   here — the button keeps this bar's 22px geometry in both states. */

/* The scrollable text element must constrain itself — flex min-width on the
   parent does NOT propagate to a scrollable child (min-width only takes effect
   on the overflow element itself). Without this, a long unbreakable line (e.g.
   a selected table rendered as text) stretches the text element to near-full
   width, so its vertical scrollbar lands mid-bar instead of at the right edge.
   The 24px is the copy button's real footprint (22px wide + its 2px inset) and
   is the ONLY reservation made for it — see the padding-right override on
   .qq-quoted-text--expanded below. Reserving it twice left a dead strip on the
   right that made the quote read as if it never filled the bar. */
.qq-quoted-text--expanded {
  flex: 1;
  min-width: 0;
  max-width: calc(100% - 24px);
}

/* Quote text: single line by default; expand on click to show full content.
   Right padding keeps the text clear of the floating copy button. */
.qq-quoted-text {
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  padding-right: 24px;
}

.qq-quoted-text--expanded {
  white-space: pre-wrap;
  overflow-y: auto;
  text-overflow: clip;
  word-break: break-word;
  max-height: 120px;
  /* The expanded state reserves the copy button via max-width instead (see the
     geometry rule above). Keeping the base 24px here as well would subtract the
     same space twice and leave the text ~40px short of the bar's right edge. */
  padding-right: 0;
}

/* ===== Pending attachment chip (composer mode) =====
   Read-only preview of what the bar will attach on commit. Mirrors the chat
   input's .attachment-ref tint, but on this bar's sharp-corner scale
   (--radius-xs, never the pill/round radii used elsewhere) and with no close
   button — the entry is not in the chat context yet, so dismissing the bar is
   the way to discard it. */
.qq-pending-attachment {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  align-self: flex-start;
  max-width: 100%;
  padding: 0 var(--space-3);
  height: 22px;
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color) 20%, transparent);
  border-radius: var(--radius-xs);
  color: var(--accent-color);
  font-size: var(--font-size-xs);
}

.qq-pending-icon {
  flex-shrink: 0;
}

.qq-pending-label {
  font-family: var(--font-mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

/* Input container — square, matching the bar's sharp geometry. Background stays
   --bg-primary (brighter than the bar's --bg-tertiary) so the editable area
   still reads as a distinct field without needing a radius. */
.qq-input-container {
  display: flex;
  flex-direction: column;
  background: var(--bg-primary, #fff);
  border: none;
  border-radius: 0;
  overflow: hidden;
  transition: background var(--duration-slow), box-shadow var(--duration-slow);
}

.qq-input-container:focus-within {
  background: var(--bg-primary);
  box-shadow: inset 0 0 0 1px var(--accent-color);
}

/* Vertical padding is symmetric on purpose: the row is `align-items:
   flex-end`, so any top/bottom difference reads directly as the buttons sitting
   off-centre against the textarea (it was 2px top / 4px bottom). 3px keeps the
   row's total height while making the two equal. */
.qq-input-row {
  display: flex;
  align-items: flex-end;
  gap: var(--space-1);
  padding: 3px var(--space-2);
}

.qq-textarea {
  flex: 1;
  padding: var(--space-2) var(--space-4);
  border: none;
  background: transparent;
  color: var(--text-primary);
  /* Mirrors .chat-textarea: same type scale as the chat message body, and the
     line box is the integer --input-line-height so a single line stays
     vertically centred in WebView (see the token's comment). */
  font-size: var(--font-size-md);
  line-height: var(--input-line-height);
  outline: none;
  resize: none;
  overflow-y: auto;
  min-height: calc(var(--input-line-height) + var(--space-2) * 2);
  max-height: calc(var(--input-line-height) * 3 + var(--space-2) * 2); /* 3 lines + padding-top + padding-bottom */
  font-family: inherit;
}

.qq-textarea::placeholder {
  color: var(--text-muted);
}

/* Add + send: square 26px, matching the icon buttons elsewhere in the app.
   Square (not round) because these live in the hard-edged EXPANDED panel — the
   rounded pill language belongs to the collapsed toolbar only. */
.qq-add-btn,
.qq-send-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  background: var(--accent-color);
  color: #fff;
  border: none;
  border-radius: var(--radius-xs);
  cursor: pointer;
  transition: background var(--duration-base), opacity var(--duration-base);
  flex-shrink: 0;
  /* Long-press target (see .quote-bar-action): the WebView must not claim the
     hold for its native selection menu. */
  user-select: none;
  -webkit-user-select: none;
  -webkit-touch-callout: none;
}

.qq-add-btn {
  background: transparent;
  color: var(--accent-color);
  border: 1px solid color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
}

@media (hover: hover) {
  .qq-add-btn:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  }

  .qq-send-btn:hover {
    background: var(--accent-hover);
  }
}

.qq-send-btn:active {
  opacity: var(--opacity-hover);
}

.qq-send-btn.disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

/* ===== Transitions =====
   A fade with a small scale-in, not the old `translateY(-100%)` slide: that
   read as "dropping from the header", which was true of the pinned top banner
   but is wrong now that the bar pops up next to the selection (it would slide
   down from above its own anchor). The scale stays subtle so the anchored bar
   does not appear to jump into place. */
.quote-bar-enter-active {
  transition: opacity 0.3s cubic-bezier(0.4, 0, 0.2, 1), transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.quote-bar-leave-active {
  transition: opacity var(--duration-slow) ease-in, transform var(--duration-slow) ease-in;
}

.quote-bar-enter-from,
.quote-bar-leave-to {
  opacity: 0;
  transform: scale(0.96);
}
</style>

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
          @click.stop="handleAdd"
          :title="t('quoteBar.quoteSelection')"
          :aria-label="t('quoteBar.quoteSelection')"
        >
          <Quote :size="14" />
          <span>{{ t('quoteBar.quoteSelection') }}</span>
        </button>
      </div>

      <!-- Expanded: quoted snippet (full, when there is one) + input. -->
      <div v-else class="quote-bar-expanded">
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
              @keydown.enter.exact.prevent="handleSend"
              @input="autoResizeTextarea"
            />
            <button class="qq-add-btn" @click="handleAdd" :title="t('quoteBar.addToChat')" :aria-label="t('quoteBar.addToChat')">
              <Plus :size="13" />
            </button>
            <button class="qq-send-btn" :class="{ disabled: !canSend }" @click="handleSend" :title="t('quoteBar.send')">
              <Send :size="13" />
            </button>
          </div>
        </div>
      </div>

    </div>
  </Transition>
</template>

<script setup>
import { Plus, Send, Link, MessageSquare, Quote } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { canSendInput } from '@/utils/quoteQuestionUtils'
import { computeSelectionToolbarStyle } from '@/utils/selectionToolbarPosition'
import FileIcon from '@/components/common/FileIcon.vue'
import CopyButton from '@/components/common/CopyButton.vue'

const { t } = useI18n()

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

const barStyle = computed(() => {
  if (!props.anchor) return {}
  return computeSelectionToolbarStyle(props.anchor, {
    barWidth: measuredWidth.value || FALLBACK_BAR_WIDTH,
    barHeight: measuredHeight.value || FALLBACK_BAR_HEIGHT,
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
watch(
  () => [props.visible, props.anchor, expanded.value],
  async () => {
    await nextTick()
    measureBar()
  },
  { immediate: true },
)

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

function handleSend() {
  if (!canSend.value) return
  emit('send', inputText.value)
  expanded.value = false
  inputText.value = ''
}

function handleAdd() {
  emit('add', inputText.value)
  expanded.value = false
  inputText.value = ''
}

defineExpose({ expanded, showCollapsed, expand, displayQuoteText, onVisibleChange, inputRef, inputText })
</script>

<style scoped>
/* ── Two geometries in one component ──
   The EXPANDED panel keeps the bar's original hard-edged language
   (--radius-xs container, 0 on the blocks inside it): it is a wide input
   surface, and a rounded card there reads as pasted-on.

   The COLLAPSED toolbar (issue #544) is deliberately the opposite: a compact
   pill that floats next to the selection. That is a considered exception to the
   sharp-corner rule, not drift — the guard in quoteQuestionBar.test.ts was
   narrowed to the expanded panel so this pill is allowed. Keep every new
   collapsed control on the pill scale; do not round the expanded panel. */
.quote-question-bar {
  position: fixed;
  top: calc(var(--header-height) + 8px + var(--header-safe-area-top));
  left: 8px;
  right: 8px;
  background: color-mix(in srgb, var(--bg-tertiary) 88%, var(--bg-elevated, var(--bg-tertiary)));
  border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
  border-radius: var(--radius-xs);
  box-shadow: var(--shadow-lg);
  z-index: var(--z-quote-bar);
  max-width: 600px;
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
   `max-content` would stretch it across the screen. Give the anchored expanded
   panel a fixed, comfortable width instead. */
.quote-question-bar--anchored .quote-bar-expanded {
  width: 320px;
  max-width: calc(100vw - 16px);
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
  border-radius: var(--radius-full);
  transition: color var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .quote-bar-action:hover {
    color: var(--text-primary);
    background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  }
}

/* The quote action is the primary one (it is the action the issue asks for),
   so it carries the accent tint at rest. */
.quote-bar-action:last-child {
  color: var(--accent-color);
}

@media (hover: hover) {
  .quote-bar-action:last-child:hover {
    background: color-mix(in srgb, var(--accent-color) 14%, transparent);
  }
}

/* ===== Expanded panel ===== */
.quote-bar-expanded {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
}

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

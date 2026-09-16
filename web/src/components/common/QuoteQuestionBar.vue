<template>
  <Transition name="quote-bar">
    <div v-if="visible && (quoteData || composerMode)" ref="barRef" class="quote-question-bar">

      <!-- Collapsed row: quoted snippet (single-line) + add action.
           Clicking the quote area expands the input box and the quote together.
           Copy button floats absolutely at the snippet's top-right.
           Gated on showCollapsed, which requires a quote: the row exists to
           preview one, so with no quote the bar is always expanded. -->
      <div v-if="showCollapsed" class="quote-bar-row" @click="expand()" @pointerdown="onRowPointerDown">
        <div class="qq-quoted-snippet qq-quoted-snippet--inline">
          <span class="qq-quoted-text">{{ displayQuoteText }}</span>
          <button class="qq-copy-btn" :class="{ 'is-copied': copied }" @click.stop="handleCopyQuote" :title="copied ? t('common.copied') : t('common.copy')" :aria-label="copied ? t('common.copied') : t('common.copy')">
            <span v-if="copied" class="qq-copied-text">{{ t('common.copied') }}</span>
            <Copy v-else :size="14" />
          </button>
        </div>
        <button class="quote-bar-add" @click.stop="handleAdd" :title="t('quoteBar.addToChat')" :aria-label="t('quoteBar.addToChat')">
          <Plus :size="14" />
        </button>
      </div>

      <!-- Expanded: quoted snippet (full, when there is one) + input. -->
      <div v-else class="quote-bar-expanded">
        <!-- Quoted snippet — fully shown when expanded. Absent when the user has
             not selected anything yet (composer mode): the bar then asks for a
             message directly. -->
        <div v-if="quoteData" class="qq-quoted-snippet">
          <span class="qq-quoted-text qq-quoted-text--expanded">{{ displayQuoteText }}</span>
          <button class="qq-copy-btn" :class="{ 'is-copied': copied }" @click.stop="handleCopyQuote" :title="copied ? t('common.copied') : t('common.copy')" :aria-label="copied ? t('common.copied') : t('common.copy')">
            <span v-if="copied" class="qq-copied-text">{{ t('common.copied') }}</span>
            <Copy v-else :size="14" />
          </button>
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
import { Plus, Send, Copy } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { truncateQuoteText, canSendInput } from '@/utils/quoteQuestionUtils'
import { copyText } from '@/utils/clipboard.ts'

const { t } = useI18n()

const props = defineProps({
  visible: Boolean,
  quoteData: Object,
  // Opened from an entry point (e.g. the issue/PR detail header) rather than by
  // a text selection. The bar is then useful with no quote at all, so it skips
  // the collapsed preview and goes straight to the input.
  composerMode: Boolean,
})
const emit = defineEmits(['add', 'send', 'close', 'pin', 'unpin'])

const expanded = ref(false)
const inputText = ref('')
const inputRef = ref(null)
const barRef = ref(null)
const copied = ref(false)
let copyTimer = null

// Quote text: single-line preview while collapsed, full text once expanded.
const displayQuoteText = computed(() => {
  if (!props.quoteData) return ''
  const text = props.quoteData.text || ''
  return expanded.value ? text : truncateQuoteText(text, 80)
})

const canSend = computed(() => canSendInput(inputText.value))

/**
 * The bar has exactly two render states, and this is the one that decides
 * between them:
 *
 *   collapsed → the quote preview row (only meaningful WITH a quote)
 *   expanded  → quoted block (if any) + the input
 *
 * The collapsed row is deliberately unreachable without a quote. There is
 * nothing to preview, and the outer `v-if` already refuses to render the bar at
 * all unless there is a quote or composer mode; composer mode additionally
 * forces expanded. Naming the rule keeps a future `quoteData = null` from
 * falling through both branches and leaving an empty, bordered shell.
 */
const showCollapsed = computed(() => !expanded.value && !!props.quoteData)

// Both PC and mobile show the collapsed bar on selection: it has no input, so it
// never grabs focus and doesn't disturb the active selection. Clicking the row
// expands it and focuses the input. Reset back to collapsed on hide.
function onVisibleChange(val) {
  if (!val) {
    expanded.value = false
    inputText.value = ''
    copied.value = false
    clearTimeout(copyTimer)
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
    // Don't hijack Enter while typing in an editable field / on interactive
    // elements (they confirm via their own handlers).
    const t = e.target
    const tag = t?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t?.isContentEditable) return
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
  clearTimeout(copyTimer)
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

// Copy the quoted text to the clipboard. Shows a brief Check feedback on the
// button. @click.stop keeps the collapsed row's expand() from firing.
function handleCopyQuote() {
  const text = props.quoteData?.text || ''
  if (!text) return
  copyText(text, () => {
    copied.value = true
    clearTimeout(copyTimer)
    copyTimer = setTimeout(() => { copied.value = false }, 1500)
  })
}

defineExpose({ expanded, showCollapsed, expand, displayQuoteText, onVisibleChange, inputRef, inputText, copied, handleCopyQuote })
</script>

<style scoped>
/* ── Geometry: one sharp-corner language ──
   This floating bar is deliberately hard-edged: --radius-xs (3px) on the
   container and 0 on the blocks inside it. The point is internal consistency —
   before this it mixed six radii (14px container, 6px button, 0 on the
   snippet, a 20px pill input, 50% circles), so no two surfaces agreed and the
   rounded input looked pasted onto a rounded card. Keep any new element in this
   file on the same scale rather than reaching for --radius-md/lg/full. */
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

/* ===== Collapsed row ===== */
.quote-bar-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  cursor: pointer;
  transition: background var(--duration-base);
}

.quote-bar-row:active {
  background: var(--bg-tertiary);
}

/* Collapsed-state add action. Square 26px to match .qq-add-btn/.qq-send-btn in
   the expanded state and the icon buttons elsewhere in the app — the same
   action must not change size or shape between states. */
.quote-bar-add {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  cursor: pointer;
  transition: opacity var(--duration-base), background var(--duration-base);
  flex-shrink: 0;
  background: transparent;
  color: var(--accent-color);
  border: 1px solid color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
  border-radius: var(--radius-xs);
}

@media (hover: hover) {
  .quote-bar-add:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  }
}

/* Copy button — floats at the snippet's top-right, overlaying the text.
   position:absolute keeps it out of the text flow (see .qq-quoted-snippet).
   Width is auto so the "已复制" feedback text fits; min-width keeps the
   icon-only idle state square. Square corners like every other control here. */
.qq-copy-btn {
  position: absolute;
  top: 2px;
  right: 2px;
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 22px;
  height: 22px;
  padding: 0 var(--space-2);
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

/* Copied feedback state — shows "已复制" text (same pattern as ChatMessageItem) */
.qq-copy-btn.is-copied {
  color: var(--accent-color);
  background: transparent;
}

.qq-copied-text {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  white-space: nowrap;
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

/* The scrollable text element must constrain itself — flex min-width on the
   parent does NOT propagate to a scrollable child (min-width only takes effect
   on the overflow element itself). Without this, a long unbreakable line (e.g.
   a selected table rendered as text) stretches the text element to near-full
   width, so its vertical scrollbar lands mid-bar instead of at the right edge.
   max-width leaves room for the floating copy button; the 8px right padding
   keeps the scrollbar clear of it. */
.qq-quoted-text--expanded {
  flex: 1;
  min-width: 0;
  max-width: calc(100% - 40px);
}

/* Collapsed inline variant — single row, no flex-start. Matches the expanded
   snippet's padding so the quote block does not resize when the bar expands. */
.qq-quoted-snippet--inline {
  align-items: center;
  padding: var(--space-2) var(--space-3);
  margin: 0;
  border-radius: 0;
  background: color-mix(in srgb, var(--accent-color) 10%, var(--bg-tertiary));
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

.qq-input-row {
  display: flex;
  align-items: flex-end;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-2) var(--space-2);
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

/* Add + send: square 26px, matching .quote-bar-add in the collapsed state and
   the icon buttons elsewhere in the app. A circle here would be the only
   rounded control in an otherwise hard-edged bar. */
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

/* ===== Transitions (对齐 CompletionPopover 滑下+淡入动效) ===== */
.quote-bar-enter-active {
  transition: opacity 0.3s cubic-bezier(0.4, 0, 0.2, 1), transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.quote-bar-leave-active {
  transition: opacity var(--duration-slow) ease-in, transform var(--duration-slow) ease-in;
}

.quote-bar-enter-from,
.quote-bar-leave-to {
  opacity: 0;
  transform: translateY(-100%);
}
</style>

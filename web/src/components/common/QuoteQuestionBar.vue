<template>
  <Transition name="quote-bar">
    <div v-if="visible && quoteData" ref="barRef" class="quote-question-bar">

      <!-- Collapsed row (mobile): quoted snippet (single-line) + add action.
           Clicking the quote area expands the input box and the quote together. -->
      <div v-if="!expanded" class="quote-bar-row" @click="expand()" @pointerdown="onRowPointerDown">
        <div class="qq-quoted-snippet qq-quoted-snippet--inline">
          <span class="qq-quoted-text">{{ displayQuoteText }}</span>
        </div>
        <button class="quote-bar-add" @click.stop="handleAdd" :title="t('quoteBar.addToChat')" :aria-label="t('quoteBar.addToChat')">
          <Plus :size="14" />
        </button>
      </div>

      <!-- Expanded: quoted snippet (full) + input -->
      <div v-else class="quote-bar-expanded">
        <!-- Quoted snippet — fully shown when expanded -->
        <div class="qq-quoted-snippet">
          <span class="qq-quoted-text qq-quoted-text--expanded">{{ displayQuoteText }}</span>
        </div>

        <!-- Input -->
        <div class="qq-input-container">
          <div class="qq-input-row">
            <button v-if="inputText" class="qq-clear-btn" @click="inputText = ''" :title="t('quoteBar.clear')">
              <XCircle :size="16" />
            </button>
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
              <Plus :size="14" />
            </button>
            <button class="qq-send-btn" :class="{ disabled: !canSend }" @click="handleSend" :title="t('quoteBar.send')">
              <Send :size="14" />
            </button>
          </div>
        </div>
      </div>

    </div>
  </Transition>
</template>

<script setup>
import { XCircle, Plus, Send } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { truncateQuoteText, canSendInput } from '@/utils/quoteQuestionUtils'

const { t } = useI18n()

const props = defineProps({
  visible: Boolean,
  quoteData: Object,
})
const emit = defineEmits(['add', 'send', 'close', 'pin', 'unpin'])

const expanded = ref(false)
const inputText = ref('')
const inputRef = ref(null)
const barRef = ref(null)

// Quote text: single-line preview while collapsed, full text once expanded.
const displayQuoteText = computed(() => {
  if (!props.quoteData) return ''
  const text = props.quoteData.text || ''
  return expanded.value ? text : truncateQuoteText(text, 80)
})

const canSend = computed(() => canSendInput(inputText.value))

// Both PC and mobile show the collapsed bar on selection: it has no input, so it
// never grabs focus and doesn't disturb the active selection. Clicking the row
// expands it and focuses the input. Reset back to collapsed on hide.
function onVisibleChange(val) {
  if (!val) {
    expanded.value = false
    inputText.value = ''
  }
}

watch(() => props.visible, onVisibleChange)

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

// Escape closes the bar.
function onKeyDown(e) {
  if (!props.visible) return
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
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
  const lineHeight = parseFloat(computed.lineHeight) || 20
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

defineExpose({ expanded, expand, displayQuoteText, onVisibleChange, inputRef, inputText })
</script>

<style scoped>
.quote-question-bar {
  position: fixed;
  top: calc(var(--header-height, 40px) + 8px + var(--header-safe-area-top, 0px));
  left: 8px;
  right: 8px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 0;
  box-shadow: var(--shadow-md);
  z-index: 2400;
  max-width: 600px;
  margin: 0 auto;
  overflow: hidden;
}

/* ===== Collapsed row ===== */
.quote-bar-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 8px 10px;
  cursor: pointer;
  transition: background 0.15s;
}

.quote-bar-row:active {
  background: var(--bg-tertiary);
}

.quote-bar-add {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  border: none;
  cursor: pointer;
  transition: opacity 0.15s, background 0.15s;
  flex-shrink: 0;
  background: transparent;
  color: var(--accent-color);
  border: 1px solid color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
}

@media (hover: hover) {
  .quote-bar-add:hover {
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
  }
}

/* ===== Expanded panel ===== */
.quote-bar-expanded {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
}

/* Quoted snippet block */
.qq-quoted-snippet {
  display: flex;
  align-items: flex-start;
  gap: 5px;
  padding: 6px 8px;
  background: var(--bg-tertiary);
  border-left: 2px solid var(--accent-color);
  border-radius: 0;
  margin: 0 2px;
  flex: 1;
  min-width: 0;
}

/* Collapsed inline variant — single row, no flex-start */
.qq-quoted-snippet--inline {
  align-items: center;
  padding: 5px 8px;
  margin: 0;
  border-radius: 0;
}

/* Quote text: single line by default; expand on click to show full content */
.qq-quoted-text {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.qq-quoted-text--expanded {
  white-space: pre-wrap;
  overflow-y: auto;
  text-overflow: clip;
  word-break: break-word;
  max-height: 120px;
}

/* Input container — capsule style */
.qq-input-container {
  display: flex;
  flex-direction: column;
  background: var(--bg-tertiary);
  border: none;
  border-radius: 0;
  overflow: hidden;
  transition: background 0.2s, box-shadow 0.2s;
}

.qq-input-container:focus-within {
  background: var(--bg-primary);
  box-shadow: 0 0 0 1px var(--accent-color);
}

.qq-input-row {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  padding: 4px 6px 6px;
}

.qq-textarea {
  flex: 1;
  padding: 4px 8px;
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: 14px;
  line-height: 20px;
  outline: none;
  resize: none;
  overflow-y: auto;
  min-height: 28px;
  max-height: calc(20px * 3 + 4px + 4px); /* 3 lines + padding-top + padding-bottom */
  font-family: inherit;
}

.qq-textarea::placeholder {
  color: var(--text-muted);
}

.qq-clear-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted);
  padding: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 0;
  transition: color 0.15s, background 0.15s;
  flex-shrink: 0;
  align-self: flex-end;
}

@media (hover: hover) {
  .qq-clear-btn:hover {
    color: var(--danger-color);
    background: color-mix(in srgb, var(--danger-color) 8%, transparent);
  }
}

.qq-add-btn,
.qq-send-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  background: var(--accent-color);
  color: #fff;
  border: none;
  border-radius: 0;
  cursor: pointer;
  transition: background 0.15s, opacity 0.15s;
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
  opacity: 0.8;
}

.qq-send-btn.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* ===== Transitions ===== */
.quote-bar-enter-active {
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}

.quote-bar-leave-active {
  transition: all 0.15s ease-in;
}

.quote-bar-enter-from,
.quote-bar-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>

<template>
  <Transition name="quote-bar">
    <div v-if="visible && quoteData" ref="barRef" class="quote-question-bar">

      <!-- Collapsed: quoted snippet (single-line) + 对话 button -->
      <div v-if="!expanded" class="quote-bar-row" @click="expand()">
        <div class="qq-quoted-snippet qq-quoted-snippet--inline">
          <MessageSquare :size="12" class="qq-quoted-icon" />
          <span class="qq-quoted-text qq-quoted-text--single">{{ fullQuoteText }}</span>
        </div>
        <button class="quote-bar-btn" @click.stop="expand">
          {{ t('quoteBar.chat') }}
        </button>
      </div>

      <!-- Expanded: quoted snippet + input -->
      <div v-else class="quote-bar-expanded">
        <!-- Quoted snippet -->
        <div class="qq-quoted-snippet">
          <MessageSquare :size="12" class="qq-quoted-icon" />
          <span class="qq-quoted-text">{{ fullQuoteText }}</span>
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
import { MessageSquare, XCircle, Send } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { truncateQuoteText, canSendInput } from '@/utils/quoteQuestionUtils'

const { t } = useI18n()

const props = defineProps({
  visible: Boolean,
  quoteData: Object,
})
const emit = defineEmits(['send', 'close', 'pin', 'unpin'])

const expanded = ref(false)
const inputText = ref('')
const inputRef = ref(null)
const barRef = ref(null)

const fullQuoteText = computed(() => {
  if (!props.quoteData) return ''
  return truncateQuoteText(props.quoteData.text || '')
})

const canSend = computed(() => canSendInput(inputText.value))

// Reset when bar hides
watch(() => props.visible, (val) => {
  if (!val) {
    expanded.value = false
    inputText.value = ''
  }
})

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

onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown, true)
})

onUnmounted(() => {
  document.removeEventListener('pointerdown', onPointerDown, true)
})

async function expand() {
  emit('pin')
  expanded.value = true
  await nextTick()
  inputRef.value?.focus()
}

function collapse() {
  expanded.value = false
  inputText.value = ''
  emit('unpin')
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

// Watch inputText changes (both user input and programmatic changes)
// to ensure textarea height stays in sync with content
watch(inputText, () => nextTick(() => autoResizeTextarea()))

function handleSend() {
  if (!canSend.value) return
  emit('send', inputText.value)
  expanded.value = false
  inputText.value = ''
}

defineExpose({ expanded, expand, inputText })
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
  max-width: 400px;
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

.quote-bar-btn {
  flex-shrink: 0;
  padding: 6px 14px;
  border: none;
  border-radius: 0;
  background: var(--accent-color);
  color: #fff;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}

.quote-bar-btn:active {
  opacity: 0.8;
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

/* Collapsed inline variant — single row, no flex-start, all corners rounded */
.qq-quoted-snippet--inline {
  align-items: center;
  padding: 5px 8px;
  margin: 0;
  border-radius: 0;
}

.qq-quoted-icon {
  flex-shrink: 0;
  color: var(--accent-color);
  opacity: 0.6;
  margin-top: 1px;
}

.qq-quoted-snippet--inline .qq-quoted-icon {
  margin-top: 0;
}

.qq-quoted-text {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-secondary);
  word-break: break-all;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* Collapsed single-line variant */
.qq-quoted-text--single {
  -webkit-line-clamp: 1;
  white-space: nowrap;
  word-break: normal;
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

.qq-clear-btn:hover {
  color: var(--danger-color);
  background: color-mix(in srgb, var(--danger-color) 8%, transparent);
}

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

.qq-send-btn:hover {
  background: var(--accent-hover);
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

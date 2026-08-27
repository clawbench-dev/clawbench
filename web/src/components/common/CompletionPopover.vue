<template>
  <Teleport to="body">
    <Transition name="completion-popover">
      <div
        v-if="active"
        class="completion-popover-backdrop"
        @click.self="dismiss"
      >
        <div class="completion-popover">
          <div class="completion-popover-header">
            <span class="completion-popover-icon"><Bot :size="14" /></span>
            <span class="completion-popover-title" :title="active.title">{{ active.title || '未命名会话' }}</span>
            <span class="completion-popover-open" role="button" :aria-label="openLabel" :title="openLabel" @click="openSession">
              <CornerDownLeft :size="16" />
            </span>
          </div>
          <div v-if="active.projectName" class="completion-popover-project" :title="active.projectPath || active.projectName">
            <Folder :size="12" /> {{ active.projectName }}{{ active.projectPath ? ' · ' + active.projectPath : '' }}
          </div>
          <div v-if="active.userMessage" class="completion-popover-user-msg" :title="active.userMessage">
            <MessageSquare :size="12" /> {{ active.userMessage }}
          </div>
          <div class="completion-popover-summary markdown-body" v-html="summaryHtml" @click="handleSummaryClick"></div>
          <div class="completion-popover-input">
            <textarea
              ref="inputRef"
              v-model="inputText"
              class="completion-popover-textarea"
              rows="1"
              :placeholder="inputPlaceholder"
              @keydown.enter.exact.prevent="handleSend"
              @input="autoResizeTextarea"
            />
            <button class="completion-popover-send" :class="{ disabled: !canSend }" @click="handleSend" :title="gt('chat.popover.send')" :aria-label="gt('chat.popover.send')">
              <Send :size="14" />
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { CornerDownLeft, Bot, Folder, MessageSquare, Send } from 'lucide-vue-next'
import { useCompletionPopover } from '@/composables/useCompletionPopover'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader'
import { gt } from '@/composables/useLocale'
import { canSendInput } from '@/utils/quoteQuestionUtils'

const { active, dismiss } = useCompletionPopover()

const openLabel = computed(() => active.value?.kind === 'task'
    ? gt('chat.popover.openTask')
    : gt('chat.popover.openSession'))

const inputPlaceholder = computed(() => active.value?.kind === 'task'
    ? gt('chat.popover.replyTask')
    : gt('chat.popover.replySession'))

// 基础 Markdown 渲染（轻量路径：跳过路径/commit 注解与 KaTeX，与流式文本同款）
const summaryHtml = computed(() => {
    const summary = active.value?.summary || ''
    if (!summary) return ''
    return renderMarkdownHtml(summary, { skipEnhancements: true, skipKatex: true })
})

// ── 快捷输入框 ──
const inputText = ref('')
const inputRef = ref<HTMLTextAreaElement | null>(null)
const sending = ref(false)

const canSend = computed(() => canSendInput(inputText.value) && !sending.value)

// 弹窗切换时重置输入框
watch(active, () => {
    inputText.value = ''
    sending.value = false
})

function autoResizeTextarea(): void {
    const el = inputRef.value
    if (!el) return
    el.style.height = 'auto'
    const computedStyle = getComputedStyle(el)
    const lineHeight = parseFloat(computedStyle.lineHeight) || 20
    const paddingTop = parseFloat(computedStyle.paddingTop) || 0
    const paddingBottom = parseFloat(computedStyle.paddingBottom) || 0
    const maxContentHeight = lineHeight * 3
    el.style.height = Math.min(el.scrollHeight, maxContentHeight + paddingTop + paddingBottom) + 'px'
}

// 发送到弹窗对应的会话，发送后关闭弹窗
async function handleSend(): Promise<void> {
    const item = active.value
    const text = inputText.value.trim()
    if (!item || !text || sending.value) return
    sending.value = true
    try {
        const url = `/api/ai/chat?session_id=${encodeURIComponent(item.sessionId)}`
        await fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message: text }),
        })
        dismiss()
    } catch {
        sending.value = false
    }
}

// 仅通过"打开会话"按钮进入导航（点击卡片本体不导航）
function openSession(): void {
    const item = active.value
    if (!item) return
    if (item.kind === 'task') {
        window.dispatchEvent(new CustomEvent('clawbench-open-task', {
            detail: { taskId: item.taskId, executionId: item.executionId, projectPath: item.projectPath },
        }))
    } else {
        window.dispatchEvent(new CustomEvent('clawbench-open-session', {
            detail: { sessionId: item.sessionId, projectPath: item.projectPath },
        }))
    }
    dismiss()
}

// 摘要内代码块复制/换行、表格操作等按钮点击不应触发任何导航/关闭
function handleSummaryClick(event: MouseEvent): void {
    if (handleCodeBlockClick(event) || handleTableBlockClick(event)) return
}
</script>

<style>
.completion-popover-backdrop {
    position: fixed;
    inset: 0;
    z-index: 9998;
    display: flex;
    justify-content: center;
    align-items: flex-start;
    padding-top: calc(8px + var(--header-safe-area-top, 0px));
    background: transparent;
}

.completion-popover {
    max-width: min(480px, 92vw);
    width: 100%;
    background: color-mix(in srgb, var(--bg-tertiary) 88%, var(--bg-elevated, var(--bg-tertiary)));
    color: var(--text-primary);
    border-radius: 0;
    padding: 8px 10px;
    box-shadow: var(--shadow-lg, 0 8px 24px rgba(0, 0, 0, 0.35));
    border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
    -webkit-tap-highlight-color: transparent;
    user-select: none;
    overflow: hidden;
}

/* PC 模式加宽通知栏，避免过窄难看 */
@media (min-width: 768px) {
    .completion-popover {
        max-width: min(680px, 92vw);
    }
}

.completion-popover-header {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 4px;
}

.completion-popover-icon {
    font-size: 14px;
    flex-shrink: 0;
}

.completion-popover-title {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.completion-popover-open {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 0;
    background: var(--accent-color);
    color: #fff;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
    transition: opacity 0.15s ease, transform 0.15s ease;
}

.completion-popover-open:hover {
    opacity: 0.85;
    transform: scale(1.05);
}

.completion-popover-project,
.completion-popover-user-msg {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    line-height: 1.4;
    color: var(--text-tertiary, var(--text-secondary, var(--text-primary)));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    margin-bottom: 4px;
    opacity: 0.9;
}

.completion-popover-summary {
    max-height: 40vh;
    overflow-y: auto;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-secondary, var(--text-primary));
    word-break: break-word;
}

/* 覆盖全局 .markdown-body 规则：卡片已有自身 padding，去掉重复 padding；
   消除最后一个子元素（段落/列表）的底部 margin，避免文字结束后残留空隙 */
.completion-popover-summary.markdown-body {
    padding: 0;
    flex: none;
}

.completion-popover-summary.markdown-body > :last-child,
.completion-popover-summary.markdown-body > :last-child > :last-child {
    margin-bottom: 0;
}

/* 快捷输入框 */
.completion-popover-input {
    display: flex;
    align-items: flex-end;
    gap: 4px;
    margin-top: 6px;
    padding: 4px 6px 4px 8px;
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: 0;
}

.completion-popover-input:focus-within {
    border-color: var(--accent-color);
}

.completion-popover-textarea {
    flex: 1;
    min-width: 0;
    padding: 3px 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: 13px;
    line-height: 18px;
    outline: none;
    resize: none;
    overflow-y: auto;
    max-height: calc(18px * 3);
    font-family: inherit;
}

.completion-popover-textarea::placeholder {
    color: var(--text-muted);
}

.completion-popover-send {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    padding: 0;
    background: var(--accent-color);
    color: #fff;
    border: none;
    border-radius: 0;
    cursor: pointer;
    transition: opacity 0.15s;
}

.completion-popover-send.disabled {
    opacity: 0.4;
    cursor: not-allowed;
}

/* Android 通知风格：从顶部滑下 + 淡入（标准缓动曲线），离开反向滑回 */
.completion-popover-enter-active {
    transition: opacity 0.3s cubic-bezier(0.4, 0, 0.2, 1), transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.completion-popover-leave-active {
    transition: opacity 0.2s ease-in, transform 0.2s ease-in;
}

.completion-popover-enter-from,
.completion-popover-leave-to {
    opacity: 0;
    transform: translateY(-120%);
}
</style>

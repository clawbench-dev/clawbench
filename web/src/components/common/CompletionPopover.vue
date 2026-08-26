<template>
  <Teleport to="body">
    <div
      v-if="active"
      class="completion-popover-backdrop"
      @click.self="dismiss"
    >
      <Transition name="completion-popover">
        <div class="completion-popover">
          <div class="completion-popover-header">
            <span class="completion-popover-icon">🤖</span>
            <span class="completion-popover-title" :title="active.title">{{ active.title || '未命名会话' }}</span>
            <span class="completion-popover-open" role="button" :aria-label="openLabel" :title="openLabel" @click="openSession">
              <CornerDownLeft :size="14" />
            </span>
          </div>
          <div class="completion-popover-summary markdown-body" v-html="summaryHtml" @click="handleSummaryClick"></div>
        </div>
      </Transition>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { CornerDownLeft } from 'lucide-vue-next'
import { useCompletionPopover } from '@/composables/useCompletionPopover'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader'
import { gt } from '@/composables/useLocale'

const { active, dismiss } = useCompletionPopover()

const openLabel = computed(() => active.value?.kind === 'task'
    ? gt('chat.popover.openTask')
    : gt('chat.popover.openSession'))

// 基础 Markdown 渲染（轻量路径：跳过路径/commit 注解与 KaTeX，与流式文本同款）
const summaryHtml = computed(() => {
    const summary = active.value?.summary || ''
    if (!summary) return ''
    return renderMarkdownHtml(summary, { skipEnhancements: true, skipKatex: true })
})

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
    border-radius: 14px;
    padding: 10px 14px;
    box-shadow: var(--shadow-lg, 0 8px 24px rgba(0, 0, 0, 0.35));
    border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
    -webkit-tap-highlight-color: transparent;
    user-select: none;
    overflow: hidden;
}

.completion-popover-header {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
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
    color: var(--text-secondary, var(--text-primary));
    opacity: 0.75;
    padding: 4px;
    border-radius: 6px;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
}

.completion-popover-open:hover {
    opacity: 1;
    background: color-mix(in srgb, var(--text-primary) 12%, transparent);
}

.completion-popover-summary {
    max-height: 40vh;
    overflow-y: auto;
    font-size: 13px;
    line-height: 1.5;
    color: var(--text-secondary, var(--text-primary));
    word-break: break-word;
}

.completion-popover-enter-active,
.completion-popover-leave-active {
    transition: opacity 0.25s ease, transform 0.25s ease;
}

.completion-popover-enter-from,
.completion-popover-leave-to {
    opacity: 0;
    transform: translateY(-12px);
}
</style>

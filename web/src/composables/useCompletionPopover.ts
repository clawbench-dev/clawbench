import { ref } from 'vue'

/**
 * 单个完成弹窗条目。
 *
 * - kind === 'session': 普通聊天会话完成，点击跳转到该会话
 * - kind === 'task':    定时任务单次执行完成，点击跳转到任务执行详情
 */
export interface CompletionPopoverItem {
    sessionId: string
    title: string
    /** Markdown 原文，组件内用 renderMarkdownHtml 渲染 */
    summary: string
    kind: 'session' | 'task'
    projectPath?: string
    taskId?: string
    executionId?: string
}

// 模块级单例状态，跨组件共享
const queue = ref<CompletionPopoverItem[]>([])
const active = ref<CompletionPopoverItem | null>(null)

function showNext(): void {
    const next = queue.value.shift()
    if (!next) {
        active.value = null
        return
    }
    active.value = next
}

/**
 * 入队一个完成弹窗。当前没有展示项时立即展示；
 * 已有展示项时排队，等前一个手动关闭后依次展示（不扎堆）。
 * 弹窗不自动关闭，需用户点击关闭/空白处/卡片导航后 dismiss。
 */
function push(item: CompletionPopoverItem): void {
    if (!active.value) {
        // 无展示项：直接入队由 showNext 消费（保持单一推进路径）
        queue.value.push(item)
        showNext()
        return
    }
    queue.value.push(item)
}

/** 手动隐藏当前项并推进下一个。 */
function dismiss(): void {
    showNext()
}

/** 测试用：清空队列与当前展示项。 */
function reset(): void {
    queue.value = []
    active.value = null
}

export function useCompletionPopover() {
    return {
        queue,
        active,
        push,
        dismiss,
        reset,
    }
}

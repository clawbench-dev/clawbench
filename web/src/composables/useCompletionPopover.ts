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
    /** 最近一条用户消息纯文本（单行省略号展示） */
    userMessage?: string
    /** 最近一条用户消息是否携带文件附件（展示附件胶囊 chip，不泄漏具体文件） */
    userHasFiles?: boolean
    /** 项目显示名（仅跨项目弹窗提供，本项目为空） */
    projectName?: string
    /** 运行会话/任务的 agent id（渲染后端图标用） */
    agentId?: string
    kind: 'session' | 'task'
    projectPath?: string
    taskId?: string
    executionId?: string
}

// 模块级单例状态，跨组件共享
const queue = ref<CompletionPopoverItem[]>([])
const active = ref<CompletionPopoverItem | null>(null)

// 当前展示项的展示开始时间戳（毫秒）——点击空白处关闭时的防误触保护依据
let activeShownAt = 0

/** 通知面板最小展示时长：期间点击空白处不关闭，避免刚弹出被误点 */
const MIN_DISMISS_MS = 1000

function showNext(): void {
    const next = queue.value.shift()
    if (!next) {
        active.value = null
        return
    }
    active.value = next
    activeShownAt = Date.now()
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

/**
 * 点击空白处关闭当前项。带防误触保护：展示不足 MIN_DISMISS_MS 时忽略，
 * 避免通知刚弹出就被误点关掉。返回是否真正关闭。
 */
function dismissOnBackdrop(): boolean {
    if (Date.now() - activeShownAt < MIN_DISMISS_MS) return false
    dismiss()
    return true
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
        dismissOnBackdrop,
        reset,
    }
}

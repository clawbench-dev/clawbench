import { ref } from 'vue'

/**
 * 单个应用内完成通知条目。
 *
 * 这是**纯通知**，不是预览卡片：只承载标题 + 单行纯文本正文，点击整卡跳转。
 * 因此没有 summary Markdown、没有用户消息引用、没有附件信息——那些是旧的
 * 可交互预览卡片（可展开近全屏 + 快捷回复）才需要的字段。
 *
 * - kind === 'session': 会话事件，跳转到该会话
 * - kind === 'task':    任务执行事件，跳转到任务执行详情
 * - kind === 'forge':   仓库事件（议题/合并请求/流水线），跳转到议题与合并页签
 */
export interface CompletionPopoverItem {
    /** 合并键：同键的 forge 条目会合并成一条"N 条新变化"，避免一次轮询刷屏 */
    groupKey: string
    /** 标题（会话标题 / 任务名 / forge 的 "owner/repo · 合并请求 #42"） */
    title: string
    /** 单行纯文本正文（超长由 CSS 省略号截断） */
    body: string
    kind: 'session' | 'task' | 'forge'
    /** 合并条数。>1 时标题显示 "repoLabel · N 条新变化"（仅 forge 会出现） */
    count?: number
    /** 合并标题里用的仓库标识（owner/repo），仅 forge */
    repoLabel?: string
    /** 会话 id（kind === 'session'） */
    sessionId?: string
    /** 任务 id / 执行 id（kind === 'task'） */
    taskId?: string
    executionId?: string
    /** forge 条目级已读键（`issue/12`、`pr/7`）。流水线没有可派生的 run id，故为空 */
    forgeItemKey?: string
    /** 运行会话/任务的 agent id（渲染后端图标用） */
    agentId?: string
    projectPath?: string
}

/** 队列上限：超出丢弃最旧的排队项，保证卡片不落后于现实（active 不受影响）。 */
const MAX_QUEUE = 3

/**
 * 自动关闭时长。纯通知对标桌面 notify：用户不点击即自行消失，
 * 错过的内容靠未读角标找回（角标不受任何通知开关影响）。
 */
const AUTO_DISMISS_MS = 5000

// 模块级单例状态，跨组件共享
const queue = ref<CompletionPopoverItem[]>([])
const active = ref<CompletionPopoverItem | null>(null)

// 自动关闭计时器。每个新展示项重新计时；手动关闭/重置都要清掉，
// 否则旧计时器会在新条目展示中途把它关掉。
let autoDismissTimer: ReturnType<typeof setTimeout> | null = null

function clearAutoDismiss(): void {
    if (autoDismissTimer !== null) {
        clearTimeout(autoDismissTimer)
        autoDismissTimer = null
    }
}

function showNext(): void {
    clearAutoDismiss()
    const next = queue.value.shift()
    if (!next) {
        active.value = null
        return
    }
    active.value = next
    autoDismissTimer = setTimeout(() => {
        autoDismissTimer = null
        dismiss()
    }, AUTO_DISMISS_MS)
}

/**
 * 入队一个通知。当前没有展示项时立即展示；已有展示项时排队，等前一个关闭
 * （用户点击或 5 秒自动关闭）后依次展示，不扎堆。
 *
 * 同 `groupKey` 的**排队中**条目会被合并而不是新增：一次 forge 轮询会派发
 * 整批事件，逐条排队会把队列堵死几分钟。合并只作用于 queue，不动 active——
 * 正在展示的卡片不该在用户眼前突然变样。
 */
function push(item: CompletionPopoverItem): void {
    const existing = queue.value.find(q => q.groupKey === item.groupKey)
    if (existing) {
        // 合并：累加条数并采用最新事件作为正文（用户关心的是"现在有什么"）
        existing.count = (existing.count || 1) + 1
        existing.title = item.title
        existing.body = item.body
        existing.forgeItemKey = item.forgeItemKey
        if (!active.value) showNext()
        return
    }
    queue.value.push(item)
    // 队列上限：丢最旧的排队项（active 正在展示，不在 queue 里，天然不受影响）
    while (queue.value.length > MAX_QUEUE) queue.value.shift()
    if (!active.value) showNext()
}

/** 手动隐藏当前项并推进下一个。 */
function dismiss(): void {
    clearAutoDismiss()
    showNext()
}

/** 测试用：清空队列与当前展示项（含待触发的自动关闭计时器）。 */
function reset(): void {
    clearAutoDismiss()
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

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
    /**
     * 事件类型标识（卡片顶部的彩色 chip），如「会话已完成」「任务失败」
     * 「合并请求 #42 · 已合并」。这是卡片上"发生了什么"的唯一载体——标题只
     * 回答"是谁"，两者分开后通知无需读完整句就能判断要不要点。
     */
    eventLabel: string
    /**
     * 事件类型的语义分类，决定 chip 配色（成功/失败/中断/进行中）。
     * 由调用方判定：只有它知道原始的 status / event_type。
     */
    eventTone: 'success' | 'danger' | 'warning' | 'info'
    /** 主体标题：会话名 / 任务名 / 仓库标识（谁出了事） */
    title: string
    /**
     * 类别标识 chip（会话 / 任务 / 议题与合并）。与 eventLabel 并排构成
     * 「类别 + 事件」两枚 chip：类别定归属、事件定内容。
     */
    kindLabel?: string
    /** 单行纯文本正文（超长由 CSS 省略号截断） */
    body: string
    kind: 'session' | 'task' | 'forge'
    /** 合并条数。>1 时 chip 改为「N 条新变化」（仅 forge 会出现） */
    count?: number
    /** 会话 id（kind === 'session'） */
    sessionId?: string
    /** 任务 id / 执行 id（kind === 'task'） */
    taskId?: string
    executionId?: string
    /** forge 条目级已读键（`issue/12`、`pr/7`）。流水线没有可派生的 run id，故为空 */
    forgeItemKey?: string
    /** 运行会话/任务的 agent id（渲染后端图标用） */
    agentId?: string
    /** 项目路径。仅跨项目时填充（本项目留空）——它是"外部"视觉区分的开关 */
    projectPath?: string
    /** 项目显示名（路径 basename）。仅跨项目时填充，在卡片底部突出展示 */
    projectName?: string
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

// 计时器的绝对到期时刻。暂停时用它与当前时间算出剩余量——恢复时只补上
// 剩余的那部分，而不是重新计满 5 秒（用户移开鼠标不该白送一轮完整时长）。
let autoDismissDeadline = 0

// 暂停期间保留的剩余毫秒数；null 表示当前未暂停。用 null 而非 0 区分
// "没暂停"和"暂停时刚好只剩 0ms"。
let pausedRemainingMs: number | null = null

function clearAutoDismiss(): void {
    if (autoDismissTimer !== null) {
        clearTimeout(autoDismissTimer)
        autoDismissTimer = null
    }
    pausedRemainingMs = null
}

/** 启动（或重启）计时器，ms 后自动关闭。 */
function armAutoDismiss(ms: number): void {
    autoDismissTimer = setTimeout(() => {
        autoDismissTimer = null
        dismiss()
    }, ms)
    autoDismissDeadline = Date.now() + ms
}

function showNext(): void {
    clearAutoDismiss()
    const next = queue.value.shift()
    if (!next) {
        active.value = null
        return
    }
    active.value = next
    armAutoDismiss(AUTO_DISMISS_MS)
}

/**
 * 暂停自动关闭（鼠标悬停时调用）。记下剩余时间，恢复时从这里接着走。
 *
 * 暂停只对**当前展示项**有效：期间若切换到了下一项，`showNext` 会清掉暂停
 * 状态并重新计满——新条目理应得到完整的展示时长。
 */
function pauseAutoDismiss(): void {
    if (autoDismissTimer === null) return
    clearTimeout(autoDismissTimer)
    autoDismissTimer = null
    // 可能为负（暂停调用晚于到期），钳到 0 让恢复后立即关闭
    pausedRemainingMs = Math.max(0, autoDismissDeadline - Date.now())
}

/** 恢复自动关闭（鼠标移出时调用）。未处于暂停态时是空操作。 */
function resumeAutoDismiss(): void {
    if (pausedRemainingMs === null) return
    const remaining = pausedRemainingMs
    pausedRemainingMs = null
    armAutoDismiss(remaining)
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
        // 合并：累加条数并采用最新事件作为标识与正文（用户关心的是"现在有什么"）
        existing.count = (existing.count || 1) + 1
        existing.eventLabel = item.eventLabel
        existing.eventTone = item.eventTone
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
        pauseAutoDismiss,
        resumeAutoDismiss,
    }
}

<template>
  <Teleport to="body">
    <div v-if="active" class="completion-notify-layer" :class="{ 'is-desktop': isPC }">
      <Transition :name="transitionName" appear>
        <div
          :key="active.groupKey"
          class="completion-notify"
          :class="{ 'is-desktop': isPC, 'is-external': !!active.projectPath }"
          role="button"
          tabindex="0"
          :aria-label="navigateLabel"
          @click="activate"
          @keydown.enter.prevent="activate"
          @keydown.space.prevent="activate"
          @mouseenter="pauseAutoDismiss"
          @mouseleave="resumeAutoDismiss"
          @focusin="pauseAutoDismiss"
          @focusout="resumeAutoDismiss"
        >
          <!-- 头部：图标 + 主类别徽章 + 事件类型标题 + 关闭。
               类别是分类（会话/任务/议题与合并），事件是结果——两者层级不同，
               因此类别保持徽章形态，事件用纯文字标题。 -->
          <div class="completion-notify-header">
            <AgentIcon v-if="agentBackend" :backend="agentBackend" :size="16" class="completion-notify-icon" />
            <span v-if="active.kindLabel" class="completion-notify-category">{{ active.kindLabel }}</span>
            <span class="completion-notify-kind" :class="`is-${active.eventTone}`">{{ displayKind }}</span>
            <button
              class="completion-notify-close"
              type="button"
              :aria-label="gt('chat.popover.close')"
              @click.stop="dismiss"
            >
              <X :size="14" />
            </button>
          </div>
          <!-- 正文：标题段（谁 / 哪个仓库）+ 内容段（摘要），两段分开排版 -->
          <div class="completion-notify-main">
            <div class="completion-notify-title">{{ active.title }}</div>
            <div v-if="active.body" class="completion-notify-body">{{ active.body }}</div>
          </div>
          <!-- 外部项目区隔带：跨项目通知才出现。负 margin 抵消卡片内边距后铺满
               整宽，accent 淡底 + 上边框把它从正文里分离出来——塞进正文行会被
               当成普通弱化信息忽略掉，而"不是本项目"正是点击前的判断依据。 -->
          <div
            v-if="active.projectPath"
            class="completion-notify-project"
          >
            <span class="completion-notify-project-badge">
              <ExternalLink :size="10" />
              <span>{{ gt('chat.popover.external') }}</span>
            </span>
            <span class="completion-notify-project-name">{{ active.projectName || active.projectPath }}</span>
            <span class="completion-notify-project-path">{{ active.projectPath }}</span>
          </div>
        </div>
      </Transition>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { X, ExternalLink } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useCompletionPopover } from '@/composables/useCompletionPopover'
import { useAgents } from '@/composables/useAgents'
import { gt } from '@/composables/useLocale'
import { usePlatformDetect } from '@/composables/usePlatformDetect'

const { active, dismiss, pauseAutoDismiss, resumeAutoDismiss } = useCompletionPopover()
const { getAgentBackend } = useAgents()
const { isPC } = usePlatformDetect()

// 桌面端右下角滑入、移动端顶部滑下——两套动效方向相反，靠 Transition 名称切换。
const transitionName = computed(() => isPC.value ? 'completion-notify-desktop' : 'completion-notify-mobile')

const agentBackend = computed(() => {
    const agentId = active.value?.agentId
    if (!agentId) return ''
    return getAgentBackend(agentId)
})

// 合并后的 forge 条目 chip 改为"N 条新变化"：一次轮询可能派发几十条，
// 逐条展示既无意义又堵队列，用户真正需要知道的是"这个仓库有动静"。
const displayKind = computed(() => {
    const item = active.value
    if (!item) return ''
    if (item.count && item.count > 1) {
        return gt('chat.popover.mergedCount', { count: item.count })
    }
    return item.eventLabel
})

const navigateLabel = computed(() => {
    const kind = active.value?.kind
    if (kind === 'task') return gt('chat.popover.openTask')
    if (kind === 'forge') return gt('chat.popover.openForge')
    return gt('chat.popover.openSession')
})

/**
 * 点击整卡 = 跳转（对标桌面端 notify 的点击行为），并顺带标记已读。
 *
 * 已读是 fire-and-forget：跳转不该等一个网络往返，且标记失败也不该阻止
 * 用户到达目标位置（角标会由下一次刷新自我修正）。
 */
function activate(): void {
    const item = active.value
    if (!item) return
    markRead(item)
    if (item.kind === 'task') {
        window.dispatchEvent(new CustomEvent('clawbench-open-task', {
            detail: { taskId: item.taskId, executionId: item.executionId, projectPath: item.projectPath },
        }))
    } else if (item.kind === 'forge') {
        window.dispatchEvent(new CustomEvent('clawbench-open-forge', {
            detail: { projectPath: item.projectPath },
        }))
    } else {
        window.dispatchEvent(new CustomEvent('clawbench-open-session', {
            detail: { sessionId: item.sessionId, projectPath: item.projectPath },
        }))
    }
    dismiss()
}

/** 按条目类型调用对应的已读端点。失败静默——已读不是跳转的前置条件。 */
function markRead(item: NonNullable<typeof active.value>): void {
    try {
        if (item.kind === 'session' && item.sessionId) {
            const params = new URLSearchParams({ session_id: item.sessionId })
            if (item.projectPath) params.set('project_path', item.projectPath)
            void fetch(`/api/ai/chat/read?${params.toString()}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
            }).catch(() => {})
        } else if (item.kind === 'task' && item.taskId) {
            void fetch(`/api/tasks/${item.taskId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action: 'read', executionId: item.executionId }),
            }).catch(() => {})
        } else if (item.kind === 'forge' && item.forgeItemKey) {
            // 流水线事件没有可派生的条目键（run id 未下发），只跳转不标记。
            void fetch('/api/forge/read', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ itemKey: item.forgeItemKey }),
            }).catch(() => {})
        }
    } catch {
        // Non-critical
    }
}
</script>

<style>
/* ── 定位层 ──
   pointer-events: none 是刻意的：这是纯通知，不是模态。旧的预览卡片用全屏
   遮罩拦截点击并支持"点空白关闭"，多次挡住顶栏交互；现在整层透传，
   只有卡片自身可点。 */
.completion-notify-layer {
    position: fixed;
    inset: 0;
    z-index: var(--z-popover-backdrop);
    display: flex;
    justify-content: center;
    align-items: flex-start;
    padding-top: calc(8px + var(--header-safe-area-top, 0px));
    pointer-events: none;
}

/* 桌面端：右下角，对齐系统通知的常规位置 */
.completion-notify-layer.is-desktop {
    align-items: flex-end;
    justify-content: flex-end;
    padding: 0 var(--space-5) var(--space-5) 0;
}

.completion-notify {
    display: flex;
    flex-direction: column;
    width: 100%;
    max-width: min(480px, 92vw);
    /* 底部 padding 归零：外部项目区隔带要贴到卡片下沿（用负 margin 出血） */
    padding: var(--space-4) var(--space-5) 0;
    background: color-mix(in srgb, var(--bg-tertiary) 88%, var(--bg-elevated, var(--bg-tertiary)));
    color: var(--text-primary);
    /* Sharp-corner geometry, same language as the rest of the app's floating
       surfaces (QuoteQuestionBar, dock cards). */
    border-radius: var(--radius-xs);
    box-shadow: var(--shadow-lg);
    border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
    pointer-events: auto;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
    user-select: none;
    overflow: hidden;
}

/* 外部项目：整卡换色。边框与底色都偏向 accent，让"这不是本项目的通知"在
   余光里就能分辨——只靠底部一条区隔带，卡片主体看起来仍像本项目的。 */
.completion-notify.is-external {
    border-color: color-mix(in srgb, var(--accent-color) 55%, transparent);
    background: color-mix(in srgb, var(--accent-color) 6%, var(--bg-tertiary));
}

/* ── 头部区：图标 + 类型标签 + 关闭 ──
   类型信息（类别/事件）集中在这里，与正文区分开——头部回答"这是什么通知"，
   正文回答"具体什么事"。关闭按钮靠 margin-left:auto 推到最右。 */
.completion-notify-header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    /* 无项目区隔带时由 main 承担底部间距，这里只留与正文的间隔 */
    margin-bottom: var(--space-3);
}

.completion-notify-icon {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
}

/* 正文区：标题段 + 内容段 */
.completion-notify-main {
    min-width: 0;
    /* 有项目区隔带时补上被卡片 padding 让出的下边距 */
    padding-bottom: var(--space-4);
}

.completion-notify.is-external .completion-notify-main {
    padding-bottom: var(--space-3);
}

@media (min-width: 768px) {
    .completion-notify {
        /* 桌面端收窄：这是通知不是面板。过宽时一行能塞下整段摘要，读起来
           像在看正文；窄卡片更贴近系统通知的形态，也让右下角的占用更小。 */
        max-width: min(420px, 92vw);
    }
}

.completion-notify-icon {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
}

/* 主类别徽章（会话 / 任务 / 议题与合并）：中性灰的细描边小标签，与 SessionList
   的 session-item-badge 同一套语言。刻意中性——它回答"哪个子系统"属于分类，
   不该和事件标题的语义色抢注意力。 */
.completion-notify-category {
    flex-shrink: 0;
    padding: 1px 6px;
    font-size: var(--font-size-sm);
    line-height: var(--line-height-snug);
    font-weight: var(--font-weight-medium);
    white-space: nowrap;
    border-radius: var(--radius-xs);
    color: var(--text-muted);
    border: 1px solid color-mix(in srgb, currentColor 45%, transparent);
    background: color-mix(in srgb, currentColor 10%, transparent);
}

/* 事件类型标题（如「会话已完成」）：头部的主文字，**不是徽章**——无边框、
   无底色、无内边距，只有语义色。它回答"发生了什么"，是卡片上最该一眼看到的
   信息，所以用 lg（14px，与对话框标题同档）而非更小的标签字号。
   允许省略号截断（forge 的「合并请求 #42 · 已合并」较长），不再像徽章那样
   强制完整显示——它是唯一文本，截断尾部仍可读。 */
.completion-notify-kind {
    flex: 1;
    min-width: 0;
    font-size: var(--font-size-lg);
    line-height: var(--line-height-snug);
    font-weight: var(--font-weight-semibold);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

/* 语义配色：绿=成功、红=失败、黄=中断、蓝=进行中/中性（含 forge 的六类事件）。
   这是卡片上唯一的彩色信息——它回答"结果如何"，最需要一眼看到。 */
.completion-notify-kind.is-success { color: var(--color-success); }
.completion-notify-kind.is-danger { color: var(--color-red); }
.completion-notify-kind.is-warning { color: var(--color-yellow); }
.completion-notify-kind.is-info { color: var(--color-info); }

/* 标题段：正文区第一段，单行省略。字号与聊天正文同级
   （.chat-message 也是 --font-size-md + --line-height-snug）。 */
.completion-notify-title {
    font-size: var(--font-size-md);
    line-height: var(--line-height-snug);
    font-weight: var(--font-weight-semibold);
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

/* 内容段：最多 4 行，超出用省略号收尾。单行时摘要几乎读不出信息（"已完成
   登录流程重构…" 后面全被截掉），放宽到 4 行才能在卡片内判断结果好坏——
   但仍要封顶，否则一段长回复会把通知撑成面板。
   字号/行高对齐聊天正文（.chat-message），不再降一档——用户反馈"字体有点小"。
   用 -webkit-line-clamp 而非固定 max-height：行数由行高自动换算，改字号
   或行高时不用同步改数值。 */
.completion-notify-body {
    margin-top: var(--space-2);
    font-size: var(--font-size-md);
    line-height: var(--line-height-snug);
    color: var(--text-secondary, var(--text-primary));
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 4;
    line-clamp: 4;
    overflow: hidden;
    /* 长 URL / 无空格串要能断行，否则会把卡片横向撑破。
       用 overflow-wrap 而非 word-break: break-word —— 后者已弃用。 */
    overflow-wrap: break-word;
}

/* ── 外部项目区隔带 ──
   负 margin 抵消卡片内边距后铺满整宽，accent 淡底 + 上边框形成独立区带。
   与卡片主体用不同底色分层，项目名加粗突出（用户一眼要看到"哪个项目"）。 */
.completion-notify-project {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-1) calc(-1 * var(--space-5)) 0;
    padding: var(--space-3) var(--space-5);
    background: color-mix(in srgb, var(--accent-color) 8%, var(--bg-primary, #fff));
    border-top: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
    min-width: 0;
}

/* "外部"徽章：accent 描边小标签，提示这是其他项目的会话。
   与事件类型标题不同——这个仍是真正的徽章（描边+淡底），字号留在 sm。 */
.completion-notify-project-badge {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 1px 6px;
    font-size: var(--font-size-sm);
    line-height: var(--line-height-snug);
    font-weight: var(--font-weight-medium);
    color: var(--accent-color);
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-color) 35%, transparent);
    border-radius: var(--radius-xs);
}

.completion-notify-project-badge svg {
    flex-shrink: 0;
}

/* 项目名：突出显示——加粗 + 正文色，是这行的主信息 */
.completion-notify-project-name {
    flex-shrink: 0;
    font-size: var(--font-size-sm);
    line-height: var(--line-height-normal);
    font-weight: var(--font-weight-semibold);
    color: var(--text-secondary, var(--text-primary));
}

/* 项目路径：弱化补充，占剩余宽度并尾部省略（flex 容器内 text-overflow
   不生效，所以省略必须落在这一层） */
.completion-notify-project-path {
    flex: 1;
    min-width: 0;
    font-size: var(--font-size-xs);
    line-height: var(--line-height-normal);
    color: var(--text-hint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

/* 关闭按钮：方形 22px，尖角，与卡片其余控件同一套圆角语言。
   必须显式 border: none —— 该 class 若从 <a> 复用会被 UA 样式带出边框。 */
.completion-notify-close {
    flex-shrink: 0;
    /* 关闭按钮靠右侧：事件类型标题带 flex:1 撑开剩余空间，天然把它推到底。 */
    display: flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border: none;
    border-radius: var(--radius-xs);
    cursor: pointer;
    transition: opacity var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
    .completion-notify-close:hover {
        background: color-mix(in srgb, var(--text-primary) 10%, transparent);
    }
}

/* ── 动效：移动端顶部滑下（Android 通知风格），桌面端右下角滑入 ── */
.completion-notify-mobile-enter-active {
    transition: opacity 0.3s cubic-bezier(0.4, 0, 0.2, 1), transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.completion-notify-mobile-leave-active {
    transition: opacity var(--duration-slow) ease-in, transform var(--duration-slow) ease-in;
}

.completion-notify-mobile-enter-from,
.completion-notify-mobile-leave-to {
    opacity: 0;
    transform: translateY(-120%);
}

.completion-notify-desktop-enter-active {
    transition: opacity 0.3s cubic-bezier(0.4, 0, 0.2, 1), transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.completion-notify-desktop-leave-active {
    transition: opacity var(--duration-slow) ease-in, transform var(--duration-slow) ease-in;
}

.completion-notify-desktop-enter-from,
.completion-notify-desktop-leave-to {
    opacity: 0;
    transform: translateX(120%);
}

@media (prefers-reduced-motion: reduce) {
    .completion-notify-mobile-enter-active,
    .completion-notify-mobile-leave-active,
    .completion-notify-desktop-enter-active,
    .completion-notify-desktop-leave-active {
        transition: none;
    }
}
</style>

<template>
  <Teleport to="body">
    <div v-if="active" class="completion-notify-layer" :class="{ 'is-desktop': isPC }">
      <Transition :name="transitionName" appear>
        <div
          :key="active.groupKey"
          class="completion-notify"
          :class="{ 'is-desktop': isPC }"
          role="button"
          tabindex="0"
          :aria-label="navigateLabel"
          @click="activate"
          @keydown.enter.prevent="activate"
          @keydown.space.prevent="activate"
        >
          <AgentIcon v-if="agentBackend" :backend="agentBackend" :size="16" class="completion-notify-icon" />
          <div class="completion-notify-text">
            <div class="completion-notify-title" :title="displayTitle">{{ displayTitle }}</div>
            <div v-if="active.body" class="completion-notify-body" :title="active.body">{{ active.body }}</div>
          </div>
          <button
            class="completion-notify-close"
            type="button"
            :aria-label="gt('chat.popover.close')"
            :title="gt('chat.popover.close')"
            @click.stop="dismiss"
          >
            <X :size="14" />
          </button>
        </div>
      </Transition>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { X } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useCompletionPopover } from '@/composables/useCompletionPopover'
import { useAgents } from '@/composables/useAgents'
import { gt } from '@/composables/useLocale'
import { usePlatformDetect } from '@/composables/usePlatformDetect'

const { active, dismiss } = useCompletionPopover()
const { getAgentBackend } = useAgents()
const { isPC } = usePlatformDetect()

// 桌面端右下角滑入、移动端顶部滑下——两套动效方向相反，靠 Transition 名称切换。
const transitionName = computed(() => isPC.value ? 'completion-notify-desktop' : 'completion-notify-mobile')

const agentBackend = computed(() => {
    const agentId = active.value?.agentId
    if (!agentId) return ''
    return getAgentBackend(agentId)
})

// 合并后的 forge 条目用"N 条新变化"代替单条标题：一次轮询可能派发几十条，
// 逐条展示既无意义又堵队列，用户真正需要知道的是"这个仓库有动静"。
const displayTitle = computed(() => {
    const item = active.value
    if (!item) return ''
    if (item.count && item.count > 1 && item.repoLabel) {
        return `${item.repoLabel} · ${gt('chat.popover.mergedCount', { count: item.count })}`
    }
    return item.title
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
    align-items: flex-start;
    gap: var(--space-4);
    width: 100%;
    max-width: min(480px, 92vw);
    padding: var(--space-4) var(--space-5);
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

@media (min-width: 768px) {
    .completion-notify {
        max-width: min(680px, 92vw);
    }
}

.completion-notify-icon {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    /* 与标题首行文字对齐（卡片是 flex-start，图标需自带上边距补偿行高） */
    padding-top: 1px;
}

.completion-notify-text {
    flex: 1;
    min-width: 0;
}

.completion-notify-title {
    font-size: var(--font-size-md);
    font-weight: var(--font-weight-semibold);
    line-height: var(--line-height-snug);
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

/* 单行纯文本正文，超长省略号——对标系统通知的 body。 */
.completion-notify-body {
    margin-top: 2px;
    font-size: var(--font-size-sm);
    line-height: var(--line-height-normal);
    color: var(--text-secondary, var(--text-primary));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

/* 关闭按钮：方形 22px，尖角，与卡片其余控件同一套圆角语言。
   必须显式 border: none —— 该 class 若从 <a> 复用会被 UA 样式带出边框。 */
.completion-notify-close {
    flex-shrink: 0;
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

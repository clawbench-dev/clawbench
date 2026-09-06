<template>
  <Teleport to="body">
    <div
      v-if="active"
      class="completion-popover-backdrop"
      :class="{ 'is-expanded': expanded }"
      @click.self="handleBackdropClick"
    >
      <Transition name="completion-popover-card" mode="out-in" appear>
        <div :key="active.sessionId + active.kind" class="completion-popover" :class="{ 'is-expanded': expanded }">
          <div class="completion-popover-header">
            <AgentIcon v-if="agentBackend" :backend="agentBackend" :size="16" class="completion-popover-icon" />
            <span class="completion-popover-title" :title="active.title">{{ active.title || '未命名会话' }}</span>
          </div>
          <div v-if="active.userMessage || active.userHasFiles" class="completion-popover-meta completion-popover-meta-user">
            <div
              v-if="active.userMessage"
              class="completion-popover-user-quote"
              :class="{ 'is-expanded': userMessageExpanded }"
              role="button"
              :aria-expanded="userMessageExpanded"
              @click="toggleUserMessage"
            >
              <MessageSquare :size="12" class="completion-popover-user-quote-icon" />
              <span class="completion-popover-user-quote-text">{{ active.userMessage }}</span>
            </div>
            <span v-if="active.userHasFiles" class="completion-popover-attachment-chip">
              <Paperclip :size="11" />
              {{ gt('chat.popover.attachment') }}
            </span>
          </div>
          <div
            class="completion-popover-summary markdown-body"
            :class="expanded ? 'is-expanded-summary' : 'is-collapsed'"
            :title="expanded ? undefined : gt('chat.popover.expand')"
            v-html="summaryHtml"
            @click="handleSummaryClick"
          ></div>
          <div v-show="expanded" class="completion-popover-input">
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
          <div class="completion-popover-actions">
            <button class="completion-popover-action-btn completion-popover-mark-read" type="button" :aria-label="gt('chat.popover.markRead')" @click="handleMarkRead">
              <Check :size="14" />
              <span class="completion-popover-action-label">{{ gt('chat.popover.markRead') }}</span>
            </button>
            <button class="completion-popover-action-btn completion-popover-open" type="button" :aria-label="openLabel" @click="openSession">
              <Search :size="15" />
              <span class="completion-popover-action-label">{{ openLabel }}</span>
            </button>
          </div>
          <div v-if="active.projectName" class="completion-popover-footer">
            <span class="completion-popover-project" :title="active.projectPath || active.projectName">
              <span class="completion-popover-project-badge">
                <ExternalLink :size="10" />
                <span>{{ gt('chat.popover.external') }}</span>
              </span>
              <span class="completion-popover-project-name">{{ active.projectName }}</span>
              <span v-if="active.projectPath" class="completion-popover-project-path">{{ active.projectPath }}</span>
            </span>
          </div>
        </div>
      </Transition>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Search, Send, Check, ExternalLink, MessageSquare, Paperclip } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useCompletionPopover } from '@/composables/useCompletionPopover'
import { useAgents } from '@/composables/useAgents'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { rewriteImageUrls, getThumbWidth } from '@/utils/chatRenderUtils'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader'
import { gt } from '@/composables/useLocale'
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { useToast } from '@/composables/useToast'
import { canSendInput } from '@/utils/quoteQuestionUtils'
import { store } from '@/stores/app'

const { active, dismiss, dismissOnBackdrop } = useCompletionPopover()
const { getAgentBackend } = useAgents()
const toast = useToast()
const { isPC } = usePlatformDetect()

// 展开态（近全屏可滚动 + 显示输入框/图片）。默认折叠（紧凑预览），
// 点击摘要内容区展开。展开后不提供"收起"——回到折叠态无操作意义，
// 用户通过 打开会话/标记已读/点空白 完成或离开。
const expanded = ref(false)
function expand(): void {
    expanded.value = true
    // 展开为全屏阅读态时同时展开用户消息引用块，确保用户能看全问题
    userMessageExpanded.value = true
}

const agentBackend = computed(() => {
    const agentId = active.value?.agentId
    if (!agentId) return ''
    return getAgentBackend(agentId)
})

const openLabel = computed(() => active.value?.kind === 'task'
    ? gt('chat.popover.openTask')
    : gt('chat.popover.openSession'))

const inputPlaceholder = computed(() => active.value?.kind === 'task'
    ? gt('chat.popover.replyTask')
    : gt('chat.popover.replySession'))

// 摘要 Markdown：折叠与展开共用同一份完整渲染。折叠态靠 CSS 裁剪 + 隐藏
// img（不滚动），展开态展示全部并可滚动。相对路径图片按会话归属项目解析：
// 本项目会话用当前项目根，跨项目用其 projectPath，避免误按当前项目解析导致裂图。
// 轻量渲染（skipEnhancements）跳过路径/commit 注解与 KaTeX，保留富文本与代码块
// 表头；随后仅对图片做改写（缩略图 + lightbox 包装）。
const summaryHtml = computed(() => {
    const item = active.value
    const summary = item?.summary || ''
    if (!summary) return ''
    const base = renderMarkdownHtml(summary, { skipEnhancements: true, skipKatex: true })
    const projectRoot = item?.projectPath || store.state.projectRoot
    if (!projectRoot) return base
    return rewriteImageUrls(base, projectRoot, getThumbWidth(isPC.value))
})

// ── 快捷输入框 ──
const inputText = ref('')
const inputRef = ref<HTMLTextAreaElement | null>(null)
const sending = ref(false)

const canSend = computed(() => canSendInput(inputText.value) && !sending.value)

// 用户消息引用块：折叠时单行省略，点击展开完整内容
const userMessageExpanded = ref(false)
function toggleUserMessage(): void {
    userMessageExpanded.value = !userMessageExpanded.value
}

// 弹窗切换时重置输入框与展开态（immediate：mount 时也重置一次）
watch(active, () => {
    inputText.value = ''
    sending.value = false
    userMessageExpanded.value = false
    expanded.value = false
}, { immediate: true })

// 点击 backdrop 空白处关闭（带最小停留时长防误触保护）
function handleBackdropClick(): void {
    dismissOnBackdrop()
}

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
        // 外部项目会话：携带其所属项目路径，后端据此通过归属校验
        const params = new URLSearchParams({ session_id: item.sessionId })
        if (item.projectPath) params.set('project_path', item.projectPath)
        const url = `/api/ai/chat?${params.toString()}`
        const res = await fetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message: text }),
        })
        if (!res.ok) {
            // 发送失败：保持弹窗打开并复位，让用户可重试
            sending.value = false
            return
        }
        // 发送成功后清空该会话未读（独立 /read 端点，不影响其他会话未读）
        await markRead(item)
        // 气泡提示发送成功，给用户确认感
        toast.show(gt('chat.popover.sentToast'), { type: 'success', duration: 2000 })
        dismiss()
    } catch {
        sending.value = false
    }
}

// 标记会话已读：调用 /api/ai/chat/read 清空未读状态
async function markRead(item: NonNullable<typeof active.value>): Promise<void> {
    const params = new URLSearchParams({ session_id: item.sessionId })
    if (item.projectPath) params.set('project_path', item.projectPath)
    try {
        await fetch(`/api/ai/chat/read?${params.toString()}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
        })
    } catch {
        // 标记已读失败不阻塞发送流程——消息已发出
    }
}

// 标记会话已读按钮：不发送消息，仅清除未读状态，成功后关闭弹窗
async function handleMarkRead(): Promise<void> {
    const item = active.value
    if (!item || sending.value) return
    sending.value = true
    try {
        await markRead(item)
        // 气泡提示标记成功，给用户确认感
        toast.show(gt('chat.popover.markedReadToast'), { type: 'success', duration: 2000 })
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

// ── 摘要点击 ──
// 命中交互元素（代码/表格按钮、lightbox、链接、播放器等）不放行到展开——
// 这些按钮的点击由 handleCodeBlockClick / handleTableBlockClick / Lightbox
// 全局监听自行处理；其余"空白/正文"点击：折叠态展开面板，展开态不动作。
function isInteractiveTarget(target: EventTarget | null): boolean {
    const el = (target as HTMLElement | null)?.closest?.(
        '.code-block-copy-btn, .code-block-wrap-btn, .table-block-copy-btn, .table-block-wrap-btn, ' +
        '.table-block-header-actions, .lightbox-img-wrap, .lightbox-expand-icon, .chat-audio-player, ' +
        '.chat-video-player, a, button'
    )
    return !!el
}

function handleSummaryClick(event: MouseEvent): void {
    if (handleCodeBlockClick(event) || handleTableBlockClick(event)) return
    if (isInteractiveTarget(event.target)) return
    if (!expanded.value) expand()
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

/* 展开态：面板垂直居中、四周等距留边 —— 短内容自然高度居中，
   长内容撑到留白处形成"几乎占满屏幕"的近全屏卡片 */
.completion-popover-backdrop.is-expanded {
    align-items: center;
    padding: 16px;
    padding-top: calc(16px + var(--header-safe-area-top, 0px));
}

.completion-popover {
    max-width: min(480px, 92vw);
    width: 100%;
    background: color-mix(in srgb, var(--bg-tertiary) 88%, var(--bg-elevated, var(--bg-tertiary)));
    color: var(--text-primary);
    border-radius: 12px;
    padding: 8px 10px;
    box-shadow: var(--shadow-lg, 0 8px 24px rgba(0, 0, 0, 0.35));
    border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
    -webkit-tap-highlight-color: transparent;
    user-select: none;
    overflow: hidden;
}

/* 展开态卡片：占满可用高度（由居中遮罩的 16px 四周留边约束），
   内容区内部滚动 */
.completion-popover.is-expanded {
    display: flex;
    flex-direction: column;
    max-height: 100%;
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
    gap: 8px;
    margin-bottom: 6px;
}

.completion-popover-icon {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
}

.completion-popover-title {
    flex: 1;
    min-width: 0;
    font-size: 14px;
    font-weight: 600;
    line-height: 1.4;
    color: var(--text-primary);
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
    border-radius: 50%;
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

/* 元信息行（项目、用户消息）：小号、弱化，与正文形成层次 */
.completion-popover-meta {
    display: flex;
    align-items: center;
    min-width: 0;
    margin-bottom: 4px;
    padding-left: 2px;
}

/* 底部 Footer（外部项目行）：贴满卡片宽度、无外边距的独立区隔带，
   accent 淡底与上方内容区完全分开（用负 margin 抵消卡片内边距横向铺满） */
.completion-popover-footer {
    display: flex;
    align-items: center;
    margin: 8px -10px -8px;
    padding: 6px 10px;
    background: color-mix(in srgb, var(--accent-color) 8%, var(--bg-primary, #fff));
    border-top: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
}

.completion-popover-footer .completion-popover-project {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    line-height: 1.5;
    color: var(--text-secondary, var(--text-primary));
}

.completion-popover-project {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    font-size: 11px;
    line-height: 1.5;
    color: var(--text-tertiary, var(--text-secondary));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.completion-popover-project {
    gap: 5px;
}

/* "外部"徽章：accent 色描边小标签，图标+文字，提示这是其他项目的会话 */
.completion-popover-project-badge {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 1px 5px;
    font-size: 10px;
    line-height: 1.4;
    font-weight: 500;
    color: var(--accent-color);
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-color) 35%, transparent);
    border-radius: 999px;
}

.completion-popover-project-badge svg {
    flex-shrink: 0;
}

/* 项目行（图标+名称+路径）整行单行展示：
   flex 容器内 text-overflow 不生效，省略逻辑放在路径 span；
   图标与项目名 flex-shrink:0 保持完整，路径尾部溢出省略 */
.completion-popover-project > svg {
    flex-shrink: 0;
}

.completion-popover-project-name {
    flex-shrink: 0;
}

/* 用户消息：引用式样块 — 无圆角、左侧 accent 竖线描边、淡色底，
   与 QuoteQuestionBar 的引用片段视觉一致。点击可展开/收起完整内容。
   宽度随内容自适应，超出卡片时单行省略 */
.completion-popover-meta-user {
    justify-content: flex-start;
    padding-left: 0;
    flex-wrap: wrap;
    row-gap: 4px;
}

/* 附件胶囊 chip：表示用户消息带附件，不泄漏具体文件。与文字引用块同行并存 */
.completion-popover-attachment-chip {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-left: 6px;
    padding: 2px 9px;
    font-size: 11px;
    line-height: 1.5;
    font-weight: 500;
    color: var(--accent-color);
    background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-color) 40%, transparent);
    border-radius: 999px;
    user-select: none;
}

.completion-popover-meta-user .completion-popover-attachment-chip:first-child {
    margin-left: 0;
}

.completion-popover-user-quote {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    max-width: 100%;
    padding: 3px 8px;
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-secondary);
    background: color-mix(in srgb, var(--accent-color) 10%, var(--bg-tertiary));
    border-left: 2px solid var(--accent-color);
    border-radius: 0;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
    transition: background 0.15s;
}

.completion-popover-user-quote:active {
    background: color-mix(in srgb, var(--accent-color) 16%, var(--bg-tertiary));
}

.completion-popover-user-quote-text {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.completion-popover-user-quote-icon {
    flex-shrink: 0;
    color: var(--accent-color);
}

.completion-popover-user-quote.is-expanded .completion-popover-user-quote-text {
    white-space: pre-wrap;
    overflow: visible;
    text-overflow: clip;
    word-break: break-word;
}

.completion-popover-project-name {
    font-weight: 600;
    color: var(--text-secondary, var(--text-primary));
}

.completion-popover-project-path {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    opacity: 0.7;
}

.completion-popover-summary {
    max-height: 28vh;
    overflow-y: auto;
    font-size: 13px;
    line-height: 1.6;
    color: var(--text-secondary, var(--text-primary));
    word-break: break-word;
    padding-top: 2px;
}

/* ── 折叠态摘要：富文本预览按固定高度裁剪（不滚动），底部淡出渐变，
   暗示下方还有更多内容。图片此态不展示（CSS 隐藏）。点击内容区展开。
   底部留出 ~40px 淡出带，与下方按钮行重叠（负 margin）以省纵向空间。 ── */
.completion-popover-summary.markdown-body.is-collapsed {
    max-height: 132px;
    overflow-y: hidden;
    position: relative;
    cursor: pointer;
    -webkit-mask-image: linear-gradient(to bottom, #000 32%, transparent 100%);
    mask-image: linear-gradient(to bottom, #000 32%, transparent 100%);
}

.completion-popover-summary.is-collapsed img {
    display: none;
}

/* ── 展开态摘要：卡片撑满时由内部滚动承接长内容（flex 布局见 .is-expanded 卡片） ── */
.completion-popover.is-expanded .completion-popover-summary {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    max-height: none;
}

/* 输入框与 footer 在展开卡片列布局中不得随剩余高度伸缩 */
.completion-popover.is-expanded .completion-popover-input,
.completion-popover.is-expanded .completion-popover-footer {
    flex: 0 0 auto;
}

/* 展开态下图片以实际渲染展示，容器需要有定位与 lightbox 悬停图标样式；
   镜像 MarkdownPreview 的 lightbox-img-wrap 规则（img 由 v-html 注入，
   非 scoped 才可命中） */
.completion-popover-summary .lightbox-img-wrap {
    position: relative;
    display: inline-block;
}

.completion-popover-summary .lightbox-img-wrap .lightbox-expand-icon {
    display: none;
    position: absolute;
    top: 4px;
    right: 4px;
    width: 24px;
    height: 24px;
    border-radius: 4px;
    background: rgba(0, 0, 0, 0.5);
    color: #fff;
    cursor: pointer;
    z-index: 2;
    pointer-events: auto;
}

@media (hover: hover) {
    .completion-popover-summary .lightbox-img-wrap:hover .lightbox-expand-icon {
        display: flex;
        align-items: center;
        justify-content: center;
    }
}

.completion-popover-summary .lightbox-img-wrap .lightbox-expand-icon::after {
    content: '⤢';
    font-size: 14px;
    line-height: 1;
}

/* 有元信息行时，正文用分隔线+更大间距分层；
   用户消息气泡与助手消息之间除外（气泡已有实底底色，分隔线多余） */
.completion-popover-meta:not(.completion-popover-meta-user) + .completion-popover-summary {
    border-top: 1px solid color-mix(in srgb, var(--text-primary) 16%, transparent);
    padding-top: 10px;
    margin-top: 6px;
}

/* 覆盖全局 .markdown-body 规则：卡片已有自身 padding，去掉重复 padding；
   只清左右下，保留顶部——分隔线的 padding-top: 8px 需生效 */
.completion-popover-summary.markdown-body {
    padding-left: 0;
    padding-right: 0;
    padding-bottom: 0;
    flex: none;
}

.completion-popover-summary.markdown-body > :last-child,
.completion-popover-summary.markdown-body > :last-child > :last-child {
    margin-bottom: 0;
}

/* 快捷输入框 — 与聊天界面 ChatInputBar 输入框样式对齐（圆角 20px、固定不随高度变化）。
   背景用 --bg-primary（白/更亮）与卡片的 --bg-tertiary 底色区分，避免融合 */
.completion-popover-input {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: flex-end;
    gap: 2px;
    margin-top: 6px;
    padding: 4px 6px 6px;
    background: var(--bg-primary, #fff);
    border: none;
    border-radius: 20px;
    overflow: hidden;
    transition: background 0.2s, box-shadow 0.2s;
}

.completion-popover-input:focus-within {
    background: var(--bg-primary, #fff);
    box-shadow: 0 0 0 1px var(--accent-color, #0066cc);
}

.completion-popover-textarea {
    flex: 1;
    min-width: 0;
    padding: 4px 8px;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: 16px;
    line-height: 20px;
    outline: none;
    resize: none;
    overflow-y: auto;
    min-height: 28px;
    max-height: calc(20px * 3 + 4px + 4px); /* 3 行 + 上下 padding */
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
    width: 28px;
    height: 28px;
    padding: 0;
    background: var(--accent-color);
    color: #fff;
    border: none;
    border-radius: 50%;
    cursor: pointer;
    transition: opacity 0.15s;
}

.completion-popover-send.disabled {
    opacity: 0.4;
    cursor: not-allowed;
}

/* ── 底部动作按钮：标记已读 / 打开 —— 胶囊形（图标 + 文字） ──
   标记已读：描边弱化次级样式；打开：accent 实底主操作 */
.completion-popover-action-btn {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 28px;
    padding: 0 12px;
    border-radius: 999px;
    font-size: 12px;
    line-height: 1;
    font-weight: 500;
    cursor: pointer;
    transition: opacity 0.15s, background 0.15s, box-shadow 0.15s;
    -webkit-tap-highlight-color: transparent;
}

.completion-popover-mark-read {
    background: transparent;
    color: var(--accent-color);
    border: 1px solid color-mix(in srgb, var(--accent-color) 45%, var(--border-color));
}

@media (hover: hover) {
    .completion-popover-mark-read:hover {
        background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    }
}

.completion-popover-open {
    background: var(--accent-color);
    color: #fff;
    border: none;
}

@media (hover: hover) {
    .completion-popover-open:hover {
        opacity: 0.88;
    }
}

/* ── 底部动作行：标记已读 / 打开 按钮靠右，不属于标题栏 ── */
.completion-popover-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
    margin-top: 6px;
}

.completion-popover.is-expanded .completion-popover-actions {
    flex: 0 0 auto;
}

/* 折叠态：动作行用负 margin 抬入摘要底部淡出带，与渐变区重叠省纵向空间。
   容器顶部用"透明→卡色"渐变承接摘要的 mask 淡出，避免生硬接缝；
   按钮浮于渐变带内、卡片底角靠右。 */
.completion-popover:not(.is-expanded) .completion-popover-actions {
    position: relative;
    margin-top: -34px;
    padding-top: 16px;
    background: linear-gradient(
        to bottom,
        transparent 0%,
        color-mix(in srgb, var(--bg-tertiary) 88%, var(--bg-elevated, var(--bg-tertiary))) 55%
    );
    border-radius: 0 0 12px 12px;
}

/* Android 通知风格：卡片从顶部滑下 + 淡入（标准缓动曲线），离开反向滑回 */
.completion-popover-card-enter-active {
    transition: opacity 0.3s cubic-bezier(0.4, 0, 0.2, 1), transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.completion-popover-card-leave-active {
    transition: opacity 0.2s ease-in, transform 0.2s ease-in;
}

.completion-popover-card-enter-from,
.completion-popover-card-leave-to {
    opacity: 0;
    transform: translateY(-120%);
}
</style>

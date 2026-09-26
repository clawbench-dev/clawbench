<template>
  <!--
    One row of a message index / conversation TOC.

    Shared by two hosts so their rows can never drift apart:
      - UserMsgIndexDrawer.vue (the in-app conversation index, a BottomSheet)
      - SessionShareView.vue   (the public conversation-share TOC rail/drawer)

    Purely presentational: the host owns the list, filtering and scrolling; this
    component renders one row and emits `select` when it is activated.
  -->
  <div
    class="msg-item"
    :class="{
      active,
      'msg-item-active': navActive,
      'msg-item--assistant': msg.role === 'assistant',
    }"
    :aria-current="active || undefined"
    tabindex="0"
    role="button"
    @click="$emit('select', msg)"
    @keydown.enter="$emit('select', msg)"
  >
    <span class="msg-node">
      <span class="msg-index">{{ index }}</span>
    </span>
    <div class="msg-body">
      <span
        class="msg-role-tag"
        :class="msg.role === 'assistant' ? 'role-assistant' : 'role-user'"
        :title="roleLabel"
        :aria-label="roleLabel"
      >
        <Bot v-if="msg.role === 'assistant'" :size="12" />
        <User v-else :size="12" />
      </span>
      <span class="msg-text" :class="{ 'msg-text--muted': isPlaceholder }" v-html="rowHighlight"></span>
      <span v-if="showTime && msg.createdAt" class="msg-time">{{ formatRelativeTime(msg.createdAt) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Bot, User } from 'lucide-vue-next'
import { assistantIndexText, formatIndexMsg } from '@/utils/userMsgIndexUtils.ts'
import { highlightText } from '@/utils/searchUtils'
import { formatRelativeTime } from '@/utils/format.ts'

interface IndexMsg {
  id?: number | string
  role?: string
  summary?: string
  content?: string
  createdAt?: string
  blocks?: Array<{ type?: string; text?: string }>
  files?: Array<string | { path?: string }>
}

const props = withDefaults(defineProps<{
  msg: IndexMsg
  /** 1-based ordinal shown in the timeline node. */
  index: number
  /** The message currently in view (scroll-spy / conversation position). */
  active?: boolean
  /** Keyboard-navigation cursor (the in-app drawer's ↑/↓ selection). */
  navActive?: boolean
  /** Query used to wrap matches in <mark>; empty renders plain escaped text. */
  searchQuery?: string
  /** Hide the timestamp column (used where rows are narrower, e.g. the share TOC). */
  showTime?: boolean
}>(), {
  active: false,
  navActive: false,
  searchQuery: '',
  showTime: true,
})

defineEmits<{ select: [msg: IndexMsg] }>()

const { t } = useI18n()

/** Visible labels the rows may render; both are searchable. */
const rowLabels = computed(() => ({
  attachment: t('chat.messageList.userMsgIndexAttachment'),
  noText: t('chat.messageList.conversationIndexNoText'),
}))

/** Whether a row renders its "no text" placeholder (dimmed styling). */
const isPlaceholder = computed(
  () => props.msg.role === 'assistant' && !assistantIndexText(props.msg),
)

/**
 * The role chip shows only an icon, so the label lives in title/aria-label —
 * otherwise the role would be invisible to screen readers and to anyone who
 * cannot tell the two glyphs apart.
 */
const roleLabel = computed(() =>
  props.msg.role === 'assistant'
    ? t('chat.messageList.conversationIndexRoleAssistant')
    : t('chat.messageList.conversationIndexRoleUser'),
)

/** Row display text with the active query's matches wrapped in <mark>. */
const rowHighlight = computed(() =>
  highlightText(formatIndexMsg(props.msg, rowLabels.value), props.searchQuery),
)
</script>

<style scoped>
/* ── Message items ──
   Each row is exactly ONE line: the role chip, the (ellipsised) preview and
   the timestamp sit side by side. `--rail-punch` is the row's own opaque
   background, used by the node to mask the rail running behind it — it must
   track every row background below, or the node shows a mismatched halo.
   `--msg-row-bg` lets a host override the surface the rows sit on (the share
   TOC rail uses --bg-primary; the in-app drawer keeps --bg-secondary). */
.msg-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-6);
  padding: 8px var(--space-4) 8px 12px;
  border-radius: var(--radius-lg);
  cursor: pointer;
  transition: background var(--duration-base) ease;
  -webkit-tap-highlight-color: transparent;
  --rail-punch: var(--msg-row-bg, var(--bg-secondary));
}

/* Timeline rail — one continuous 2px line through the node centres. Each row
   draws a SOLID segment so consecutive rows join seamlessly (a per-row gradient
   would fade the line at every row boundary, which reads as a broken rail).
   Only the first/last rows are trimmed, so the rail starts and ends on a node
   instead of dangling past the list. */
.msg-item::before {
  content: '';
  position: absolute;
  left: 22px;
  top: 0;
  bottom: 0;
  width: 2px;
  background: color-mix(in srgb, var(--accent-color) 26%, transparent);
}

.msg-item:first-child::before {
  top: 50%;
}

.msg-item:last-child::before {
  bottom: 50%;
}

/* A lone row has no rail to draw between nodes. */
.msg-item:first-child:last-child::before {
  display: none;
}

@media (hover: hover) {
  .msg-item:hover {
    border-radius: 0;
    --rail-punch: color-mix(in srgb, var(--text-primary) 5%, var(--msg-row-bg, var(--bg-secondary)));
    background: var(--rail-punch);
  }
  .msg-item:hover .msg-node {
    background: color-mix(in srgb, var(--accent-color) 16%, var(--msg-row-bg, var(--bg-secondary)));
    border-color: color-mix(in srgb, var(--accent-color) 34%, transparent);
  }
}

.msg-item:active {
  opacity: var(--opacity-soft);
}

.msg-item.active {
  border-radius: 0;
  --rail-punch: color-mix(in srgb, var(--accent-color) 10%, var(--msg-row-bg, var(--bg-secondary)));
  background: var(--rail-punch);
  box-shadow: inset 3px 0 0 var(--accent-color);
}

.msg-item-active {
  --rail-punch: color-mix(in srgb, var(--text-primary) 7%, var(--msg-row-bg, var(--bg-secondary)));
  background: var(--rail-punch);
}

/* ── Timeline node (number badge) ── */
.msg-node {
  position: relative;
  z-index: 1;
  flex-shrink: 0;
  width: 22px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--msg-row-bg, var(--bg-secondary));
  border: 1.5px solid color-mix(in srgb, var(--text-muted) 45%, transparent);
  box-shadow: 0 0 0 3px var(--rail-punch);
  transition: background var(--duration-base), border-color var(--duration-base), color var(--duration-base);
}

.msg-item.active .msg-node {
  background: var(--accent-color);
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px var(--rail-punch);
}

.msg-index {
  font-size: 10px;
  font-weight: var(--font-weight-bold);
  color: var(--text-secondary);
  line-height: 1;
  transition: color var(--duration-base);
}

.msg-item.active .msg-index {
  color: #fff;
}

.msg-item.active .msg-text {
  color: var(--accent-color, #0066cc);
}

/* ── Message body (single line: role chip · preview · time) ── */
.msg-body {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  min-width: 0;
}

/* Role chip: an icon-only square, sized to match the timeline node so the two
   columns of the row line up. The role name lives in title/aria-label. */
.msg-role-tag {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  border-radius: var(--radius-sm);
  border: 1px solid transparent;
}

.msg-role-tag.role-user {
  color: var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  border-color: color-mix(in srgb, var(--accent-color) 24%, transparent);
}

.msg-role-tag.role-assistant {
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--text-secondary) 12%, transparent);
  border-color: color-mix(in srgb, var(--text-secondary) 24%, transparent);
}

/* The row's only flexible element: everything past one line is clipped with an
   ellipsis, so every row keeps the same height regardless of message length. */
.msg-text {
  flex: 1 1 auto;
  min-width: 0;
  font-size: var(--font-size-md);
  color: var(--text-primary);
  line-height: var(--line-height-normal);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Placeholder rows (an assistant turn with no text) read as secondary. */
.msg-text--muted {
  color: var(--text-muted);
  font-style: italic;
}

/* Assistant rows use a lighter node outline so the two roles are
   distinguishable even when the row is not the active one. */
.msg-item--assistant .msg-node {
  border-color: color-mix(in srgb, var(--text-muted) 28%, transparent);
}

.msg-text :deep(mark) {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
  color: inherit;
  border-radius: var(--radius-xs);
  padding: 0 1px;
}

.msg-time {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  flex-shrink: 0;
  font-size: 10.5px;
  color: var(--text-muted, #999);
  line-height: 1;
  letter-spacing: 0.2px;
}

.msg-time::before {
  content: '';
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: var(--border-color);
}
</style>

<style>
/* Dark theme override — non-scoped for the [data-theme] selector. Softer mark
   fill keeps highlighted query text readable on dark backgrounds. */
[data-theme-base="dark"] .msg-text mark {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 28%, transparent);
  color: inherit;
}
</style>

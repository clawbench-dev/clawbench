<template>
  <div class="chat-message" :class="[msg.role, { 'has-metadata': msg.role === 'assistant' && msg.metadata }]" :data-msg-key="msg.id ? 'db-' + msg.id : null" :data-streaming="msg.streaming ? 'true' : null">

    <div v-if="msg.role === 'system'" class="chat-system-row">{{ msg.content }}</div>
    <template v-else>
    <!-- Group-chat speaker attribution: a member (not the user) produced this
         assistant message. agentId is the member row id, resolved via the
         speaker resolver prop. Sits OUTSIDE the bubble, at the row's top-left,
         above the message box.

         When the host routes the discussion it @-mentions its targets. The
         "@" sigil is NOT part of the pill: it sits OUTSIDE the chip, which
         carries only the icon + agent name. The chips stay left-aligned, right
         after the speaker label — the bubble stays purely the host's prose. -->
    <div v-if="msg.role === 'assistant' && speaker" class="msg-speaker" :class="{ 'msg-speaker-host': isHostMessage }">
      <AgentIcon :backend="speaker.backend" :name="speaker.name" :avatar="speaker.avatar" size="lg" />
      <span class="msg-speaker-name">{{ speaker.name }}</span>
      <span v-if="isHostMessage" class="msg-speaker-host-tag"><Crown :size="11" class="msg-speaker-host-crown" />{{ t('group.host') }}</span>
    </div>

    <!-- Mention summary row: a one-line "本条 @ 了 B、C" above the bubble, for
         BOTH host and free modes. The mentions also render INLINE in the body
         (as @name chips, see renderMentionChips); this row is the scannable
         summary. It shows the raw parsed targets (a routing intent that may
         name a member who has since left must stay visible).
         ASSISTANT-only: the row exists to scan an AGENT's routing at a glance.
         On a user bubble the chips already sit inline in the message the user
         just typed, so a second "@ B" line would only duplicate them. -->
    <div v-if="msg.role === 'assistant' && mentionTargets.length > 0" class="msg-mention-summary">
      <span class="msg-mention-summary-at">@</span>
      <span class="msg-mention-summary-names">{{ mentionTargets.map((s) => s.name).join('、') }}</span>
    </div>

    <!-- Message card (bubble). The meta bar deliberately lives OUTSIDE this
         element so it sits on the panel background for both roles. -->
    <div class="msg-card" :class="{ 'msg-card-host': isHostMessage }">
    <!-- Collapsible content wrapper -->
    <div ref="wrapperRef" class="msg-content-wrapper">
      <FileAttachmentList v-if="msg.role === 'user' && msg.files && msg.files.length > 0 && !hasImagesInContent(msg.content)" :files="msg.files" @file-tag-click="$emit('file-tag-click', $event)" />

      <!-- Message content — unified ContentBlocks rendering for both user and assistant -->
      <ContentBlocks
        v-if="msg.blocks"
        :blocks="msg.blocks"
        :msgId="msg.id"
        :msgIndex="index"
        :sessionId="sessionId"
        :expandedTools="expandedTools"
        :blockTasks="blockTasks"
        :blockAskQuestions="blockAskQuestions"
        :streaming="msg.streaming"
        :startedAt="msg.createdAt"
        :cancelled="msg.cancelled"
        :summary="msg.summary"
        :summaryCards="msg.summaryCards"
        :showingSummary="showSummary"
        :renderTextBlock="renderTextBlock"
        :formatToolInput="formatToolInput"
        :toolCallSummary="toolCallSummary"
        :truncate="truncate"
        :getAgentBackend="getAgentBackend"
        :getAgentName="getAgentName"
        :getAgentAvatar="getAgentAvatar"
        :staticBlockCache="staticBlockCache"
        :active="active"
        :readOnly="readOnly"
        :isGroupSession="isGroupSession"
        @toggle-tool="$emit('toggle-tool', $event)"
        @show-tool-detail="$emit('show-tool-detail', $event)"
        @task-card-click="$emit('task-card-click', $event)"
        @send-message="(text, cardKey) => $emit('send-message', text, cardKey)"
        @render-flush="$emit('render-flush')"
        @toggle-summary="$emit('toggle-summary', msg.id)"
        @resume-session="$emit('resume-session', $event)"
        @reset-session="$emit('reset-session', $event)"

      />
    </div>

    <!-- File changes banner — standalone button above toolbar -->
    <button v-if="msg.role === 'assistant' && !msg.streaming && hasFileChanges" class="chat-file-changes-banner" @click="fileChangesDrawer.open()">
      <FileDiff :size="14" />
      <span>{{ t('chat.fileChanges.title') }}</span>
      <span class="chat-file-changes-count count-badge count-badge--md">{{ fileChanges.created.length + fileChanges.modified.length }}</span>
    </button>

    <!-- Cancelled marker: shown after file changes banner, hidden when last block is thinking (shown inline in thinking-header instead) -->
    <div v-if="msg.cancelled && !isLastBlockThinking" class="chat-cancelled-mark">{{ t('chat.contentBlocks.cancelled') }}</div>

    <!-- Host's private notes (密送), at the BOTTOM of the bubble.
         Two forms, deliberately different:
           - a note addressed to the USER is expanded by default, labelled
             "to you" and visually distinct — it carries something meant for the
             reader (e.g. their secret word), so hiding it would be wrong.
           - notes addressed to AI members stay collapsed: the reader can audit
             them, but they are not addressed to the reader.
         The collapsed header does NOT list the target names — knowing WHO got a
         note is itself a hint (in a deduction game, "B got a private note" is
         information). The targets stay visible per entry once expanded.
         Sits INSIDE .msg-card, unlike the @-mention routing chips which live in
         the speaker row above the bubble. -->
    <template v-if="groupRouting.bcc.length > 0">
      <div v-for="(e, i) in bccToUser" :key="'u' + i" class="msg-bcc msg-bcc-user">
        <div class="msg-bcc-header is-static">
          <User :size="12" class="msg-bcc-user-icon" />
          <span class="msg-bcc-title">{{ t('group.bcc.toYou') }}</span>
        </div>
        <div class="msg-bcc-body">
          <div class="msg-bcc-entry-content" v-html="e.html"></div>
        </div>
      </div>
      <div v-if="bccToMembers.length > 0" class="msg-bcc">
        <button
          type="button"
          class="msg-bcc-header"
          :aria-expanded="bccExpanded"
          @click="bccExpanded = !bccExpanded"
        >
          <Lock :size="12" class="msg-bcc-lock" />
          <span class="msg-bcc-title">{{ t('group.bcc.title') }}</span>
          <ChevronDown :size="14" class="msg-bcc-chevron" :class="{ 'is-collapsed': !bccExpanded }" />
        </button>
        <div v-show="bccExpanded" class="msg-bcc-body">
          <div v-for="(e, i) in bccToMembers" :key="i" class="msg-bcc-entry">
            <div class="msg-bcc-entry-targets">{{ t('group.bcc.to') }}: {{ e.targetLabels.join('、') }}</div>
            <div class="msg-bcc-entry-content" v-html="e.html"></div>
          </div>
        </div>
      </div>
    </template>
    </div><!-- /.msg-card -->

    <!-- ── Meta bar (OUTSIDE the bubble, both roles) ──
         Assistant: duration + friendly time on the left, actions on the right.
         User: friendly time + copy + details. Same row layout, same styles —
         only which actions are meaningful differs. -->
    <div v-if="showMetaBar" class="chat-meta-bar" :class="msg.role === 'user' ? 'chat-meta-bar-user' : 'chat-meta-bar-assistant'">
      <span class="chat-meta-info">
        <span v-if="msg.role === 'assistant' && msg.metadata?.wallMs" class="chat-meta-duration">{{ formatDuration(msg.metadata.wallMs) }}</span>
        <span v-if="relativeTime" class="chat-meta-time" :class="{ 'chat-meta-sep': msg.role === 'assistant' && msg.metadata?.wallMs }">{{ relativeTime }}</span>
      </span>
      <div class="chat-meta-actions">
        <!-- Summary/original toggle — deliberately FIRST in the row. It is the
             reading-mode control for this message (which view of the reply you
             are looking at), so it leads; everything after it is an action *on*
             the message. Assistant-only, and hidden while streaming since there
             is no settled content to toggle yet. -->
        <template v-if="msg.role === 'assistant'">
          <span v-if="!readOnly && !msg.streaming" ref="toggleWrapRef" class="chat-summary-anchor">
            <SummaryToggle v-if="!msg._summarizing" mode="button" :showing-summary="showSummary" i18n-prefix="chat.message" @toggle="handleToggleSummary" />
            <LoadingIndicator v-else size="sm" inline />
          </span>
          <span v-if="msg._loadingOriginal" class="chat-summary-anchor">
            <LoadingIndicator size="sm" inline />
          </span>
        </template>
        <!-- Quote this message as a whole. Rendered for BOTH roles: quoting a
             user message (e.g. to re-ask about it) is as useful as quoting a
             reply. Gated like the rest of the meta bar, and skipped for queued
             bubbles which have no settled content yet. -->
        <button
          v-if="!readOnly && !msg.streaming && quotableText"
          class="chat-action-btn"
          :title="t('quoteBar.quoteMessage')"
          :aria-label="t('quoteBar.quoteMessage')"
          @click="$emit('quote-message', msg)"
        >
          <MessageSquareQuote :size="14" />
        </button>
        <template v-if="msg.role === 'assistant'">
          <button v-if="speakableText && !readOnly" ref="speakBtnRef" class="chat-action-btn chat-speak-btn" :class="{ 'chat-action-btn--wide': autoSpeech.isActive(msg.id), active: autoSpeech.isActive(msg.id), loading: autoSpeech.isGeneratingText(msg.id) }" :title="speakBtnLabel" :aria-label="speakBtnLabel" @click.stop="handleSpeak">
            <!-- Generating states: summarizing / synthesizing -->
            <template v-if="autoSpeech.isGeneratingText(msg.id)">
              <Clock :size="14" class="speak-spinner" />
              <span>{{ autoSpeech.getPhaseLabel(msg.id) ? t('chat.speech.' + autoSpeech.getPhaseLabel(msg.id)) : '' }}</span>
            </template>
            <!-- Playing state -->
            <template v-else-if="autoSpeech.isPlayingAudio(msg.id)">
              <Pause :size="14" />
              <span>{{ t('chat.message.speaking') }}</span>
            </template>
            <!-- Default idle state -->
            <template v-else>
              <Volume2 :size="14" />
            </template>
          </button>
        </template>
        <CopyButton
          v-if="!readOnly && !msg.streaming && (msg.role === 'assistant' || copyableUserText)"
          :text="copyPayload"
          title-key="chat.message.copy"
          class="chat-action-btn"
        />
        <template v-if="msg.role === 'assistant'">
          <button v-if="!readOnly && !msg.streaming && !hideSessionActions && !isGroupSession" class="chat-action-btn" :class="{ 'is-forking': isForking }" :disabled="isForking" @click="$emit('fork-from-message', msg)" :title="isForking ? t('chat.busy.forking') : t('chat.actions.forkSession')">
            <LoadingIndicator v-if="isForking" size="sm" inline />
            <Split v-else :size="14" />
          </button>
          <button
            v-if="!readOnly && !msg.streaming && !hideSessionActions && !isGroupSession"
            class="chat-action-btn"
            :disabled="isLastMessage"
            :title="isLastMessage ? t('chat.session.nothingToRewind') : t('chat.actions.rewindSession')"
            @click="$emit('rewind-from-message', msg)"
          >
            <Rewind :size="14" />
          </button>
        </template>
        <button v-if="!readOnly && !msg.streaming" class="chat-action-btn" @click="$emit('show-metadata', msg)" :title="t('chat.message.viewDetails')">
          <Info :size="14" />
        </button>
      </div>
    </div>
    </template>

    <!-- File changes sheet -->
    <FileChangesDrawer
      :open="fileChangesDrawer.effectiveOpen.value"
      :created="fileChanges.created"
      :modified="fileChanges.modified"
      @close="fileChangesDrawer.close()"
      @open-file="handleOpenFilePayload"
      @select-file="handleSelectFile"
    />

    <!-- File diffs drill-down sheet (all Write/Edit diffs for one file) -->
    <FileDiffsDrawer
      :open="fileDiffsDrawer.effectiveOpen.value"
      :file-path="selectedFile?.path || ''"
      :tool-name="selectedFile?.toolName || ''"
      :blocks="msg.blocks || []"
      :msg-id="msg.id"
      :tool-ids="selectedFile?.toolIds || []"
      :session-id="sessionId"
      :format-tool-input="formatToolInput"
      @close="fileDiffsDrawer.close()"
      @file-open="handleOpenFilePayload"
      @back="handleFileDiffsBack"
    />
  </div>
</template>

<script setup>
import { ref, inject, computed, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Clock, Pause, Volume2, Info, FileDiff, Split, Rewind, MessageSquareQuote, ChevronDown, Lock, User, Crown } from 'lucide-vue-next'
import { formatDuration, formatRelativeTime } from '@/utils/format.ts'
import { escapeHtml } from '@/utils/html.ts'
import { extractSpeakableText } from '@/composables/useAutoSpeech.ts'
import { extractFileChanges } from '@/utils/chatStreamUtils.ts'
import { parseGroupRouting, stripGroupProtocolTags, GROUP_USER_TARGET_NAME, resolveMentionDisplayName } from '@/utils/groupRouting.ts'
import { quotableMessageText } from '@/utils/quoteItem.ts'
import { isShowingSummary, normalizeDisplayMode } from '@/utils/chatSessionUtils.ts'
import { localConfig } from '@/composables/useSettingsConfig'
import { openFilePath } from '@/composables/useFilePathAnnotation.ts'
import { store } from '@/stores/app.ts'
import ContentBlocks from './ContentBlocks.vue'
import FileAttachmentList from './FileAttachmentList.vue'
import FileChangesDrawer from './FileChangesDrawer.vue'
import FileDiffsDrawer from './FileDiffsDrawer.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import SummaryToggle from '@/components/common/SummaryToggle.vue'
import CopyButton from '@/components/common/CopyButton.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'

const { t } = useI18n()

const props = defineProps({
  msg: Object,
  index: Number,
  expandedTools: Object,
  blockTasks: Object,
  blockAskQuestions: Object,
  agents: Array,
  staticBlockCache: Object,
  active: { type: Boolean, default: true },
  /** True when this message is the most recent assistant reply in the list (drives the 'mixed' display mode). */
  isLastAssistant: { type: Boolean, default: false },
  /** True when this message is the very last entry in the rendered list — rewind
   *  has nothing to truncate after it, so the rewind button is disabled. */
  isLastMessage: { type: Boolean, default: false },
  /** Read-only hosts (task execution detail) hide the fork/rewind pair: those
   *  two actions need a live session to branch or truncate, which a finished
   *  execution record does not have. The rest of the bar (summary toggle,
   *  speak, copy, details) stays useful there. */
  hideSessionActions: { type: Boolean, default: false },
  /** Group-chat session: fork/rewind operate on ONE agent's history, but the
   *  group timeline is an aggregation of several members' sessions, so both are
   *  meaningless (and broken) here. Also forwarded to ContentBlocks to suppress
   *  its reset-session button. */
  isGroupSession: { type: Boolean, default: false },
  /** Message id whose fork button is mid-flight. The fork POST can take
   *  seconds (it copies the whole history), so the clicked button swaps to a
   *  spinner and disables — without it the click reads as a no-op. */
  forkingMessageId: { type: [Number, String], default: null },
  /** Public share page: the viewer is anonymous, so every per-message action
   *  is suppressed — speak/fork/rewind need a live session or auth, copy/
   *  details duplicate what the snapshot already renders (details also exposes
   *  token/cost metadata), and quote needs a chat composer the reader does not
   *  have. The summary/original switch is suppressed too: it is an app-side
   *  reading preference, and a read-only transcript has no business offering a
   *  choice the reader cannot evaluate (the snapshot always carries the original
   *  blocks; `showSummary` falls back to the summary only when there are none).
   *  Only the timestamp line is kept — it is information, not a control. */
  readOnly: { type: Boolean, default: false },
  /** Resolves a group-chat speaker (msg.agentId = member row id) to
   *  { name, backend }. Returns null outside a group / when unresolved. */
  resolveSpeaker: { type: Function, default: null },
  /** Resolves a member DISPLAY NAME (a routing target) to
   *  { name, backend, avatar } for @-mention chips. */
  resolveSpeakerByName: { type: Function, default: null },
  /** Host member row id, so the host's messages get a distinct style. */
  hostMemberId: { type: String, default: '' },
  /** The configured group-chat user nickname (chat.user_nickname). Renders the
   *  reserved human target and is what an @chip shows for the reader. Defaults
   *  to the built-in reserved name so callers that predate the setting work. */
  userNickname: { type: String, default: GROUP_USER_TARGET_NAME },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-metadata', 'file-tag-click', 'task-card-click', 'send-message', 'render-flush', 'toggle-summary', 'ensure-content', 'resume-session', 'fork-from-message', 'rewind-from-message', 'reset-session', 'quote-message'])

// Group-chat speaker for this message (null for user/single-agent messages).
const speaker = computed(() => {
  const id = props.msg?.agentId
  if (!id || typeof props.resolveSpeaker !== 'function') return null
  return props.resolveSpeaker(id)
})
const isHostMessage = computed(() => !!props.msg?.agentId && props.msg.agentId === props.hostMemberId)

// Group-chat mention parsing. In BOTH modes an agent's speech may carry
// <clawbench-mention> tags: host mode's routing, free mode's @-relay. A USER
// message carries them too — the user's @ cards serialize to the same tags on
// send, including `private` ones (密送). The tags are rendered inline as @name
// chips by renderMentionChips; this parse feeds the private-note card, and (for
// agent speech) the summary row. Unparseable tags are NOT stripped from the
// body (parseGroupRouting never mutates text).
//
// Agent speech keeps its original gate (a resolved speaker): outside a group
// `resolveSpeaker` is null, so a single-agent reply never parses. A USER row has
// no agentId, so it is admitted only in a GROUP session — that is what lets the
// reader audit the private notes they sent. Without the user branch the parse
// yields nothing and the note card is silently missing (C3).
const groupRouting = computed(() => {
  const empty = { found: false, speakers: [], instruction: '', before: '', after: '', bcc: [], mentions: [], end: false, raw: '' }
  const isUserInGroup = props.isGroupSession && props.msg?.role === 'user'
  if (!speaker.value && !isUserInGroup) return empty
  return parseGroupRouting(groupProtocolText.value || '')
})

/**
 * The RAW protocol text of this message, for BOTH roles: an agent's speech
 * (extractSpeakableText, which skips tool/thinking noise) or a user message's
 * own text. Unlike `msgText` (assistant-only), this feeds the mention parse so
 * a user's private notes are parsed and rendered in their own bubble.
 */
const groupProtocolText = computed(() => {
  if (props.msg?.role === 'user') {
    return extractSpeakableText(props.msg?.blocks || []) || (props.msg?.content || '')
  }
  return msgText.value || ''
})

// Private notes (密送) addressed to AI members are collapsed by default — the
// user can audit them, but they are not addressed to the reader.
const bccExpanded = ref(false)

// bccToUser / bccToMembers split the notes by audience and are declared BELOW
// the chatRender destructuring, because each note's body is rendered through the
// same markdown pipeline the bubble uses (see renderBccNote).

// mentionTargets maps the parsed mention targets to display info for the
// summary row. Names that no longer resolve (removed members) still render as a
// plain @name so the routing intent stays visible. The reserved human target is
// shown as the configured nickname (chat.user_nickname), matching what the
// agents actually wrote.
const mentionTargets = computed(() => {
  const resolve = props.resolveSpeakerByName
  return groupRouting.value.speakers.map((name) => {
    const hit = typeof resolve === 'function' ? resolve(name) : null
    if (hit) return name === props.userNickname ? { ...hit, name: props.userNickname } : hit
    return { name: name === props.userNickname ? props.userNickname : name, backend: '', avatar: '' }
  })
})

const autoSpeech = inject('autoSpeech')
const wrapperRef = ref(null)
const speakBtnRef = ref(null)
const toggleWrapRef = ref(null)

/** True while THIS message's fork is in flight (id match against the parent's
 *  in-flight marker). Drives the button's spinner + disabled state. */
const isForking = computed(() => props.forkingMessageId != null && String(props.forkingMessageId) === String(props.msg?.id))

// ── Summary/original toggle scroll anchoring ──
// The toggle button sits in the bottom meta bar, BELOW the message content.
// Switching summary↔original changes the content height, which would push/pull
// the button vertically and force the user to scroll to find it again. Instead
// we record the button's viewport top + the scroll container's scrollTop before
// toggling, then after the re-render adjust scrollTop so the button stays pinned
// to the exact same screen position.
function handleToggleSummary() {
  const wrap = toggleWrapRef.value
  const scroller = wrap?.closest('.chat-messages')
  const anchor = wrap?.getBoundingClientRect().top
  const baseScroll = scroller?.scrollTop ?? 0
  if (!wrap || !scroller || anchor == null) {
    emit('toggle-summary', props.msg?.id)
    return
  }
  // Guard: if the user has manually scrolled since toggling, stop re-anchoring
  // so we don't fight the user's scroll gesture.
  const toggleStartScroll = scroller.scrollTop
  const SCROLL_DRIFT_GUARD = 20
  const adjust = () => {
    if (Math.abs(scroller.scrollTop - toggleStartScroll) > SCROLL_DRIFT_GUARD) return
    const newTop = wrap.getBoundingClientRect().top
    scroller.scrollTop = baseScroll + (newTop - anchor)
  }
  emit('toggle-summary', props.msg?.id)
  nextTick(adjust)
  // The original view may load lazily (content fetched after nextTick), resizing
  // the message later. Watch the content wrapper briefly and keep re-anchoring
  // until the height settles.
  const contentEl = wrapperRef.value
  if (contentEl && typeof ResizeObserver !== 'undefined') {
    let settled = false
    const ro = new ResizeObserver(() => { if (!settled) adjust() })
    ro.observe(contentEl)
    setTimeout(() => { settled = true; ro.disconnect() }, 600)
  }
}

// Extract text content from message blocks for TTS.
// Uses extractSpeakableText to include AskUserQuestion blocks.
// Falls back to the summary text when blocks are empty (summary-first loading
// strips content), so the read-aloud button stays available in summary view.
// Global display mode: 'summary' | 'original' | 'mixed'. In 'mixed' the LAST
// assistant reply renders full text (behaves like 'original'), older messages
// render their summaries (behaves like 'summary').
const displayMode = computed(() => normalizeDisplayMode(localConfig.messageDisplayMode))

const msgText = computed(() => {
  if (props.msg?.role !== 'assistant') return ''
  const text = extractSpeakableText(props.msg?.blocks || [])
  if (text) return text
  if (showSummary.value && props.msg?.summary) return props.msg.summary
  return ''
})

// The text the user READS/HEARS: the same as msgText, minus any private notes
// (and with public mentions unwrapped to plain prose). A private note is visible
// only in the collapsed card; it must not be read aloud, copied, quoted, or
// counted as message content. `msgText` itself stays RAW because `groupRouting`
// parses the note out of it.
//
// Only a GROUP session carries the protocol, so the strip is gated on it: a
// single chat's reply that merely DISCUSSES the tag syntax must not be
// truncated by a literal (unclosed) tag in its prose (regression, msg 58879).
const speakableText = computed(() => (props.isGroupSession ? stripGroupProtocolTags(msgText.value) : msgText.value))

// Friendly relative timestamp shown in the meta bar for BOTH roles.
// formatRelativeTime returns '' for missing/invalid dates (including Go zero-value
// times), so the label and its separator stay hidden when there is nothing to show.
const relativeTime = computed(() => (props.msg?.createdAt ? formatRelativeTime(props.msg.createdAt) : ''))

// Copyable text for a user message — the raw content (blocks are the same text
// for user rows; `content` survives summary-stripped payloads).
const copyableUserText = computed(() => {
  if (props.msg?.role !== 'user') return ''
  return extractSpeakableText(props.msg?.blocks || []) || (props.msg?.content || '')
})

// Meta bar visibility. Assistant keeps its old gate (it owns the action bar);
// user messages show it as soon as there is a timestamp or copyable text.
const showMetaBar = computed(() => {
  if (props.msg?.role === 'assistant') {
    return !props.msg.streaming && !!(speakableText.value || props.msg.blocks?.length || props.msg.summary)
  }
  if (props.msg?.role !== 'user' || props.msg.streaming) return false
  return !!(relativeTime.value || copyableUserText.value)
})

/**
 * The text a "quote this message" action would capture — deliberately the SAME
 * text the user reads, not the raw blocks:
 *   - assistant: `msgText` (extractSpeakableText, which skips tool/thinking noise);
 *   - user: the message content.
 * Empty means there is nothing worth quoting, and the button is hidden.
 */
// Shared with ChatPanelContent's quote handler so both entries strip the host's
// private notes identically (a quote feeds a member's injected context). Only a
// group session has the protocol, so the strip is gated on it (a single chat's
// prose must not be truncated by a literal tag mentioned in discussion).
const quotableText = computed(() => quotableMessageText(props.msg?.role, props.msg?.blocks, props.msg?.content, props.msg?.summary, props.isGroupSession))

// Accessible name/tooltip for the read-aloud button. While audio is playing the
// button acts as a stop control, so it must not advertise "read aloud".
const speakBtnLabel = computed(() => {
  const id = props.msg?.id
  if (autoSpeech.isPlayingAudio(id)) return t('chat.message.speaking')
  if (autoSpeech.isGeneratingText(id)) {
    const phase = autoSpeech.getPhaseLabel(id)
    return phase ? t('chat.speech.' + phase) : t('chat.message.readAloud')
  }
  return t('chat.message.readAloud')
})

// Whether to render the summary view. Computed from message state (summary
// exists, content stripped), the user's explicit preference, and the global
// default display mode. While the full text is being lazily fetched in
// original (or mixed-last-assistant) view, keep showing the summary as a
// placeholder so the message bubble is never blank.
const showSummary = computed(() => {
  if (!props.msg) return false
  // readOnly (public share page) always renders the ORIGINAL content: the
  // snapshot never strips blocks, and a read-only transcript has no business
  // offering a summary/original switch the reader cannot evaluate.
  //
  // Never render blank, though: a message with a summary but no blocks (rare —
  // the agent emitted metadata only) still shows its summary.
  if (props.readOnly) {
    const hasBlocks = !!props.msg.blocks?.length
    const hasSummary = props.msg.summary != null && props.msg.summary !== ""
    return !hasBlocks && hasSummary
  }
  return isShowingSummary(props.msg, displayMode.value, { isLastAssistant: props.isLastAssistant })
})

// A summarized message whose content was stripped by the backend has nothing
// to render in original view — request the full text once. Only applies when
// this message is actually rendered as original: global 'original' mode, or
// 'mixed' mode where this is the most recent assistant reply. Older messages
// in 'mixed' show summaries and must NOT trigger lazy fetches (that would pull
// the full content of every summarized message on screen).
// Guarded by _loadingOriginal, blocks-present, and _loadAttempted (latch set
// after the first fetch completes) to fire exactly once even on failure.
const wantsOriginal = computed(() => displayMode.value === 'original' || (displayMode.value === 'mixed' && !!props.isLastAssistant))
const needsLazyOriginal = computed(() =>
  wantsOriginal.value &&
  props.msg?.summary != null && props.msg.summary !== '' &&
  (!props.msg.blocks || props.msg.blocks.length === 0) &&
  props.msg.showingSummary === undefined &&
  props.msg._loadingOriginal !== true &&
  props.msg._loadAttempted !== true
)

watch(needsLazyOriginal, (needs) => {
  if (needs && props.msg) emit('ensure-content', props.msg)
}, { immediate: true })

// Handle speak button click: play or stop (no popover)
function handleSpeak() {
  if (autoSpeech.isActive(props.msg?.id)) {
    autoSpeech.stopAudio()
  } else if (speakableText.value && props.msg?.id) {
    autoSpeech.speakText(props.msg.id, speakableText.value)
  }
}

const chatRender = inject('chatRender', {})
const chatSession = inject('chatSession', {})

const { renderTextBlock, toolCallSummary, formatToolInput, truncate, hasImagesInContent } = chatRender
const { getAgentBackend, getAgentName, getAgentAvatar } = chatSession
const sessionId = computed(() => chatSession.sessionId?.() || '')

// Private-note (密送) card bodies render through the SAME markdown pipeline as
// the bubble body, so an agent that formats its note (bold, lists, inline code,
// a table, a path) gets it rendered instead of shown as literal markers. The
// rendered HTML is baked into each entry (e.html) so the template stays a plain
// v-html. `renderTextBlock` is injected; when absent (a bare host that provides
// no chatRender) fall back to an HTML-escaped plain-text body so the note is
// never dropped and never injected raw.
//
// `noteIdx` is passed as the block index but NEGATED: renderTextBlock uses it to
// key the shared blockAskQuestions / scheduled-task side tables (`${msgId}-${i}`),
// and real block indices are 0..n-1. A non-negative index would let a note's
// stray <clawbench-ask-question> tag overwrite the message's own block-0 card;
// a negative index can never collide with a real block.
function renderBccNote(content, noteIdx) {
  if (typeof renderTextBlock !== 'function') return escapeHtml(content)
  // The streaming flag is forwarded so a note revealed mid-turn skips the
  // enhancements (path verification) exactly like the bubble body does, then
  // takes the full pipeline once the turn settles.
  return renderTextBlock(content || '', String(props.msg?.id ?? ''), -(noteIdx + 1), !!props.msg?.streaming)
}

// Split the notes by audience. The user's own notes render expanded and
// labelled "to you"; the rest stay behind a collapsed header whose title
// deliberately omits the target names (who got a note is itself a hint). Each
// entry keeps its raw `content` (for the target label and tests) and gains
// `html` (the rendered body). The split preserves the ORIGINAL bcc index so
// renderBccNote's key stays unique across both lists.
//
// `targetLabels` resolves each raw target for DISPLAY. An agent writes display
// names, but a USER's note carries member ROW IDs (the frontend writes ids) — so
// without resolving, the user's own auditable card would read "发给: <uuid>".
// The resolution order (id then name) mirrors the inline @chips.
const bccEntries = computed(() =>
  groupRouting.value.bcc.map((e, i) => ({
    ...e,
    toUser: e.targets.includes(props.userNickname),
    targetLabels: e.targets.map((target) =>
      resolveMentionDisplayName(target, props.resolveSpeaker, props.resolveSpeakerByName, props.userNickname, props.userNickname),
    ),
    html: renderBccNote(e.content, i),
  })),
)
const bccToUser = computed(() => bccEntries.value.filter((e) => e.toUser))
const bccToMembers = computed(() => bccEntries.value.filter((e) => !e.toUser))

// File changes extraction (Write → created, Edit → modified).
// Uses summaryCards as fallback when blocks are empty (summary-only view).
const fileChanges = computed(() => {
  if (props.msg?.role !== 'assistant' || props.msg.streaming) return { created: [], modified: [] }
  return extractFileChanges(props.msg?.blocks || [], props.msg?.summaryCards)
})
const hasFileChanges = computed(() => fileChanges.value.created.length > 0 || fileChanges.value.modified.length > 0)

/** Whether the last block is a thinking block (avoids duplicate cancelled marker — inline one is shown in thinking-header instead). */
const isLastBlockThinking = computed(() => {
  const blocks = props.msg?.blocks
  if (!blocks || blocks.length === 0) return false
  return blocks[blocks.length - 1].type === 'thinking'
})

const fileChangesDrawer = useTabDrawer('chat')
const fileDiffsDrawer = useTabDrawer('chat')

// File selected for drill-down: { path, toolName: 'Write' | 'Edit', toolIds: string[] }
const selectedFile = ref(null)

function handleSelectFile(payload) {
  selectedFile.value = payload
  fileChangesDrawer.close()
  fileDiffsDrawer.open()
}

function handleFileDiffsBack() {
  fileDiffsDrawer.close()
  fileChangesDrawer.open()
}

// Handles open-file payloads: either a plain path string (from FileChangesDrawer)
// or { path, lineStart, lineEnd } (from FileDiffsDrawer's diff file-open buttons).
function handleOpenFilePayload(payload) {
  const path = typeof payload === 'string' ? payload : payload.path
  const lineStart = typeof payload === 'string' ? undefined : payload.lineStart
  const lineEnd = typeof payload === 'string' ? undefined : payload.lineEnd
  const lineRanges = typeof payload === 'string' ? undefined : payload.lineRanges
  // AI may return absolute paths (e.g. /home/user/project/src/foo.ts).
  // Strip projectRoot prefix so openFilePath doesn't treat them as external.
  const root = store.state.projectRoot
  const relPath = root && path.startsWith(root + '/') ? path.slice(root.length + 1) : path
  if (lineRanges) openFilePath(relPath, lineStart, lineEnd, 'chat', lineRanges)
  else openFilePath(relPath, lineStart, lineEnd, 'chat')
}

/**
 * Payload for the copy button.
 *
 * Identical to `quotableText` above — both answer "the message's own text,
 * role-appropriately". Kept as its own name so the copy button's intent reads
 * at the call site, but it must not diverge from the quote payload: a message
 * that can be quoted but not copied (or vice versa) is a bug.
 */
const copyPayload = quotableText
</script>

<style scoped>
/* ── Message card (the bubble itself) ──
   The meta bar is a SIBLING of this element, so the card owns every bubble
   visual (background, radius, padding, clipping). */
.msg-card {
    padding: var(--space-4) var(--space-6);
    min-width: 0;
    max-width: 100%;
    box-sizing: border-box;
}

/* ── Message content wrapper ── */
.msg-content-wrapper {
  position: relative;
}

/* ── Cancelled marker (shown after file changes banner) ── */
.chat-cancelled-mark {
  display: inline-block;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-xs);
  margin-top: var(--space-2);
}

/* ── File changes banner ── */
.chat-file-changes-banner {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-3) var(--space-5);
    margin-top: var(--space-3);
    border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
    border-radius: var(--radius-xs);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
    color: var(--accent-color, #0066cc);
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-medium);
    cursor: pointer;
    transition: background var(--duration-base), border-color var(--duration-base), box-shadow var(--duration-base);
}

@media (hover: hover) {
  .chat-file-changes-banner:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 16%, transparent);
    border-color: color-mix(in srgb, var(--accent-color, #0066cc) 55%, transparent);
    box-shadow: 0 1px 3px color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}

.chat-file-changes-banner svg {
    flex-shrink: 0;
}

.chat-file-changes-count {
    margin-left: auto;
    font-weight: var(--font-weight-semibold);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
}

/* Chat Meta Bar — duration/time info + message actions.
   Sits OUTSIDE the bubble (sibling of .msg-card), so it always renders on the
   panel background and never inherits the user bubble's white-on-accent text. */
.chat-meta-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: var(--space-2);
    gap: var(--space-3);
    padding: 0 var(--space-6);
    min-width: 0;
}

/* Both roles share the row. The user row is already right-aligned and
   shrink-to-fit (align-items: flex-end on .chat-message.user), so the bar needs
   no alignment of its own — only the shared row layout above. */

.chat-meta-info {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--font-size-xs);
    color: color-mix(in srgb, var(--text-secondary) 70%, transparent);
    min-width: 0;
    overflow: hidden;
}

.chat-meta-sep::before {
    content: '·';
    margin-right: var(--space-3);
}

.chat-meta-duration {
    font-variant-numeric: tabular-nums;
}

.chat-meta-time {
    white-space: nowrap;
}

/* Speak button active state */
.chat-action-btn.active {
    opacity: 1;
    color: var(--accent-color, #0066cc);
}

/* Copy-button feedback: the glyph swap and the success tint both come from the
   shared CopyButton + css/copy-button.css. Do NOT add a `.is-copied` colour
   here — a scoped rule compiles to (0,3,0) and would override the shared tint,
   which is exactly the per-surface drift this unification removed. */

@media (hover: hover) {
  .chat-action-btn.active:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  }
}

/* Meta bar action buttons container */
.chat-meta-actions {
    display: flex;
    align-items: center;
    gap: var(--space-1);
}

/* Wrapper around the summary/original toggle button — used as the scroll anchor */
.chat-summary-anchor {
    display: inline-flex;
    align-items: center;
}

/* Speak button loading spinner animation */
.chat-action-btn.loading .speak-spinner {
    animation: speak-spin 1s linear infinite;
}

/* In-flight fork button. It stays `disabled` so it cannot be clicked twice, but
   the shared :disabled rule mutes it to --opacity-disabled — and the spinner IS
   the feedback that the click registered, so dimming it undercuts the point.
   Restore full opacity here (scoped styles carry a [data-v-*] attribute, so this
   (0,3,0) rule wins over the shared (0,2,0) one) while keeping the muted cursor
   and the native disabled semantics. The spinner itself follows the accent so
   it reads as "working", not as a greyed-out control. */
.chat-action-btn.is-forking {
    opacity: 1;
    cursor: default;
}

.chat-action-btn.is-forking .li-spinner {
    --li-color: var(--accent-color);
}

@keyframes speak-spin {
    to { transform: rotate(360deg); }
}


@media (hover: hover) {
  .chat-meta-bar-user:hover {
    color: var(--text-secondary);
  }
}

/* ── Host private notes (密送), inside the bubble at its bottom ── */
.msg-bcc {
  margin-top: var(--space-3);
  border-top: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
  padding-top: var(--space-2);
}
/* <button> defaults to text-align:center — force left so the row reads as a
   disclosure header, not a centered label. */
.msg-bcc-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: 0;
  border: none;
  background: none;
  text-align: left;
  cursor: pointer;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}
.msg-bcc-lock {
  flex-shrink: 0;
}
.msg-bcc-title {
  font-weight: var(--font-weight-medium, 500);
}
.msg-bcc-chevron {
  flex-shrink: 0;
  transition: transform var(--duration-base) ease;
}
.msg-bcc-chevron.is-collapsed {
  transform: rotate(-90deg);
}
/* A note addressed to the READER (the user): expanded, accented, and visually
   distinct from the collapsed member notes, so "this one is for me" reads at a
   glance. */
.msg-bcc-user {
  margin-top: var(--space-3);
  border-top: none;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  border-left: 3px solid var(--accent-color);
  background: color-mix(in srgb, var(--accent-color) 10%, transparent);
}
.msg-bcc-user .msg-bcc-header.is-static {
  cursor: default;
  color: var(--accent-color);
}
.msg-bcc-user .msg-bcc-title {
  font-weight: var(--font-weight-semibold, 600);
}
.msg-bcc-user-icon {
  flex-shrink: 0;
}
.msg-bcc-body {
  margin-top: var(--space-2);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.msg-bcc-entry {
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
}
.msg-bcc-entry-targets {
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  margin-bottom: var(--space-1);
}
.msg-bcc-entry-content {
  font-size: var(--font-size-sm);
  color: var(--text-primary);
  word-break: break-word;
}
</style>

<style>
/* Chat message - non-scoped for v-html penetration.
   .chat-message is now the row container (bubble + meta bar); the bubble
   visuals live on .msg-card so the meta bar can sit outside them. */
.chat-message {
    display: flex;
    flex-direction: column;
    font-size: var(--font-size-md);
    line-height: var(--line-height-snug);
    min-width: 0;
    word-wrap: break-word;
    word-break: break-word;
    max-width: 100%;
    box-sizing: border-box;
    contain: style;
    /* DO NOT reintroduce `content-visibility: auto` here.
       It was tried (16382ef31) to skip layout/paint for offscreen messages, but
       it makes scrollHeight depend on a per-ELEMENT remembered height: an
       offscreen message is laid out from `contain-intrinsic-size: auto 240px`
       until it has been measured once. Message heights range from ~66px to
       several thousand (code blocks, thinking), so that guess is wildly wrong.

       ChatMessageList remounts the whole list on every structural change
       (`:key="listKey"`, which includes the message COUNT — so every send).
       Remounting creates fresh elements, discarding the remembered heights, so
       at that instant scrollHeight collapses to the estimate. The browser then
       clamps scrollTop up to the shrunken maximum (the "jumps to the middle"
       half) and followToBottom pins against that same wrong height; as the
       browser re-measures, the ResizeObserver backstop re-pins and the view
       lurches down again (the second half). Both hops are instant scrollTop
       writes, so it reads as a flicker rather than a scroll.

       Measured on a real 20-message/12.7MB session: scrollTop 5018 → 4050
       (up 968px) → 5710 (down 1660px). Isolated to this rule — with it
       removed, or with no remount, the jump is 0px. */
}

/* ── Leaked form/XML control wrapping guard ──
   A malformed <clawbench-ask-question> payload (e.g. an <option> without a <question>)
   fails isValidAskContent, so detectAskQuestion() reports not-found and the raw
   XML is NOT stripped — it falls through to markdown, where marked passes the
   tags through and DOMPurify keeps them. Those become REAL form elements in the
   bubble.

   The problem: the UA stylesheet gives <option> `white-space: nowrap` (and
   <select> `pre`). With no way to wrap, one long option label lays out as a
   single line — measured 485px past the bubble for a prose label and 5394px for
   a 600-char URL — and .chat-message.assistant's `overflow: hidden` then clips
   it silently with no way to scroll. Resetting white-space restores normal
   wrapping; overflow-wrap guarantees even an unbreakable token breaks.

   Scoped to .chat-message (both roles) and to exactly the tags DOMPurify
   preserves from a leaked payload. None of them is legitimately rendered as
   content in a chat bubble, so this cannot affect real markdown output. */
.chat-message :is(option, optgroup, select, textarea, label, fieldset, legend, header) {
    white-space: normal;
    overflow-wrap: anywhere;
}

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   ⚠️  CRITICAL — Android WebView GPU Ghost Artifact Fix
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   DO NOT REMOVE this rule. It is the sole fix for a persistent Android
   WebView rendering bug where layout reflow causes GPU compositing
   cross-layer pixel pollution — phantom metadata text (e.g. model name,
   timestamp) from one message appears overlaid on another message.

   Root cause: WebView's GPU compositor incorrectly re-composites adjacent
   layers when a layout reflow occurs (e.g. DOM insertion/removal, height
   changes). This happens ~2s after opening a session when the "all loaded"
   hint's <Transition> leave animation removes a DOM node from .chat-load-area.

   Fix: `will-change: transform` forces each .chat-message into its own
   independent GPU compositing layer. Reflows still happen, but they can no
   longer cause cross-layer pixel contamination.

   Previous attempt (v-if→v-show everywhere) was a whack-a-mole approach
   that was incomplete and lost Transition animations. This single rule
   makes ALL layout reflows harmless in WebView.

   Scoped to [data-app-mode] (WebView only) to avoid unnecessary GPU
   memory overhead on desktop browsers.
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */
:root[data-app-mode] .chat-message {
    will-change: transform;
}

/* ── File attachment in messages ── */
.chat-files {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  gap: var(--space-3);
  margin: var(--space-2) 0;
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.chat-files::-webkit-scrollbar {
  display: none;
}

/* File card: filename pill */
.chat-message .chat-file-tag,
.chat-message .chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  border-radius: var(--radius-sm);
  height: 40px;
  padding:0 var(--space-5);
  font-size: var(--font-size-sm);
  text-decoration: none;
  cursor: pointer;
  transition: opacity var(--duration-base);
  flex-shrink: 0;
  box-sizing: border-box;
}

.chat-message .attachment-filename {
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-message .attachment-filesize {
  font-size: var(--font-size-2xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Image card: square thumbnail */
.chat-message .chat-file-attachment.attachment-image-only {
  width: 40px;
  height: 40px;
  padding: 0;
  overflow: hidden;
  border-radius: var(--radius-sm);
}

.chat-message .attachment-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.chat-file-tag-path {
  font-family: var(--font-mono);
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  overflow-y: hidden;
  white-space: nowrap;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.chat-file-tag-path::-webkit-scrollbar {
  display: none;
}

/* User message: common colors */
.chat-message.user .chat-file-tag,
.chat-message.user .chat-file-attachment {
  color: rgba(255, 255, 255, 0.95);
}

.chat-message.user .chat-file-tag-path,
.chat-message.user .attachment-filename {
  color: rgba(255, 255, 255, 0.95);
}

/* User message: solid border (both upload and ref) */
.chat-message.user .attachment-upload,
.chat-message.user .attachment-ref {
  background: rgba(255, 255, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.35);
}

.chat-message.user .attachment-file-icon {
  background: rgba(0, 0, 0, 0.15);
  border-radius: var(--radius-sm);
  padding: var(--space-1);
}

@media (hover: hover) {
  .chat-message.user .attachment-upload:hover,
  .chat-message.user .attachment-ref:hover,
  .chat-message.user .chat-file-tag:hover {
    background: rgba(255, 255, 255, 0.25);
  }
}

/* Assistant message: common colors */
.chat-message.assistant .chat-file-tag,
.chat-message.assistant .chat-file-attachment {
  color: var(--text-secondary);
}

.chat-message.assistant .chat-file-tag-path,
.chat-message.assistant .attachment-filename {
  color: var(--text-secondary);
}

/* Assistant message: solid border (both upload and ref) */
.chat-message.assistant .attachment-upload,
.chat-message.assistant .attachment-ref {
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
}

@media (hover: hover) {
  .chat-message.assistant .attachment-upload:hover,
  .chat-message.assistant .attachment-ref:hover,
  .chat-message.assistant .chat-file-tag:hover {
    background: var(--bg-secondary);
  }
}

/* ── Row layout (bubble + meta bar) ──
   .chat-message no longer paints a background: it is the flex row that holds
   .msg-card (the bubble) and .chat-meta-bar (outside it). Kept in the global
   block so the role colours and alignment still reach v-html descendants.

   The user row spans the full column (no margin-right) so its meta bar lines up
   with the assistant one. The 10px right inset belongs to the BUBBLE, not the
   row — keeping it on the row would shift the meta bar inward and misalign the
   two rows' right edges. */
.chat-message.user {
    color: white;
    align-self: stretch;
    align-items: flex-end;
    max-width: 100%;
}

.chat-message.assistant {
    color: var(--text-primary);
    align-self: stretch;
    align-items: stretch;
    position: relative;
    min-width: 0;
    overflow-wrap: break-word;
}

/* ── The bubble itself ── */
.chat-message.assistant .msg-card {
    background: var(--bg-tertiary);
    border-radius: 0;
    overflow: hidden;
}

.chat-message.user .msg-card {
    background: var(--user-msg-color);
    border-radius: 20px 20px 0 20px;
    margin-right: var(--space-5);
    max-width: calc(100% - 20px);
    overflow: hidden;
}

.chat-message.user pre {
    padding: var(--space-5);
    margin: var(--space-3) 0;
    border-radius: var(--radius-sm);
    overflow-x: auto;
    max-width: 100%;
    box-sizing: border-box;
    word-break: normal;
    word-wrap: normal;
    white-space: pre;
    background: rgba(0, 0, 0, 0.15);
}

.chat-message.user pre code {
    white-space: pre;
    word-break: normal;
}

/* Word-wrap mode: override pre/code white-space from rules above */
.chat-message.user .code-block-wrapper.word-wrap pre {
    overflow: visible;
    white-space: pre-wrap;
}

.chat-message.user .code-block-wrapper.word-wrap pre code {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}

.chat-message.user code {
    font-family: var(--font-mono);
    padding: var(--space-1) var(--space-3);
    font-size: var(--font-size-md);
    background: rgba(0, 0, 0, 0.15);
}

.chat-message.user h1,
.chat-message.user h2,
.chat-message.user h3 {
    margin: var(--space-3) 0 3px;
    font-weight: var(--font-weight-semibold);
}

.chat-message.user h1 { font-size: var(--font-size-2xl); }
.chat-message.user h2 { font-size: var(--font-size-lg); }
.chat-message.user h3 { font-size: var(--font-size-md); }

.chat-message.user p {
    margin: 3px 0;
}

.chat-message.user ul,
.chat-message.user ol {
    margin: var(--space-3) 0;
}

.chat-message.user blockquote {
    margin: var(--space-3) 0;
    padding:5px var(--space-5);
    border-left-color: rgba(255, 255, 255, 0.35);
    background: rgba(0, 0, 0, 0.1);
}

.chat-message.user a {
    word-break: break-all;
    overflow-wrap: break-word;
    color: white;
    text-decoration: underline;
    text-underline-offset: 2px;
}

@media (hover: hover) {
  .chat-message.user a:hover {
    color: rgba(255, 255, 255, 0.85);
  }
}

.chat-message.user img {
    margin: var(--space-3) 0;
}

.chat-message.user hr {
    margin: var(--space-4) 0;
    border-top-color: rgba(255, 255, 255, 0.25);
}

.chat-message.user .table-wrap {
    overflow-x: auto;
    border: none;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    margin: 0.75em 0;
}

.chat-message.user .table-block-wrapper .table-wrap {
    margin: 0;
    border-radius: 0;
}

.chat-message.user table {
    display: block;
    margin: 0;
}

.chat-message.user th {
    font-size: var(--font-size-md);
    color: rgba(255, 255, 255, 0.95);
    background: rgba(0, 0, 0, 0.15);
    border-color: rgba(255, 255, 255, 0.2);
}

.chat-message.user td {
    white-space: nowrap;
    border-color: rgba(255, 255, 255, 0.15);
}

.chat-message.user tr:nth-child(odd) td {
    background: rgba(0, 0, 0, 0.08);
}

.chat-message.user tr:nth-child(even) td {
    background: rgba(0, 0, 0, 0.15);
}

.chat-message.user .chat-file-path {
    background: rgba(0, 0, 0, 0.15);
    color: rgba(255, 255, 255, 0.9);
}

.chat-message.user .chat-file-open-btn {
    color: rgba(255, 255, 255, 0.7);
}

@media (hover: hover) {
  .chat-message.user .chat-file-open-btn:hover {
    color: white;
    background: rgba(255, 255, 255, 0.15);
  }
}
.chat-message.user .chat-file-open-btn.external {
    color: #f0a04b;
}

.chat-message.user .chat-commit-hash-pending {
    color: inherit;
}

.chat-message.user .chat-commit-hash {
    color: rgba(255, 255, 255, 0.9);
}

.chat-message.user .chat-commit-open-btn {
    color: rgba(255, 255, 255, 0.7);
}

@media (hover: hover) {
  .chat-message.user .chat-commit-open-btn:hover {
    color: white;
    background: rgba(255, 255, 255, 0.15);
  }
}

.chat-message.assistant pre {
    padding: var(--space-5);
    margin: var(--space-3) 0;
    border-radius: var(--radius-sm);
    overflow-x: auto;
    max-width: 100%;
    box-sizing: border-box;
    word-break: normal;
    word-wrap: normal;
    white-space: pre;
}

.chat-message.assistant pre code {
    white-space: pre;
    word-break: normal;
}

/* Word-wrap mode: override pre/code white-space from rules above */
.chat-message.assistant .code-block-wrapper.word-wrap pre {
    overflow: visible;
    white-space: pre-wrap;
}

.chat-message.assistant .code-block-wrapper.word-wrap pre code {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}

.chat-message.assistant code {
    font-family: var(--font-mono);
    padding: var(--space-1) var(--space-3);
    font-size: var(--font-size-md);
}

.chat-message.assistant h1,
.chat-message.assistant h2,
.chat-message.assistant h3 {
    margin: var(--space-3) 0 3px;
    font-weight: var(--font-weight-semibold);
}

.chat-message.assistant h1 { font-size: var(--font-size-2xl); }
.chat-message.assistant h2 { font-size: var(--font-size-lg); }
.chat-message.assistant h3 { font-size: var(--font-size-md); }

.chat-message.assistant p {
    margin: 3px 0;
}

.chat-message.assistant ul,
.chat-message.assistant ol {
    margin: var(--space-3) 0;
}

.chat-message.assistant blockquote {
    margin: var(--space-3) 0;
    padding:5px var(--space-5);
}

.chat-message.assistant a {
    word-break: break-all;
    overflow-wrap: break-word;
}

.chat-message.assistant img {
    margin: var(--space-3) 0;
}

.chat-message.assistant hr {
    margin: var(--space-4) 0;
}

.chat-message.assistant .table-wrap {
    overflow-x: auto;
    border: none;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    margin: 0.75em 0;
}

.chat-message.assistant .table-block-wrapper .table-wrap {
    margin: 0;
    border-radius: 0;
}

.chat-message.assistant table {
    display: block;
    margin: 0;
}

.chat-message.assistant th {
    font-size: var(--font-size-md);
    color: var(--text-primary);
}

.chat-message.assistant td {
    white-space: nowrap;
}

/* ── Audio player in chat (non-scoped for v-html penetration) ── */
.chat-message .chat-audio-wrapper {
  margin: var(--space-2) 0;
}

.chat-message .chat-audio-player {
  width: 100%;
  max-width: 280px;
  height: 28px;
  border-radius: var(--radius-sm);
  outline: none;
  display: block;
}

.chat-message .chat-audio-player::-webkit-media-controls-panel {
  padding:0 var(--space-2);
}

.chat-message .chat-audio-player::-webkit-media-controls-play-button {
  margin:0 var(--space-1);
}

.chat-message .chat-audio-player::-webkit-media-controls-current-time-display,
.chat-message .chat-audio-player::-webkit-media-controls-time-remaining-display {
  font-size: var(--font-size-xs);
}

/* ── Video player in chat (non-scoped for v-html penetration) ──
   Same reason as the audio block above: the <video> is injected through
   v-html by convertVideoLinks, so Vue never stamps it with this component's
   scope attribute and a scoped rule can never match it. Without these the
   player falls back to the video's intrinsic resolution and overflows the
   bubble (issue #497). */
.chat-message .chat-video-wrapper {
  margin: var(--space-4) 0;
}

.chat-message .chat-video-player {
  width: 100%;
  max-width: 400px;
  max-height: 225px;
  border-radius: var(--radius-sm);
  outline: none;
  background: #000;
}

/* ── Inline image alignment (non-scoped for v-html penetration) ──
   `chat-img` is stamped on the <img> by rewriteImageUrls, i.e. also injected
   via v-html. Sizing lives in markdown-common.css; this only cancels the
   baseline gap. */
.chat-message .chat-img {
  vertical-align: middle;
}

/* ── Group-chat speaker header (outside the bubble, top-left of the row) ── */
.msg-speaker {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin: 0 0 var(--space-2) var(--space-2);
  font-size: var(--font-size-lg);
  color: var(--text-secondary);
}
.msg-speaker-name {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}
.msg-speaker-host-tag {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  color: var(--accent-color, #0066cc);
  font-size: var(--font-size-xs);
}
.msg-speaker-host-crown {
  flex-shrink: 0;
}
/* The host's bubble is centered with an accent border to read as "chair". */
.msg-card-host {
  border-left: 2px solid var(--accent-color, #0066cc);
}
/* Mention summary row: a one-line "本条 @ 了 B、C" above the bubble, for BOTH
   host and free modes. The mentions also render inline in the body (as
   .msg-mention-chip); this row is the scannable summary. */
.msg-mention-summary {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
  max-width: 100%;
  flex-wrap: wrap;
  /* Align with the speaker row above (same 4px inset): both are the message's
     attribution rows sitting OUTSIDE the bubble, so they share one left edge.
     Without this the row sits flush against the panel edge. */
  margin: 0 0 var(--space-1) var(--space-2);
  font-size: var(--font-size-sm);
  color: var(--accent-color, #0066cc);
}
.msg-mention-summary-at {
  font-weight: var(--font-weight-medium);
}
.msg-mention-summary-names {
  font-weight: var(--font-weight-medium);
  white-space: normal;
  overflow-wrap: anywhere;
}
/* Inline @name chip, injected into the rendered markdown body by
   renderMentionChips (a plain <span>, so DOMPurify keeps it by default). */
.msg-mention-chip {
  display: inline-block;
  padding: 0 var(--space-2);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  color: var(--accent-color, #0066cc);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  line-height: 1.5;
}

/* ── Group-chat system event row (member joined / left) ──
   A centered thin pill: NOT a bubble (no .msg-card, no avatar, no meta bar).
   Global block (not scoped) because it is a shared row style, and because a
   future v-html/markdown surface could reuse the class name. */
.chat-system-row {
  align-self: center;
  max-width: 100%;
  margin: var(--space-2) auto;
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-full, 999px);
  background: color-mix(in srgb, var(--text-muted) 12%, transparent);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-snug);
  text-align: center;
  box-sizing: border-box;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>

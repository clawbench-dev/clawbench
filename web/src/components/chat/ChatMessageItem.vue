<template>
  <div class="chat-message" :class="[msg.role, { 'has-metadata': msg.role === 'assistant' && msg.metadata }]" :data-msg-key="msg.id ? 'db-' + msg.id : null" :data-streaming="msg.streaming ? 'true' : null">

    <div v-if="msg.role === 'system'" class="chat-system-row">
      <Megaphone class="chat-system-icon" :size="13" aria-hidden="true" />
      <span class="chat-system-text">{{ msg.content }}</span>
    </div>
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
      <!-- The avatar is the entry point to the speaking agent's config page.
           Only when it can actually resolve a real agent id AND this is not the
           read-only share page — otherwise it stays a plain (non-interactive)
           icon, so a share viewer never gets a dead button. -->
      <button
        v-if="canOpenAgentConfig"
        type="button"
        class="msg-speaker-btn"
        :title="t('session.configAgent')"
        :aria-label="t('session.configAgent')"
        @click.stop="$emit('open-agent-config', speaker.agentId)"
      >
        <AgentIcon :backend="speaker.backend" :name="speaker.name" :avatar="speaker.avatar" size="lg" />
      </button>
      <AgentIcon v-else :backend="speaker.backend" :name="speaker.name" :avatar="speaker.avatar" size="lg" />
      <span class="msg-speaker-name">{{ speaker.name }}</span>
      <RealAgentNameTag class="msg-speaker-real" :agent-name="speaker.agentName" :display-name="speaker.name" />
      <!-- Auto-cruise completion badge: this speaker declared the goal met
           (<clawbench-goal-satisfied/>) in this message. A per-message fact, so
           it stays on the history bubble permanently (the vote tally resets each
           confirmation round, but the bubble is already-persisted content). -->
      <span v-if="groupRouting.goalSatisfied" class="msg-goal-satisfied" :title="t('group.goalSatisfied')">
        <Check :size="11" class="msg-goal-satisfied-icon" />{{ t('group.goalSatisfied') }}
      </span>
      <span v-if="isHostMessage" class="msg-speaker-host-tag"><Crown :size="11" class="msg-speaker-host-crown" />{{ t('group.host') }}</span>
    </div>

    <!-- Mention summary row: a one-line "本条 @ 了 B、C" above the bubble, for
         BOTH host and free modes. The mentions also render INLINE in the body
         (as @name chips, see renderMentionChips); this row is the scannable
         summary. It shows the raw parsed targets (a routing intent that may
         name a member who is currently disabled must stay visible).
         ASSISTANT-only: the row exists to scan an AGENT's routing at a glance.
         On a user bubble the chips already sit inline in the message the user
         just typed, so a second "@ B" line would only duplicate them. -->
    <div v-if="msg.role === 'assistant' && mentionTargets.length > 0" class="msg-mention-summary">
      <span class="msg-mention-summary-at">@</span>
      <span class="msg-mention-summary-names">{{ mentionTargets.map((s) => s.name).join(t('common.listSeparator')) }}</span>
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
        :role="msg.role"
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

    <!-- Injected-into-running-reply marker (user bubbles only). A mid-turn
         injection that MERGED into the running reply (claude/codex: no split
         boundary) lands AFTER the still-streaming assistant reply with nothing
         following it, which reads as "sent but ignored". This quiet annotation
         explains it. Split-capable backends (CodeBuddy) never set msg.injected,
         so their bubble is unchanged. -->
    <div v-if="msg.role === 'user' && msg.injected" class="chat-injected-mark">
      <CornerDownRight :size="12" aria-hidden="true" />
      <span>{{ t('chat.pending.mergedIntoPrev') }}</span>
    </div>

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
            <div class="msg-bcc-entry-targets">{{ t('group.bcc.to') }}: {{ e.targetLabels.join(t('common.listSeparator')) }}</div>
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
          <span v-if="canUseLocalActions && !msg.streaming && canToggleSummary" ref="toggleWrapRef" class="chat-summary-anchor">
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
          <MessageSquare :size="14" />
        </button>
        <template v-if="msg.role === 'assistant'">
          <button v-if="speakableText && !readOnly" ref="speakBtnRef" class="chat-action-btn chat-speak-btn" :class="{ 'chat-action-btn--wide': autoSpeech.isPlayingAudio(msg.id), active: autoSpeech.isActive(msg.id), loading: autoSpeech.isGeneratingText(msg.id) }" :title="speakBtnLabel" :aria-label="speakBtnLabel" @click.stop="handleSpeak">
            <!-- Generating states carry NO text: the two phases are told apart by
                 the glyph and its motion alone. Summarizing breathes a Sparkles
                 in place (the same glyph SummaryToggle uses for "summary");
                 synthesizing uses the app's one shared loading ring — the same
                 "still working, not playing" language the input bar's voice
                 button uses for its transcribing state. Waveform bars would be
                 wrong here: they already mean "audio is playing" (the input
                 bar's recording state), which is the very state that follows.
                 The phase name is not lost — speakBtnLabel still puts it on
                 title/aria-label, which is where a screen reader reads it. -->
            <template v-if="autoSpeech.isGeneratingText(msg.id)">
              <Sparkles v-if="autoSpeech.getPhaseLabel(msg.id) === 'summarizing'" :size="14" class="speak-breathe" />
              <LoadingIndicator v-else size="sm" inline />
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
          v-if="canUseLocalActions && !msg.streaming && (msg.role === 'assistant' || copyableUserText)"
          :text="copyPayload"
          title-key="chat.message.copy"
          class="chat-action-btn"
        />
        <template v-if="msg.role === 'assistant'">
          <!-- Fork is available in group chats too: the source history is
               carried as injected background rather than copied rows, so the
               member-row ids on the group timeline never reach the new session.
               Rewind stays hidden — truncating a group timeline would leave every
               member's seen_cursor pointing past deleted rows. -->
          <button v-if="!readOnly && !msg.streaming && !hideSessionActions" class="chat-action-btn" :class="{ 'is-forking': isForking }" :disabled="isForking" @click="$emit('fork-from-message', msg)" :title="isForking ? t('chat.busy.forking') : t('chat.actions.forkSession')">
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
        <button v-if="canUseLocalActions && !msg.streaming" class="chat-action-btn" @click="$emit('show-metadata', msg)" :title="t('chat.message.viewDetails')">
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
      :read-only="readOnly"
      @close="fileChangesDrawer.close()"
      @open-file="handleOpenFilePayload"
      @select-file="handleSelectFile"
    />

    <!-- File diffs drill-down sheet (all Write/Edit diffs for one file) -->
    <FileDiffsDrawer
      :open="fileDiffsDrawer.effectiveOpen.value"
      :read-only="readOnly"
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
import { Sparkles, Pause, Volume2, Info, FileDiff, Split, Rewind, MessageSquare, ChevronDown, Lock, User, Crown, Megaphone, CornerDownRight, Check } from 'lucide-vue-next'
import { formatDuration, formatRelativeTime } from '@/utils/format.ts'
import { escapeHtml } from '@/utils/html.ts'
import { extractSpeakableText } from '@/composables/useAutoSpeech.ts'
import { extractFileChanges } from '@/utils/chatStreamUtils.ts'
import { parseGroupRouting, stripGroupProtocolTags, GROUP_USER_TARGET_NAME, resolveMentionDisplayName, stripGoalSatisfiedTag } from '@/utils/groupRouting.ts'
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
import RealAgentNameTag from '@/components/common/RealAgentNameTag.vue'

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
  /** Group-chat session. Fork IS offered here (the source history is injected
   *  as background instead of copied rows); rewind is not, because truncating a
   *  shared timeline would leave every member's seen_cursor pointing past
   *  deleted rows. Also forwarded to ContentBlocks to suppress its
   *  reset-session button. */
  isGroupSession: { type: Boolean, default: false },
  /** Message id whose fork button is mid-flight. The fork POST can take
   *  seconds (it copies the whole history), so the clicked button swaps to a
   *  spinner and disables — without it the click reads as a no-op. */
  forkingMessageId: { type: [Number, String], default: null },
  /** Public share page: the viewer is anonymous, so every action that needs a
   *  live session or auth is suppressed — speak/fork/rewind need a live session
   *  or the authenticated TTS endpoints, and quote needs a chat composer the
   *  reader does not have. Copy / details / the summary-original switch are
   *  local-only (clipboard, already-loaded metadata, a view toggle), so a host
   *  that wants them sets `allowLocalActions`.
   *  Only the timestamp line is kept when this is the sole flag. */
  readOnly: { type: Boolean, default: false },
  /** Read-only host that may still offer the actions which need NEITHER auth
   *  NOR a live session: copy, view-details and the summary/original toggle.
   *  Set by the public share page — a reader can legitimately copy text, read a
   *  message's model/token metadata and pick a reading mode.
   *
   *  Deliberately SEPARATE from `readOnly` so BtwAnswerDrawer and
   *  TaskExecDetail (which pass readOnly for the same anonymity reasons but
   *  have no use for the bar) keep their all-actions-hidden behaviour.
   *  Ignored when `readOnly` is false. */
  allowLocalActions: { type: Boolean, default: false },
  /** Resolves a group-chat speaker (msg.agentId = member row id) to
   *  { name, backend, avatar, agentName }. Returns null outside a group / when
   *  unresolved. `agentName` is the underlying real agent name, set only when it
   *  differs from the display `name` (rendered as a muted suffix). */
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

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-metadata', 'file-tag-click', 'task-card-click', 'send-message', 'render-flush', 'toggle-summary', 'ensure-content', 'resume-session', 'fork-from-message', 'rewind-from-message', 'reset-session', 'quote-message', 'open-agent-config'])

// Group-chat speaker for this message (null for user/single-agent messages).
const speaker = computed(() => {
  const id = props.msg?.agentId
  if (!id || typeof props.resolveSpeaker !== 'function') return null
  return props.resolveSpeaker(id)
})
const isHostMessage = computed(() => !!props.msg?.agentId && props.msg.agentId === props.hostMemberId)

// The speaker header opens the agent's config page. It needs the REAL agent id
// (speaker.agentId) — msg.agentId is the member ROW id, which no config page can
// resolve. Suppressed on the read-only share page (there is no settings tab to
// open) so a share viewer never sees a dead button.
const canOpenAgentConfig = computed(() => !props.readOnly && !!speaker.value?.agentId)

// Actions that work without auth and without a live session: copy, view-details
// and the summary/original toggle. Everything else keeps its own `!readOnly`
// gate (quote needs a composer, speak needs the TTS endpoints, fork/rewind need
// a live session).
const canUseLocalActions = computed(() => !props.readOnly || props.allowLocalActions)

// Whether the summary toggle can do anything.
//
// In the interactive app a message with no summary still gets a button: clicking
// it GENERATES one (ChatPanelContent.handleToggleSummary). A read-only host has
// no such path — SessionShareView's handler returns immediately when
// msg.summary is empty — so showing the button there would be a control that
// looks live and does nothing, the same defect this change set fixes elsewhere.
const canToggleSummary = computed(() => {
  if (!props.readOnly) return true
  return props.msg?.summary != null && props.msg.summary !== ''
})

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
  const empty = { found: false, speakers: [], instruction: '', before: '', after: '', bcc: [], mentions: [], end: false, goalSatisfied: false, raw: '' }
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
//
// Single chat still removes the auto-cruise completion signal — it is a
// self-closing, content-free tag the AI was instructed to emit, never part of
// the reply's prose, so stripping it loses nothing (and it must not be read
// aloud/copied/quoted). stripGoalSatisfiedTag does ONLY that; it never touches
// mention-like prose, so 58879 cannot recur.
const speakableText = computed(() => {
  if (props.isGroupSession) return stripGroupProtocolTags(msgText.value)
  return stripGoalSatisfiedTag(msgText.value)
})

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
  const hasBlocks = !!props.msg.blocks?.length
  const hasSummary = props.msg.summary != null && props.msg.summary !== ""

  // Plain read-only host (btw drawer, task execution detail) always renders the
  // ORIGINAL content: there is no toggle, so nothing else could select the
  // summary. Never render blank, though: a message with a summary but no blocks
  // (rare — the agent emitted metadata only) still shows its summary.
  if (props.readOnly && !props.allowLocalActions) return !hasBlocks && hasSummary

  const showing = isShowingSummary(props.msg, displayMode.value, { isLastAssistant: props.isLastAssistant })
  // A read-only host has no lazy-fetch path, so a summary-only message must never
  // render blank when the reader is on the original view.
  if (props.readOnly && !hasBlocks) return hasSummary
  return showing
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

/* ── Injected-into-running-reply marker (user bubbles) ──
   Quiet, muted annotation: it explains WHY this bubble sits after a reply that
   was still streaming, so it must read as incidental metadata, not a warning.
   Same inline treatment as .chat-cancelled-mark (it sits in .msg-card, a plain
   block, so it stays content-width and left-aligned inside the card). */
.chat-injected-mark {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  padding: 0 var(--space-2);
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
    box-shadow: 0 1px calc(3 * var(--dp)) color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
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

/* Speak button: the "summarizing" phase breathes its glyph in place. This is
   the only channel that distinguishes it from "synthesizing" (which uses the
   shared loading ring), so — like BusyBar and the session status slot — it
   deliberately has NO prefers-reduced-motion opt-out: freezing it would make
   the two phases look identical. The phase name is still available to screen
   readers via the button's title/aria-label. See design-guide.md § 动效. */
.chat-action-btn.loading .speak-breathe {
    animation: speak-breathe 1.6s ease-in-out infinite;
}

@keyframes speak-breathe {
    0%, 100% { opacity: 0.45; transform: scale(0.9); }
    50%      { opacity: 1;    transform: scale(1); }
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
   cross-layer pixel pollution — phantom metadata text from one message
   appears overlaid on another message, or a stale slice of a reply's own
   bubble/meta bar survives below it.

   Root cause: WebView's GPU compositor incorrectly re-composites when a
   layout reflow occurs (DOM insertion/removal, height changes).

   Fix: `contain: paint` bounds every row's paint to its own box, so one
   row's pixels can never land in a neighbour's area. That is the whole
   isolation mechanism — and it is deliberately achieved WITHOUT promoting
   each row to its own compositing layer.

   ⚠️ DO NOT reintroduce `will-change: transform` (or a transform) on this
   rule. It was the previous fix and it backfired: a promoted row is a
   GPU texture whose size changes whenever the row grows, and this WebView
   version redraws the resized texture into the delta instead of repainting
   it. Users saw the result as a thin band of the reply's own pixels squashed
   into the strip just below its action bar (measured 4px = `.chat-meta-bar`'s
   `margin-top`, containing a vertically squashed copy of "1m · 刚刚" and its
   action icons), appearing exactly when a reply finished streaming and the
   meta bar — a new child — grew the row. Every row being a layer also made
   the scroller's edge tiles resize-able, the source of the earlier ~9px
   `--bg-tertiary` band. Keeping rows uncomposited lets the scroller repaint
   the changed region normally; containment alone already prevents the
   cross-row bleeding the layer promotion was originally added for.

   Scoped to [data-app-mode] (native hosts) so desktop browsers, which do
   not have this compositor bug, keep normal paint behaviour.
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */
:root[data-app-mode] .chat-message {
    /* ⚠️ CONTAINMENT CLIPS INJECTED FIXED OVERLAYS. This rule is only safe
       because of the escape hatch below it — do NOT read it as "this row holds
       nothing that overflows". It does: `useCodeBlockHeader.ts` injects a
       `position: fixed` `.table-block-copy-menu` into every markdown table at
       RUNTIME, and paint containment both clips it to the row and makes the row
       its containing block. On a 1–2 row table the menu is taller than the
       table, so its last items (HTML / TSV) landed below the row's bottom edge:
       invisible, unclickable, and the click fell through to the message behind
       it — with no error anywhere.

       The owner of that DOM is NOT this file, so "grep this file for `fixed`"
       is a blind spot: the only way to find such overlays is to grep the
       RENDER pipeline for elements it injects into `v-html` content. Whenever
       one is added, the escape hatch below has to cover it too. */
    contain: paint;
}

/* ── Escape hatch: an open table copy menu needs the clip to be lifted ──
   `contain: paint` implies layout + style containment and makes the row a
   containing block for fixed descendants, so a fixed menu inside the bubble is
   clipped at the row's box — the exact defect the `contain: paint` above
   introduced for short tables.

   Declarative rather than JS-managed on purpose: the menu can be closed from
   half a dozen places (App.vue's outside-click, `useChatMessageInteractions`'
   document listener, the wrap toggle, a code-block click, scroll-out). A row
   class toggled by each of those would leak on any path that forgot to clear
   it, and a leaked class permanently disables the ghost fix. `:has()` cannot
   get out of sync because the menu's own state IS the condition.

   `contain: style` (not `none`): it restores exactly the containment the
   unscoped `.chat-message` rule declares, so only the PAINT clip is dropped.
   `will-change: transform` is re-applied HERE, not on the base rule: the
   fixed menu is positioned against the row only while the row is a containing
   block for fixed descendants, and dropping `contain: paint` would otherwise
   let it jump to the viewport. Scoping the promotion to the open menu keeps
   rows uncomposited (the ghost fix above) for the ~99.9% of the time no menu
   is open, while the containing block's origin is identical in both states —
   verified in Chromium — so the menu does not jump when this rule flips. */
:root[data-app-mode] .chat-message:has(.table-block-copy-menu.is-open) {
    will-change: transform;
    contain: style;
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
  height: calc(40 * var(--dp));
  padding: 0 var(--space-5);
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
  width: calc(40 * var(--dp));
  height: calc(40 * var(--dp));
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

/* ── The bubble itself ──
   Square and full-bleed by default: with the pane at or below the reading
   measure there is no side gutter, and a rounded band touching both edges
   would just look clipped. */
.chat-message.assistant .msg-card {
    background: var(--bg-tertiary);
    border-radius: 0;
    overflow: hidden;
}

/* ── Wide pane: the reply becomes a card ──
   Once the pane outgrows --chat-content-max-width the message column is inset
   from the pane on both sides (see ChatMessageList's `--chat-content-inset`),
   so the grey band ends in mid-air. Square corners there read as a hard-cut
   slab; rounding them makes the edge look deliberate.

   `60rem` IS the measure: `--dp` is 1rem/15, so 900 design px = 60rem, and
   `calc(60rem + 1px)` flips exactly when the gutter stops being zero. Written
   in rem rather than px so the threshold follows the UI scale — verified in
   Chromium, Firefox and WebKit that a fractional/calc rem threshold evaluates
   against the ROOT font size inside a container query.

   The container is `.chat-panel-content` (container-name: chat-pane). Views
   that reuse ChatMessageItem without that ancestor — the share page, the /btw
   drawer — keep the square band, which is right: they are always one fixed
   column wide and have no pane gutter to justify a card. */
@container chat-pane (min-width: calc(60rem + 1px)) {
    .chat-message.assistant .msg-card {
        border-radius: var(--radius-lg);
    }
}

.chat-message.user .msg-card {
    background: var(--user-msg-color);
    border-radius: calc(20 * var(--dp)) calc(20 * var(--dp)) 0 calc(20 * var(--dp));
    margin-right: var(--space-5);
    max-width: calc(100% - calc(20 * var(--dp)));
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
    margin: var(--space-3) 0 calc(3 * var(--dp));
    font-weight: var(--font-weight-semibold);
}

.chat-message.user h1 { font-size: var(--font-size-2xl); }
.chat-message.user h2 { font-size: var(--font-size-lg); }
.chat-message.user h3 { font-size: var(--font-size-md); }

.chat-message.user p {
    margin: calc(3 * var(--dp)) 0;
}

.chat-message.user ul,
.chat-message.user ol {
    margin: var(--space-3) 0;
}

.chat-message.user blockquote {
    margin: var(--space-3) 0;
    padding: calc(5 * var(--dp)) var(--space-5);
    border-left-color: rgba(255, 255, 255, 0.35);
    background: rgba(0, 0, 0, 0.1);
}

.chat-message.user a {
    word-break: break-all;
    overflow-wrap: break-word;
    color: white;
    text-decoration: underline;
    text-underline-offset: calc(2 * var(--dp));
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

/* File-path / commit / worktree annotations inside the user bubble.
   The chip plate, glyph colours and hover plate all come from `currentColor`
   (annotation-buttons.css + markdown-common.css), which is white here — so no
   role-specific colour override is needed, and any fixed one would fight the
   theme: the bubble is a saturated fill whose contrast against white ranges
   from 3.3:1 (ayu-light) to 12:1 (high-contrast-light), so a hard-coded
   white-alpha or `--text-muted` is only ever correct on a few themes.

   The commit hash drops the accent hue here on purpose: tinting it toward the
   bubble's white costs up to 1.5:1 (light-modern), and legibility has to win
   over the "this is a link" hue on a saturated backdrop. The dashed/pointing
   affordance still marks it as actionable. */
.chat-message.user .chat-commit-hash {
    color: inherit;
}

.chat-message.user .chat-commit-hash-pending {
    color: inherit;
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
    margin: var(--space-3) 0 calc(3 * var(--dp));
    font-weight: var(--font-weight-semibold);
}

.chat-message.assistant h1 { font-size: var(--font-size-2xl); }
.chat-message.assistant h2 { font-size: var(--font-size-lg); }
.chat-message.assistant h3 { font-size: var(--font-size-md); }

.chat-message.assistant p {
    margin: calc(3 * var(--dp)) 0;
}

.chat-message.assistant ul,
.chat-message.assistant ol {
    margin: var(--space-3) 0;
}

.chat-message.assistant blockquote {
    margin: var(--space-3) 0;
    padding: calc(5 * var(--dp)) var(--space-5);
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
  max-width: calc(280 * var(--dp));
  height: calc(28 * var(--dp));
  border-radius: var(--radius-sm);
  outline: none;
  display: block;
}

.chat-message .chat-audio-player::-webkit-media-controls-panel {
  padding: 0 var(--space-2);
}

.chat-message .chat-audio-player::-webkit-media-controls-play-button {
  margin: 0 var(--space-1);
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
  max-width: calc(400 * var(--dp));
  max-height: calc(225 * var(--dp));
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
/* The avatar doubles as the entry point to the agent's config page. It wraps
   only the icon, so it must carry no button chrome of its own — the size is the
   icon's, and the whole control is the touch target. Hover raises the icon to
   signal it is interactive. */
.msg-speaker-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  border: none;
  background: none;
  cursor: pointer;
  border-radius: var(--radius-full);
  transition: opacity var(--duration-fast) ease;
}
.msg-speaker-btn:hover {
  opacity: 0.8;
}
.msg-speaker-btn:active {
  opacity: 0.6;
}
/* .msg-speaker-real (the real-agent disclosure chip) is styled by the shared
   RealAgentNameTag component; no override needed here. */
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
/* Auto-cruise completion badge: same shape/scale as the host tag (a small
   inline pill next to the speaker name), tinted with the success token so it
   reads as "done" rather than "role". `flex-shrink:0` keeps it from being
   squeezed when the speaker row wraps. */
.msg-goal-satisfied {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: 0 var(--space-2);
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--color-success, #16a34a) 15%, transparent);
  color: var(--color-success, #16a34a);
  font-size: var(--font-size-xs);
  flex-shrink: 0;
}
.msg-goal-satisfied-icon {
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

/* ── Group-chat system event row (member joined / disabled / enabled) ──
   A centered thin pill: NOT a bubble (no .msg-card, no avatar, no meta bar).
   A single Megaphone glyph marks it as an announcement — the same unified icon
   the conversation index and the share-selection dialog use for system rows.
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
  /* Icon + text on one baseline; the icon is decorative (aria-hidden). */
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.chat-system-icon {
  flex-shrink: 0;
  opacity: var(--opacity-hover, 0.7);
}

.chat-system-text {
  min-width: 0;
}
</style>

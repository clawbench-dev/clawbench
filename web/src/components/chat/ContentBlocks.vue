<template>
  <div class="content-blocks" :class="{ 'content-blocks-nested': nested }" ref="contentRootRef">
    <!-- Summary mode: render summary as a single text block.
         Using v-show for summary to avoid Vue Fragment patching issues when
         switching between v-if/v-else branches with nested template v-for.
         Previously, v-if/v-else with template wrappers caused the v-else
         branch to render as an empty comment node because Vue 3's patch
         algorithm fails to correctly transition between different Fragment
         structures (summary div vs blocks template v-for). -->
    <div v-if="!nested" v-show="showingSummary && summary" v-html="renderTextBlock(summary || '', msgId, 0, false)"></div>
    <!-- Summary mode: render structured cards (tools / tasks / ask-questions)
         directly from summaryCards — NOT by traversing blocks (which may be empty in
         summary-first loading). -->
    <template v-if="!nested && showingSummary && summary">
      <!-- Warning/error banners from summaryCards.warnings — the summary view
           renders with stripped content blocks ({"blocks":[]}), so warning/error
           banners are carried here to stay visible (mirrors file-changes cards). -->
      <template v-for="(w, wi) in summaryWarnings" :key="'sum-warn-' + wi">
        <!-- Error block -->
        <div v-if="w.type === 'error'" class="chat-error-card">
          <AlertTriangle :size="14" class="error-icon" />
          <span class="error-text">{{ getWarningText(w) }}</span>
          <span v-if="errorSourceLabel(w)" class="error-source-chip" :class="'src-' + (w.error_source || '')">{{ errorSourceLabel(w) }}</span>
          <LoginCommandButton v-if="w.login_cmd" :command="w.login_cmd" />
          <!-- 只读宿主（公开会话分享页 / btw 抽屉）不绑定 reset-session，渲染出来就是个死按钮 -->
          <button v-else-if="isResetableReason(w.reason) && !isGroupSession && !readOnly" class="warning-reset-btn" @click.stop="$emit('reset-session', { reason: w.reason })">
            {{ t('chat.contentBlocks.resetSession') }}
          </button>
        </div>
        <!-- Warning block: severe (disconnect/timeout/restart) renders as error-level red -->
        <div v-else-if="w.type === 'warning' && isSevereWarning(w)" class="chat-error-card">
          <AlertTriangle :size="14" class="error-icon" />
          <span class="error-text">{{ getWarningText(w) }}</span>
          <span v-if="errorSourceLabel(w)" class="error-source-chip" :class="'src-' + (w.error_source || '')">{{ errorSourceLabel(w) }}</span>
          <LoginCommandButton v-if="w.login_cmd" :command="w.login_cmd" />
          <!-- 只读宿主（公开会话分享页 / btw 抽屉）不绑定 reset-session，渲染出来就是个死按钮 -->
          <button v-else-if="isResetableReason(w.reason) && !isGroupSession && !readOnly" class="warning-reset-btn" @click.stop="$emit('reset-session', { reason: w.reason })">
            {{ t('chat.contentBlocks.resetSession') }}
          </button>
        </div>
        <!-- Warning block: normal (parse errors, stderr) renders as amber -->
        <div v-else-if="w.type === 'warning'" class="chat-warning-card">
          <AlertCircle :size="14" class="warning-icon" />
          <span class="warning-text">{{ getWarningText(w) }}</span>
          <span v-if="errorSourceLabel(w)" class="error-source-chip" :class="'src-' + (w.error_source || '')">{{ errorSourceLabel(w) }}</span>
          <!-- 继续 = 往会话里发一条消息；只读宿主（分享页）没有 send-message 监听者 -->
          <button v-if="w.reason === 'restart' && !readOnly" class="warning-continue-btn" @click.stop="$emit('send-message', t('chat.contentBlocks.continue'))">
            {{ t('chat.contentBlocks.continue') }}
          </button>
          <LoginCommandButton v-if="w.login_cmd" :command="w.login_cmd" />
          <!-- 只读宿主（公开会话分享页 / btw 抽屉）不绑定 reset-session，渲染出来就是个死按钮 -->
          <button v-else-if="isResetableReason(w.reason) && !isGroupSession && !readOnly" class="warning-reset-btn" @click.stop="$emit('reset-session', { reason: w.reason })">
            {{ t('chat.contentBlocks.resetSession') }}
          </button>
        </div>
      </template>
      <!-- Tool cards from summaryCards.tools (AskUserQuestion only —
           PermissionApproval is filtered out upstream in summaryTools) -->
      <template v-for="(tool, ti) in summaryTools" :key="'sum-tool-' + ti">
        <!-- Ask tool absorbed into the merged summary ask card below (only when it
             actually contributed questions; a malformed ask tool keeps its own card). -->
        <template v-if="isAskQuestion(tool.name || '') && shouldMergeSummaryAskCards && extractAskQuestions(tool.input).length > 0"></template>
        <!-- Answerable interactive tool: unified card
             (header strip + body in one box). -->
        <div v-else-if="shouldAutoExpandTool(tool.name || '') && isUnifiedCardTool(tool.name)" class="tool-detail chat-inline-card" :class="!tool.done ? 'is-pending' : ''" :data-tool-name="tool.name" :data-category="getToolIcon(tool.name).category" @click="handleToolDetailClick" @input="handleToolDetailInput" @keydown="handleToolDetailKeydown">
          <div class="chat-card-strip" @click.stop="handleSummaryToolClick(tool, ti)">
            <component :is="getToolIcon(tool.name).icon" :size="12" class="tool-icon" />
            <span class="tool-name">{{ unifiedCardTitle(tool.name, tool.input, tool.display_name) }}</span>
            <span v-if="toolCallSummary(tool)" class="tool-summary">{{ toolCallSummary(tool) }}</span>
            <!-- Pending spinner: only meaningful when the tool is actually answerable. -->
            <LoadingIndicator v-if="showAskPending(tool)" class="tool-spinner" size="sm" inline />
            <!-- Done: green check -->
            <CheckCircle2 v-else :size="14" color="#22c55e" class="tool-check" />
          </div>
          <div class="chat-card-body">
            <div v-html="formatToolInput(tool.input || {}, tool.name, { done: tool.done, status: tool.status, output: tool.output, askKey: askKeyForSummaryTool(tool) })"></div>
          </div>
        </div>
        <template v-else>
          <!-- All auto-expand tools render as unified cards above, so this
               branch only sees click-to-open pills (opens the detail drawer). -->
          <div class="chat-tool-call done" :data-category="getToolIcon(tool.name).category" @click.stop="handleSummaryToolClick(tool, ti)">
            <component :is="getToolIcon(tool.name).icon" :size="12" class="tool-icon" />
            <span class="tool-name">{{ toolDisplayName(tool.name, tool.input, tool.display_name) }}</span>
            <CheckCircle2 :size="14" color="#22c55e" class="tool-check" />
          </div>
        </template>
      </template>
      <!-- Task cards from summaryCards.taskIDs (task data fetched in real time) -->
      <template v-for="tid in summaryTaskIDs" :key="'sum-task-' + tid">
        <TaskChatCard
          :task="summaryTaskData[tid]?.task"
          :loading="!!summaryTaskData[tid]?.loading"
          :deleted="!!summaryTaskData[tid]?.deleted"
          :get-agent-backend="getAgentBackend"
          :get-agent-name="getAgentName"
          :get-agent-avatar="getAgentAvatar"
          @select="$emit('task-card-click', tid)"
        />
      </template>
      <!-- Ask-question card. Sources: summaryCards.askQuestions (<clawbench-ask-question>
           cards, already aggregated into one array) and AskUserQuestion tool cards
           whose input carries questions. When more than one source exists they are
           merged into this single card so every question is answered with one submit. -->
      <template v-if="summaryAskQuestions.length || shouldMergeSummaryAskCards">
        <div class="tool-detail chat-inline-card done" data-category="ask" data-tool-name="AskUserQuestion" @click="handleToolDetailClick" @input="handleToolDetailInput" @keydown="handleToolDetailKeydown">
          <div class="chat-card-strip" @click.stop="$emit('toggle-tool', 'summary-ask')">
            <component :is="getToolIcon('AskUserQuestion').icon" :size="12" class="tool-icon" />
            <span class="tool-name">{{ t('tool.askUser.name') }}</span>
            <CheckCircle2 :size="14" color="#f59e0b" class="tool-warn" />
          </div>
          <div class="chat-card-body">
            <div v-html="formatToolInput({ questions: mergedSummaryAskQuestions }, 'AskUserQuestion', { askKey: summaryAskKey })"></div>
          </div>
        </div>
      </template>
    </template>
    <!-- Original content mode -->
    <template v-if="nested || !showingSummary || !summary">
    <template v-for="(block, bi) in blocks" :key="stableBlockKey(bi, block)">
      <!-- Sub-agent child blocks are rendered nested under their parent Agent
           block below; skip them in the flat stream. Children always arrive
           AFTER their parent (the parent id is known when they are emitted), so
           this branch never flips mid-stream for a live block. -->
      <template v-if="isChildBlock(block)"></template>
      <!-- Member fallback group: this block's parent Agent card is not in this
           message, so the member's output groups under a synthetic header at
           its first block instead of interleaving with the lead's. -->
      <div v-else-if="memberGroupNameForAnchor(block)" class="subagent-group member-group" :class="{ 'subagent-group-open': isMemberGroupOpen(memberGroupNameForAnchor(block)) }" :style="memberGroupAccentStyle(memberGroupNameForAnchor(block))">
        <div :ref="(el) => setMemberPillRef(memberGroupKey(memberGroupNameForAnchor(block)), el)" class="chat-tool-call" :class="{ done: !memberGroupStreaming(memberGroupNameForAnchor(block)), 'chat-tool-call-group': true, 'chat-tool-call-group-open': isMemberGroupOpen(memberGroupNameForAnchor(block)) }" data-category="agent" :data-member-name="memberGroupNameForAnchor(block)" @click.stop="toggleMemberGroup(memberGroupNameForAnchor(block))">
          <Users :size="12" class="tool-icon" />
          <span class="tool-name">{{ memberGroupNameForAnchor(block) }}</span>
          <span v-if="memberFallbackStepCount(memberGroupNameForAnchor(block)) > 0" class="subagent-group-count">{{ t('chat.contentBlocks.subagentSteps', { count: memberFallbackStepCount(memberGroupNameForAnchor(block)) }) }}</span>
          <LoadingIndicator v-if="memberGroupStreaming(memberGroupNameForAnchor(block))" class="tool-spinner" size="sm" inline />
          <CheckCircle2 v-else :size="14" color="#22c55e" class="tool-check" />
          <button
            type="button"
            class="subagent-group-toggle"
            :aria-expanded="isMemberGroupOpen(memberGroupNameForAnchor(block))"
            :aria-label="isMemberGroupOpen(memberGroupNameForAnchor(block)) ? t('chat.contentBlocks.subagentCollapse') : t('chat.contentBlocks.subagentExpand')"
            :title="isMemberGroupOpen(memberGroupNameForAnchor(block)) ? t('chat.contentBlocks.subagentCollapse') : t('chat.contentBlocks.subagentExpand')"
            @click.stop="toggleMemberGroup(memberGroupNameForAnchor(block))"
          >
            <ChevronUp v-if="isMemberGroupOpen(memberGroupNameForAnchor(block))" :size="12" />
            <ChevronDown v-else :size="12" />
          </button>
        </div>
        <div class="subagent-group-body-wrapper" :class="{ 'subagent-group-body-open': isMemberGroupOpen(memberGroupNameForAnchor(block)), 'subagent-group-body-nostransition': collapsingMemberGroups[memberGroupKey(memberGroupNameForAnchor(block))] }">
          <div class="subagent-group-body">
            <ContentBlocks
              v-if="isMemberGroupMounted(memberGroupNameForAnchor(block))"
              :blocks="memberFallbackBlocks[memberGroupNameForAnchor(block)] || []"
              :root-blocks="rootBlocksResolved"
              :nested="true"
              :msg-id="props.msgId"
              :msg-index="props.msgIndex"
              :session-id="props.sessionId"
              :expanded-tools="props.expandedTools"
              :block-tasks="props.blockTasks"
              :block-ask-questions="props.blockAskQuestions"
              :streaming="props.streaming"
              :started-at="props.startedAt"
              :cancelled="props.cancelled"
              :render-text-block="props.renderTextBlock"
              :format-tool-input="props.formatToolInput"
              :tool-call-summary="props.toolCallSummary"
              :truncate="props.truncate"
              :get-agent-backend="props.getAgentBackend"
              :get-agent-name="props.getAgentName"
              :get-agent-avatar="props.getAgentAvatar"
              :static-block-cache="props.staticBlockCache"
              :active="props.active"
              :read-only="props.readOnly"
              :role="props.role"
              @toggle-tool="$emit('toggle-tool', $event)"
              @show-tool-detail="forwardSubagentToolDetail"
              @task-card-click="$emit('task-card-click', $event)"
              @send-message="(text, cardKey) => $emit('send-message', text, cardKey)"
              @render-flush="$emit('render-flush')"
              @resume-session="$emit('resume-session', $event)"
              @reset-session="$emit('reset-session', $event)"
            />
            <button
              v-if="isMemberGroupOpen(memberGroupNameForAnchor(block))"
              type="button"
              class="subagent-group-footer"
              @click.stop="toggleMemberGroup(memberGroupNameForAnchor(block))"
            >
              <ChevronUp :size="12" />
              <span>{{ t('chat.contentBlocks.subagentCollapse') }}</span>
            </button>
          </div>
        </div>
      </div>
      <template v-else-if="isMemberGroupChild(block)"></template>
      <!-- Thinking block: streaming or expanded shows inline content, collapsed shows clickable chip -->
      <div v-else-if="block.type === 'thinking'"
        :ref="(el) => setThinkingRef(stableBlockKey(bi, block), el)"
        class="chat-thinking"
        :class="{
          'thinking-streaming': isThinkingStreaming(block),
          'thinking-expanded-done': isThinkingExpandedDone(block, bi),
          'thinking-collapsed': isThinkingCollapsed(block, bi),
          'thinking-collapsing': !!collapsingThinking[stableBlockKey(bi, block)],
          'thinking-expanding': !!expandingThinking[stableBlockKey(bi, block)],
        }"
      >
        <div class="thinking-header" @click.stop="handleThinkingClick(block, bi)">
          <Brain :size="12" class="thinking-icon" />
          <span class="thinking-label">{{ t('chat.message.deepThinking') }}</span>
          <!-- Status indicators: right-aligned, same pattern as tool_use -->
          <LoadingIndicator v-if="isThinkingStreaming(block)" class="thinking-spinner" size="sm" inline />
          <!-- Cancelled marker: show inline in thinking header when this is the last block and message was cancelled.
               Prevents the cancelled mark from being visually hidden/trapped under the collapsed thinking chip. -->
          <span v-else-if="isLastBlock(bi) && cancelled" class="chat-cancelled-mark-inline">{{ t('chat.contentBlocks.cancelled') }}</span>
          <ChevronUp v-else-if="isThinkingExpandedDone(block, bi) || expandingThinking[stableBlockKey(bi, block)]" :size="12" class="thinking-chevron" />
          <ChevronDown v-else :size="12" class="thinking-chevron" />
        </div>
        <!-- Content wrapper: CSS grid 0fr/1fr transition for smooth expand/collapse -->
        <div class="thinking-content-wrapper"
          :class="{
            'thinking-content-open': isThinkingStreaming(block) || isThinkingExpandedDone(block, bi) || !!expandingThinking[stableBlockKey(bi, block)],
          }"
        >
          <div
            class="thinking-inline-content"
            :ref="(el) => setThinkingInlineRef(stableBlockKey(bi, block), el)"
            @scroll="handleThinkingInlineScroll(stableBlockKey(bi, block), $event)"
            v-html="getThinkingHtml(bi, block)"
          ></div>
        </div>
      </div>
      <!-- Tool use block -->
      <template v-else-if="block.type === 'tool_use'">
        <!-- Ask card absorbed into the merged card rendered at the anchor block -->
        <template v-if="isSuppressedAskCard(bi)"></template>
        <!-- AskUserQuestion / PermissionApproval: unified interactive card
             (status strip + body in one box). -->
        <div v-else-if="shouldAutoExpand(block) && isUnifiedCardTool(block.name)" class="tool-detail chat-inline-card" :class="!block.done ? 'is-pending' : ''" :data-tool-name="block.name" :data-category="getToolIcon(block.name).category" :data-session-id="sessionId" :data-tool-call-id="block.id" @click="handleToolDetailClick" @input="handleToolDetailInput" @keydown="handleToolDetailKeydown">
          <div class="chat-card-strip" @click.stop="handleToolClick(block, key(bi), absIdx(bi))">
            <component :is="getToolIcon(block.name).icon" :size="12" class="tool-icon" />
            <span class="tool-name">{{ unifiedCardTitle(block.name, block.input, block.display_name) }}</span>
            <span v-if="toolCallSummary(block)" class="tool-summary">{{ toolCallSummary(block) }}</span>
            <!-- Pending spinner: only meaningful when the tool is actually answerable.
                 Malformed AskUserQuestion input (no valid questions) can never be answered,
                 so it renders as a done card with an invalid-format notice instead. -->
            <LoadingIndicator v-if="askCardPending(bi, block)" class="tool-spinner" size="sm" inline />
            <!-- Done with error: red X -->
            <XCircle v-else-if="block.status === 'error'" :size="14" color="#ef4444" class="tool-error-icon" />
            <!-- Done (success or unknown): green check -->
            <CheckCircle2 v-else :size="14" color="#22c55e" class="tool-check" />
          </div>
          <div class="chat-card-body">
            <!-- Merged card: one body holding every question in the message. Only
                 the anchor hosts it — a malformed/slim ask block keeps its own body
                 (invalid-format notice / lazy input) instead of duplicating it. -->
            <div v-if="isMergedAskAnchor(bi)" v-html="formatToolInput({ questions: mergedAskQuestions }, 'AskUserQuestion', { askKey: mergedAskKey(bi) })"></div>
            <div v-else v-html="formatToolInput(block.input, block.name, { done: block.done, status: block.status, output: block.output, askKey: askKeyForBlock(bi, block) })"></div>
          </div>
        </div>
        <template v-else>
          <div :ref="(el) => setSubagentPillRef(subagentGroupKey(bi, block), el)" class="chat-tool-call" :class="{ done: block.done, 'chat-tool-call-group': hasSubagentGroup(block), 'chat-tool-call-group-open': hasSubagentGroup(block) && isSubagentGroupOpen(bi, block) }" :data-category="getToolIcon(block.name).category" :style="memberAccentStyle(block)" @click.stop="handleToolClick(block, key(bi), absIdx(bi))">
            <!-- A team member's card wears the team glyph, exactly like the
                 member fallback group header below. Two paths render the same
                 member, so they must look identical: before this, the card
                 showed the generic Agent/Bot icon while the post-interruption
                 group showed Users, and the same member changed face depending
                 on whether its parent card was still resolvable. -->
            <Users v-if="isAgentCardWithMembers(block)" :size="12" class="tool-icon" />
            <component v-else :is="getToolIcon(block.name).icon" :size="12" class="tool-icon" />
            <span class="tool-name">{{ isAgentCardWithMembers(block) ? agentPillTitle(block) : toolDisplayName(block.name, block.input, block.display_name) }}</span>
            <span v-if="toolCallSummary(block)" class="tool-summary">{{ toolCallSummary(block) }}</span>
            <!-- Sub-agent: step count + expand/collapse toggle live IN the pill,
                 so the Agent call and its sub-agent work are one row instead of
                 two stacked bars showing the same name. -->
            <template v-if="hasSubagentGroup(block)">
              <span v-if="subagentStepCount(block.id) > 0" class="subagent-group-count">{{ t('chat.contentBlocks.subagentSteps', { count: subagentStepCount(block.id) }) }}</span>
              <LoadingIndicator v-if="isSubagentStreaming(block)" class="tool-spinner" size="sm" inline />
              <XCircle v-else-if="block.status === 'error'" :size="14" color="#ef4444" class="tool-error-icon" />
              <CheckCircle2 v-else :size="14" color="#22c55e" class="tool-check" />
              <button
                type="button"
                class="subagent-group-toggle"
                :aria-expanded="isSubagentGroupOpen(bi, block)"
                :aria-label="isSubagentGroupOpen(bi, block) ? t('chat.contentBlocks.subagentCollapse') : t('chat.contentBlocks.subagentExpand')"
                :title="isSubagentGroupOpen(bi, block) ? t('chat.contentBlocks.subagentCollapse') : t('chat.contentBlocks.subagentExpand')"
                @click.stop="toggleSubagentGroup(bi, block)"
              >
                <ChevronUp v-if="isSubagentGroupOpen(bi, block)" :size="12" />
                <ChevronDown v-else :size="12" />
              </button>
            </template>
            <template v-else>
              <!-- Loading: spinner -->
              <LoadingIndicator v-if="!block.done" class="tool-spinner" size="sm" inline />
              <!-- Done with error: red X -->
              <XCircle v-else-if="block.status === 'error'" :size="14" color="#ef4444" class="tool-error-icon" />
              <!-- Done (success or unknown): green check -->
              <CheckCircle2 v-else :size="14" color="#22c55e" class="tool-check" />
            </template>
          </div>
          <!-- All auto-expand tools render as unified cards above, so this
               branch only sees click-to-open pills (opens the detail drawer). -->
        </template>
        <!-- Sub-agent group body: nested thinking/text/tool_use produced by the
             sub-agent this Agent call spawned, rendered by a recursive
             ContentBlocks instance so they get the exact same styles (thinking
             callout/pill, tool pills) as top-level content. The recursive
             subtree is mounted lazily on first open: a never-opened group (e.g.
             a collapsed 4558-block sub-agent) pays no component/index cost. -->
        <!-- The accent is set on the GROUP, not only on the pill: the expanded
             body is a child of this element, so a pill-only custom property
             never reaches it and the body keeps .subagent-group's default
             pink. The member fallback group below sets it here for exactly that
             reason — both paths must paint the same member the same colour. -->
        <div v-if="hasSubagentGroup(block)" class="subagent-group" :class="{ 'subagent-group-open': isSubagentGroupOpen(bi, block) }" :style="memberAccentStyle(block)">
          <div class="subagent-group-body-wrapper" :class="{ 'subagent-group-body-open': isSubagentGroupOpen(bi, block), 'subagent-group-body-nostransition': collapsingSubagentGroups[subagentGroupKey(bi, block)] }">
            <div class="subagent-group-body">
              <ContentBlocks
                v-if="isSubagentGroupMounted(bi, block)"
                :blocks="childBlocksOf(block.id)"
                :root-blocks="rootBlocksResolved"
                :nested="true"
                :msg-id="props.msgId"
                :msg-index="props.msgIndex"
                :session-id="props.sessionId"
                :expanded-tools="props.expandedTools"
                :block-tasks="props.blockTasks"
                :block-ask-questions="props.blockAskQuestions"
                :streaming="props.streaming"
                :started-at="props.startedAt"
                :cancelled="props.cancelled"
                :render-text-block="props.renderTextBlock"
                :format-tool-input="props.formatToolInput"
                :tool-call-summary="props.toolCallSummary"
                :truncate="props.truncate"
                :get-agent-backend="props.getAgentBackend"
                :get-agent-name="props.getAgentName"
                :get-agent-avatar="props.getAgentAvatar"
                :static-block-cache="props.staticBlockCache"
                :active="props.active"
                :read-only="props.readOnly"
                :role="props.role"
                @toggle-tool="$emit('toggle-tool', $event)"
                @show-tool-detail="forwardSubagentToolDetail"
                @task-card-click="$emit('task-card-click', $event)"
                @send-message="(text, cardKey) => $emit('send-message', text, cardKey)"
                @render-flush="$emit('render-flush')"
                @resume-session="$emit('resume-session', $event)"
                @reset-session="$emit('reset-session', $event)"
              />
              <!-- Footer: a bottom affordance to collapse the group (the pill
                   chevron also toggles, but after scrolling a long sub-agent
                   trace the bottom is the natural place to close it). -->
              <button
                v-if="isSubagentGroupOpen(bi, block)"
                type="button"
                class="subagent-group-footer"
                @click.stop="toggleSubagentGroup(bi, block)"
              >
                <ChevronUp :size="12" />
                <span>{{ t('chat.contentBlocks.subagentCollapse') }}</span>
              </button>
            </div>
          </div>
        </div>
      </template>
      <!-- Error block -->
      <div v-else-if="block.type === 'error'" class="chat-error-card">
        <AlertTriangle :size="14" class="error-icon" />
        <span class="error-text">{{ getWarningText(block) }}</span>
        <span v-if="errorSourceLabel(block)" class="error-source-chip" :class="'src-' + (block.error_source || '')">{{ errorSourceLabel(block) }}</span>
        <LoginCommandButton v-if="block.login_cmd" :command="block.login_cmd" />
        <!-- 同上：只读宿主没有 reset-session 监听者，按钮必须一起藏掉 -->
        <button v-else-if="isResetableReason(block.reason) && !isGroupSession && !readOnly" class="warning-reset-btn" @click.stop="$emit('reset-session', { reason: block.reason })">
          {{ t('chat.contentBlocks.resetSession') }}
        </button>
      </div>
      <!-- Warning block: severe (disconnect/timeout/restart) renders as error-level red -->
      <div v-else-if="block.type === 'warning' && isSevereWarning(block)" class="chat-error-card">
        <AlertTriangle :size="14" class="error-icon" />
        <span class="error-text">{{ getWarningText(block) }}</span>
        <span v-if="errorSourceLabel(block)" class="error-source-chip" :class="'src-' + (block.error_source || '')">{{ errorSourceLabel(block) }}</span>
        <LoginCommandButton v-if="block.login_cmd" :command="block.login_cmd" />
        <!-- 同上：只读宿主没有 reset-session 监听者，按钮必须一起藏掉 -->
        <button v-else-if="isResetableReason(block.reason) && !isGroupSession && !readOnly" class="warning-reset-btn" @click.stop="$emit('reset-session', { reason: block.reason })">
          {{ t('chat.contentBlocks.resetSession') }}
        </button>
      </div>
      <!-- Warning block: normal (parse errors, stderr) renders as amber -->
      <div v-else-if="block.type === 'warning'" class="chat-warning-card">
        <AlertCircle :size="14" class="warning-icon" />
        <span class="warning-text">{{ getWarningText(block) }}</span>
        <span v-if="errorSourceLabel(block)" class="error-source-chip" :class="'src-' + (block.error_source || '')">{{ errorSourceLabel(block) }}</span>
        <button v-if="block.reason === 'restart' && !readOnly" class="warning-continue-btn" @click.stop="$emit('send-message', t('chat.contentBlocks.continue'))">
          {{ t('chat.contentBlocks.continue') }}
        </button>
        <LoginCommandButton v-if="block.login_cmd" :command="block.login_cmd" />
        <!-- 同上：只读宿主没有 reset-session 监听者，按钮必须一起藏掉 -->
        <button v-else-if="isResetableReason(block.reason) && !isGroupSession && !readOnly" class="warning-reset-btn" @click.stop="$emit('reset-session', { reason: block.reason })">
          {{ t('chat.contentBlocks.resetSession') }}
        </button>
      </div>
      <!-- Task card(s) — simplified: click navigates to Tasks tab -->
      <template v-else-if="block.type === 'text' && hasScheduledTasks(bi)">
        <div v-if="getBlockHtml(bi, block)" v-html="getBlockHtml(bi, block)"></div>
        <TaskChatCard
          v-for="(sKey, sIdx) in scheduledTaskKeys(bi)"
          :key="sIdx"
          :task="blockTasks[sKey].task"
          :loading="!!blockTasks[sKey].loading"
          :deleted="!!blockTasks[sKey].deleted"
          :get-agent-backend="getAgentBackend"
          :get-agent-name="getAgentName"
          :get-agent-avatar="getAgentAvatar"
          @select="$emit('task-card-click', blockTasks[sKey].taskId)"
        />
      </template>
      <!-- Ask question card (from <clawbench-ask-question> tag in text) — must come before generic text block.
           The card renders whenever the block's own text parses into questions,
           so it does not depend on the blockAskQuestions side table (which
           loadHistory clears and a static-cache hit never refills). -->
      <template v-else-if="block.type === 'text' && (textAskQuestions(bi, block).length > 0 || detectAskQuestionInText(block))">
        <!-- Surrounding text (with ask-question tag stripped) -->
        <div v-if="getBlockHtml(bi, block)" v-html="getBlockHtml(bi, block)"></div>
        <template v-if="textAskQuestions(bi, block).length > 0 && !isSuppressedAskCard(bi)">
          <div class="tool-detail chat-inline-card done" data-category="ask" data-tool-name="AskUserQuestion" @click="handleToolDetailClick" @input="handleToolDetailInput" @keydown="handleToolDetailKeydown">
            <div class="chat-card-strip" @click.stop="$emit('toggle-tool', key(bi))">
              <component :is="getToolIcon('AskUserQuestion').icon" :size="12" class="tool-icon" />
              <span class="tool-name">{{ t('tool.askUser.name') }}</span>
              <span class="tool-summary">{{ askQuestionSummary({ questions: textAskQuestions(bi, block) }) }}</span>
              <CheckCircle2 :size="14" color="#f59e0b" class="tool-warn" />
            </div>
            <div class="chat-card-body">
              <!-- Merged card: one body holding every question in the message. -->
              <div v-if="isMergedAskAnchor(bi)" v-html="formatToolInput({ questions: mergedAskQuestions }, 'AskUserQuestion', { askKey: mergedAskKey(bi) })"></div>
              <div v-else v-html="formatToolInput({ questions: textAskQuestions(bi, block) }, 'AskUserQuestion', { askKey: askKeyForTextBlock(bi) })"></div>
            </div>
          </div>
        </template>
      </template>

      <!-- Text block with slash command badge (agent /command or ClawBench /cb-* command).
           The leading icon is what tells the kinds apart (Bot = agent command,
           Wrench = ClawBench built-in) — colour alone was too subtle, and a
           solid fill reads as heavy/ugly inside the user bubble.

           USER MESSAGES ONLY: the badge means "this is what the user sent as a
           command". An assistant reply starting with "/tmp 已 100% 满…" (a
           sentence beginning with a path) or "#1 …" would otherwise lose its
           Markdown rendering and show a bogus "/tmp" / "#1" command chip. -->
      <template v-else-if="block.type === 'text' && isUserMessage && extractSlashCommand(block.text || '')">
        <span class="slash-command-badge" :class="{ 'clawbench-command-badge': extractSlashCommand(block.text)!.clawbench }">
          <component :is="extractSlashCommand(block.text)!.clawbench ? Wrench : Bot" :size="12" class="command-badge-icon" />
          {{ extractSlashCommand(block.text)!.command }}
        </span>
        <span v-if="extractSlashCommand(block.text)!.rest.trim()" class="at-command-rest">{{ extractSlashCommand(block.text)!.rest.trim() }}</span>
      </template>
      <!-- Text block with an explicit "#skill" reference badge. Same shape as the
           slash badge; the Sparkles icon distinguishes it. User messages only —
           same reason as the slash badge (a reply starting with "#1" or "## 结论"
           is prose, not a skill reference). -->
      <template v-else-if="block.type === 'text' && isUserMessage && extractSkillCommand(block.text || '')">
        <span class="slash-command-badge skill-command-badge">
          <Sparkles :size="12" class="command-badge-icon" />
          {{ '#' + extractSkillCommand(block.text)!.name }}
        </span>
        <span v-if="extractSkillCommand(block.text)!.rest.trim()" class="at-command-rest">{{ extractSkillCommand(block.text)!.rest.trim() }}</span>
      </template>
      <!-- Text block: streaming uses throttled render to avoid UI freeze -->
      <div v-else-if="block.type === 'text'" v-html="getBlockHtml(bi, block)"></div>
    </template>
    </template>
    <!-- Loading dots while AI is still streaming (not when cancelled, and not when showing summary) -->
    <div v-if="!nested && streaming && !cancelled && !(showingSummary && summary)" class="streaming-status">
      <div class="placeholder-dots"><span></span><span></span><span></span></div>
      <span class="streaming-elapsed" role="timer">{{ elapsedLabel }}</span>
    </div>

  </div>
</template>

<script setup lang="ts">
/* eslint-disable @typescript-eslint/no-explicit-any -- defineProps runtime declarations require any for complex prop types */
import { ref, shallowRef, watch, onUnmounted, computed, onMounted, onUpdated, reactive, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import i18n from '@/i18n'
import { handleToolAction, shouldAutoExpandTool, updateAskSubmitState, classifyAskQuestionsInput, restoreAskStatesInContainer, handleAskSupplementaryInput, handleAskSubmitShortcut } from '@/utils/renderToolDetail.ts'
import { askCardKey } from '@/utils/askQuestionState.ts'
import { detectAskQuestion } from '@/utils/streamPerf.ts'
import { askItemsToInputMap } from '@/utils/askQuestion.ts'
import { getToolIcon, toolDisplayName } from '@/utils/icons'
import { teamMemberColorVar } from '@/utils/teamMemberColor'
import { Brain, ChevronDown, ChevronUp, AlertCircle, AlertTriangle, XCircle, CheckCircle2, Bot, Wrench, Sparkles, Users } from 'lucide-vue-next'
import TaskChatCard from '@/components/chat/TaskChatCard.vue'
import LoginCommandButton from '@/components/chat/LoginCommandButton.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { renderMarkdownHtml, renderMermaidInElement } from '@/composables/useMarkdownRenderer.ts'
import { store } from '@/stores/app.ts'
import { getShareToolCall } from '@/share/shareMode'
import { apiGet } from '@/utils/api'
import { appLog, diagLog } from '@/utils/appLog'
import { StreamFrameScheduler } from '@/utils/streamFrameScheduler'
import { useThinkingContent } from '@/composables/useThinkingContent.ts'
import { thinkingRenderSource } from '@/utils/chatStreamUtils.ts'
import { isThinkingUserAwayFromBottom } from '@/utils/thinkingScroll'
import { verifyFilePaths, invalidateNegativePathCache } from '@/composables/useFilePathAnnotation.ts'
import { verifyCommitHashes } from '@/composables/useCommitHashAnnotation.ts'
import { tableWrapVersion } from '@/utils/tableWrapPref.ts'
// Footer pill buttons (.fbtn) — the PermissionApproval card buttons share this
// language, so the styles must be present wherever the chat surfaces render.
import '@/assets/modal-footer-btn.css'
import {
  isSevereWarning,
  getWarningText as getWarningTextUtil,
  getErrorSourceLabel,
  askQuestionSummary as askQuestionSummaryUtil,
  extractAskQuestions,
  blockKey,
  blockTaskKey as blockTaskKeyUtil,
  buildTaskKeyIndex,
  hasScheduledTasks as hasScheduledTasksUtil,
  scheduledTaskKeys as scheduledTaskKeysUtil,
  extractSlashCommand,
  extractSkillCommand,
} from '@/utils/contentBlocks.ts'

const TAG = 'ContentBlocks'

const { t } = useI18n()
const thinkingContent = useThinkingContent()

/**
 * Current UI locale, read from the global i18n instance rather than
 * `useI18n()`'s `locale` ref.
 *
 * The annotators bake translated labels into the cached HTML via `gt()`, which
 * resolves against `i18n.global.locale` — so that is the value the cache key
 * must track. Taking `locale` out of `useI18n()` instead would add a separate
 * reactivity subscription on this component and measurably perturb the
 * streaming render path (two streaming-cost tests regressed).
 */
function i18nGlobalLocale(): string {
  return String(i18n.global.locale.value)
}

/**
 * Rendering inputs that are NOT captured by (msgId, blockIdx, text) but DO
 * change the rendered HTML, and so must participate in the cache key:
 *
 *   - `projectRoot`: the path annotators resolve every detected path relative
 *     to it, so the same block renders a different `data-file-path` under a
 *     different project (or worktree) root.
 *   - `locale`: the annotators bake translated labels into the HTML
 *     (`gt('chat.attach.openFile')` in useFilePathAnnotation, `gt('common.copy')`
 *     in useCodeBlockHeader), and switching language is live — no reload.
 *
 * This is read inside `getBlockHtml`, i.e. during render, which is what makes
 * it a reactive dependency: when either input changes, Vue re-runs the render
 * and the lookup below misses (the key changed) instead of serving stale HTML.
 * Merely clearing the cache on change is NOT enough — a cache hit returns
 * before `renderTextBlock` runs, so the render effect would hold no dependency
 * on the input and the DOM would keep the previous project's annotations.
 */
const cacheScope = computed(() =>
  `${store.state.projectRoot}\u0000${i18nGlobalLocale()}\u0000${tableWrapVersion.value}`,
)

// Auto-expand tools (AskUserQuestion, PermissionApproval) need input to render inline.
// In slim format, input is absent from DB-loaded content — fetch from API automatically.
const fetchedAutoExpandBlocks = new Set<string>()
onMounted(() => {
  for (let i = 0; i < props.blocks.length; i++) {
    const block = props.blocks[i]
    if (block.type === 'tool_use' && shouldAutoExpandTool(block.name || '')) {
      const hasInput = block.input && Object.keys(block.input).length > 0
      if (!hasInput && block.id && props.msgId) {
        const cacheKey = `${block.id}:${props.msgId}`
        if (fetchedAutoExpandBlocks.has(cacheKey)) continue
        fetchedAutoExpandBlocks.add(cacheKey)
        fetchToolCallInputForAutoExpand(block, props.msgId)
      }
    }
  }
})
async function fetchToolCallInputForAutoExpand(block: any, msgId: string | number) {
  // Session-share mode inlines tool input/output into the snapshot, so consult
  // the provider before reaching for the authenticated detail endpoint.
  const shared = getShareToolCall(msgId, block.id)
  if (shared) {
    if (shared.input && (!block.input || Object.keys(block.input).length === 0)) block.input = shared.input
    if (shared.output && !block.output) block.output = shared.output
    if (shared.status) block.status = shared.status
    if (shared.done !== undefined) block.done = shared.done
    return
  }

  try {
    let url = `/api/ai/chat/tool-call?tool_id=${encodeURIComponent(block.id)}&message_id=${encodeURIComponent(msgId)}`
    if (props.sessionId) url += `&session_id=${encodeURIComponent(props.sessionId)}`
    const resp = await fetch(url)
    if (!resp.ok) return
    const data = await resp.json()
    if (data.input) {
      const input = typeof data.input === 'string' ? JSON.parse(data.input) : data.input
      if (input && Object.keys(input).length > 0) {
        block.input = input
      }
    }
    if (data.output && !block.output) {
      block.output = data.output
    }
  } catch { /* best effort */ }
}

// Re-export utility functions with i18n context bound
function getWarningText(block: any) { return getWarningTextUtil(block, t) }
function errorSourceLabel(block: any) { return getErrorSourceLabel(block, t) }

/** Reasons that indicate a stuck/broken agent session — showing a "reset session"
 *  button lets the user recycle the agent connection and recover.
 *  User-initiated cancels (user_cancel/context_cancel) are excluded. */
// `auth_required` is deliberately absent. The agent refused because nobody is
// signed in; recycling the connection (what this button does) cannot sign the
// user in, and the backend excludes it from the retryable reasons for the same
// reason. That banner gets a "copy the login command" action instead (see
// LoginCommandButton), driven by the structured `login_cmd` field.
const RESETABLE_REASONS = new Set(['empty', 'agent_no_run', 'agent_init_timeout', 'request_failed', 'refused', 'backend_exit', 'timeout', 'parse_error', 'panic', 'disconnect'])
function isResetableReason(reason: string | undefined): boolean {
  return !!reason && RESETABLE_REASONS.has(reason)
}
function askQuestionSummary(input: any) { return askQuestionSummaryUtil(input) }

function shouldAutoExpand(block: any) {
  return shouldAutoExpandTool(block.name || '')
}

/** True when the tool is AskUserQuestion (case-insensitive). */
function isAskQuestion(name: string) {
  return (name || '').toLowerCase() === 'askuserquestion'
}

/** True when the tool is PermissionApproval (case-insensitive). */
function isPermissionApproval(name: string) {
  return (name || '').toLowerCase() === 'permissionapproval'
}

/** Auto-expand interactive tools rendered as ONE unified inline card:
 *  header strip + body inside a single bordered box. AskUserQuestion and
 *  PermissionApproval share this visual language. */
function isUnifiedCardTool(name: string) {
  return isAskQuestion(name) || isPermissionApproval(name)
}

/** Card strip label. AskUserQuestion shows its own tool label; the permission
 *  card strip announces the request ("Permission Request") while the body
 *  details the requesting tool + target command/file. */
function unifiedCardTitle(name: string, input: any, displayName?: string) {
  if (isPermissionApproval(name)) return t('tool.permission.title')
  return toolDisplayName(name, input, displayName)
}

/**
 * Label for an Agent-family tool pill.
 *
 * An Agent Team member's card is titled with the MEMBER's name, not the
 * subagent TYPE. Every member of a CodeBuddy team is spawned as
 * `subagent_type: "general-purpose"`, so the type alone renders a column of
 * identical "General-purpose" cards that say nothing about who did the work.
 *
 * The member name is the more specific attribution whenever it is present, so
 * it wins over `display_name`. It arrives on a later frame of the same tool
 * call, which is why the block's attribution is filled in incrementally (see
 * AccumulateBlock) — before that frame lands the card briefly shows the type,
 * which is the best information available at that moment.
 *
 * `member_name` on the block is the primary source. Messages persisted before
 * the block learned to keep it have an empty value, but their CHILDREN still
 * carry the attribution, so the name is derived from them — that makes old
 * history render correctly without a data migration.
 */
function agentPillTitle(block: any): string {
  const own = block?.member_name
  if (own) return String(own)
  for (const child of childBlocksOf(block?.id)) {
    if (child?.member_name) return String(child.member_name)
  }
  return toolDisplayName(block?.name, block?.input, block?.display_name)
}

/** Whether this tool pill IS an Agent card hosting a member timeline — i.e. the
 *  member-name titling applies.
 *
 *  Keyed on the block hosting children rather than on the tool name or the
 *  `agent` icon category: SendMessage/TeamCreate/ComputerUse share that
 *  category and must keep their own labels, and a member's OWN Bash/Read cards
 *  carry `member_name` too — titling those with the member name would turn a
 *  tool trace into a wall of identical names. */
function isAgentCardWithMembers(block: any): boolean {
  if (block?.type !== 'tool_use') return false
  const name = String(block?.name || '').toLowerCase()
  if (name !== 'agent' && name !== 'task') return false
  // Any member evidence counts: the card's own attribution, a child's, or a
  // colour. Colour alone is enough because it is written by the same member
  // frames — and requiring the NAME would leave a card that has only the colour
  // with the generic Agent glyph and no accent.
  return (
    !!block?.member_name ||
    childBlocksOf(block?.id).some((c) => c?.member_name || c?.member_color)
  )
}

/**
 * Whether an AskUserQuestion tool card should show its pending spinner.
 * A not-done AskUserQuestion whose input is MALFORMED (no renderable question —
 * e.g. a leftover XML fragment or junk entries) can never be answered, so it
 * renders as a done card with an invalid-format notice instead of an endless
 * spinner over an empty card. Input that is merely empty (slim/skeleton block,
 * content not yet written) keeps the spinner.
 */
function showAskPending(block: any) {
  if (!block || block.done) return false
  const name = (block.name || '').toLowerCase()
  if (name === 'askuserquestion' && classifyAskQuestionsInput(block.input) === 'malformed') return false
  return true
}

/** Handle tool call bar click: open overlay for regular tools, toggle inline for AskUserQuestion. */
function handleToolClick(block: any, blockKeyStr: string, blockIdx: number) {
  // AskUserQuestion stays inline — toggle expand state
  if (shouldAutoExpand(block)) {
    emit('toggle-tool', blockKeyStr)
    return
  }
  // All other tools: open the overlay with block data
  // Slim format: input/output may be absent — overlay will fetch from API if needed
  emit('show-tool-detail', {
    name: block.name,
    input: block.input,
    output: block.output,
    status: block.status,
    done: block.done,
    display_name: block.display_name,
    summary: block.summary,
    tool_id: block.id,
    // The drawer renders the elapsed time from this field. Omitting it made the
    // time vanish on every host that has no live-block watcher (the share page):
    // the chat panel masked the gap by re-reading duration from the live block
    // once the overlay opened, so only the share page showed nothing.
    duration_ms: block.duration_ms,
    msgId: props.msgId,
    blockIdx,
  })
}

const props = defineProps({
  blocks: { type: Array as () => any[], default: () => [] },
  msgId: { type: [String, Number], default: '' },
  msgIndex: { type: Number, default: 0 },
  sessionId: { type: String, default: '' },
  expandedTools: { type: Object as () => Record<string, any>, default: () => ({}) },
  blockTasks: { type: Object as () => Record<string, any>, default: () => ({}) },
  blockAskQuestions: { type: Object as () => Record<string, any>, default: () => ({}) },
  streaming: { type: Boolean, default: false },
  startedAt: { type: String, default: '' },
  cancelled: { type: Boolean, default: false },
  summary: { type: String as () => string | null, default: null },
  summaryCards: { type: Object as () => Record<string, any> | null, default: null },
  showingSummary: { type: Boolean, default: false },
  // Render functions
  renderTextBlock: { type: Function as any, required: true },
  formatToolInput: { type: Function as any, required: true },
  toolCallSummary: { type: Function as any, required: true },
  truncate: { type: Function as any, default: (s: string) => s },
  getAgentBackend: { type: Function as any, default: () => '' },
  getAgentName: { type: Function as any, default: () => '' },
  getAgentAvatar: { type: Function as any, default: () => '' },
  // Performance: static block cache from useChatRender (Problem 6)
  staticBlockCache: { type: Object as () => any, default: null },
  active: { type: Boolean, default: true },
  // Recursive rendering: a nested instance renders one sub-agent group's child
  // blocks. `nested` suppresses the footer/summary chrome; `rootBlocks` is the
  // top-level blocks array so absolute indices (for caches + the detail drawer)
  // resolve correctly at any depth.
  nested: { type: Boolean, default: false },
  rootBlocks: { type: Array as () => any[], default: null },
  // Read-only rendering (public share page): interactive tool actions that POST
  // back to the server (permission approval, ask-question submit, session reset,
  // "continue" = sending a message) are suppressed — those hosts bind no
  // `reset-session` / `send-message` listener, so rendering the button would give
  // the reader a control that does nothing. Display-only affordances (expanding a
  // tool, copying, opening the detail drawer, copying the login command) keep
  // working.
  readOnly: { type: Boolean, default: false },
  // Group-chat session: the reset-session button recycles ONE agent's connection,
  // which has no meaning on a multi-agent timeline (which member would it reset?).
  // The group orchestrator owns member lifecycle. Hides the reset button only;
  // the "continue" button (a plain message into the group) stays.
  isGroupSession: { type: Boolean, default: false },
  // Message role ('user' | 'assistant' | ...). Only user messages get the
  // slash / #skill command badges: those badges replace the whole text block
  // with "chip + rest", which is only truthful for a message the user actually
  // sent as a command. Assistant prose frequently STARTS with a path or a
  // numbered item ("/tmp 已 100% 满…", "#1 …", "## 结论"), and batching it as a
  // command also loses its Markdown rendering.
  role: { type: String, default: '' },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'task-card-click', 'send-message', 'render-flush', 'resume-session', 'reset-session'])

/** Command badges are a user-message affordance only (see the `role` prop). */
const isUserMessage = computed(() => props.role === 'user')

const elapsedSeconds = ref(0)
let elapsedTimer: ReturnType<typeof setInterval> | null = null
let fallbackStartedAt = Date.now()

function updateElapsed() {
  const parsedStartedAt = Date.parse(props.startedAt)
  const startedAt = Number.isFinite(parsedStartedAt) ? parsedStartedAt : fallbackStartedAt
  elapsedSeconds.value = Math.max(0, Math.floor((Date.now() - startedAt) / 1000))
}

function stopElapsedTimer() {
  if (elapsedTimer) {
    clearInterval(elapsedTimer)
    elapsedTimer = null
  }
}

watch([() => props.streaming, () => props.startedAt], ([streaming]) => {
  stopElapsedTimer()
  if (!streaming) return
  fallbackStartedAt = Date.now()
  updateElapsed()
  elapsedTimer = setInterval(updateElapsed, 1000)
}, { immediate: true })

// Deliberately NOT formatDuration(): this is a live counter that ticks every
// second, so it needs second-level precision to visibly move. formatDuration
// caps at the largest unit (a 1h20m turn would read "1.3h" and appear frozen
// for ~6 minutes at a time). Every *static* duration in the app goes through
// formatDuration instead.
const elapsedLabel = computed(() => {
  if (elapsedSeconds.value < 60) return `${elapsedSeconds.value}s`
  const minutes = Math.floor(elapsedSeconds.value / 60)
  const seconds = String(elapsedSeconds.value % 60).padStart(2, '0')
  if (minutes >= 60) {
    const hours = Math.floor(minutes / 60)
    const remainingMinutes = String(minutes % 60).padStart(2, '0')
    return `${hours}h ${remainingMinutes}m ${seconds}s`
  }
  return `${minutes}m ${seconds}s`
})

// ── Absolute index resolution (recursive rendering) ──
// A nested instance (sub-agent group) renders its own child blocks with local
// indices 0..n-1, but every per-block cache key and the detail drawer's live
// lookup are keyed by the ABSOLUTE index in the root blocks array. Resolve by
// object identity so nested instances stay correctly keyed at any depth.
// A single identity Map per root array keeps lookups O(1) — absIdx is called
// several times per block per render, and a sub-agent can have thousands of
// child blocks (a naive indexOf would be O(n) each time).
const rootBlocksResolved = computed(() => props.rootBlocks || props.blocks)
const rootIndexByIdentity = computed(() => {
  const map = new Map<any, number>()
  const arr = rootBlocksResolved.value
  for (let i = 0; i < arr.length; i++) map.set(arr[i], i)
  return map
})
function absIdx(bi: number): number {
  if (!props.nested || !props.rootBlocks) return bi
  const idx = rootIndexByIdentity.value.get(props.blocks[bi])
  return idx !== undefined ? idx : bi
}

// Key helper: use msgId if available, otherwise msgIndex
function key(bi: number) {
  return blockKey(props.msgId, absIdx(bi))
}

// Key for blockTasks/blockAskQuestions lookup — prefix format used in useChatRender.ts
function blockTaskKey(bi: number) {
  return blockTaskKeyUtil(props.msgId, absIdx(bi))
}

// Quick check if block text contains the tag — used in v-else-if condition
// to enter the clawbench-ask-question branch (which triggers renderTextBlock to fill blockAskQuestions).
// The actual card UI is gated by the parsed questions being non-empty, so false
// positives from this simple check are harmless — they just trigger a
// renderTextBlock call that won't produce a card for non-question content.
function detectAskQuestionInText(block: any) {
  return block.text && block.text.includes('<clawbench-ask-question')
}

/**
 * Questions a TEXT block carries, parsed from the block's own text.
 *
 * `blockAskQuestions` (the shared side table in useChatRender) is filled by
 * renderTextBlock and is therefore only populated when the full pipeline ran
 * for this block. Two things break that:
 *
 *  - `syncSessionState` deletes every entry on each loadHistory — which the
 *    terminal `done` handler triggers immediately after a turn.
 *  - A `StaticBlockCache` hit returns the rendered HTML BEFORE renderTextBlock
 *    runs, so the table is never refilled for that block.
 *
 * A LIVE finalized bubble keeps the raw `<clawbench-ask-question>` tag in its
 * text: the backend only converts the tag into an AskUserQuestion tool block at
 * Finalize, and the converted blocks are never re-sent over the WS stream. (A
 * row loaded from the DB is already converted, which is why switching sessions
 * "fixed" the missing card — that path renders a tool block and needs no side
 * table.) So after the post-done loadHistory the live bubble had a tag, no side
 * table entry and a warm HTML cache: no card at all, until a session switch
 * replaced the live object with the converted DB row.
 *
 * Parsing from the text makes the card a function of the block itself, so no
 * side table can invalidate it. Memoized by text (a message can re-render
 * hundreds of times while streaming) and skipped while streaming, where a
 * half-written payload must not surface a card.
 */
const textAskCache = new Map<string, Array<Record<string, unknown>>>()
const TEXT_ASK_CACHE_MAX = 200
function parseAskQuestionsFromText(text: unknown): Array<Record<string, unknown>> {
  if (props.streaming) return []
  if (typeof text !== 'string' || !text.includes('<clawbench-ask-question')) return []
  const cached = textAskCache.get(text)
  if (cached) return cached
  const result = detectAskQuestion(text)
  const questions = result.items.length > 0
    ? extractAskQuestions(askItemsToInputMap(result.items))
    : []
  if (textAskCache.size >= TEXT_ASK_CACHE_MAX) textAskCache.clear()
  textAskCache.set(text, questions)
  return questions
}

/** Identity of one question, used to recognise the SAME card in two shapes. */
function askQuestionSignature(q: Record<string, unknown>): string {
  const options = Array.isArray(q.options)
    ? (q.options as Array<Record<string, unknown>>).map(o => String(o?.label ?? '')).join('\u0001')
    : ''
  return `${q.header ?? ''}\u0000${q.question ?? ''}\u0000${options}`
}

/** Signatures of every question carried by a CONVERTED AskUserQuestion tool
 *  block in this message (the persisted shape the backend produces at
 *  Finalize). */
const convertedAskSignatures = computed(() => {
  const out = new Set<string>()
  for (const b of props.blocks || []) {
    if (b?.type !== 'tool_use' || !isAskQuestion(b.name || '')) continue
    for (const q of extractAskQuestions(b.input)) out.add(askQuestionSignature(q))
  }
  return out
})

/**
 * Questions a TEXT block renders as a card, from either source: the shared
 * side table (when the full pipeline has run for this block) or the block's own
 * text. Template-facing — a single call site keeps the two sources in sync.
 *
 * Questions already present in a converted AskUserQuestion tool block of the
 * same message are dropped. Finalize converts a `<clawbench-ask-question>` text
 * block into that tool block, but the live bubble keeps the ORIGINAL text (the
 * converted blocks are never re-sent over the WS stream) — and `mergeStreamBlocks`
 * then appends the DB's tool block next to it. Both shapes describe the same
 * card, so the merged card listed every question twice until the next
 * loadHistory replaced the live bubble with the DB row. Filtering here keeps the
 * card a function of the blocks alone, so no reload is needed to dedupe.
 */
function textAskQuestions(bi: number, block: any): Array<Record<string, unknown>> {
  const parsed = props.blockAskQuestions[blockTaskKey(bi)]
  let questions: Array<Record<string, unknown>> = []
  if (parsed && Array.isArray(parsed.questions)) {
    questions = extractAskQuestions(parsed)
  }
  if (questions.length === 0) questions = parseAskQuestionsFromText(block?.text)
  if (questions.length === 0) return questions
  const converted = convertedAskSignatures.value
  if (converted.size === 0) return questions
  return questions.filter(q => !converted.has(askQuestionSignature(q)))
}

// ── Multi ask-card merging ──
// One assistant message can carry several question cards (e.g. two
// AskUserQuestion tool calls, or a tool call plus a text <clawbench-ask-question> tag).
// Rendered separately, each card has its own Submit that fires a message
// immediately — answering one leaves the others stranded with no way to reply.
// When a message has MORE THAN ONE answerable ask card we render a single merged
// card (at the first card's position) holding every question, so the user
// selects all answers and submits once. A message with a single ask card is
// untouched.

/** Renderable questions contributed by one block (empty when it has no ask card). */
function askQuestionsOfBlock(bi: number, block: any): Array<Record<string, unknown>> {
  if (block?.type === 'tool_use' && isAskQuestion(block.name || '')) {
    return extractAskQuestions(block.input)
  }
  if (block?.type === 'text') {
    return textAskQuestions(bi, block)
  }
  return []
}

/** True when this block's ask card is actually rendered by the template branch
 *  chain. A text block carrying BOTH a <scheduled-task> and a <clawbench-ask-question>
 *  tag hits the scheduled-task branch first, so its ask card never renders — it
 *  must not host the merged card. AskUserQuestion tool blocks always render. */
function rendersAskCard(bi: number, block: any): boolean {
  if (block?.type === 'tool_use') return isAskQuestion(block.name || '')
  if (block?.type === 'text') return !hasScheduledTasks(bi)
  return false
}

/** Local indices of blocks contributing renderable questions in THIS flat
 *  stream. Blocks rendered under a sibling sub-agent group are excluded: they
 *  render nested in a recursive instance, which performs its own merge among its
 *  own children — merging here would pull a sub-agent's question into the main
 *  agent's card. Orphans (parent id absent from this array) render flat in this
 *  stream, so they merge here like any other flat card. */
const answerableAskIndices = computed(() => {
  const out: number[] = []
  const blocks = props.blocks || []
  for (let bi = 0; bi < blocks.length; bi++) {
    const block = blocks[bi]
    if (isChildBlock(block)) continue
    if (askQuestionsOfBlock(bi, block).length > 0) out.push(bi)
  }
  return out
})

/** Answerable blocks that actually render an ask card — the only valid homes for
 *  the merged card. */
const renderableAskIndices = computed(() =>
  answerableAskIndices.value.filter(bi => rendersAskCard(bi, props.blocks[bi])),
)

/** Merge when more than one ask card is answerable AND at least one of them
 *  actually renders (otherwise there is nowhere to host the merged card, and
 *  suppressing the rest would silently drop every question). */
const shouldMergeAskCards = computed(() =>
  answerableAskIndices.value.length > 1 && renderableAskIndices.value.length > 0,
)

/** Block index hosting the merged card (first renderable ask card); -1 when not merging. */
const mergedAskAnchorIdx = computed(() =>
  shouldMergeAskCards.value ? renderableAskIndices.value[0] : -1,
)

/** Every question across the message's ask cards, in order. */
const mergedAskQuestions = computed(() => {
  const out: Array<Record<string, unknown>> = []
  for (const bi of answerableAskIndices.value) {
    out.push(...askQuestionsOfBlock(bi, props.blocks[bi]))
  }
  return out
})

/** True when this block's ask card is absorbed into the merged card (render nothing). */
function isSuppressedAskCard(bi: number): boolean {
  return shouldMergeAskCards.value && bi !== mergedAskAnchorIdx.value && answerableAskIndices.value.includes(bi)
}

/** True when this block hosts the merged multi-question card. */
function isMergedAskAnchor(bi: number): boolean {
  return shouldMergeAskCards.value && bi === mergedAskAnchorIdx.value
}

/**
 * Pending spinner for an ask card. The merged card stays pending while ANY
 * contributing tool_use ask block is still awaiting an answer (text-mode
 * <clawbench-ask-question> cards are already resolved when they render, so they never keep
 * it pending). Every other card mirrors its own showAskPending — a malformed or
 * still-loading ask block keeps its own spinner/notice instead of inheriting the
 * merged card's state.
 */
function askCardPending(bi: number, block: any): boolean {
  if (isMergedAskAnchor(bi)) {
    return answerableAskIndices.value.some(i => {
      const b = props.blocks[i]
      return b?.type === 'tool_use' && showAskPending(b)
    })
  }
  return showAskPending(block)
}

// Pre-computed index: block index → sorted array of task keys.
const taskKeyIndex = computed(() => buildTaskKeyIndex(props.msgId, props.blockTasks))

// Check if a block has any tasks
function hasScheduledTasks(bi: number) {
  return hasScheduledTasksUtil(taskKeyIndex.value, bi)
}

// Return all task keys for a block, sorted by tag index
function scheduledTaskKeys(bi: number) {
  return scheduledTaskKeysUtil(taskKeyIndex.value, bi)
}

// ── Summary-mode structured cards (rendered from summaryCards, no block traversal) ──
// PermissionApproval is filtered out: it is an actionable-only card (its buttons
// need the live session ID + tool call ID, which the read-only summary view does
// not carry), so in summary mode it would only render dead buttons. Older
// persisted summaries may still contain it, hence the defensive filter.
const summaryTools = computed(() => (props.summaryCards?.tools || []).filter((t: any) => !isPermissionApproval(t?.name || '')))
const summaryTaskIDs = computed(() => props.summaryCards?.taskIDs || [])
const summaryAskQuestions = computed(() => props.summaryCards?.askQuestions || [])
const summaryWarnings = computed(() => props.summaryCards?.warnings || [])

// Summary mode has no content blocks, so its ask cards come from two places:
// summaryCards.askQuestions (<clawbench-ask-question> cards) and summaryCards.tools
// entries whose input carries a questions array (AskUserQuestion tool calls).
// When a message has more than one such card, merge them into ONE card so the
// user can answer every question with a single submit (mirrors block mode).
//
// A question present in BOTH sources is one card described twice: the backend
// converts a text card into an AskUserQuestion tool block at Finalize, and a
// summary written from the pre-conversion blocks keeps the text-card copy
// alongside it. Both sources stay counted (that is what suppresses the separate
// tool card in favour of the merged one), but the merged body lists each
// question once — the tool block's copy wins, being the durable form.
const summaryAskSources = computed<Array<Record<string, unknown>>>(() => {
  const sources: Array<Record<string, unknown>> = []
  if (summaryAskQuestions.value.length > 0) {
    sources.push({ questions: summaryAskQuestions.value })
  }
  for (const tool of summaryTools.value) {
    if (!isAskQuestion(tool?.name || '')) continue
    const questions = extractAskQuestions(tool.input)
    if (questions.length > 0) sources.push({ questions })
  }
  return sources
})
const shouldMergeSummaryAskCards = computed(() => summaryAskSources.value.length > 1)
const mergedSummaryAskQuestions = computed(() => {
  const out: Array<Record<string, unknown>> = []
  const seen = new Set<string>()
  for (const src of summaryAskSources.value) {
    for (const q of src.questions as Array<Record<string, unknown>>) {
      const sig = askQuestionSignature(q)
      if (seen.has(sig)) continue
      seen.add(sig)
      out.push(q)
    }
  }
  return out
})

// ── Ask-card identity for the answer-state store ──
//
// The card body is rendered through `v-html`, so its selection / note /
// submitted flag would otherwise live only in the DOM and be lost on any
// re-render (see askQuestionState.ts). Each card therefore carries a stable
// `data-ask-key`, and the update hook below writes the stored state back after
// every re-render.
//
// The key must survive a reload. Tool cards use `block.id` — the backend's
// `ask-<uuid>`, persisted to chat_tool_calls — which is stable. Text-mode
// cards have no such id before the backend converts them at Finalize, so they
// are keyed by position and, by design, do not carry answers across that
// conversion (a re-answer of the converted card is a fresh card).
function askKeyForBlock(bi: number, block: any): string {
  return askCardKey(props.sessionId, 'tool', String(block?.id || `idx-${absIdx(bi)}`))
}
function mergedAskKey(bi: number): string {
  // One merged card per message, anchored at its first contributing block.
  return askCardKey(props.sessionId, 'msg', String(props.msgId || absIdx(bi)))
}
function askKeyForTextBlock(bi: number): string {
  return askCardKey(props.sessionId, 'text', String(blockTaskKey(bi)))
}
function askKeyForSummaryTool(tool: any): string {
  return askCardKey(props.sessionId, 'tool', String(tool?.id || tool?.name || 'unknown'))
}
const summaryAskKey = computed(() => askCardKey(props.sessionId, 'summary', String(props.msgId || 'summary')))

// ── Sub-agent grouping (recursive) ──
// Blocks carrying a `parent_tool_call_id` were produced by a sub-agent spawned
// by that Agent tool call; they render nested under the Agent block instead of
// in the flat stream. Grouping is computed from the ROOT blocks array so it is
// identical at every recursion depth (a depth-2 sub-agent's children group under
// its own id).
//
// Orphans — a child whose parent id matches no block in the message (e.g. the
// Agent block was rejected/removed) — would otherwise be skipped by the flat
// loop AND never rendered by any group, silently losing content. They are
// rendered flat as a fallback instead.
const childBlocksByParent = computed(() => {
  const map: Record<string, any[]> = {}
  for (const b of rootBlocksResolved.value) {
    const pid = b?.parent_tool_call_id
    if (pid) {
      if (!map[pid]) map[pid] = []
      map[pid].push(b)
    }
  }
  return map
})

/** Child blocks of the Agent tool call with the given id (empty when none). */
function childBlocksOf(agentToolId: string | undefined): any[] {
  if (!agentToolId) return []
  return childBlocksByParent.value[agentToolId] || []
}

// ── Agent Team member grouping (fallback) ──
// Agent Team members normally reach the frontend with a backfilled
// `parent_tool_call_id` (backend joins member name → spawning Agent card), so
// they group under that Agent card like any sub-agent.
//
// That join is only valid WITHIN the message that contains the Agent card:
//   - the Agent card lives in the message that spawned it; a later message
//     (same live connection, or after "continue") carries member content whose
//     parent id points at a block in an EARLIER message, which this component
//     never sees (each message renders its own ContentBlocks instance), and
//   - a respawned connection loses the in-memory member → tool-call map
//     entirely, so member frames arrive with no parent at all.
// In both cases every member's output would render flat in one interleaved
// stream, indistinguishable from the lead's.
//
// The fallback: member content whose parent is NOT resolvable in this message
// groups by MEMBER NAME instead, into one synthetic group per member. Member
// name is the wire's own attribution key (`_meta["codebuddy.ai/memberEvent"]`)
// and survives both failures. Resolvable member content keeps using the real
// Agent card, so a healthy message is unchanged.

/** True when this block is member content whose real parent is not rendered in
 *  this message — i.e. it belongs to a member fallback group.
 *
 *  Top-level instances only. A nested instance renders ONE group's children,
 *  where the parent link is intentionally absent from the local array (the
 *  hosting Agent card lives in the parent instance), so re-evaluating the
 *  fallback there would either recurse forever (member group body) or steal
 *  sub-agent children into a bogus member group. */
function needsMemberFallback(block: any): boolean {
  if (props.nested) return false
  const name = block?.member_name
  if (!name) return false
  // A block that HOSTS other blocks is an Agent card, not member content — even
  // though it now carries the member name (the backend attributes the spawn
  // frame to its member so the card can be titled and coloured). Folding it
  // into the member's own fallback group would nest the timeline inside itself
  // and lose the card's expand/collapse affordance.
  if (block?.id && hostBlockIds.value.has(block.id)) return false
  const pid = block?.parent_tool_call_id
  return !pid || !currentBlockIds.value.has(pid)
}

/** Ids of blocks in this array that are the PARENT of some other block — i.e.
 *  Agent cards hosting a timeline. Computed from `props.blocks` so it describes
 *  exactly what this instance renders. */
const hostBlockIds = computed(() => {
  const ids = new Set<string>()
  for (const b of props.blocks) {
    const pid = b?.parent_tool_call_id
    if (pid) ids.add(pid)
  }
  return ids
})

/** Member name of the fallback group a block belongs to ('' when none). */
function memberGroupNameOf(block: any): string {
  return needsMemberFallback(block) ? String(block.member_name) : ''
}

/** Blocks belonging to a member's fallback group, in stream order. Scoped to
 *  THIS instance's array so a nested instance renders only its own subset —
 *  the group then replaces exactly the blocks the flat loop would have shown. */
const memberFallbackBlocks = computed(() => {
  const map: Record<string, any[]> = {}
  for (const b of props.blocks) {
    const name = memberGroupNameOf(b)
    if (!name) continue
    if (!map[name]) map[name] = []
    map[name].push(b)
  }
  return map
})

/** Member color for a fallback group header (from any of its blocks). */
function memberFallbackColor(name: string): string | undefined {
  const blocks = memberFallbackBlocks.value[name]
  if (!blocks) return undefined
  for (const b of blocks) {
    if (b?.member_color) return b.member_color
  }
  return undefined
}

/** Number of tool calls in a member's fallback group. */
function memberFallbackStepCount(name: string): number {
  const blocks = memberFallbackBlocks.value[name]
  if (!blocks) return 0
  return blocks.filter((b) => b.type === 'tool_use').length
}

/** Expand key for a member fallback group (stable across reloads: keyed by
 *  message + member name, not by block position). */
function memberGroupKey(name: string): string {
  return `member-group-${props.msgId ?? props.msgIndex ?? 0}-${name}`
}

function isMemberGroupOpen(name: string): boolean {
  return !!props.expandedTools[memberGroupKey(name)]
}

/** Accent style for a member fallback group header. */
function memberGroupAccentStyle(name: string): Record<string, string> | undefined {
  const color = memberFallbackColor(name)
  if (!color) return undefined
  return { '--subagent-accent': teamMemberColorVar(color) }
}

// Pill elements by group key, so collapsing can anchor the pill to the top of
// the scroll viewport (same treatment as sub-agent groups).
const memberPillEls = new Map<string, HTMLElement>()
function setMemberPillRef(key: string, el: any) {
  if (el) memberPillEls.set(key, el as HTMLElement)
  else memberPillEls.delete(key)
}

/**
 * Toggle a member fallback group. Mirrors toggleSubagentGroup: collapsing
 * shrinks a potentially huge group, so the pill is anchored to the top of the
 * scroll container and restored after the DOM updates.
 */
function toggleMemberGroup(name: string) {
  const key = memberGroupKey(name)
  const wasOpen = isMemberGroupOpen(name)
  if (!wasOpen) {
    emit('toggle-tool', key)
    return
  }
  const pill = memberPillEls.get(key)
  const scroller = pill?.closest('.chat-messages') as HTMLElement | null
  if (!pill || !scroller) {
    emit('toggle-tool', key)
    return
  }
  const pillTopInContent = scroller.scrollTop + (pill.getBoundingClientRect().top - scroller.getBoundingClientRect().top)
  collapsingMemberGroups[key] = true
  emit('toggle-tool', key)
  nextTick(() => {
    const maxScroll = scroller.scrollHeight - scroller.clientHeight
    const target = Math.max(0, Math.min(pillTopInContent, maxScroll))
    if (Math.abs(target - scroller.scrollTop) > 1) {
      scroller.scrollTop = target
    }
    requestAnimationFrame(() => { delete collapsingMemberGroups[key] })
  })
}

/** Groups whose recursive body has been mounted at least once. */
const mountedMemberGroups = reactive<Record<string, boolean>>({})

/** Groups mid-collapse (transition disabled so the anchor can be measured). */
const collapsingMemberGroups = reactive<Record<string, boolean>>({})

function isMemberGroupMounted(name: string): boolean {
  const k = memberGroupKey(name)
  if (isMemberGroupOpen(name)) mountedMemberGroups[k] = true
  return !!mountedMemberGroups[k]
}

/** Whether the member's fallback group is still producing output (drives the
 *  header spinner). A member is "running" while any of its tool calls is
 *  unfinished; a group with no tool calls yet counts as running while the
 *  message streams. */
function memberGroupStreaming(name: string): boolean {
  if (!props.streaming) return false
  const blocks = memberFallbackBlocks.value[name]
  if (!blocks) return false
  const tools = blocks.filter((b) => b.type === 'tool_use')
  if (tools.length === 0) return true
  return tools.some((b) => !b.done)
}

/** Ids of blocks present in the CURRENT blocks array (not the root). A block is
 *  skipped in this iteration iff its parent is rendered in this same array —
 *  i.e. the parent hosts a group right here. In a nested instance the array is
 *  already a group's children, so they are not skipped (and a depth-2 child
 *  whose parent is a sibling in this array is skipped, rendering under that
 *  parent's own recursive group). */
const currentBlockIds = computed(() => {
  const ids = new Set<string>()
  for (const b of props.blocks) {
    if (b?.id) ids.add(b.id)
  }
  return ids
})

/** True when this block is rendered under a sibling parent's group (so it is
 *  skipped in this flat loop). Orphans — whose parent id matches no block — and
 *  blocks whose parent is not in this array render flat as a fallback. */
function isChildBlock(block: any): boolean {
  const pid = block?.parent_tool_call_id
  if (!pid) return false
  return currentBlockIds.value.has(pid)
}

/** True when this block belongs to a member fallback group AND is not that
 *  group's anchor — i.e. it is rendered inside the group body, so the flat loop
 *  must skip it. */
function isMemberGroupChild(block: any): boolean {
  const name = memberGroupNameOf(block)
  if (!name) return false
  return memberFallbackAnchor.value[name] !== block
}

/** The block that hosts each member fallback group: the member's FIRST block in
 *  this message. Rendering the group there keeps the member's output at the
 *  point in the conversation where it actually arrived. */
/** The block that hosts each member fallback group, in this message.
 *
 *  Rendering the group at the member's first block keeps its output at the
 *  point in the conversation where it actually arrived.
 *
 *  Preference is a NON-tool block (text/thinking) over a tool_use block: the
 *  anchor block is replaced by the group header pill, so anchoring on a tool
 *  call would HIDE that tool call's own card (its name, status, output) behind
 *  a pill that only shows the member name. A member whose only output is one
 *  tool call is common, and in that case the tool must stay visible.
 *  When every block is a tool call the first one anchors, which is the only
 *  choice left — the group still needs a header. */
const memberFallbackAnchor = computed(() => {
  const firstAny: Record<string, any> = {}
  const firstNonTool: Record<string, any> = {}
  for (const b of props.blocks) {
    const name = memberGroupNameOf(b)
    if (!name) continue
    if (!firstAny[name]) firstAny[name] = b
    if (!firstNonTool[name] && b?.type !== 'tool_use') firstNonTool[name] = b
  }
  const map: Record<string, any> = {}
  for (const name of Object.keys(firstAny)) {
    map[name] = firstNonTool[name] || firstAny[name]
  }
  return map
})

/** Whether this block is the anchor (header pill) of a member fallback group. */
function memberGroupNameForAnchor(block: any): string {
  const name = memberGroupNameOf(block)
  if (!name) return ''
  return memberFallbackAnchor.value[name] === block ? name : ''
}

/** Expand key for a sub-agent group: reuses the Agent block's stable key. */
function subagentGroupKey(bi: number, block: any): string {
  return `subagent-${stableBlockKey(bi, block)}`
}

/** Whether the sub-agent group under this Agent block is expanded. */
function isSubagentGroupOpen(bi: number, block: any): boolean {
  return !!props.expandedTools[subagentGroupKey(bi, block)]
}

// Pill elements by group key, so collapsing can anchor the pill to the top of
// the scroll viewport (see toggleSubagentGroup).
const subagentPillEls = new Map<string, HTMLElement>()
function setSubagentPillRef(key: string, el: any) {
  if (el) subagentPillEls.set(key, el as HTMLElement)
  else subagentPillEls.delete(key)
}

/**
 * Toggle a sub-agent group. Collapsing shrinks the group by potentially
 * thousands of pixels, which would violently shift everything below the
 * viewport. Before collapsing we anchor the Agent pill to the top of its
 * scroll container and restore that offset after the DOM updates, so the
 * pill (and the following content) stay put instead of jumping.
 */
function toggleSubagentGroup(bi: number, block: any) {
  const key = subagentGroupKey(bi, block)
  const wasOpen = isSubagentGroupOpen(bi, block)
  if (!wasOpen) {
    // Expanding: no anchoring needed (content grows below the pill).
    emit('toggle-tool', key)
    return
  }
  const pill = subagentPillEls.get(key)
  const scroller = pill?.closest('.chat-messages') as HTMLElement | null
  if (!pill || !scroller) {
    emit('toggle-tool', key)
    return
  }
  // Distance from the pill's top to the scroller's visible top, plus the
  // current scrollTop. After collapse we restore scrollTop so the pill keeps
  // the same on-screen position (clamped to the pill's new max scroll).
  const pillTopInContent = scroller.scrollTop + (pill.getBoundingClientRect().top - scroller.getBoundingClientRect().top)
  // Disable the grid transition for this collapse so the height change is
  // applied synchronously — nextTick then measures the final layout and the
  // anchor restore is exact. Expansion animates normally.
  collapsingSubagentGroups[key] = true
  emit('toggle-tool', key)
  nextTick(() => {
    const maxScroll = scroller.scrollHeight - scroller.clientHeight
    const target = Math.max(0, Math.min(pillTopInContent, maxScroll))
    if (Math.abs(target - scroller.scrollTop) > 1) {
      scroller.scrollTop = target
    }
    // Re-enable the transition on the next frame so a later expand animates.
    requestAnimationFrame(() => { delete collapsingSubagentGroups[key] })
  })
}

// Groups whose recursive body has been mounted at least once. Once mounted it
// stays mounted so the collapse animation works on subsequent toggles; an
// unopened group never instantiates its (possibly huge) child subtree.
const mountedSubagentGroups = reactive<Record<string, boolean>>({})

// Groups mid-collapse. Collapse is instant (transition disabled) so the
// viewport anchor can be restored at nextTick against the final height; only
// expansion animates.
const collapsingSubagentGroups = reactive<Record<string, boolean>>({})

/** Whether to mount the recursive body for this group (first open or later). */
function isSubagentGroupMounted(bi: number, block: any): boolean {
  const k = subagentGroupKey(bi, block)
  if (isSubagentGroupOpen(bi, block)) mountedSubagentGroups[k] = true
  return !!mountedSubagentGroups[k]
}

/** Whether the parent Agent tool is still running (drives the group spinner). */
function isSubagentStreaming(block: any): boolean {
  return !block?.done
}

/** Step count for a group: number of child tool calls (the meaningful unit of
 *  sub-agent progress). */
function subagentStepCount(agentToolId: string | undefined): number {
  return childBlocksOf(agentToolId).filter((b) => b.type === 'tool_use').length
}

/** Whether the Agent block has a sub-agent group to render. */
function hasSubagentGroup(block: any): boolean {
  return !!block?.id && childBlocksOf(block.id).length > 0
}

/**
 * Per-block accent style for Agent Team members.
 *
 * An Agent card that hosts a team member's timeline is attributed to that member
 * (member_name/member_color on the block, see codebuddy_team_bridge.go). Setting
 * --subagent-accent on the card makes the group border match the member's colour
 * in the roster; a block without attribution keeps the default accent.
 */
function memberAccentStyle(block: any): Record<string, string> | undefined {
  const color = memberColorOf(block)
  if (!color) return undefined
  return { '--subagent-accent': teamMemberColorVar(color) }
}

/**
 * The member colour for an Agent card.
 *
 * The card's own `member_color` is the primary source, but persisted history
 * routinely has it EMPTY while its CHILDREN carry it (verified on real rows:
 * every Agent card had member_color null, while the member's own blocks had
 * green/yellow/red/…). Without the fallback the card loses its accent on every
 * reload, which is exactly the "pre-interruption members have no colour" bug —
 * the live stream attributed the colour to the card, but the reload did not.
 *
 * Children are the same source the fallback group header uses
 * (memberFallbackColor), so both paths agree by construction.
 */
function memberColorOf(block: any): string | undefined {
  if (block?.member_color) return String(block.member_color)
  const id = block?.id
  if (!id) return undefined
  for (const child of childBlocksOf(String(id))) {
    if (child?.member_color) return String(child.member_color)
  }
  return undefined
}

/** Forward a child block's tool-detail event with its real root index. */
function forwardSubagentToolDetail(payload: any) {
  emit('show-tool-detail', payload)
}



// Local map of scheduled-task data keyed by task id, fetched in real time when the
// summary is visible with taskIDs. Unlike blockTasks (which scans text blocks), summary
// mode has empty blocks, so we fetch directly from the /api/tasks list API.
const summaryTaskData: Record<string, any> = reactive({})

async function fetchSummaryTaskData() {
  const ids = summaryTaskIDs.value
  if (!ids || ids.length === 0) return
  const pending = ids.filter((id: string) => !summaryTaskData[id]?.task && !summaryTaskData[id]?.loading && !summaryTaskData[id]?.deleted)
  if (pending.length === 0) return
  for (const id of pending) {
    summaryTaskData[id] = { taskId: id, task: null, loading: true, deleted: false }
  }
  try {
    const data: any = await apiGet('/api/tasks')
    const taskMap = new Map((data.tasks || []).map((t: any) => [t.id, t]))
    for (const id of pending) {
      const entry = summaryTaskData[id]
      entry.loading = false
      const task = taskMap.get(id)
      if (task) entry.task = task
      else entry.deleted = true
    }
  } catch (error) {
    appLog.e(TAG, '[ContentBlocks] fetchSummaryTaskData failed', error)
    for (const id of pending) summaryTaskData[id].loading = false
  }
}

watch(summaryTaskIDs, fetchSummaryTaskData, { immediate: true })

// Keep summary task cards in sync with store.state.tasks (global polling) so status
// changes refresh in real time. Deletion is owned by fetchSummaryTaskData (which uses
// the authoritative /api/tasks list); the store watch only syncs status. It must NOT
// mark tasks deleted when the store list is empty — an empty list can be an app reset
// or a not-yet-populated store, not an actual deletion.
watch(
  () => store.state.tasks,
  (tasks: any[]) => {
    const ids = summaryTaskIDs.value
    if (!ids || ids.length === 0) return
    const taskMap = new Map((tasks || []).map((t: any) => [t.id, t]))
    for (const id of ids) {
      const entry = summaryTaskData[id]
      if (!entry || entry.deleted) continue
      const updated = taskMap.get(id)
      if (updated) {
        entry.task = updated
        entry.loading = false
      }
    }
  },
  { deep: true },
)

function handleSummaryToolClick(tool: any, ti: number) {
  const name = tool.name || ''
  if (shouldAutoExpandTool(name)) {
    emit('toggle-tool', `summary-tool-${ti}`)
    return
  }
  emit('show-tool-detail', {
    name,
    input: tool.input,
    output: tool.output,
    status: 'success',
    done: true,
    display_name: tool.display_name,
    tool_id: tool.id,
    // Same reason as handleToolClick: the summary card's payload must carry the
    // elapsed time itself, not rely on a watcher that only some hosts install.
    duration_ms: tool.duration_ms,
    msgId: props.msgId,
    blockIdx: ti,
  })
}

/** Generate a stable key for a block, used for v-for :key and animation state.
 *  tool_use: block.id (unique tool call ID from backend)
 *  thinking: block.think_id (the identity the backend mints when the block
 *            opens and sends on the WS event), falling back to block._key for a
 *            live block built from a payload that carried no think_id (a server
 *            older than that field, e.g. the disk-served frontend running
 *            against an un-restarted binary).
 *  text: text-${bi} (text blocks merge so index is stable)
 *  other: type-bi (fallback)
 *  The index fallback uses the ABSOLUTE root index (absIdx) so a nested
 *  instance's keys never collide with the parent's. */
function computeStableBlockKey(bi: number, block: any) {
  if (block.type === 'tool_use' && block.id) return block.id
  if (block.type === 'thinking') {
    if (block.think_id) return block.think_id
    if (block._key) return block._key
  }
  return `${block.type || 'other'}-${absIdx(bi)}`
}

// Per-render memo for `stableBlockKey`. The template calls it roughly a dozen
// times per thinking block per render pass (v-for :key, both :ref closures, the
// scroll closure, four class bindings, and again inside each of the three
// thinking-state helpers) and every call re-derived it from `absIdx`. At 83
// thinking blocks re-rendering on each of ~290 streaming frames that was
// hundreds of thousands of calls in the 2026-09-20 freeze trace, showing up as
// 101ms of self time in `stableBlockKey` plus a share of the 555ms spent in the
// :ref closures and the 182ms in the thinking-state helpers that call it.
//
// Validated on the inputs that actually determine the key, not on a watcher:
//   * `block` — `props.blocks` is mutated in place while streaming (blocks are
//     appended, text grows, a tool block's id can be adopted), so the array
//     reference alone does not change. Comparing the block object catches every
//     replacement, including a reordered array.
//   * the two arrays — the index fallback embeds `absIdx(bi)`, which resolves
//     through the parent's root array, so a replaced array can change the key
//     for an otherwise unchanged (index, block) pair.
const _blockKeyCache = new Map<number, { block: any; blocks: any; root: any; key: string }>()

function stableBlockKey(bi: number, block: any) {
  const hit = _blockKeyCache.get(bi)
  if (hit && hit.block === block && hit.blocks === props.blocks && hit.root === props.rootBlocks) {
    return hit.key
  }
  const key = computeStableBlockKey(bi, block)
  _blockKeyCache.set(bi, { block, blocks: props.blocks, root: props.rootBlocks, key })
  return key
}

function handleThinkingClick(block: any, bi: number) {
  const blockKey = stableBlockKey(bi, block)
  if (isThinkingCollapsed(block, bi)) {
    // Expand inline with animation
    expandingThinking.value[blockKey] = true
    thinkingExpanded.value[blockKey] = true
    invalidateBlockHtml()
    // Slim block (think_id, no text): lazy-load the thinking text on expand.
    // Provisional while the turn runs: a block can be `done` and still grow (the
    // accumulator has no done check), so reading now may only get a prefix —
    // flagged so the auto-load refetches the final text once the turn ends.
    if (!block.text && block.think_id) {
      thinkingContent.loadThinking(block.think_id, props.msgId, props.sessionId, props.streaming)
        .catch(() => { /* error surfaced via errors ref */ })
    }
    // Clean up expanding state after animation
    const t = setTimeout(() => {
      delete expandingThinking.value[blockKey]
    }, EXPAND_TRANSITION_MS)
    _collapseTimers.push(t)
  } else if (isThinkingExpandedDone(block, bi)) {
    // Retry failed lazy-load when clicking an error-state slim block;
    // otherwise collapse.
    if (!block.text && block.think_id && thinkingContent.errors.value[block.think_id]) {
      thinkingContent.loadThinking(block.think_id, props.msgId, props.sessionId, props.streaming)
        .catch(() => { /* error surfaced via errors ref */ })
    } else {
      triggerThinkingCollapse(blockKey)
    }
  }
}

/** Whether a thinking block should show inline streaming content.
 *  Thinking blocks can be marked as done via the `thinking_done` SSE event
 *  (ACP backend), which sets `block.done = true`. When explicitly done, the
 *  spinner and inline content should hide immediately. Otherwise, fall back
 *  to the message-level `streaming` prop. */
function isThinkingStreaming(block: any) {
  if (block.done) return false
  return props.streaming
}

/** Whether a thinking block is done (not streaming) but still expanded with inline content visible. */
function isThinkingExpandedDone(block: any, bi: number) {
  if (isThinkingStreaming(block)) return false
  if (collapsingThinking.value[stableBlockKey(bi, block)]) return false
  return !!thinkingExpanded.value[stableBlockKey(bi, block)]
}

/** Whether a thinking block is collapsed to a chip (done, not streaming, not expanded, not collapsing). */
function isThinkingCollapsed(block: any, bi: number) {
  if (isThinkingStreaming(block)) return false
  if (collapsingThinking.value[stableBlockKey(bi, block)]) return false
  if (thinkingExpanded.value[stableBlockKey(bi, block)]) return false
  return !props.streaming || block.done // block is done
}

/** Whether the given block index is the last block in the blocks array. */
function isLastBlock(bi: number) {
  return bi === (props.blocks?.length || 0) - 1
}

// ── Thinking block collapse/expand animation state ──
const collapsingThinking = ref<Record<string, any>>({})   // { [blockKey]: true } for blocks mid-collapse
const expandingThinking = ref<Record<string, any>>({})    // { [blockKey]: true } for blocks mid-expand
const thinkingExpanded = ref<Record<string, any>>({})     // { [blockKey]: true } — completed blocks that are still expanded (only collapses on manual click)
let _collapseElKeys = new Set<string>()       // blockKeys of thinking blocks tracked during streaming
let _collapseTimers: ReturnType<typeof setTimeout>[] = []             // setTimeout IDs for collapse animation (cleaned up on unmount)

// Animation constants
const EXPAND_TRANSITION_MS = 300     // ms — expand animation duration
const COLLAPSE_TRANSITION_MS = 350   // ms — collapse animation duration

/** Trigger the collapse animation for a completed thinking block. */
function triggerThinkingCollapse(blockKey: string) {
  // Mark as collapsing — this removes thinking-content-open from the wrapper,
  // triggering the CSS grid 0fr transition. We also clear thinkingExpanded so
  // the wrapper transitions from 1fr→0fr immediately.
  collapsingThinking.value[blockKey] = true
  delete thinkingExpanded.value[blockKey]
  invalidateBlockHtml()
  // After transition completes, clean up collapsing state
  const t = setTimeout(() => {
    delete collapsingThinking.value[blockKey]
  }, COLLAPSE_TRANSITION_MS)
  _collapseTimers.push(t)
}

/** Track thinking block keys during streaming for collapse animation. */
function setThinkingRef(key: string, el: any) {
  if (el) {
    _collapseElKeys.add(key)
  } else {
    _collapseElKeys.delete(key)
  }
}

// ── Thinking inline-content scroll follow ──
// The streaming thinking box is a fixed-height (`max-height` + `overflow-y`)
// scroll container. Each streaming render batch rewrites its innerHTML and the
// browser keeps the old scrollTop, so new reasoning lines accumulate below the
// viewport unless we re-pin the box to the bottom. Follow is per-block: the box
// follows the live stream while the user is at its bottom, and stops as soon as
// they scroll away to read earlier reasoning.
const thinkingInlineEls = new Map<string, HTMLElement>()
let thinkingScrollLeft: Record<string, boolean> = {}

function setThinkingInlineRef(key: string, el: any) {
  if (el) {
    thinkingInlineEls.set(key, el as HTMLElement)
  } else {
    thinkingInlineEls.delete(key)
    delete thinkingScrollLeft[key]
  }
}

/** User scrolled inside a thinking box: re-sample the per-block follow latch
 *  from the distance to the bottom. Distance-only (no direction test) for the
 *  same reason as the outer chat list — a 1px layout nudge during streaming
 *  must not be mistaken for a deliberate scroll up. */
function handleThinkingInlineScroll(key: string, event: Event) {
  const el = event.currentTarget as HTMLElement
  const dist = el.scrollHeight - el.scrollTop - el.clientHeight
  thinkingScrollLeft[key] = isThinkingUserAwayFromBottom(dist)
}

/** After a streaming content update, pin each live thinking box to its bottom
 *  unless the user is reading earlier reasoning in that box. Only boxes still
 *  mid-stream follow — finished boxes that merely remain in the DOM (collapsed
 *  or user-expanded) must keep their manual position. */
function followThinkingScrollToBottom() {
  nextTick(() => {
    for (const [key, el] of thinkingInlineEls) {
      if (!el) continue
      const isStreamingLive = !!el.closest('.chat-thinking')?.classList.contains('thinking-streaming')
      if (!isStreamingLive) continue
      if (thinkingScrollLeft[key]) continue
      // Only follow boxes whose content is actually overflowing (a short box
      // with no scrollbar must not be forced — scrollTop is 0 either way).
      if (el.scrollHeight <= el.clientHeight) continue
      el.scrollTop = el.scrollHeight
    }
  })
}

/** Click inside expanded tool-detail: dispatch to tool action handlers first, then fall through to generic behavior. */
function handleToolDetailClick(event: Event) {
  // Read-only (share) mode: never dispatch a tool action. handleToolAction can
  // POST to the server (permission respond, ask-question submit), which an
  // anonymous viewer must not do and which would fail anyway.
  if (props.readOnly) return

  // Try tool-specific action handler first (via data-tool-name on the .tool-detail container)
  const el = event.currentTarget as HTMLElement | null
  const toolName = el?.dataset?.toolName
  if (toolName && handleToolAction(toolName, event as any, emit as any)) return
  // Allow file-open buttons, file-path spans, commit-hash elements, and table rows to bubble
  const target = event.target as HTMLElement
  if (target.closest('.chat-file-open-btn') || target.closest('.chat-file-path') || target.closest('a[href]') || target.closest('.chat-commit-hash, .chat-commit-open-btn') || target.closest('.chat-worktree-btn') || target.closest('tbody tr[data-row-idx]')) {
    return
  }
  event.stopPropagation()
}

function handleToolDetailInput(event: Event) {
  // Typing in the supplementary field also persists the note, so it survives a
  // re-render (the field itself is part of the v-html body).
  if (handleAskSupplementaryInput(event)) return
  const target = event.target as HTMLElement
  const askView = target.closest('.ask-question-view')
  if (askView) {
    updateAskSubmitState(askView)
  }
}

/**
 * Ctrl/Cmd+Enter inside an ask card submits it, so the answer needs no reach for
 * the button. Bound on the same container as the click handler, which is what
 * scopes it to "the card the keystroke happened in" — several cards (chat list,
 * tool-detail drawer) can be on screen at once.
 *
 * Read-only (share) mode never dispatches, for the same reason
 * handleToolDetailClick bails: the emit would try to POST to an auth-protected
 * endpoint from an anonymous viewer.
 */
function handleToolDetailKeydown(event: KeyboardEvent) {
  if (props.readOnly) return
  handleAskSubmitShortcut(event, emit as any)
}

// The ask-card body is rebuilt from an HTML string whenever the rendered
// content changes (a loadHistory reload after switching tabs or backgrounding,
// a merged-card branch flip) OR the component is remounted wholesale.
//
// Both paths must be covered. ChatMessageList binds :key="listKey"
// (sessionId|msgs.length|first|last), so a message arriving — e.g. a reply that
// landed while the app was backgrounded — remounts the entire list. onUpdated
// does NOT fire on initial mount, and an AskUserQuestion card carries its input
// inline (interactive tool) so nothing triggers a later update: an update-only
// hook silently left the restored answer unapplied. Found via end-to-end
// testing in a real browser after the update-only version passed every test.
//
// `restoreAskStatesInContainer` returns immediately when the subtree holds no
// keyed card, so the streaming-frame cost is a single querySelectorAll.
const contentRootRef = ref<HTMLElement | null>(null)
function restoreAskStates() {
  const root = contentRootRef.value
  if (root) restoreAskStatesInContainer(root)
}
onMounted(() => nextTick(restoreAskStates))
onUpdated(restoreAskStates)

/**
 * Re-verify path/commit annotations in this component's own subtree.
 *
 * `data-path-type` is applied ONLY by `verifyFilePaths` mutating the live DOM —
 * it is never part of the HTML string that `StaticBlockCache` stores. The
 * pipeline schedules verification from `renderMarkdown`, which runs inside
 * `renderTextBlock`; on a cache hit `getBlockHtml` returns the stored string
 * before `renderTextBlock` is reached, so nothing re-schedules it.
 *
 * Every rebuild of the DOM from cached HTML therefore used to leave spans that
 * look annotated but do nothing when clicked. That happens on any `listKey`
 * change — a new message arriving remounts the whole list, as do `loadMore`
 * and session switches — which is why a hard refresh (empty cache → full
 * pipeline) appeared to fix it.
 *
 * Scoped to this component's root so the batch-exists request only covers
 * paths this subtree actually renders, and gated on the absence of
 * `data-path-type` so an already-verified span is not re-requested on every
 * streaming frame.
 */
function reverifyAnnotations() {
  const root = contentRootRef.value
  if (!root) return
  const paths: string[] = []
  const shas: string[] = []
  for (const el of root.querySelectorAll('.chat-file-path[data-file-path]:not([data-path-type])')) {
    const p = el.getAttribute('data-file-path')
    if (p) paths.push(p)
  }
  for (const el of root.querySelectorAll('.chat-commit-hash-pending[data-commit-sha]')) {
    const s = el.getAttribute('data-commit-sha')
    if (s) shas.push(s)
  }
  if (paths.length > 0) void verifyFilePaths([...new Set(paths)], root)
  if (shas.length > 0) void verifyCommitHashes([...new Set(shas)], root)
}
// A cache hit skips renderTextBlock, so this hook is the only thing that can
// re-verify on the update path. onMounted covers the list-remount path, where
// onUpdated never fires (same reason the ask-state hook above needs both).
onMounted(() => nextTick(reverifyAnnotations))
onUpdated(() => nextTick(reverifyAnnotations))

/**
 * Render any Mermaid blocks this component still holds as raw source.
 *
 * The chat Mermaid pass is a ONE-SHOT render: `updateRenderedContents(true)`
 * schedules a single `renderMermaidInElement` on a `nextTick`
 * (useChatRender.ts), and that function only picks up
 * `pre.mermaid:not([data-rendered])`. Any `<pre class="mermaid">` written into
 * the DOM AFTER that pass is therefore never rendered and stays visible as raw
 * source. Two writers reliably land late:
 *
 *  - `listKey` embeds `messages.length` (ChatMessageList.vue), so a new message
 *    remounts the whole list, recreating every `pre.mermaid` from cached HTML.
 *  - The newest message's text block is re-patched by a late `v-html` write
 *    (throttled flush / cache upgrade), inserting its block after the pass.
 *
 * Measured on a real session: every cold load ended with exactly ONE leftover
 * `pre.mermaid`, always in the LAST assistant message — i.e. the diagram the
 * user had just asked for. Any later render pass converted it (which is why a
 * theme switch or session re-open appeared to "fix" it).
 *
 * Same shape as `reverifyAnnotations` above: scoped to this component's subtree
 * (so concurrent instances never race for the same element) and idempotent.
 * `onMounted` covers the list-remount path, where `onUpdated` never fires.
 *
 * Deliberately skipped while `streaming`: the fence is still incomplete
 * mid-stream, and the post-streaming pass owns that first render. The
 * synchronous guard keeps the lazy 608KB Mermaid import out of the update path
 * when there is nothing to do.
 */
function renderPendingMermaid() {
  if (props.streaming) return
  const root = contentRootRef.value
  if (!root || !root.querySelector('pre.mermaid:not([data-rendered])')) return
  void renderMermaidInElement(root, 'chat-mermaid-block')
}
onMounted(() => nextTick(renderPendingMermaid))
onUpdated(() => nextTick(renderPendingMermaid))

// ── Throttled streaming render ──
// Rendered-HTML cache for streaming text/thinking blocks, keyed by stable block
// key. Deliberately a `shallowRef<Map>` rather than a `ref<Record>`:
//
//  * A deep `ref` proxies the whole record, so every `cache.value[key]` read in
//    the template went through Vue's reactive `get` trap — and each write
//    replaced the object (`{ ...cache, [k]: v }`), re-proxying all entries. On
//    the message that produced the 2026-09-20 freeze trace (241 blocks, ~180
//    stream events/s) that was tens of thousands of proxy allocations per
//    second, showing up as reactive-`get` self time plus GC.
//  * `shallowRef` still triggers on `.value = newMap`, so the template
//    re-renders after a throttled flush exactly as before — it just does not
//    pay per-key proxy cost, because the Map it hands back is raw.
//
// Always assign a NEW Map (never mutate in place): `shallowRef` only tracks
// `.value` assignment.
const blockHtmlCache = shallowRef<Map<string, string>>(new Map())
// Source text behind each cache entry. The flush reuses the cached HTML when the
// source is unchanged instead of re-running marked + DOMPurify for every block
// every 300ms — in a long turn with concurrent sub-agents that was thousands of
// sanitize passes per flush, freezing the main thread for seconds.
// Kept non-reactive (a Map) so it never triggers the blockHtmlCache watcher.
const _blockHtmlSource = new Map<string, string>()
let _throttleTimer: ReturnType<typeof setTimeout> | null = null
let _throttlePending = false
const THROTTLE_MS = 300
// How many trailing blocks count as "currently visible" while streaming, for
// the done-block auto-load. The live path keeps the newest reasoning rendered
// open, so those must load without a click; anything further back is a
// collapsed chip and loads on expand instead. Keeps a long turn from firing one
// request per finished block on every render.
const DONE_AUTOLOAD_TAIL = 6
const _blockFlushScheduler = new StreamFrameScheduler()

/**
 * Options for rendering a thinking block WHILE streaming.
 *
 * Deliberately identical to the text-block streaming path (`renderTextBlock`
 * with streaming=true): both enhancements are skipped. The two paths used to
 * diverge — text skipped path annotation, thinking kept it — and that
 * asymmetry was not a decision, just two independently-evolved call sites.
 *
 * What the divergence cost: a turn routinely names a file BEFORE writing it.
 * The thinking block annotated that path mid-stream, verified it against a
 * not-yet-created file and cached 'none'; when the turn's final text then
 * reported the same path, verification hit the cached 'none' and STRIPPED the
 * annotation — the file existed, the path stayed dead until a hard refresh.
 * The turn-end `invalidateNegativePathCache()` below only papered over it.
 *
 * Nothing is lost: once streaming ends the block takes the non-streaming
 * branch and is annotated in full, and `reverifyAnnotations` covers the
 * paths `renderMarkdownHtml` discards.
 */
const THINKING_STREAMING_OPTS = { skipEnhancements: true, skipKatex: true } as const

/**
 * The text a thinking block should render during streaming.
 *
 * Thin wrapper over the shared `thinkingRenderSource` that supplies the
 * lazy-loaded prefix from the thinking-text cache. Both render paths (this
 * component's `getThinkingHtml` and the throttled `flushBlockHtml`) go through
 * it so they can never disagree about a block's content — see the helper's doc
 * for what that divergence cost.
 */
function thinkingSource(block: any): string {
  return thinkingRenderSource(block, block?.think_id ? thinkingContent.cachedText(block.think_id) : undefined)
}

/** Drop every cached block's HTML and its source, forcing a full re-render.
 *  Use this instead of assigning `blockHtmlCache.value = new Map()` directly —
 *  clearing only the HTML would leave the source map matching, and the next
 *  flush would treat the (now empty) cache as up to date and render nothing. */
function invalidateBlockHtml() {
  blockHtmlCache.value = new Map()
  _blockHtmlSource.clear()
}

function flushBlockHtml() {
  _throttleTimer = null
  if (!_throttlePending) return
  // Skip rendering when panel not visible
  if (!props.active) {
    _throttlePending = false
    return
  }
  _throttlePending = false
  // Schedule the actual work in the next rAF to coalesce with
  // other streaming updates (debouncedRender, scrollTick).
  _blockFlushScheduler.schedule('flush', () => {
    const prevCache = blockHtmlCache.value
    const newCache = new Map<string, string>()
    // Rebuilt in the same pass so entries for blocks that disappeared
    // (content_reset, merge, rewind) don't linger and wrongly mark a future
    // same-keyed block as already rendered.
    const newSources = new Map<string, string>()
    for (let i = 0; i < (props.blocks?.length || 0); i++) {
      const block = props.blocks[i]
      // Sub-agent children are not rendered in this flat loop (the template
      // skips them via isChildBlock and mounts them inside their parent's
      // recursive group). Rendering their markdown here would compute HTML
      // that never reaches the DOM — measured at 98.4% of all text/thinking
      // blocks in a concurrent-subagent turn, i.e. the bulk of the freeze.
      if (isChildBlock(block)) continue
      if (block.type === 'text') {
        const key = stableBlockKey(i, block)
        const src = block.text ?? ''
        // Unchanged source → reuse the existing HTML (no marked/DOMPurify).
        const cached = prevCache.get(key)
        if (_blockHtmlSource.get(key) === src && cached !== undefined) {
          newCache.set(key, cached)
        } else {
          // streaming=true: deferred rendering — pure markdown only
          newCache.set(key, props.renderTextBlock(block.text, props.msgId, i, true))
        }
        newSources.set(key, src)
      } else if (block.type === 'thinking') {
        const key = `t-${stableBlockKey(i, block)}`
        // The SOURCE must match what getThinkingHtml renders, not just block.text.
        // A thinking block can carry a lazy-loaded prefix (think_id → chat_thinking)
        // that is stitched onto its live deltas — so its rendered text is
        // mergeThinkingPrefix(cachedText, block.text), not block.text. Using
        // block.text here made this throttled flush render the block WITHOUT its
        // prefix, while the normal render path (getThinkingHtml) rendered it WITH
        // the prefix. The two alternate every ~300ms, so the block visibly
        // blanked and refilled on a loop — the reported "flashes every few
        // seconds, looks like it clears and reloads, no new text appears".
        const src = thinkingSource(block)
        const cached = prevCache.get(key)
        if (_blockHtmlSource.get(key) === src && cached !== undefined) {
          newCache.set(key, cached)
        } else {
          // Thinking blocks use renderMarkdownHtml during streaming. Same
          // options as a streaming text block — see THINKING_STREAMING_OPTS.
          newCache.set(key, renderMarkdownHtml(src, THINKING_STREAMING_OPTS))
        }
        newSources.set(key, src)
      }
    }
    blockHtmlCache.value = newCache
    _blockHtmlSource.clear()
    for (const [k, v] of newSources) _blockHtmlSource.set(k, v)
    // Throttled render flush can change content height (paragraph wrapping, code blocks, etc.)
    // without a corresponding onScrollBottom call from the stream handler. Notify the parent
    // so it can re-sync the scroll position if the user is at the bottom.
    emit('render-flush')
  })
}

function getBlockHtml(bi: number, block: any) {
  const ai = absIdx(bi)
  // A text block persisted with empty content has no `text` key at all
  // (ContentBlock.Text is `omitempty` on the Go side), so block.text is
  // undefined here. Normalize once: every path below (cache key, markdown
  // pipeline) expects a string, and an undefined used to throw inside the
  // cache key builder and blank the entire message.
  const text: string = block.text ?? ''
  if (!props.streaming) {
    // Non-streaming: full pipeline with cache
    if (props.staticBlockCache) {
      // Read the scope before the lookup: this both keys the entry correctly
      // and registers the render-time dependency on projectRoot/locale.
      const scope = cacheScope.value
      const cached = props.staticBlockCache.get(props.msgId, ai, text, scope)
      if (cached !== undefined) {
        return cached
      }
      // First render: use full pipeline directly — no deferred rendering.
      // Deferred rendering (skipEnhancements=true → scheduleUpgrade) causes
      // scrollHeight to change after initial paint, creating a visible "snap"
      // when scrollToBottom corrects for the height difference.
      const fullHtml = props.renderTextBlock(text, props.msgId, ai, false, false)
      props.staticBlockCache.set(props.msgId, ai, text, fullHtml, false, scope)
      return fullHtml
    }
    return props.renderTextBlock(text, props.msgId, ai, false)
  }
  // Streaming + panel not visible: skip expensive markdown parsing
  if (!props.active) {
    return ''
  }
  // Streaming: deferred rendering with throttling
  const key = stableBlockKey(bi, block)
  const src = text
  const cache = blockHtmlCache.value
  const cached = cache.get(key)
  if (cached !== undefined) {
    // Only re-render when the source actually changed. Re-rendering on every
    // template pass (which the flush itself triggers by replacing the cache)
    // re-armed the 300ms timer forever, so a long turn never stopped
    // re-running marked + DOMPurify. An unchanged block just serves its HTML.
    if (_blockHtmlSource.get(key) === src) {
      return cached
    }
    if (!_throttleTimer) {
      // Copy-on-write: the flush reads the previous Map, and the template may
      // read the new one mid-pass, so never mutate the Map in place.
      const newCache = new Map(cache)
      newCache.set(key, props.renderTextBlock(text, props.msgId, ai, true))
      blockHtmlCache.value = newCache
      // The write above is fresh for this text — record it so the next flush
      // does not re-render (and so it is not treated as stale).
      _blockHtmlSource.set(key, src)
      _throttleTimer = setTimeout(flushBlockHtml, THROTTLE_MS)
    } else {
      _throttlePending = true
    }
    return blockHtmlCache.value.get(key) ?? ''
  }
  const html = props.renderTextBlock(text, props.msgId, ai, true)
  const withHtml = new Map(cache)
  withHtml.set(key, html)
  blockHtmlCache.value = withHtml
  _blockHtmlSource.set(key, src)
  return html
}

/** Get HTML for thinking block content.
 *
 *  A block can hold BOTH a lazy-loaded prefix (think_id → chat_thinking) and
 *  live deltas (text). That happens when a block's think_id is adopted from the
 *  DB marker on a session switch: the prefix is everything flushed before the
 *  switch, `text` is what arrived after. Rendering only `text` dropped the
 *  prefix — the reported "same block, front half gone, continues from the
 *  middle". mergeThinkingPrefix stitches them (trimming any overlap).
 *
 *  Slim blocks with no live text render purely from the lazy-load cache. */
function getThinkingHtml(bi: number, block: any) {
  // Read-only hosts (the public share page) render a settled snapshot: every
  // thinking block starts collapsed and is hidden by CSS, yet `v-html` would
  // still run full markdown + DOMPurify rendering for text nobody can see. A
  // shared conversation routinely carries megabytes of reasoning across
  // hundreds of blocks (measured: 4.4 MB / 134k DOM nodes on a 49-message
  // thread = ~7s of blocked main thread), so here a collapsed block renders
  // nothing and materializes only when the user expands it.
  //
  // Scoped to readOnly on purpose: the interactive app's streaming path calls
  // this every frame and its collapse animation repaints the content for the
  // 350ms grid transition, so that path is left exactly as it was.
  if (props.readOnly && isThinkingCollapsed(block, bi)) return ''

  const src = thinkingSource(block)
  if (src) {
    return getThinkingTextHtml(src, bi, block)
  }
  if (block.think_id) {
    if (thinkingContent.errors.value[block.think_id]) {
      return `<div class="thinking-load-error"><span>${t('chat.contentBlocks.thinkingLoadFailed')}</span><button class="thinking-retry-btn" onclick="this.closest('.chat-thinking').querySelector('.thinking-header').click()">${t('chat.contentBlocks.retry')}</button></div>`
    }
    return '<div class="placeholder-dots"><span></span><span></span><span></span></div>'
  }
  return ''
}

/** Existing throttled streaming/inline render path (unchanged behavior). */
function getThinkingTextHtml(text: string, bi: number, block: any) {
  if (!props.streaming || !props.active) {
    return renderMarkdownHtml(text)
  }
  // Streaming: skip KaTeX and path annotation (formulas incomplete, and the
  // file a turn names may not be written yet — see THINKING_STREAMING_OPTS).
  const streamingOpts = THINKING_STREAMING_OPTS
  const cacheKey = `t-${stableBlockKey(bi, block)}`
  // Streaming: deferred rendering with throttling (same pattern as text blocks)
  const cache = blockHtmlCache.value
  const cached = cache.get(cacheKey)
  if (cached !== undefined) {
    // Unchanged source → serve the cached HTML instead of re-rendering (and
    // re-arming the timer) on every template pass. See getBlockHtml.
    if (_blockHtmlSource.get(cacheKey) === text) {
      return cached
    }
    if (!_throttleTimer) {
      const newCache = new Map(cache)
      newCache.set(cacheKey, renderMarkdownHtml(text, streamingOpts))
      blockHtmlCache.value = newCache
      _blockHtmlSource.set(cacheKey, text ?? '')
      _throttleTimer = setTimeout(flushBlockHtml, THROTTLE_MS)
    } else {
      _throttlePending = true
    }
    return blockHtmlCache.value.get(cacheKey) ?? ''
  }
  const html = renderMarkdownHtml(text, streamingOpts)
  const withHtml = new Map(cache)
  withHtml.set(cacheKey, html)
  blockHtmlCache.value = withHtml
  _blockHtmlSource.set(cacheKey, text ?? '')
  return html
}

watch(() => props.streaming, (streaming, wasStreaming) => {
  if (wasStreaming && !streaming) {
    if (_throttleTimer) { clearTimeout(_throttleTimer); _throttleTimer = null }
    _throttlePending = false
    _blockFlushScheduler.cancelAll()
    // A turn routinely names a file BEFORE writing it. Whichever pass verifies
    // such a path while it does not exist yet caches 'none', and a cached
    // 'none' is never re-checked — so a later turn that creates the file and
    // reports it would hit the stale negative and have its annotation
    // stripped (only a hard refresh recovered it). Thinking blocks used to be
    // the mid-stream trigger; now that streaming skips enhancements, the
    // remaining ones are the post-streaming pass of an earlier turn and any
    // tool-detail markdown the user expanded before the file existed.
    //
    // Drop the negative entries before the post-streaming re-render below
    // re-verifies every span, so this turn's newly created files resolve.
    // Verified 'file'/'dir' entries are kept — a turn cannot invalidate them.
    invalidateNegativePathCache()
    // Collapse all completed thinking blocks when message ends
    for (const blockKey of _collapseElKeys) {
      delete thinkingExpanded.value[blockKey]
      delete expandingThinking.value[blockKey]
      delete collapsingThinking.value[blockKey]
    }
    // Streaming ended: release the inner-scroll follow latches so the next
    // streamed deep-think starts pinned to the bottom again.
    thinkingScrollLeft = {}
    // Clear throttle cache and force a full re-render of thinking HTML
    invalidateBlockHtml()
  }
})

// Watch for thinking blocks that become "done" mid-stream (via thinking_done SSE event).
// Only the block currently being streamed stays expanded — when its output
// completes it collapses immediately. Blocks the user manually expanded are kept open.
let _prevDoneKeys = new Set<string>()
// The watched value is a joined STRING, not a fresh array: a watcher returning an
// array is never `Object.is`-equal to its previous value, so it fired on every
// blocks mutation — invalidating the whole HTML cache on each streaming tick and
// defeating the incremental cache. A string compares by value, so this only runs
// on a real done-set change. Building it with map/filter/join also avoids the
// indexOf-per-thinking-block (O(n²) on a message with thousands of blocks).
watch(
  () => props.blocks.map((b: any, i: number) => (b?.type === 'thinking' && b.done ? stableBlockKey(i, b) : '')).filter(Boolean).join('|'),
  (joined: string) => {
    const doneKeys = joined ? joined.split('|') : []
    if (!props.streaming) {
      // Not streaming: nothing to collapse live; remember the done set for later.
      _prevDoneKeys = new Set(doneKeys)
      return
    }
    const doneSet = new Set(doneKeys)
    for (const key of doneKeys) {
      // Collapse only the blocks that JUST finished streaming (newly done),
      // skipping ones already collapsed/collapsing or manually expanded.
      if (_prevDoneKeys.has(key)) continue
      if (thinkingExpanded.value[key] || collapsingThinking.value[key]) continue
      triggerThinkingCollapse(key)
    }
    _prevDoneKeys = doneSet
    // Clear throttle cache so DOM re-renders with complete thinking content
    invalidateBlockHtml()
  },
)

// Reset cache when panel becomes active — allows re-render with fresh markdown
watch(() => props.active, (active) => {
  if (active) {
    invalidateBlockHtml()
    if (_throttleTimer) { clearTimeout(_throttleTimer); _throttleTimer = null }
    _throttlePending = false
  }
})

// Load the text of thinking blocks that must be visible NOW, without a click.
//
// A thinking block's reasoning lives in chat_thinking, and the content row only
// carries a slim {think_id} marker. getThinkingHtml renders that marker's block
// from the lazy-load cache; with no cache entry it falls through to
// placeholder-dots (three pulsing dots) — which the user reads as "this thinking
// block is stuck and never finishes".
//
// Three shapes need a load, and all three were previously unhandled or handled
// by a separate watcher:
//
//   in_progress  — the block is still streaming. Its already-emitted prefix must
//                  be fetched immediately or the block "continues from the
//                  middle". The snapshot may be incomplete, so it is cached as
//                  PROVISIONAL (see useThinkingContent) and replaced later.
//   done + provisional — the block finished while holding a provisional
//                  snapshot. That snapshot is a prefix of the finished
//                  reasoning, so it must be refetched; nothing else would ever
//                  replace it (the render path serves the cache whenever an
//                  entry exists). Reported case: a 25131-char reasoning frozen
//                  at its first 10175 chars.
//   done + never loaded — the block finished before this client looked at it.
//                  Every other path excluded it (in_progress does not match,
//                  provisional does not apply) and a chip showing only dots
//                  gives no reason to click. Observed 40120 times in one
//                  session's client.log as
//                  done=true cached=none loading=false err=none.
//
// Scoped to the blocks the live view actually renders open — an expanded block,
// or (while streaming) the tail — so a finished conversation does not bulk-load
// hundreds of collapsed blocks, and so the auto-load does not preempt the click
// path (a user expanding a FAILED block to retry must not silently receive the
// cached failure). Everything else keeps lazy-load-on-click.
watch(
  () => {
    const blocks = props.blocks || []
    const turnOver = !props.streaming
    const tailStart = props.streaming ? Math.max(0, blocks.length - DONE_AUTOLOAD_TAIL) : blocks.length
    const ids: string[] = []
    for (let bi = 0; bi < blocks.length; bi++) {
      const b = blocks[bi] as any
      if (b?.type !== 'thinking' || !b.think_id) continue
      if (thinkingContent.errors.value[b.think_id]) continue
      // A block with live deltas still needs its prefix fetched: the render
      // path stitches prefix+text, so without the prefix the front half is
      // missing. (A block with text but no think_id has nothing to fetch.)
      const cached = thinkingContent.cachedText(b.think_id)
      const provisional = thinkingContent.isProvisional(b.think_id)

      // Still streaming: always (re)load. loadThinking is idempotent for a
      // provisional entry, so re-firing on every blocks change is cheap.
      if (b.in_progress) { ids.push(`p:${b.think_id}`); continue }
      if (!b.done) continue

      // Finished. Reload when the cached text is a stale mid-stream snapshot
      // (nothing else would replace it), or when nothing was ever loaded AND
      // the block is in the streaming tail the live view renders open.
      //
      // `done` does NOT mean the text is final, so the final fetch waits for the
      // turn to end. The backend flushes before forwarding thinking_done, so the
      // text is complete AS OF that event — but the accumulator keeps appending
      // later deltas to the SAME block (it has no done check: a thinking →
      // thinking_done → content → thinking sequence yields one block whose text
      // grows after done). A "final" fetch on done would therefore cache a
      // prefix and clear the provisional flag, after which `cached !== undefined`
      // skipped it forever — the reported truncated reasoning (cached 760 of
      // final 844; another case 17124 of 18887). At turn end the row is complete
      // (the terminal `done` is sent after Finalize), so that is when we refetch.
      if (provisional) { if (turnOver) ids.push(`f:${b.think_id}`); continue }
      if (cached !== undefined) continue
      // NOT for merely-expanded blocks: expanding one goes through
      // handleThinkingClick, which loads it itself. Auto-loading it here would
      // fire a second request that races the click's own — and on a FAILED
      // block it would swallow the retry's fresh request.
      //
      // Mid-turn the snapshot is provisional (the block may still grow), so the
      // refetch above can replace it with the final text once the turn ends.
      if (bi >= tailStart) ids.push(`${turnOver ? 'f' : 'p'}:${b.think_id}`)
    }
    return ids.join('|')
  },
  (joined: string) => {
    if (!joined) return
    for (const entry of joined.split('|')) {
      // `p:` — the block is still streaming, so the result is provisional.
      const provisional = entry.startsWith('p:')
      const thinkId = entry.slice(2)
      thinkingContent.loadThinking(thinkId, props.msgId, props.sessionId, provisional)
        .then((text) => {
          diagLog(TAG, `thinking auto-load ok: msgId=${props.msgId} think_id=${thinkId} len=${text.length} provisional=${provisional}`)
          invalidateBlockHtml()
        })
        .catch((e) => { diagLog(TAG, `thinking auto-load FAILED: msgId=${props.msgId} think_id=${thinkId} err=${e?.message || e}`) })
    }
  },
  { immediate: true },
)

// Follow the live stream inside each thinking box. The thinking HTML is served
// through v-html from blockHtmlCache; whenever the cache is rewritten during
// streaming the browser keeps the box's old scrollTop, so re-pin any live box
// to its bottom (unless the user scrolled up to read earlier reasoning there).
// flush:'post' runs after the v-html DOM update lands, so scrollTop assignment
// targets the freshly-rendered content.
watch(blockHtmlCache, () => {
  if (!props.streaming) return
  followThinkingScrollToBottom()
}, { flush: 'post' })

onUnmounted(() => {
  stopElapsedTimer()
  if (_throttleTimer) { clearTimeout(_throttleTimer); _throttleTimer = null }
  _blockFlushScheduler.cancelAll()
  _collapseTimers.forEach(t => clearTimeout(t))
  _collapseTimers = []
})
</script>

<style scoped>
.streaming-status {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.streaming-elapsed {
  color: var(--text-muted, #999);
  font-size: var(--font-size-xs);
  font-variant-numeric: tabular-nums;
  line-height: 1;
}

.placeholder-dots {
  display: flex;
  gap: var(--space-2);
  align-items: center;
  padding: var(--space-4) 0 var(--space-2);
}
.placeholder-dots span {
  width: calc(7 * var(--dp)); height: 7px;
  border-radius: 50%;
  background: var(--text-muted, #999);
  animation: dot-bounce 1.2s infinite ease-in-out;
}
.placeholder-dots span:nth-child(1) { animation-delay: 0s; }
.placeholder-dots span:nth-child(2) { animation-delay: 0.2s; }
.placeholder-dots span:nth-child(3) { animation-delay: 0.4s; }

@keyframes dot-bounce {
  0%, 80%, 100% { transform: scale(0.6); opacity: var(--opacity-disabled); }
  40% { transform: scale(1); opacity: 1; }
}

/* Slim thinking block lazy-load error state */
.thinking-load-error {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-2) 0;
  font-size: var(--font-size-sm);
  color: #dc2626;
}

.thinking-retry-btn {
  border: 1px solid color-mix(in srgb, #ef4444 40%, var(--border-color));
  background: transparent;
  color: #dc2626;
  font-size: var(--font-size-xs);
  padding: 1px var(--space-4);
  border-radius: var(--radius-xs);
  cursor: pointer;
}

@media (hover: hover) {
  .thinking-retry-btn:hover {
    background: rgba(239, 68, 68, 0.08);
  }
}

/* Inline cancelled marker inside thinking header — always visible even when thinking is collapsed */
.chat-cancelled-mark-inline {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 1px var(--space-3);
  border-radius: var(--radius-xs);
  margin-left: auto;
}





.chat-error-card {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-5);
  margin: var(--space-1) 0;
  border-left: 3px solid var(--color-red, #ef4444);
  background: color-mix(in srgb, var(--color-red, #ef4444) 8%, transparent);
}

.chat-error-card .error-icon {
  flex-shrink: 0;
  color: var(--color-red, #ef4444);
}

.chat-error-card .error-text {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--color-red, #dc2626);
}

:root[data-theme-base="dark"] .chat-error-card {
  border-left-color: var(--color-red, #f87171);
  background: color-mix(in srgb, var(--color-red, #f87171) 10%, transparent);
}

:root[data-theme-base="dark"] .chat-error-card .error-icon {
  color: var(--color-red, #f87171);
}

:root[data-theme-base="dark"] .chat-error-card .error-text {
  color: var(--color-red, #fca5a5);
}

.chat-warning-card {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-5);
  margin: var(--space-1) 0;
  border-left: 3px solid var(--color-yellow, #f59e0b);
  background: color-mix(in srgb, var(--color-yellow, #f59e0b) 8%, transparent);
}

.chat-warning-card .warning-icon {
  flex-shrink: 0;
  color: var(--color-yellow, #f59e0b);
}

.chat-warning-card .warning-text {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--color-yellow, #d97706);
  white-space: pre-wrap;
  word-break: break-word;
}

:root[data-theme-base="dark"] .chat-warning-card {
  border-left-color: var(--color-yellow, #fbbf24);
  background: color-mix(in srgb, var(--color-yellow, #fbbf24) 10%, transparent);
}

:root[data-theme-base="dark"] .chat-warning-card .warning-icon {
  color: var(--color-yellow, #fbbf24);
}

:root[data-theme-base="dark"] .chat-warning-card .warning-text {
  color: var(--color-yellow, #fcd34d);
}

.error-source-chip {
  flex-shrink: 0;
  margin-left: var(--space-4);
  padding: 1px var(--space-4);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  border-radius: var(--radius-full);
  line-height: var(--line-height-relaxed);
  white-space: nowrap;
}

/* Source-specific chip colors: agent = amber, clawbench = red, network = blue */
.error-source-chip.src-agent {
  color: var(--color-yellow, #d97706);
  background: color-mix(in srgb, var(--color-yellow, #f59e0b) 14%, transparent);
}

.error-source-chip.src-clawbench {
  color: var(--color-red, #dc2626);
  background: color-mix(in srgb, var(--color-red, #ef4444) 12%, transparent);
}

.error-source-chip.src-network {
  color: var(--color-info);
  background: color-mix(in srgb, var(--color-info) 12%, transparent);
}

:root[data-theme-base="dark"] .error-source-chip.src-agent {
  color: var(--color-yellow, #fcd34d);
}

:root[data-theme-base="dark"] .error-source-chip.src-clawbench {
  color: var(--color-red, #fca5a5);
}

:root[data-theme-base="dark"] .error-source-chip.src-network {
  color: var(--color-info);
}

.chat-warning-card .warning-continue-btn {
  flex-shrink: 0;
  margin-left: auto;
  padding: var(--space-1) var(--space-5);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: #fff;
  background: #f59e0b;
  border: none;
  border-radius: var(--radius-xs);
  cursor: pointer;
  transition: background var(--duration-slow);
}

@media (hover: hover) {
  .chat-warning-card .warning-continue-btn:hover {
    background: #d97706;
  }
}

/* Reset-session button — same amber styling as the continue button, matching
   the warning banner color scheme. */
.warning-reset-btn {
  flex-shrink: 0;
  margin-left: auto;
  padding: var(--space-1) var(--space-5);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: #fff;
  background: #f59e0b;
  border: none;
  border-radius: var(--radius-xs);
  cursor: pointer;
  transition: background var(--duration-slow);
}

@media (hover: hover) {
  .warning-reset-btn:hover {
    background: #d97706;
  }
}

/* Thinking block — callout style distinct from tool calls */
.chat-thinking {
  --thinking-accent: #8b5cf6;
  --thinking-transition: 300ms ease;
  background: color-mix(in srgb, var(--thinking-accent) 4%, transparent);
  border: none;
  border-left: 3px solid color-mix(in srgb, var(--thinking-accent) 50%, transparent);
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
  margin: var(--space-3) 0;
  width: 100%;
}

:root[data-theme-base="dark"] .chat-thinking {
  --thinking-accent: #a78bfa;
}

/* Collapsed state: pill-shaped clickable chip */
.chat-thinking.thinking-collapsed {
  border-radius: var(--radius-lg);
  border-left: none;
  border: 1px solid color-mix(in srgb, var(--thinking-accent) 20%, var(--border-color));
  background: color-mix(in srgb, var(--thinking-accent) 6%, var(--bg-secondary));
}

.chat-thinking.thinking-collapsed .thinking-header {
  cursor: pointer;
}

@media (hover: hover) {
  .chat-thinking.thinking-collapsed:hover {
    background: color-mix(in srgb, var(--thinking-accent) 12%, var(--bg-secondary));
    border-color: color-mix(in srgb, var(--thinking-accent) 35%, var(--border-color));
  }
}

/* Expanded-done state: callout style, header is clickable to collapse */
.chat-thinking.thinking-expanded-done .thinking-header {
  cursor: pointer;
}

@media (hover: hover) {
  .chat-thinking.thinking-expanded-done:hover {
    background: color-mix(in srgb, var(--thinking-accent) 7%, transparent);
    border-left-color: color-mix(in srgb, var(--thinking-accent) 65%, transparent);
  }
}

/* Streaming state: callout style */
.chat-thinking.thinking-streaming {
  /* no extra rules needed — base callout style applies */
}

/* Collapse animation state: transitioning border from callout to pill */
.chat-thinking.thinking-collapsing {
  border-radius: var(--radius-lg);
  border-left: none;
  border: 1px solid color-mix(in srgb, var(--thinking-accent) 20%, var(--border-color));
  background: color-mix(in srgb, var(--thinking-accent) 6%, var(--bg-secondary));
}

.chat-thinking.thinking-collapsing .thinking-header {
  cursor: pointer;
}

/* Expand animation state: transitioning border from pill to callout */
.chat-thinking.thinking-expanding {
  /* Uses base callout style — border transition handled by content wrapper */
}

/* Content wrapper: CSS grid 0fr↔1fr transition for buttery smooth expand/collapse.
   Non-streaming ("done") states cap the expanded content at a generous viewport
   fraction with inner scrolling — the box never blows past ~half the screen even
   for very long reasoning, matching how tool-detail text is capped (500px). The
   cap lives on .thinking-inline-content (not this wrapper) because the grid
   transition must stay height-unconstrained to animate 0fr↔1fr correctly. */
.thinking-content-wrapper {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  transition: grid-template-rows var(--thinking-transition), opacity var(--duration-slow) ease, padding var(--duration-slow) ease;
}

.thinking-content-wrapper.thinking-content-open {
  grid-template-rows: 1fr;
  opacity: 1;
  padding: 0 var(--space-5) calc(3 * var(--dp));
}

.thinking-inline-content {
  overflow: hidden;
  min-height: 0;
  font-size: var(--font-size-sm);
  line-height: 1.65;
  color: var(--text-secondary);
  word-break: break-word;
}

/* Height governance across the block lifecycle:
   - While streaming the block is expanded live; cap the *expanded* content so a
     runaway deep-think never keeps stretching the message. Short content (a few
     lines) renders at natural height, so there is no awkward empty box.
   - The moment streaming ends the block auto-collapses to a chip (existing
     behavior); a user-expanded block re-opens under the large non-streaming cap.
   - Slim blocks (think_id, content fetched on demand) sit under the same large
     cap as expanded-done content.
   The two caps apply only when the grid wrapper is OPEN — while the 0fr→1fr
   expand/collapse animation runs the box animates freely between 0 and its
   content/cap height, and a closed block never leaves a stray scrollbar. */
.thinking-content-wrapper.thinking-content-open .thinking-inline-content {
  /* ~10 lines at the 12px/1.65 typography above; caps long reasoning while
     streaming without growing the message unbounded. */
  max-height: calc(220 * var(--dp));
  overflow-y: auto;
  overscroll-behavior: contain;
}

/* Large cap for *finished* content (block.done reached via thinking_done, or a
   message that stopped streaming): a generous viewport fraction — reasoning is
   usually several hundred lines, so ~half a viewport of scrollable depth keeps
   it inspectable without dominating the conversation. Only the user-expanded
   done state shows inline content at rest (finished blocks otherwise
   auto-collapse to a chip); the 0fr↔1fr expand animation stays smooth because
   the transition is not height-gated. */
.chat-thinking.thinking-expanded-done .thinking-content-wrapper.thinking-content-open .thinking-inline-content {
  max-height: min(50vh, calc(520 * var(--dp)));
}

.thinking-header {
  display: flex;
  align-items: center;
  gap: calc(5 * var(--dp));
  padding: calc(3 * var(--dp)) var(--space-5);
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.thinking-icon {
  color: color-mix(in srgb, var(--thinking-accent) 80%, transparent);
  flex-shrink: 0;
}

.thinking-label {
  font-weight: var(--font-weight-semibold);
  color: var(--thinking-accent);
  font-size: var(--font-size-xs);
  letter-spacing: 0.02em;
}

/* Spinner (unified LoadingIndicator) — right-aligned, tinted by the local accent */
.thinking-spinner {
  --li-color: var(--thinking-accent);
  margin-left: auto;
}

.thinking-chevron {
  flex-shrink: 0;
  margin-left: auto;
  color: var(--text-hint);
  transition: color var(--duration-base);
}

@media (hover: hover) {
  .chat-thinking.thinking-expanded-done:hover .thinking-chevron,
  .chat-thinking.thinking-collapsed:hover .thinking-chevron {
    color: var(--thinking-accent);
  }
}

/* Markdown styles inside thinking inline content */
.thinking-inline-content p { margin: 0 0 0.5em; }
.thinking-inline-content p:last-child { margin-bottom: 0; }
.thinking-inline-content pre {
  margin: 0.5em 0;
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-xs);
  overflow-x: auto;
  font-size: var(--font-size-xs);
}
.thinking-inline-content code {
  font-size: var(--font-size-xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
}
.thinking-inline-content pre code {
  padding: 0;
  background: none;
}
.thinking-inline-content blockquote {
  margin: 0.5em 0;
  padding: var(--space-2) var(--space-4);
  border-left: 2px solid var(--text-hint);
}
.thinking-inline-content h1,
.thinking-inline-content h2,
.thinking-inline-content h3 {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  margin: 0.5em 0 0.3em;
}
.thinking-inline-content ul,
.thinking-inline-content ol {
  margin: 0.3em 0;
  padding-left: 1.5em;
}
.thinking-inline-content table {
  border-collapse: collapse;
  margin: 0.5em 0;
  font-size: var(--font-size-xs);
}
.thinking-inline-content th,
.thinking-inline-content td {
  border: 1px solid var(--border-color);
  padding: var(--space-1) var(--space-3);
}
.thinking-inline-content th {
  background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  font-weight: var(--font-weight-semibold);
}
.thinking-inline-content strong {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}
.thinking-inline-content em {
  font-style: italic;
}
.thinking-inline-content mark {
  background: rgba(245, 158, 11, 0.2);
  color: inherit;
  padding: 1px var(--space-1);
  border-radius: var(--radius-xs);
}

/* Tool calls display */
.chat-tool-call {
  --tool-accent: var(--text-muted);
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: calc(5 * var(--dp));
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--tool-accent) 6%, var(--bg-secondary));
  border: 1px solid color-mix(in srgb, var(--tool-accent) 15%, var(--border-color));
  padding: calc(3 * var(--dp)) var(--space-4);
  border-radius: var(--radius-full);
  cursor: pointer;
  width: 100%;
  margin-top: var(--space-2);
  overflow: hidden;
}

.chat-tool-call[data-category="file"]     { --tool-accent: var(--accent-color); }
.chat-tool-call[data-category="bash"]     { --tool-accent: #10b981; }
.chat-tool-call[data-category="search"]   { --tool-accent: #8b5cf6; }
.chat-tool-call[data-category="task"]     { --tool-accent: #f59e0b; }
.chat-tool-call[data-category="plan"]     { --tool-accent: var(--accent-color); }
.chat-tool-call[data-category="agent"]    { --tool-accent: #ec4899; }
.chat-tool-call[data-category="skill"]    { --tool-accent: #06b6d4; }
.chat-tool-call[data-category="ask"]      { --tool-accent: #f97316; }
.chat-tool-call[data-category="permission"] { --tool-accent: #eab308; }
.chat-tool-call[data-category="fallback"] { --tool-accent: var(--text-muted); }

@media (hover: hover) {
  .chat-tool-call:hover {
    background: color-mix(in srgb, var(--tool-accent) 12%, var(--bg-secondary));
  }
}

.chat-tool-call .tool-icon {
    color: color-mix(in srgb, var(--tool-accent) 80%, transparent);
    flex-shrink: 0;
}

.chat-tool-call .tool-name {
  font-weight: var(--font-weight-semibold);
  color: var(--tool-accent);
  font-size: var(--font-size-xs);
}

.chat-tool-call .tool-summary {
  color: var(--text-hint);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-tool-call .tool-check {
  flex-shrink: 0;
  margin-left: auto;
}

.chat-tool-call .tool-warn {
  flex-shrink: 0;
  margin-left: auto;
}

.chat-tool-call .tool-error-icon {
  flex-shrink: 0;
  margin-left: auto;
}

/* ── Sub-agent group ──
   Collapsed: the Agent pill is IDENTICAL to every other tool pill (no special
   shape). Expanded: the pill becomes the rounded-rectangle top half and the
   body below joins it as a matching rounded-rectangle bottom half (2px pink
   accent border, small radius). The body holds a recursive ContentBlocks
   instance, so child thinking/tool blocks keep their normal callout/pill
   styling. */
.subagent-group {
  --subagent-accent: #ec4899;
  margin: 0 0 var(--space-2);
}

/* Only when expanded does the pill's shape change: it becomes the rounded-
   rectangle top half of the group (small radius, not a pill). */
.chat-tool-call-group-open {
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
}

.subagent-group-open > .subagent-group-body-wrapper {
  border-left: 2px solid color-mix(in srgb, var(--subagent-accent) 35%, transparent);
  border-right: 2px solid color-mix(in srgb, var(--subagent-accent) 35%, transparent);
  border-bottom: 2px solid color-mix(in srgb, var(--subagent-accent) 35%, transparent);
  border-bottom-left-radius: calc(6 * var(--dp));
  border-bottom-right-radius: calc(6 * var(--dp));
  background: color-mix(in srgb, var(--subagent-accent) 4%, transparent);
}

.subagent-group-count {
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
  flex-shrink: 0;
}

/* In-pill expand/collapse chevron button. */
.subagent-group-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  padding: 0;
  margin-left: var(--space-1);
  border: none;
  background: none;
  cursor: pointer;
  color: var(--text-hint);
  transition: color var(--duration-base);
}

.subagent-group-toggle:hover {
  color: var(--tool-accent, var(--subagent-accent));
}

/* Body: grid 0fr↔1fr transition (same technique as thinking). */
.subagent-group-body-wrapper {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  transition: grid-template-rows var(--duration-slow) ease, opacity var(--duration-slow) ease;
}

.subagent-group-body-wrapper.subagent-group-body-open {
  grid-template-rows: 1fr;
  opacity: 1;
}

/* Collapse is applied without a transition so the viewport anchor restore
   (toggleSubagentGroup) can measure the final height synchronously. */
.subagent-group-body-wrapper.subagent-group-body-nostransition {
  transition: none;
}

.subagent-group-body {
  overflow: hidden;
  min-height: 0;
}

.subagent-group-body-open .subagent-group-body {
  padding: var(--space-3) var(--space-5);
}

/* Bottom "collapse" affordance shown only while the group is open. */
.subagent-group-footer {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  width: 100%;
  margin-top: var(--space-3);
  padding: var(--space-2) 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  background: none;
  border: none;
  border-top: 1px solid color-mix(in srgb, var(--subagent-accent) 20%, var(--border-color));
  cursor: pointer;
  transition: color var(--duration-base);
}

.subagent-group-footer:hover {
  color: var(--subagent-accent);
}

/* Inline tool detail — only used by AskUserQuestion (other tools use ToolDetailDrawer) */
.tool-detail {
  margin: var(--space-1) 0 var(--space-2) 0;
  padding: var(--space-3) var(--space-4);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-snug);
  background: var(--bg-primary);
  border-radius: var(--radius-xs);
  border: 1px solid var(--border-color);
  white-space: normal;
  overflow-x: clip;
  overflow-y: auto;
  max-height: calc(500 * var(--dp));
  cursor: default;
}

/* Spinner (unified LoadingIndicator) — right-aligned, tinted by the local tool accent */
.tool-spinner {
  --li-color: var(--tool-accent);
  margin-left: auto;
}

/* The task preview card lives in TaskChatCard.vue, which owns its own styles. */

/* Command badge in user messages (agent /command, ClawBench /cb-*, #skill).
   inline-flex so the leading icon and the text sit on one baseline. */
.slash-command-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: 1px var(--space-4);
  border-radius: var(--radius-md);
  background: color-mix(in srgb, #0ea5e9 15%, transparent);
  color: #0ea5e9;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  margin-right: var(--space-2);
  vertical-align: baseline;
  line-height: var(--line-height-relaxed);
}

/* The icon is the primary differentiator between the three badge kinds, so it
   must never be squeezed out by a long command name. */
.command-badge-icon {
  flex-shrink: 0;
}

:root[data-theme-base="dark"] .slash-command-badge {
  background: color-mix(in srgb, #38bdf8 15%, transparent);
  color: #38bdf8;
}

/* Inside user bubble: use white-based palette for contrast against colored background */
.chat-message.user .slash-command-badge {
  background: rgba(255, 255, 255, 0.2);
  color: rgba(255, 255, 255, 0.95);
}

/* ClawBench built-in command badge (/cb-*) — purple, distinct from agent blue */
.slash-command-badge.clawbench-command-badge {
  background: color-mix(in srgb, #8b5cf6 15%, transparent);
  color: #8b5cf6;
}

:root[data-theme-base="dark"] .slash-command-badge.clawbench-command-badge {
  background: color-mix(in srgb, #a78bfa 15%, transparent);
  color: #a78bfa;
}

.chat-message.user .slash-command-badge.clawbench-command-badge {
  background: rgba(255, 255, 255, 0.2);
  color: rgba(255, 255, 255, 0.95);
}

/* Skill reference badge (#name) — teal tint, matching the completion menu's
   skill accent (completionSources.ts). Told apart from the slash badges by its
   Sparkles icon, not by colour alone. */
.slash-command-badge.skill-command-badge {
  background: color-mix(in srgb, #14b8a6 15%, transparent);
  color: #14b8a6;
  border-radius: var(--radius-full);
}

:root[data-theme-base="dark"] .slash-command-badge.skill-command-badge {
  background: color-mix(in srgb, #2dd4bf 15%, transparent);
  color: #2dd4bf;
}

/* Inside the user bubble all badges share the same translucent-white recipe;
   the icon carries the distinction (a solid fill looked heavy here). */
.chat-message.user .slash-command-badge.skill-command-badge {
  background: rgba(255, 255, 255, 0.2);
  color: rgba(255, 255, 255, 0.95);
}

.at-command-rest {
  /* Rest of the message text after the badge */
}
</style>

<style>
/* Non-scoped styles for v-html penetration — tool detail rendering */


:root[data-theme-base="dark"] .content-blocks .chat-tool-call[data-category="bash"]   { --tool-accent: #34d399; }
:root[data-theme-base="dark"] .content-blocks .chat-tool-call[data-category="search"] { --tool-accent: #a78bfa; }
:root[data-theme-base="dark"] .content-blocks .chat-tool-call[data-category="task"]   { --tool-accent: #fbbf24; }
:root[data-theme-base="dark"] .content-blocks .chat-tool-call[data-category="agent"]  { --tool-accent: #f472b6; }
:root[data-theme-base="dark"] .content-blocks .chat-tool-call[data-category="skill"]  { --tool-accent: #22d3ee; }

/* Tool output section */
.content-blocks .tool-detail .tool-output-section {
  margin-top: var(--space-3);
  border-top: 1px solid var(--border-color);
  padding-top: var(--space-3);
}

.content-blocks .tool-detail .tool-output-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-2);
}

.content-blocks .tool-detail .tool-output-label {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
  font-weight: var(--font-weight-semibold);
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .tool-output-label {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.content-blocks .tool-detail .tool-output-status {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  font-weight: var(--font-weight-semibold);
}

.content-blocks .tool-detail .tool-output-success {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .tool-output-success {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.content-blocks .tool-detail .tool-output-error {
  background: rgba(239, 68, 68, 0.12);
  color: #dc2626;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .tool-output-error {
  background: rgba(248, 113, 113, 0.15);
  color: #fca5a5;
}

.content-blocks .tool-detail .tool-output-body {
  max-height: calc(200 * var(--dp));
  overflow-y: auto;
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .tool-output-body pre {
  margin: 0;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .tool-output-content pre {
  background: var(--bg-tertiary);
  border-radius: var(--radius-xs);
  padding: var(--space-3) var(--space-4);
}

.content-blocks .tool-detail .tool-file-header {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  margin-bottom: var(--space-2);
  padding-bottom: var(--space-2);
  padding-right: calc(22 * var(--dp));
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}

.content-blocks .tool-detail .tool-file-header .chat-file-open-btn {
  position: absolute;
  top: 0;
  right: 0;
  flex-shrink: 0;
}

.content-blocks .tool-detail .tool-file-path {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--accent-color);
  word-break: break-all;
  flex: 1;
  min-width: 0;
}

.content-blocks .tool-detail .edit-diff-view {
  display: flex;
  flex-direction: column;
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .edit-diff-replace-all {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(245, 158, 11, 0.12);
  color: #d97706;
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}

.content-blocks .tool-detail .edit-diff-scroll {
  overflow-x: auto;
}

.content-blocks .tool-detail .edit-diff-body {
  white-space: pre;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  min-width: max-content;
}

.content-blocks .tool-detail .edit-diff-del {
  color: #dc2626;
  white-space: pre;
}

.content-blocks .tool-detail .edit-diff-add {
  color: #16a34a;
  white-space: pre;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .edit-diff-del {
  color: #fca5a5;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .edit-diff-add {
  color: #86efac;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .edit-diff-replace-all {
  background: rgba(251, 191, 36, 0.15);
  color: #fbbf24;
}

.content-blocks .tool-detail .file-preview-view {
  display: flex;
  flex-direction: column;
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .file-preview-body {
  white-space: pre;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  overflow-x: auto;
}

.content-blocks .tool-detail .file-preview-line {
  white-space: pre;
  color: var(--text-primary);
}

.content-blocks .tool-detail .file-write-view {
  display: flex;
  flex-direction: column;
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .file-write-badge {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(59, 130, 246, 0.12);
  color: #2563eb;
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .file-write-badge {
  background: rgba(96, 165, 250, 0.15);
  color: #93c5fd;
}

.content-blocks .tool-detail .file-write-body {
  white-space: pre;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  overflow-x: auto;
}

.content-blocks .tool-detail .file-write-line {
  white-space: pre;
  color: var(--text-primary);
}

.content-blocks .tool-detail .tool-json-body {
  white-space: pre;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  overflow-x: auto;
}

.content-blocks .tool-detail .tool-json-body code {
  font-family: inherit;
}

.content-blocks .tool-detail .bash-terminal-view {
  white-space: normal;
}

.content-blocks .tool-detail .bash-terminal-desc {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  margin-bottom: var(--space-2);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .bash-terminal-body {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  background: var(--bg-tertiary);
  border-radius: var(--radius-xs);
  padding: var(--space-3) var(--space-4);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .bash-prompt {
  color: #16a34a;
  font-weight: var(--font-weight-bold);
  margin-right: var(--space-2);
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .bash-prompt {
  color: #4ade80;
}

.content-blocks .tool-detail .bash-command {
  color: var(--text-primary);
}

/* ── Unified AskUserQuestion card ──
   Combines the old pill bar (.chat-tool-call) and the detached content box
   (.tool-detail) into ONE bordered card: a header strip (icon + name + status)
   sits directly above the question body inside the same container. */
.content-blocks .tool-detail.chat-inline-card {
  display: flex;
  flex-direction: column;
  gap: 0;
  margin: var(--space-3) 0 var(--space-1);
  padding: 0;
  border: 1px solid color-mix(in srgb, var(--tool-accent) 25%, var(--border-color));
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  overflow: hidden;
  max-height: none;
}

.content-blocks .tool-detail.chat-inline-card .chat-card-strip {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: calc(5 * var(--dp));
  padding: calc(5 * var(--dp)) var(--space-4);
  background: color-mix(in srgb, var(--tool-accent) 6%, var(--bg-secondary));
  border-bottom: 1px solid color-mix(in srgb, var(--tool-accent) 12%, var(--border-color));
  cursor: pointer;
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
  flex-shrink: 0;
  overflow: hidden;
}

@media (hover: hover) {
  .content-blocks .tool-detail.chat-inline-card .chat-card-strip:hover {
    background: color-mix(in srgb, var(--tool-accent) 11%, var(--bg-secondary));
  }
}

.content-blocks .tool-detail.chat-inline-card .chat-card-strip .tool-icon {
  color: color-mix(in srgb, var(--tool-accent) 80%, transparent);
  flex-shrink: 0;
}

.content-blocks .tool-detail.chat-inline-card .chat-card-strip .tool-name {
  font-weight: var(--font-weight-semibold);
  color: var(--tool-accent);
  font-size: var(--font-size-xs);
  flex-shrink: 0;
}

.content-blocks .tool-detail.chat-inline-card .chat-card-strip .tool-summary {
  color: var(--text-hint);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.content-blocks .tool-detail.chat-inline-card .chat-card-strip .tool-check,
.content-blocks .tool-detail.chat-inline-card .chat-card-strip .tool-warn,
.content-blocks .tool-detail.chat-inline-card .chat-card-strip .tool-error-icon,
.content-blocks .tool-detail.chat-inline-card .chat-card-strip .tool-spinner {
  flex-shrink: 0;
  margin-left: auto;
}

.content-blocks .tool-detail.chat-inline-card .chat-card-body {
  padding: var(--space-4) var(--space-5);
  overflow-y: auto;
  max-height: calc(500 * var(--dp));
}

.content-blocks .tool-detail.chat-inline-card.is-pending .chat-card-strip {
  cursor: default;
}

.content-blocks .tool-detail.chat-inline-card[data-category="ask"] { --tool-accent: #f97316; }
:root[data-theme-base="dark"] .content-blocks .tool-detail.chat-inline-card[data-category="ask"] { --tool-accent: #fb923c; }
.content-blocks .tool-detail.chat-inline-card[data-category="permission"] { --tool-accent: #eab308; }
:root[data-theme-base="dark"] .content-blocks .tool-detail.chat-inline-card[data-category="permission"] { --tool-accent: #fbbf24; }

/* ── AskUserQuestion card ── */
:root[data-theme-base="dark"] .content-blocks .chat-tool-call[data-category="ask"] { --tool-accent: #fb923c; }
:root[data-theme-base="dark"] .content-blocks .chat-tool-call[data-category="permission"] { --tool-accent: #fbbf24; }

.content-blocks .tool-detail .ask-question-view {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.content-blocks .tool-detail .ask-question-empty {
  color: var(--text-muted, #999);
  font-style: italic;
  font-size: var(--font-size-xs);
}

.content-blocks .tool-detail .ask-question-view.ask-invalid {
  padding: var(--space-1) 0;
}

.content-blocks .tool-detail .ask-question-view.ask-invalid .ask-question-empty {
  color: #b45309;
  font-style: normal;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-view.ask-invalid .ask-question-empty {
  color: #fbbf24;
}

.content-blocks .tool-detail .ask-question-item {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.content-blocks .tool-detail .ask-question-header {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: #f97316;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-header {
  color: #fb923c;
}

.content-blocks .tool-detail .ask-question-text {
  font-size: var(--font-size-sm);
  color: var(--text-primary);
  line-height: var(--line-height-normal);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .ask-question-options {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.content-blocks .tool-detail .ask-question-option {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background var(--duration-base), border-color var(--duration-base);
}

@media (hover: hover) {
  .content-blocks .tool-detail .ask-question-option:hover {
    background: color-mix(in srgb, #f97316 6%, var(--bg-secondary));
    border-color: color-mix(in srgb, #f97316 30%, var(--border-color));
  }
}

.content-blocks .tool-detail .ask-question-option.selected {
  background: color-mix(in srgb, #f97316 10%, var(--bg-secondary));
  border-color: #f97316;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-option.selected {
  background: color-mix(in srgb, #fb923c 12%, var(--bg-secondary));
  border-color: #fb923c;
}

/* The indicator's shape/geometry (size, ring, dot, check) is GLOBAL — see
   .ask-option-indicator in web/css/components.css. Only colour is per-call-site,
   consumed by the global rule through `currentColor`. Do NOT re-declare size,
   border or radius here: a scoped selector outranks the global class and would
   silently desync the two states again. */
.content-blocks .tool-detail .ask-option-indicator {
  color: var(--text-muted, #999);
}

.content-blocks .tool-detail .ask-question-option.selected .ask-option-indicator {
  color: #f97316;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-option.selected .ask-option-indicator {
  color: #fb923c;
}

.content-blocks .tool-detail .ask-option-content {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
  flex: 1;
}

.content-blocks .tool-detail .ask-option-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .ask-option-desc {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  line-height: var(--line-height-snug);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .ask-question-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-4);
}

.content-blocks .tool-detail .ask-question-recommend {
  padding: calc(5 * var(--dp)) var(--space-7);
  border: 1px solid #8b5cf6;
  border-radius: var(--radius-sm);
  background: transparent;
  color: #8b5cf6;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
  transition: background var(--duration-base), border-color var(--duration-base);
}

@media (hover: hover) {
  .content-blocks .tool-detail .ask-question-recommend:hover {
    background: color-mix(in srgb, #8b5cf6 10%, var(--bg-secondary));
  }
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-recommend {
  border-color: #a78bfa;
  color: #a78bfa;
}

@media (hover: hover) {
  :root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-recommend:hover {
    background: color-mix(in srgb, #a78bfa 12%, var(--bg-secondary));
  }
}

.content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-recommend {
  background: #8b5cf6;
  color: white;
  border-color: #8b5cf6;
  cursor: default;
  opacity: 1;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-recommend {
  background: #a78bfa;
  border-color: #a78bfa;
}

.content-blocks .tool-detail .ask-question-submit {
  align-self: flex-end;
  padding: calc(5 * var(--dp)) var(--space-7);
  border: 1px solid #f97316;
  border-radius: var(--radius-sm);
  background: #f97316;
  color: white;
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
  transition: opacity var(--duration-base), background var(--duration-base);
}

@media (hover: hover) {
  .content-blocks .tool-detail .ask-question-submit:hover:not(:disabled) {
    background: #ea580c;
  }
}

.content-blocks .tool-detail .ask-question-submit:disabled {
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
}

.content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-submit {
  background: #16a34a;
  border-color: #16a34a;
  cursor: default;
  opacity: 1;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-submit {
  background: #fb923c;
  border-color: #fb923c;
}

@media (hover: hover) {
  :root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-submit:hover:not(:disabled) {
    background: #f97316;
  }
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-submit {
  background: #22c55e;
  border-color: #22c55e;
}

.content-blocks .tool-detail .ask-question-supplementary {
  display: flex;
  flex-direction: column;
  gap: calc(3 * var(--dp));
}

.content-blocks .tool-detail .ask-supplementary-label {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
}

.content-blocks .tool-detail .ask-supplementary-input {
  /* Local copy of the vertical padding so the min/max height caps below stay in
     sync with it; the caps are 6 line boxes + padding (the auto-grow cap in
     renderToolDetail.ts uses the same 6, and the browser clamps `height` at
     max-height, which is what turns on the internal scrollbar past 6 lines). */
  --ask-supp-pad-y: calc(5 * var(--dp));
  width: 100%;
  padding: var(--ask-supp-pad-y) var(--space-4);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  /* Integer line box, matching the other single-line controls (see
     --input-line-height) — a unitless ratio rounds differently per line in
     WebView, which would make the auto-grown height drift. */
  line-height: var(--input-line-height);
  font-family: inherit;
  outline: none;
  /* Multi-line note: Enter inserts a newline, so the box must wrap and scroll
     internally rather than being resized by the user. */
  resize: none;
  overflow-y: auto;
  min-height: calc(var(--input-line-height) + var(--ask-supp-pad-y) * 2);
  max-height: calc(var(--input-line-height) * 6 + var(--ask-supp-pad-y) * 2);
  transition: border-color var(--duration-base);
  box-sizing: border-box;
}

.content-blocks .tool-detail .ask-supplementary-input::placeholder {
  color: var(--text-muted, #999);
  font-size: var(--font-size-xs);
}

.content-blocks .tool-detail .ask-supplementary-input:focus {
  border-color: #f97316;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .ask-supplementary-input:focus {
  border-color: #fb923c;
}

/* ── Grep search view ── */
.content-blocks .tool-detail .grep-search-view {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .grep-pattern-row,
.content-blocks .tool-detail .grep-path-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
}

.content-blocks .tool-detail .grep-label {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
  flex-shrink: 0;
  line-height: var(--line-height-normal);
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .grep-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.content-blocks .tool-detail .grep-pattern-text,
.content-blocks .tool-detail .grep-path-text {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

.content-blocks .tool-detail .grep-tags-row,
.content-blocks .tool-detail .bash-tags-row,
.content-blocks .tool-detail .web-search-tags-row,
.content-blocks .tool-detail .web-fetch-tags-row,
.content-blocks .tool-detail .glob-tags-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-1);
}

.content-blocks .tool-detail .grep-mode-tag {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(139, 92, 246, 0.08);
  color: #8b5cf6;
  font-weight: var(--font-weight-medium);
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .grep-mode-tag {
  background: rgba(167, 139, 250, 0.12);
  color: #a78bfa;
}

/* ── Glob pattern view ── */
.content-blocks .tool-detail .glob-pattern-view {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .glob-pattern-row,
.content-blocks .tool-detail .glob-path-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
}

.content-blocks .tool-detail .glob-label {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
  flex-shrink: 0;
  line-height: var(--line-height-normal);
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .glob-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.content-blocks .tool-detail .glob-pattern-text,
.content-blocks .tool-detail .glob-path-text {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

/* ── WebSearch view ── */
.content-blocks .tool-detail .web-search-view {
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .web-search-query {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  color: var(--text-primary);
}

.content-blocks .tool-detail .web-search-icon {
  flex-shrink: 0;
  font-size: var(--font-size-sm);
  line-height: var(--line-height-snug);
}

.content-blocks .tool-detail .web-search-text {
  white-space: pre-wrap;
  word-break: break-word;
}

/* ── WebFetch view ── */
.content-blocks .tool-detail .web-fetch-view {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .web-fetch-url-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
}

.content-blocks .tool-detail .web-fetch-label {
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
  flex-shrink: 0;
  line-height: var(--line-height-normal);
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .web-fetch-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.content-blocks .tool-detail .web-fetch-link {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  color: var(--accent-color);
  text-decoration: none;
  word-break: break-all;
}

.content-blocks .tool-detail .web-fetch-link:hover {
  text-decoration: underline;
}

.content-blocks .tool-detail .web-fetch-text {
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

.content-blocks .tool-detail .web-fetch-prompt {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  white-space: pre-wrap;
  word-break: break-word;
}

/* ── Agent call view ── */
.content-blocks .tool-detail .agent-call-view {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .agent-call-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.content-blocks .tool-detail .agent-type-badge {
  font-size: var(--font-size-2xs);
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-xs);
  background: rgba(236, 72, 153, 0.12);
  color: #db2777;
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .agent-type-badge {
  background: rgba(244, 114, 182, 0.15);
  color: #f472b6;
}

.content-blocks .tool-detail .agent-call-desc {
  color: var(--text-primary);
  font-weight: var(--font-weight-medium);
}

.content-blocks .tool-detail .agent-call-prompt {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  white-space: normal;
  word-break: break-word;
  padding: var(--space-3) var(--space-4);
  background: var(--bg-tertiary);
  border-radius: var(--radius-xs);
  font-family: inherit;
  line-height: var(--line-height-relaxed);
}
.content-blocks .tool-detail .agent-call-prompt p:first-child {
  margin-top: 0;
}
.content-blocks .tool-detail .agent-call-prompt p:last-child {
  margin-bottom: 0;
}
.content-blocks .tool-detail .agent-call-prompt h1,
.content-blocks .tool-detail .agent-call-prompt h2,
.content-blocks .tool-detail .agent-call-prompt h3,
.content-blocks .tool-detail .agent-call-prompt h4 {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  margin: var(--space-4) 0 var(--space-2);
  color: var(--text-primary);
}
.content-blocks .tool-detail .agent-call-prompt ul,
.content-blocks .tool-detail .agent-call-prompt ol {
  margin: var(--space-2) 0;
  padding-left: var(--space-8);
}
.content-blocks .tool-detail .agent-call-prompt li {
  margin: var(--space-1) 0;
}
.content-blocks .tool-detail .agent-call-prompt code {
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
}
.content-blocks .tool-detail .agent-call-prompt pre {
  margin: var(--space-2) 0;
  padding: var(--space-3) var(--space-4);
  background: var(--bg-secondary);
  border-radius: var(--radius-xs);
  overflow-x: auto;
}
.content-blocks .tool-detail .agent-call-prompt pre code {
  background: none;
  padding: 0;
  font-size: var(--font-size-xs);
}
.content-blocks .tool-detail .agent-call-prompt strong {
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}
.content-blocks .tool-detail .agent-call-prompt hr {
  border: none;
  border-top: 1px solid var(--border-color);
  margin: var(--space-3) 0;
}

/* ── Skill call view ── */
.content-blocks .tool-detail .skill-call-view {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
}

.content-blocks .tool-detail .skill-call-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.content-blocks .tool-detail .skill-call-icon {
  font-size: var(--font-size-sm);
  flex-shrink: 0;
}

.content-blocks .tool-detail .skill-call-name {
  font-weight: var(--font-weight-semibold);
  color: #0891b2;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .skill-call-name {
  color: #22d3ee;
}

.content-blocks .tool-detail .skill-call-args {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  white-space: pre-wrap;
  word-break: break-word;
  padding: var(--space-2) var(--space-4);
  background: var(--bg-tertiary);
  border-radius: var(--radius-xs);
  font-family: var(--font-mono);
  line-height: var(--line-height-normal);
}

/* ── PermissionApproval card (body inside the unified inline card) ── */
.content-blocks .tool-detail .permission-approval-view {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.content-blocks .tool-detail .permission-team-member {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  color: var(--text-secondary);
  margin-bottom: var(--space-2);
}

.content-blocks .tool-detail .permission-team-member__dot {
  width: calc(8 * var(--dp));
  height: calc(8 * var(--dp));
  border-radius: 50%;
  flex-shrink: 0;
}

.content-blocks .tool-detail .permission-tool-name {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  font-family: var(--font-mono);
  letter-spacing: calc(0.2 * var(--dp));
  text-transform: uppercase;
}

.content-blocks .tool-detail .permission-tool-detail {
  display: flex;
  flex-direction: column;
  /* No gap: the label is a TAB sitting directly on top of the command box.
     A gap here would detach it from the box's top border. */
  gap: 0;
  min-width: 0;
}

/* The label is a TAB, not a free-floating chip: it sits directly on top of the
   command box, flush with its left edge, rounded on top and SQUARE on the
   bottom. It deliberately has NO bottom border — the command box's own top
   border is its bottom edge, so all four sides of the box stay intact and the
   label's left border lines up with the box's as one continuous line.
   The background must be an OPAQUE mix against the surface behind the label
   (--bg-primary is the inline card's background) — `transparent` would let the
   card show through and break the "attached" read. */
.content-blocks .tool-detail .permission-detail-label {
  align-self: flex-start;
  font-size: var(--font-size-2xs);
  padding: 1px var(--space-3);
  border-radius: var(--radius-xs) var(--radius-xs) 0 0;
  /* Must match the command box's border exactly (same mix) so the two left
     borders read as one line. */
  border: 1px solid color-mix(in srgb, var(--tool-accent, #eab308) 14%, var(--border-color));
  border-bottom: none;
  background: color-mix(in srgb, var(--tool-accent, #eab308) 16%, var(--bg-primary));
  color: #b45309;
  font-weight: var(--font-weight-semibold);
  letter-spacing: calc(0.4 * var(--dp));
  text-transform: uppercase;
  white-space: nowrap;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .permission-detail-label {
  background: color-mix(in srgb, #fbbf24 18%, var(--bg-primary));
  color: #fbbf24;
}

.content-blocks .tool-detail .permission-tool-detail code {
  display: block;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-normal);
  color: var(--text-primary);
  background: var(--bg-tertiary);
  border: 1px solid color-mix(in srgb, var(--tool-accent, #eab308) 14%, var(--border-color));
  /* Sharp, hard corners — terminal-like command block. */
  border-radius: 0;
  padding: calc(5 * var(--dp)) var(--space-4);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .permission-options {
  display: flex;
  gap: var(--space-4);
  flex-wrap: wrap;
}

/* Permission buttons reuse the shared footer pill colour language (.fbtn /
   .fbtn-success / .fbtn-danger from modal-footer-btn.css) but drop the pill
   shape for a plain rounded-rect (radius matches the ask-question option
   blocks in the same card). Only the interaction-state keep-alive stays here. */
.content-blocks .tool-detail .permission-options .permission-btn {
  padding: 0 calc(14 * var(--dp));
  border-radius: var(--radius-sm);
}

/* ── Integrated button group (opt-in via .permission-btn-group) ──
   The default .permission-options above stays as-is for any other caller; the
   renderer adds .permission-btn-group for the permission card. One container
   owns the frame and the outer radius; the cells own nothing, so the only lines
   inside are the 1px dividers. The group hugs its labels (inline-flex, cells
   sized to content) and only truncates once it would exceed the card.

   MUST stay AFTER the base .permission-options .permission-btn rule above:
   that rule sets padding + border-radius at the SAME specificity (0,4,0), so
   only source order decides — placed earlier, the cells would silently keep a
   6px radius and 14px padding and the group would not read as one control. */
.content-blocks .tool-detail .permission-btn-group {
  display: inline-flex;
  /* Same trap as .permission-result: this is a child of the column-flex
     .permission-approval-view, so without align-self it inherits
     align-items: stretch, blockifies and spans the full card width — the
     cells stay content-sized but the frame around them does not hug. */
  align-self: flex-start;
  /* Natural width: each cell sizes to its own label, so a short row of short
     labels does not stretch across the whole card. `max-width: 100%` is the
     ceiling — past it the cells shrink and truncate instead of overflowing. */
  max-width: 100%;
  gap: 0;
  flex-wrap: nowrap;
  overflow: hidden;
  border-radius: var(--radius-sm);
  border: 1px solid color-mix(in srgb, var(--text-secondary) 40%, transparent);
}

/* Cells: no border, no radius, no background of their own — the container and
   the .fbtn-* colour rules supply all three. */
.content-blocks .tool-detail .permission-btn-group .permission-btn {
  /* Size to the label, but allow shrinking (and therefore truncating) when the
     row would exceed the container. `min-width: 0` is MANDATORY: a flex item
     defaults to `min-width: auto`, which refuses to shrink below its content —
     without it text-overflow never triggers and the group overflows the card. */
  flex: 0 1 auto;
  min-width: 0;
  /* `display: block` (not inline-flex) + line-height is what lets
     text-overflow: ellipsis actually apply to the label. */
  display: block;
  height: calc(30 * var(--dp));
  line-height: calc(30 * var(--dp));
  padding: 0 var(--space-5);
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  /* Smaller than the .fbtn default (--font-size-md): three labels share one
     row here, so the row reads better a step down. */
  font-size: var(--font-size-sm);
  border: none;
  border-radius: 0;
}

/* 1px divider drawn with box-shadow rather than border-left: a real border
   would take layout space, so each cell's share of the row would differ by 1px
   and the cells would no longer be visually even. */
.content-blocks .tool-detail .permission-btn-group > * + * {
  box-shadow: -1px 0 0 0 color-mix(in srgb, var(--text-secondary) 28%, transparent);
}

/* Buttons keep their normal shape; disabled buttons get the shared .fbtn
   opacity treatment only (the JS no longer forces an extra inline opacity). */
.content-blocks .tool-detail .permission-options .fbtn:disabled {
  opacity: var(--opacity-muted);
}

/* After a user responds (before the SSE result re-renders the badge), the
   picked button stays in the SAME soft-tint family as the idle interactive
   button (baseline for alignment) — it only saturates its border, deepens its
   tint and swaps to the Approved/Denied label. No solid fill / white text /
   shape flip, so the picked state reads as the same button, not a different
   one. opacity: 1 keeps it from being dimmed by the shared .fbtn:disabled
   rule that fades the unselected siblings. */
.content-blocks .tool-detail .permission-approval-view.permission-responded .permission-btn-allow {
  background: color-mix(in srgb, #16a34a 28%, var(--bg-tertiary));
  border-color: #16a34a;
  color: #15803d;
  opacity: 1;
}

.content-blocks .tool-detail .permission-approval-view.permission-responded .permission-btn-reject {
  background: color-mix(in srgb, #ef4444 24%, var(--bg-tertiary));
  border-color: #ef4444;
  color: #b91c1c;
  opacity: 1;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .permission-approval-view.permission-responded .permission-btn-allow {
  background: color-mix(in srgb, #22c55e 30%, var(--bg-tertiary));
  border-color: #22c55e;
  color: #86efac;
}

:root[data-theme-base="dark"] .content-blocks .tool-detail .permission-approval-view.permission-responded .permission-btn-reject {
  background: color-mix(in srgb, #f87171 26%, var(--bg-tertiary));
  border-color: #f87171;
  color: #fca5a5;
}

/* Settled-permission status chip. Sized to MATCH the option buttons above it —
   same 30px height, same --font-size-md / --font-weight-medium as .fbtn — so the
   settled state reads as the same control family rather than a small badge. */
.content-blocks .tool-detail .permission-result {
  /* This is a child of the column-flex .permission-approval-view. Without
     align-self it inherits align-items: stretch, the chip blockifies and
     stretches into a FULL-WIDTH bar (measured 600px of a 602px card) instead of
     hugging its label. */
  align-self: flex-start;
  display: inline-flex;
  /* The glyph and the label are two children, so they need an explicit gap. */
  align-items: center;
  gap: var(--space-4);
  height: calc(30 * var(--dp));
  padding: 0 var(--space-6);
  border-radius: var(--radius-sm);
  /* Same step down as the option cells above (they use --font-size-sm too), so
     the settled chip and the buttons it replaced stay on one type scale. */
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  border: 1px solid transparent;
}

/* The icon must not shrink or the label would squash it. */
.content-blocks .tool-detail .permission-result svg {
  flex-shrink: 0;
}

.content-blocks .tool-detail .permission-result-approved {
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-success) 35%, transparent);
}

.content-blocks .tool-detail .permission-result-denied {
  color: var(--color-red);
  background: color-mix(in srgb, var(--color-red) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-red) 35%, transparent);
}

/* Auto-approved renders IDENTICALLY to -approved. This reverses an earlier
   deliberate choice (a neutral tint, so it would not read as a human decision):
   the outcome is the same either way, and the distinct look read as a different
   result rather than the same one. */
.content-blocks .tool-detail .permission-result-auto-approved {
  /* Same as -approved: the user asked for one consistent "allowed" look, and
     the outcome is the same either way. */
  color: var(--color-success);
  background: color-mix(in srgb, var(--color-success) 12%, transparent);
  border-color: color-mix(in srgb, var(--color-success) 35%, transparent);
}
</style>

import { ref, reactive, nextTick, watch, markRaw, type Ref } from 'vue'
import { renderMarkdown as baseRenderMarkdown, renderMarkdownHtml, renderMermaidInElement } from '@/composables/useMarkdownRenderer.ts'
import { formatToolInput } from '@/utils/renderToolDetail.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useCommitHashAnnotation } from '@/composables/useCommitHashAnnotation.ts'
import { clearThinkingCache } from '@/composables/useThinkingContent.ts'
import { store } from '@/stores/app.ts'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog.ts'
import { createTaskBlockStore } from '@/utils/taskBlockStore.ts'
import {
  extractScheduledTaskIds,
  stripScheduledTaskTags,
  detectAskQuestion,
  stripAskQuestionTag,
  taskChanged,
  StaticBlockCache,
} from '@/utils/streamPerf.ts'
import {
  parseAssistantContent,
  toolCallSummary,
  hasImagesInContent,
  formatMessageTime,
  formatDetailTime,
  truncate,
} from '@/utils/chatBlocks.ts'

export function useChatRender(options: { messages: { value: Array<Record<string, unknown>> }; theme: { value: unknown }; currentSessionId: { value: unknown } }) {
  const { messages, theme, currentSessionId } = options
  const { verifyFilePaths } = useFilePathAnnotation()
  const { verifyCommitHashes } = useCommitHashAnnotation()

  // Local type for accessing message properties with known types
  type RenderMessage = { id?: string | number; role?: string; blocks?: Array<{ type?: string; text?: string } & Record<string, unknown>>; streaming?: boolean; [key: string]: unknown }

  const blockTasks: Record<string, unknown> = reactive({})
  const blockAskQuestions: Record<string, unknown> = reactive({})
  const expandedTools = ref({}) as Ref<Record<string, boolean>>
  let lastRenderedCount = 0

  // ── Task block store for batch fetching (ISS-013) ──
  const taskBlockStore = createTaskBlockStore()

  // Sync taskBlockStore.blocks into blockTasks for template rendering
  watch(() => ({ ...taskBlockStore.blocks }), (storeBlocks) => {
    for (const key of Object.keys(storeBlocks)) {
      blockTasks[key] = storeBlocks[key]
    }
  }, { deep: true })

  // ── StaticBlockCache for non-streaming re-renders ──
  //
  // `markRaw` is REQUIRED, not a micro-optimization. This instance is passed
  // down as a prop to ContentBlocks, and Vue would otherwise wrap it in a
  // reactive proxy. `get()` mutates internal Maps to maintain LRU order, so
  // with a proxy those writes happen to reactive state DURING render — which
  // re-triggers the very render effect that called `get()`, producing
  // "Maximum recursive updates exceeded" and a frozen UI. (Reproduced with a
  // mounted ContentBlocks; `markRaw` makes it stop.) The cache is deliberately
  // opaque state with no template dependency, so opting it out of reactivity
  // is also the semantically correct choice.
  const staticBlockCache = markRaw(new StaticBlockCache())

  // Upgrade deferred (fast-path) cache entries to full pipeline render.
  // Called via requestIdleCallback after initial fast render for instant display.
  staticBlockCache.setUpgradeFn(() => {
    let upgraded = 0
    for (const msg of messages.value as RenderMessage[]) {
      if (msg.role !== 'assistant' || !msg.blocks || msg.streaming) continue
      for (let bi = 0; bi < msg.blocks.length && upgraded < 5; bi++) {
        const block = msg.blocks[bi]
        if (block.type !== 'text' || !block.text) continue
        if (staticBlockCache.isDeferred(msg.id!, bi, block.text)) {
          // Re-render with full pipeline and replace cache entry
          const fullHtml = renderTextBlock(block.text, String(msg.id), bi, false, false)
          staticBlockCache.set(String(msg.id), bi, block.text, fullHtml, false)
          staticBlockCache.markUpgraded(String(msg.id), bi, block.text)
          upgraded++
        }
      }
    }
    // If there are more deferred entries, schedule another batch
    if (staticBlockCache.deferredCount > 0) {
      staticBlockCache.scheduleUpgrade()
    }
    // Trigger Vue re-render with upgraded content
    if (upgraded > 0) {
      updateRenderedContents(true)
    }
  })

  // Re-render when theme changes — clear caches since rendering may differ
  // (code-block syntax colours, table chrome). Theme is a rendering input, so
  // unlike session switches this genuinely invalidates every cached entry.
  watch(theme, () => {
    staticBlockCache.clear()
    updateRenderedContents(true)
  })

  // Session switches must NOT clear the rendered-block cache.
  //
  // The key contains the DB message id (globally unique across sessions) and a
  // scope covering the rendering inputs that actually vary (projectRoot,
  // locale), so an entry can never be mistaken for a different session's block
  // or for the same block under different inputs. Clearing here meant every
  // switch re-ran the full markdown pipeline for every historical block —
  // seconds of 100%-busy main thread on a heavy session, which is what made
  // "switch a few times" freeze the UI.
  // The cache is LRU-bounded (StaticBlockCache.MAX_ENTRIES), so retaining
  // across sessions cannot grow without limit.
  //
  // The thinking-text cache IS still cleared: it is keyed by session and its
  // loader re-fetches per session, so stale entries there are a correctness
  // problem rather than a perf win.
  watch(currentSessionId, () => {
    clearThinkingCache()
  })

  // Project switches drop the previous project's rendered blocks.
  //
  // Correctness no longer depends on this: `ContentBlocks` puts `projectRoot`
  // in the cache scope, so a lookup under a new root misses regardless. This
  // watcher exists to RECLAIM memory — without it the cache would retain an
  // entry for every block of every project visited, and the MAX_ENTRIES LRU
  // would evict still-reusable entries from the current project to make room
  // for ones that can never be hit again.
  //
  // It does not reintroduce the per-session cost the watcher above avoids:
  // project switches are rare (explicit project change, worktree jump) and
  // genuinely invalidate every entry, whereas session switches are frequent
  // and do not.
  //
  // Locale is deliberately NOT watched here: it is part of the same scope, so
  // a language change misses on its own. Adding a watcher would be redundant.
  watch(() => store.state.projectRoot, () => {
    staticBlockCache.clear()
  })

  type BlockTaskEntry = { taskId?: number; deleted?: boolean; loading?: boolean; task?: unknown; [key: string]: unknown }

  // Sync blockTasks with latest task data from store (global polling updates store.state.tasks).
  // Use a tasks Map for O(1) lookup, and taskChanged() for semantic comparison.
  // Deletion is owned by the authoritative /api/tasks list (via taskBlockStore / refreshTaskData);
  // this watch only syncs status. It must NOT mark tasks deleted when the store list is empty —
  // an empty list can be an app reset, a session switch, or a not-yet-populated store, not a real
  // deletion. Only a non-empty list that lacks a taskId is a real "task deleted" signal.
  watch(() => store.state.tasks, (tasks) => {
    const keys = Object.keys(blockTasks)
    if (keys.length === 0) return
    if (!tasks || tasks.length === 0) {
      // Skip deletion marking: empty list is not authoritative. Keep loading as-is;
      // taskBlockStore.fetchBatchData (ISS-013) and refreshTaskData own loading/deletion.
      return
    }
    const taskMap = new Map(tasks.map((t: Record<string, unknown>) => [t.id, t]))
    for (const key of keys) {
      const entry = blockTasks[key] as BlockTaskEntry
      if (entry.deleted) continue
      const updated = taskMap.get(entry.taskId)
      if (!updated) {
        entry.deleted = true
        entry.loading = false
      } else if (entry.task && taskChanged(entry.task as Record<string, unknown>, updated as Record<string, unknown>)) {
        entry.task = updated
      } else if (!entry.task) {
        entry.task = updated
        entry.loading = false
      }
    }
  })

  // Batch-fetch task data using the list API to avoid per-task loading flicker.
  // ISS-013: delegates to taskBlockStore which does NOT mark deleted on network error.
  async function fetchBatchTaskData(taskKeys: Array<{ key: string; taskId: number }>) {
    await taskBlockStore.fetchBatchData(taskKeys)
    // Sync store blocks into our reactive blockTasks
    for (const key of Object.keys(taskBlockStore.blocks)) {
      blockTasks[key] = taskBlockStore.blocks[key]
    }
  }

  async function refreshTaskData(taskId: number) {
    for (const key of Object.keys(blockTasks)) {
      const entry = blockTasks[key] as BlockTaskEntry
      if (entry.taskId === taskId && !entry.deleted) {
        try {
          const data = await apiGet(`/api/tasks/${taskId}`)
          const bk = blockTasks[key] as BlockTaskEntry
          bk.task = data
        } catch (err: unknown) {
          if ((err instanceof Error && (err.message.includes('404') || err.message.toLowerCase().includes('not found')))) {
            entry.deleted = true
            entry.task = null
          }
          // Other errors: leave existing data, don't mark deleted
        }
      }
    }
  }

  /**
   * Render markdown to HTML using the unified pipeline.
   * When skipEnhancements=true (streaming mode), path annotations are skipped.
   * When skipKatex=true, KaTeX rendering is also skipped (streaming, formulas may be incomplete).
   * After rendering, schedules nextTick verifyFilePaths/verifyCommitHashes if detected.
   */
  function renderMarkdown(text: string, { skipEnhancements = false, skipKatex }: { skipEnhancements?: boolean; skipKatex?: boolean } = {}): string {
    const { html, detectedPaths, detectedSHAs } = baseRenderMarkdown(text, { skipEnhancements, skipKatex })

    // Schedule async verification for detected paths/commits
    if (detectedPaths.length > 0) {
      const uniquePaths = [...new Set(detectedPaths)]
      nextTick(() => {
        const el = document.getElementById('aiChatMessages')
        if (el) verifyFilePaths(uniquePaths, el)
      })
    }
    if (detectedSHAs.length > 0) {
      const uniqueSHAs = [...new Set(detectedSHAs)]
      nextTick(() => {
        const el = document.getElementById('aiChatMessages')
        if (el) verifyCommitHashes(uniqueSHAs, el)
      })
    }

    return html
  }

  /**
   * Render a text block to HTML.
   *
   * When streaming=true (during streaming):
   *   Only pure markdown rendering — no structured detection.
   *   Tags like <scheduled-task> and <clawbench-ask-question> remain as visible text.
   *   No KaTeX, no file path annotation, no path verification.
   *
   * When streaming=false (post-streaming / history load):
   *   Full pipeline: scheduled-task extraction, ask-question detection,
   *   tag stripping, and enhanced markdown rendering.
   *
   * When deferEnhancements=true (history load fast path):
   *   Same as streaming=false but markdown rendering uses skipEnhancements=true
   *   for instant display. Tasks and ask-question detection still run.
   *   The cache upgrade mechanism will later re-render with full enhancements.
   */
  function renderTextBlock(text: string, msgId: string, blockIdx: number, streaming = false, deferEnhancements = false) {
    // ── Streaming: pure markdown only (no detections/verification) ──
    if (streaming) {
      return renderMarkdownHtml(text, { skipEnhancements: true, skipKatex: true })
    }

    // ── Post-streaming: full pipeline ──

    // Extract scheduled-task IDs and batch-fetch their data
    const taskIds = extractScheduledTaskIds(text)
    if (taskIds.length > 0) {
      const taskKeys = taskIds.map((tid, tagIdx) => ({
        key: `${msgId}-${blockIdx}-${tagIdx}`,
        taskId: Number(tid),
      }))
      fetchBatchTaskData(taskKeys)
    }

    // Detect clawbench-ask-question tags
    const askResult = detectAskQuestion(text)

    if (askResult.matches.length > 0) {
      const askKey = `${msgId}-${blockIdx}`
      if (askResult.items.length > 0) {
        blockAskQuestions[askKey] = { questions: askResult.items }
      } else {
        // A previously-parsed block whose payload now fails must not keep a
        // stale card alongside the degraded text.
        delete blockAskQuestions[askKey]
        appLog.w('AskQuestion', `payload unparseable, rendering as markdown: ${askResult.reasons.join(',')}`)
      }
      // Parsed spans are removed (the card renders them); unparsed spans are
      // replaced by their inner text, so a malformed payload degrades to
      // readable Markdown instead of exposing raw tags. Nothing is discarded.
      const cleanText = stripScheduledTaskTags(stripAskQuestionTag(text, askResult))
      return cleanText ? renderMarkdown(cleanText, { skipEnhancements: deferEnhancements }) : ''
    }

    // No ask-question: strip scheduled-task tags and render
    const cleanText = stripScheduledTaskTags(text)
    return cleanText ? renderMarkdown(cleanText, { skipEnhancements: deferEnhancements }) : ''
  }

  function extractScheduledTasks(msgs: Array<Record<string, unknown>>) {
    // Collect all task keys across messages for a single batch fetch
    const allTaskKeys = []
    for (const msg of msgs as RenderMessage[]) {
      if (msg.role === 'assistant' && msg.blocks && !msg.streaming) {
        for (let bi = 0; bi < msg.blocks.length; bi++) {
          const block = msg.blocks[bi]
          if (block.type === 'text') {
            const taskIds = extractScheduledTaskIds(block.text || '')
            for (let tagIdx = 0; tagIdx < taskIds.length; tagIdx++) {
              allTaskKeys.push({
                key: `${msg.id}-${bi}-${tagIdx}`,
                taskId: Number(taskIds[tagIdx]),
              })
            }
          }
        }
      }
    }
    if (allTaskKeys.length > 0) {
      fetchBatchTaskData(allTaskKeys)
    }
  }

  function updateRenderedContents(forceFullRender = false) {
    // Defensive: if count diverged (e.g. loadHistory replaced messages),
    // force a full rebuild.
    if (!forceFullRender && lastRenderedCount > messages.value.length) {
      forceFullRender = true
    }

    // ── Deferred rendering: only render Mermaid when not streaming ──
    // During streaming, Mermaid code blocks are incomplete — rendering them
    // would produce errors. Defer to post-streaming forceFullRender.
    if (forceFullRender) {
      lastRenderedCount = messages.value.length
      nextTick(async () => {
        const el = document.getElementById('aiChatMessages')
        if (el) await renderMermaidInElement(el, 'chat-mermaid')
      })
    } else {
      const startIdx = lastRenderedCount
      const newMsgCount = messages.value.length - startIdx

      if (newMsgCount <= 0) return

      lastRenderedCount = messages.value.length

      // Skip Mermaid rendering during streaming — it will be rendered
      // when forceFullRender triggers after streaming ends.
    }
  }

  function toggleToolDetail(key: string) {
    expandedTools.value[key] = !expandedTools.value[key]
  }

  return {
    blockTasks,
    blockAskQuestions,
    expandedTools,
    renderMarkdown,
    renderTextBlock,
    parseAssistantContent,
    extractScheduledTasks,
    refreshTaskData,
    updateRenderedContents,
    toggleToolDetail,
    formatToolInput,
    toolCallSummary,
    hasImagesInContent,
    formatMessageTime,
    formatDetailTime,
    truncate,
    // Expose cache for ContentBlocks.vue integration
    staticBlockCache,
  }
}

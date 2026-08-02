import { ref, reactive, nextTick, watch, type Ref } from 'vue'
import { renderMarkdown as baseRenderMarkdown, renderMarkdownHtml, renderMermaidInElement } from '@/composables/useMarkdownRenderer.ts'
import { formatToolInput } from '@/utils/renderToolDetail.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useCommitHashAnnotation } from '@/composables/useCommitHashAnnotation.ts'
import { store } from '@/stores/app.ts'
import { apiGet } from '@/utils/api'
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
  parseAskQuestionContent,
} from '@/utils/chatRenderUtils.ts'
import {
  parseAssistantContent,
  toolCallSummary,
  hasImagesInContent,
  formatMessageTime,
  formatDetailTime,
  truncate,
} from '@/utils/chatBlocks.ts'
import {
  humanizeCron,
  repeatLabel,
} from '@/utils/format.ts'

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
  const staticBlockCache = new StaticBlockCache()

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
  watch(theme, () => {
    staticBlockCache.clear()
    updateRenderedContents(true)
  })

  // Clear caches when session changes
  watch(currentSessionId, () => {
    staticBlockCache.clear()
  })

  type BlockTaskEntry = { taskId?: number; deleted?: boolean; loading?: boolean; task?: unknown; [key: string]: unknown }

  // Sync blockTasks with latest task data from store (global polling updates store.state.tasks).
  // Use a tasks Map for O(1) lookup, and taskChanged() for semantic comparison.
  watch(() => store.state.tasks, (tasks) => {
    const keys = Object.keys(blockTasks)
    if (keys.length === 0) return
    // Empty tasks list means all tasks were deleted — mark all blockTasks as deleted
    if (!tasks || tasks.length === 0) {
      for (const key of keys) {
        const e = blockTasks[key] as BlockTaskEntry
        if (!e.deleted) e.deleted = true
        e.loading = false
      }
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
   * When skipEnhancements=true (streaming mode), KaTeX and path annotations are skipped.
   * After rendering, schedules nextTick verifyFilePaths/verifyCommitHashes if detected.
   */
  function renderMarkdown(text: string, { skipEnhancements = false }: { skipEnhancements?: boolean } = {}): string {
    const { html, detectedPaths, detectedSHAs } = baseRenderMarkdown(text, { skipEnhancements })

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
   *   Tags like <scheduled-task> and <ask-question> remain as visible text.
   *   No KaTeX, no file path annotation, no path verification.
   *
   * When streaming=false (post-streaming / history load):
   *   Full pipeline: scheduled-task extraction, ask-question detection,
   *   tag stripping, and enhanced markdown rendering.
   *
   * When deferEnhancements=true (history load fast path):
   *   Same as streaming=false but markdown rendering uses skipEnhancements=true
   *   for instant display. Scheduled tasks and ask-question detection still run.
   *   The cache upgrade mechanism will later re-render with full enhancements.
   */
  function renderTextBlock(text: string, msgId: string, blockIdx: number, streaming = false, deferEnhancements = false) {
    // ── Streaming: pure markdown only (no detections/verification) ──
    if (streaming) {
      return renderMarkdownHtml(text, { skipEnhancements: true })
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

    // Detect ask-question tags
    const askResult = detectAskQuestion(text)

    if (askResult.found) {
      const askKey = `${msgId}-${blockIdx}`
      if (!blockAskQuestions[askKey]) {
        const parsed = parseAskQuestionContent(askResult.content!)
        if (parsed) {
          blockAskQuestions[askKey] = parsed
        }
      }
      // Remove the matched ask-question tag from the rendered text
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
    humanizeCron,
    repeatLabel,
    truncate,
    // Expose cache for ContentBlocks.vue integration
    staticBlockCache,
  }
}

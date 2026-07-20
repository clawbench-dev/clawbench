import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { shouldRetryToolFetch, resolveEffectiveMsgId, type ContentBlock } from '@/utils/chatStreamUtils.ts'
import { formatToolOutput } from '@/utils/renderToolDetail.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'ToolDetailDrawer'

interface ToolBlock {
  msgId?: string | number
  blockIdx?: number
  name?: string
  display_name?: string
  summary?: string
  input?: Record<string, unknown>
  output?: unknown
  status?: string
  done?: boolean
  tool_id?: string | number
}

interface ChatRenderRef {
  formatToolInput: (input: Record<string, unknown>, name: string, opts: Record<string, unknown>) => string
  toolCallSummary: (block: ToolBlock) => string
  [key: string]: unknown
}

interface ToolDetailDrawerOptions {
  chatRender: ChatRenderRef
  onFileOpen?: (path: string, lineStart?: number, lineEnd?: number) => void
  findLiveBlock?: (ids: { msgId: string | number; blockIdx: number }) => ToolBlock | null
  /** Optional session ID for tool-call API fallback. When the session has multiple
   *  assistant messages (e.g. AutoResumeBackend resume splits), the tool call may
   *  be stored under a different message_id. Passing session_id enables the backend
   *  to fall back to tool_id+session_id lookup. */
  sessionId?: () => string | undefined
}

/**
 * Shared tool detail drawer logic for ChatPanelContent and TaskExecDetail.
 * Uses a simple ref instead of useTabDrawer because ToolDetailDrawer is a
 * BottomSheet (teleported to <body>) — it's a user-initiated overlay that
 * should persist across tab switches, not auto-hide when switching away.
 */
export function useToolDetailDrawer(options: ToolDetailDrawerOptions) {
  const { chatRender, onFileOpen, findLiveBlock, sessionId } = options
  const { t } = useI18n()

  const drawer = useTabDrawer('chat')
  /** Read-only access — use drawer.open()/close() to mutate */
  const show = drawer.isOpen
  const toolDetailData = ref({
    name: '' as string,
    subagentType: '' as string,
    summary: '' as string,
    inputHtml: '' as string,
    outputHtml: '' as string,
    status: '' as string,
    done: true as boolean,
    displayNameOverride: '' as string,
    _fetchIds: null as { toolId: string | number; msgId: string | number } | null,
  })

  // Tracks which tool block is being shown for reactive updates (ChatPanelContent only)
  const activeToolOverlay = ref<{ msgId: string; blockIdx: number } | null>(null)

  // Fetch-in-flight guard: prevents concurrent fetchToolCallDetail calls from polling timer
  let _fetchInFlight = false
  // If user clicks retry while a fetch is in flight, flag to re-fetch after current one completes
  let _retryRequested = false

  function toolCallEmptyState(msg: string) {
    return `<div class="tool-call-empty"><span class="tool-call-empty-msg">${msg}</span><button class="tool-call-retry-btn" onclick="this.closest('.tool-call-empty').dataset.retry='1'">${t('chat.contentBlocks.retry')}</button></div>`
  }

  function handleShowToolDetail(block: ToolBlock) {
    const { formatToolInput, toolCallSummary } = chatRender

    // Reset fetch-in-flight guard for new tool detail
    _fetchInFlight = false
    _retryRequested = false

    // Store identifiers for reactive lookup (survives messages array replacement on loadHistory)
    if (block.blockIdx !== undefined) {
      activeToolOverlay.value = { msgId: String(block.msgId), blockIdx: block.blockIdx }
    }

    const hasInput = block.input && Object.keys(block.input).length > 0
    const hasOutput = !!block.output

    drawer.open()
    toolDetailData.value = {
      name: block.name || '',
      subagentType: block.display_name || (block.input as Record<string, unknown>)?.subagent_type as string || '',
      summary: block.summary || toolCallSummary(block),
      inputHtml: hasInput ? formatToolInput(block.input!, block.name || '', { done: block.done, status: block.status, output: block.output }) : '',
      outputHtml: hasOutput ? formatToolOutput(block.output as string, block.name || '') : '',
      status: block.status || '',
      done: !!block.done,
      displayNameOverride: block.name === 'DeepThink' && !block.display_name ? t('chat.message.deepThinking') : '',
      _fetchIds: null,
    }

    // Fetch tool call detail from API if input/output are missing
    if ((!hasInput || !hasOutput) && block.tool_id && block.msgId) {
      const toolId = block.tool_id
      const msgId = block.msgId
      toolDetailData.value._fetchIds = { toolId, msgId }
      fetchToolCallDetail(toolId, msgId, block)
    }
  }

  function handleOverlayRetryClick(e: MouseEvent) {
    const empty = (e.target as HTMLElement).closest('.tool-call-empty') as HTMLElement | null
    if (!empty || empty.dataset.retry !== '1') return
    empty.dataset.retry = ''
    const ids = toolDetailData.value._fetchIds
    if (!ids) return
    if (_fetchInFlight) {
      _retryRequested = true
      return
    }
    let block: ToolBlock | null = null
    if (findLiveBlock && activeToolOverlay.value) {
      block = findLiveBlock(activeToolOverlay.value)
    }
    fetchToolCallDetail(ids.toolId, ids.msgId, block || { name: toolDetailData.value.name })
  }

  async function fetchToolCallDetail(toolId: string | number, msgId: string | number, block: ToolBlock, _retryCount = 0) {
    if (_fetchInFlight) return
    _fetchInFlight = true
    if (!toolDetailData.value.inputHtml) {
      toolDetailData.value.inputHtml = '<div class="tool-call-loading"></div>'
    }
    try {
      let url = `/api/ai/chat/tool-call?tool_id=${encodeURIComponent(toolId)}&message_id=${encodeURIComponent(msgId)}`
      const sid = sessionId?.()
      if (sid) url += `&session_id=${encodeURIComponent(sid)}`
      const resp = await fetch(url)
      if (!resp.ok) {
        // Retry on 404 (tool call may not yet be persisted during streaming)
        if (shouldRetryToolFetch(resp.status, _retryCount, show.value)) {
          _fetchInFlight = false
          setTimeout(() => {
            if (!show.value) return
            let liveBlock: ToolBlock | null = null
            if (findLiveBlock && activeToolOverlay.value) {
              liveBlock = findLiveBlock(activeToolOverlay.value)
            }
            const effectiveMsgId = resolveEffectiveMsgId(liveBlock as ContentBlock | undefined, activeToolOverlay.value?.msgId, msgId)
            fetchToolCallDetail(toolId, effectiveMsgId, liveBlock || block, _retryCount + 1)
          }, 800)
          return
        }
        if (resp.status !== 404) {
          toolDetailData.value.inputHtml = toolCallEmptyState(t('chat.contentBlocks.detailsUnavailable'))
        }
        return
      }
      const data = await resp.json()
      const { formatToolInput } = chatRender
      if (data.input) {
        const input = typeof data.input === 'string' ? JSON.parse(data.input) : data.input
        toolDetailData.value.inputHtml = formatToolInput(input, block.name || data.name || '', { done: block.done, status: block.status, output: data.output || '' })
      } else {
        toolDetailData.value.inputHtml = toolCallEmptyState(t('chat.contentBlocks.detailsUnavailable'))
      }
      if (data.output) {
        toolDetailData.value.outputHtml = formatToolOutput(data.output, block.name || data.name || '')
      }
      // Sync done/status from API response so the polling watcher can stop
      if (data.done !== undefined) {
        toolDetailData.value.done = !!data.done
      }
      if (data.status !== undefined && data.status !== null) {
        toolDetailData.value.status = data.status
      }
    } catch (e) {
      appLog.w(TAG, 'Failed to fetch tool call detail:', e)
      toolDetailData.value.inputHtml = toolCallEmptyState(t('chat.contentBlocks.detailsLoadFailed'))
    } finally {
      _fetchInFlight = false
      // If user clicked retry while fetch was in flight, re-fetch immediately
      if (_retryRequested) {
        _retryRequested = false
        if (show.value && toolDetailData.value._fetchIds) {
          const { toolId, msgId } = toolDetailData.value._fetchIds
          let block: ToolBlock | null = null
          if (findLiveBlock && activeToolOverlay.value) {
            block = findLiveBlock(activeToolOverlay.value)
          }
          fetchToolCallDetail(toolId, msgId, block || { name: toolDetailData.value.name })
        }
      }
    }
  }

  function handleFileOpenInOverlay(payload: string | { path: string; lineStart?: number; lineEnd?: number }) {
    const { path, lineStart, lineEnd } = typeof payload === 'string' ? { path: payload } : payload
    drawer.close()
    if (onFileOpen) {
      onFileOpen(path, lineStart, lineEnd)
    }
  }

  function closeOverlay() {
    drawer.close()
    _fetchInFlight = false
    _retryRequested = false
  }

  const toolDetailOverlay = computed(() => ({ show: drawer.isOpen.value, ...toolDetailData.value }))

  return {
    show,
    drawer,
    toolDetailData,
    toolDetailOverlay,
    activeToolOverlay,
    handleShowToolDetail,
    handleOverlayRetryClick,
    fetchToolCallDetail,
    handleFileOpenInOverlay,
    closeOverlay,
    toolCallEmptyState,
  }
}

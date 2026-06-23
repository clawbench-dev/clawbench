import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { shouldRetryToolFetch, resolveEffectiveMsgId } from '@/utils/chatStreamUtils.ts'
import { formatToolOutput } from '@/utils/renderToolDetail.ts'
import { appLog } from '@/utils/appLog'

const TAG = 'ToolDetailOverlay'

interface ToolDetailOverlayOptions {
  chatRender: any
  onFileOpen?: (path: string, lineStart?: number, lineEnd?: number) => void
  findLiveBlock?: (ids: { msgId: string | number; blockIdx: number }) => any | null
}

/**
 * Shared tool detail overlay logic for ChatPanelContent and TaskExecDetail.
 */
export function useToolDetailOverlay(options: ToolDetailOverlayOptions) {
  const { chatRender, onFileOpen, findLiveBlock } = options
  const { t } = useI18n()

  const toolDetailOverlay = ref({
    show: false as boolean,
    name: '' as string,
    subagentType: '' as string,
    summary: '' as string,
    inputHtml: '' as string,
    outputHtml: '' as string,
    status: '' as string,
    done: true as boolean,
    _fetchIds: null as { toolId: string | number; msgId: string | number } | null,
  })

  // Tracks which tool block is being shown for reactive updates (ChatPanelContent only)
  const activeToolOverlay = ref<{ msgId: string; blockIdx: number } | null>(null)

  function toolCallEmptyState(msg: string) {
    return `<div class="tool-call-empty"><span class="tool-call-empty-msg">${msg}</span><button class="tool-call-retry-btn" onclick="this.closest('.tool-call-empty').dataset.retry='1'">${t('chat.contentBlocks.retry')}</button></div>`
  }

  function handleShowToolDetail(block: any) {
    const { formatToolInput, toolCallSummary } = chatRender

    // Store identifiers for reactive lookup (survives messages array replacement on loadHistory)
    if (block.blockIdx !== undefined) {
      activeToolOverlay.value = { msgId: String(block.msgId), blockIdx: block.blockIdx }
    }

    const hasInput = block.input && Object.keys(block.input).length > 0
    const hasOutput = !!block.output

    toolDetailOverlay.value = {
      show: true,
      name: block.name || '',
      subagentType: block.display_name || block.input?.subagent_type || '',
      summary: block.summary || toolCallSummary(block),
      inputHtml: hasInput ? formatToolInput(block.input, block.name, { done: block.done, status: block.status, output: block.output }) : '',
      outputHtml: hasOutput ? formatToolOutput(block.output, block.name) : '',
      status: block.status || '',
      done: !!block.done,
      _fetchIds: null,
    }

    // Fetch tool call detail from API if input/output are missing
    if ((!hasInput || !hasOutput) && block.tool_id && block.msgId) {
      const toolId = block.tool_id
      const msgId = block.msgId
      toolDetailOverlay.value._fetchIds = { toolId, msgId }
      fetchToolCallDetail(toolId, msgId, block)
    }
  }

  function handleOverlayRetryClick(e: MouseEvent) {
    const empty = (e.target as HTMLElement).closest('.tool-call-empty') as HTMLElement | null
    if (!empty || empty.dataset.retry !== '1') return
    empty.dataset.retry = ''
    const ids = toolDetailOverlay.value._fetchIds
    if (!ids) return
    let block = null as any
    if (findLiveBlock && activeToolOverlay.value) {
      block = findLiveBlock(activeToolOverlay.value)
    }
    fetchToolCallDetail(ids.toolId, ids.msgId, block || { name: toolDetailOverlay.value.name })
  }

  async function fetchToolCallDetail(toolId: string | number, msgId: string | number, block: any, _retryCount = 0) {
    if (!toolDetailOverlay.value.inputHtml) {
      toolDetailOverlay.value.inputHtml = '<div class="tool-call-loading"></div>'
    }
    try {
      const resp = await fetch(`/api/ai/chat/tool-call?tool_id=${encodeURIComponent(toolId)}&message_id=${encodeURIComponent(msgId)}`)
      if (!resp.ok) {
        // Retry on 404 (tool call may not yet be persisted during streaming)
        if (shouldRetryToolFetch(resp.status, _retryCount, toolDetailOverlay.value.show)) {
          setTimeout(() => {
            if (!toolDetailOverlay.value.show) return
            let liveBlock = null as any
            if (findLiveBlock && activeToolOverlay.value) {
              liveBlock = findLiveBlock(activeToolOverlay.value)
            }
            const effectiveMsgId = resolveEffectiveMsgId(liveBlock, activeToolOverlay.value?.msgId, msgId)
            fetchToolCallDetail(toolId, effectiveMsgId, liveBlock || block, _retryCount + 1)
          }, 800)
          return
        }
        if (resp.status !== 404) {
          toolDetailOverlay.value.inputHtml = toolCallEmptyState(t('chat.contentBlocks.detailsUnavailable'))
        }
        return
      }
      const data = await resp.json()
      const { formatToolInput } = chatRender
      if (data.input) {
        const input = typeof data.input === 'string' ? JSON.parse(data.input) : data.input
        toolDetailOverlay.value.inputHtml = formatToolInput(input, block.name || data.name, { done: block.done, status: block.status, output: data.output || '' })
      } else {
        toolDetailOverlay.value.inputHtml = toolCallEmptyState(t('chat.contentBlocks.detailsUnavailable'))
      }
      if (data.output) {
        toolDetailOverlay.value.outputHtml = formatToolOutput(data.output, block.name || data.name)
      }
    } catch (e) {
      appLog.w(TAG, 'Failed to fetch tool call detail:', e)
      toolDetailOverlay.value.inputHtml = toolCallEmptyState(t('chat.contentBlocks.detailsLoadFailed'))
    }
  }

  function handleFileOpenInOverlay(payload: string | { path: string; lineStart?: number; lineEnd?: number }) {
    const { path, lineStart, lineEnd } = typeof payload === 'string' ? { path: payload } : payload
    toolDetailOverlay.value.show = false
    if (onFileOpen) {
      onFileOpen(path, lineStart, lineEnd)
    }
  }

  function closeOverlay() {
    toolDetailOverlay.value.show = false
  }

  return {
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

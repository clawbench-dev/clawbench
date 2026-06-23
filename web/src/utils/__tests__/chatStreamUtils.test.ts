import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  FILE_MODIFYING_TOOLS,
  findLastBlockOfType,
  forceCleanupStreamingState,
  findStreamingMsg,
  createPendingUserMessage,
  drainQueueMessage,
  syncPendingFromBackend,
  shouldRetryToolFetch,
  resolveEffectiveMsgId,
} from '@/utils/chatStreamUtils.ts'

describe('FILE_MODIFYING_TOOLS', () => {
  it('contains Write', () => {
    expect(FILE_MODIFYING_TOOLS.has('Write')).toBe(true)
  })

  it('contains Edit', () => {
    expect(FILE_MODIFYING_TOOLS.has('Edit')).toBe(true)
  })

  it('does not contain Read', () => {
    expect(FILE_MODIFYING_TOOLS.has('Read')).toBe(false)
  })

  it('does not contain Bash', () => {
    expect(FILE_MODIFYING_TOOLS.has('Bash')).toBe(false)
  })

  it('does not contain Grep', () => {
    expect(FILE_MODIFYING_TOOLS.has('Grep')).toBe(false)
  })

  it('does not contain Glob', () => {
    expect(FILE_MODIFYING_TOOLS.has('Glob')).toBe(false)
  })

  it('is case-sensitive', () => {
    expect(FILE_MODIFYING_TOOLS.has('write')).toBe(false)
    expect(FILE_MODIFYING_TOOLS.has('edit')).toBe(false)
    expect(FILE_MODIFYING_TOOLS.has('WRITE')).toBe(false)
  })

  it('is a Set (no duplicates)', () => {
    expect(FILE_MODIFYING_TOOLS.size).toBe(2)
  })
})

describe('findLastBlockOfType', () => {
  it('finds last text block in simple array', () => {
    const blocks = [
      { type: 'text', text: 'first' },
      { type: 'text', text: 'second' },
    ]
    expect(findLastBlockOfType(blocks, 'text')!.text).toBe('second')
  })

  it('finds last thinking block', () => {
    const blocks = [
      { type: 'thinking', text: 'think1' },
      { type: 'thinking', text: 'think2' },
    ]
    expect(findLastBlockOfType(blocks, 'thinking')!.text).toBe('think2')
  })

  it('returns undefined for empty array', () => {
    expect(findLastBlockOfType([], 'text')).toBeUndefined()
  })

  it('returns undefined when no matching type', () => {
    const blocks = [{ type: 'text', text: 'hello' }]
    expect(findLastBlockOfType(blocks, 'thinking')).toBeUndefined()
  })

  it('does not cross tool_use boundary', () => {
    const blocks = [
      { type: 'text', text: 'before' },
      { type: 'tool_use', name: 'Read', id: '1', input: {} },
      { type: 'text', text: 'after' },
    ]
    // Looking for text should find 'after' (it's after the boundary, so it's the last one)
    expect(findLastBlockOfType(blocks, 'text')!.text).toBe('after')
  })

  it('returns undefined when matching type is only before tool_use boundary', () => {
    const blocks = [
      { type: 'thinking', text: 'think1' },
      { type: 'tool_use', name: 'Read', id: '1', input: {} },
    ]
    expect(findLastBlockOfType(blocks, 'thinking')).toBeUndefined()
  })

  it('finds block when no tool_use boundary exists', () => {
    const blocks = [
      { type: 'thinking', text: 'think1' },
    ]
    expect(findLastBlockOfType(blocks, 'thinking')!.text).toBe('think1')
  })

  it('handles interleaved blocks correctly', () => {
    const blocks = [
      { type: 'text', text: 'text1' },
      { type: 'thinking', text: 'think1' },
      { type: 'text', text: 'text2' },
    ]
    expect(findLastBlockOfType(blocks, 'text')!.text).toBe('text2')
    expect(findLastBlockOfType(blocks, 'thinking')!.text).toBe('think1')
  })

  it('tool_use block as sole block returns undefined for any type', () => {
    const blocks = [
      { type: 'tool_use', name: 'Read', id: '1', input: {} },
    ]
    expect(findLastBlockOfType(blocks, 'text')).toBeUndefined()
    expect(findLastBlockOfType(blocks, 'thinking')).toBeUndefined()
  })

  it('finds block after multiple tool_use boundaries', () => {
    const blocks = [
      { type: 'text', text: 'start' },
      { type: 'tool_use', name: 'Read', id: '1', input: {} },
      { type: 'text', text: 'middle' },
      { type: 'tool_use', name: 'Write', id: '2', input: {} },
      { type: 'text', text: 'end' },
    ]
    expect(findLastBlockOfType(blocks, 'text')!.text).toBe('end')
  })

  it('returns undefined for thinking block between tool_use boundaries (boundary after it)', () => {
    // When searching backward from the end, the Write tool_use at index 2
    // is encountered first, which is a boundary — so thinking is not found.
    const blocks = [
      { type: 'tool_use', name: 'Read', id: '1', input: {} },
      { type: 'thinking', text: 'think between' },
      { type: 'tool_use', name: 'Write', id: '2', input: {} },
    ]
    expect(findLastBlockOfType(blocks, 'thinking')).toBeUndefined()
  })
})

describe('forceCleanupStreamingState', () => {
  it('removes empty streaming message from array (no content, no blocks)', () => {
    const messages = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages).toHaveLength(0)
  })

  it('keeps streaming message with content', () => {
    const messages = [
      { role: 'assistant', content: 'hello', blocks: [], streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages).toHaveLength(1)
    expect(messages[0].streaming).toBeUndefined()
    expect(messages[0].content).toBe('hello')
  })

  it('keeps streaming message with blocks', () => {
    const messages = [
      { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'hello' }], streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages).toHaveLength(1)
    expect(messages[0].streaming).toBeUndefined()
  })

  it('marks unfinished tool_use as done', () => {
    const messages = [
      {
        role: 'assistant',
        content: '',
        blocks: [
          { type: 'tool_use', name: 'Read', id: '1', done: false },
          { type: 'tool_use', name: 'Write', id: '2', done: true },
          { type: 'text', text: 'hello' },
        ],
        streaming: true,
      },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages[0].blocks[0].done).toBe(true)
    expect(messages[0].blocks[1].done).toBe(true)  // Was already done
    expect(messages[0].blocks[2]).toEqual({ type: 'text', text: 'hello' })  // Unchanged
  })

  it('does not mark PermissionApproval blocks as done (requires user interaction)', () => {
    const messages = [
      {
        role: 'assistant',
        content: '',
        blocks: [
          { type: 'tool_use', name: 'Read', id: '1', done: false },
          { type: 'tool_use', name: 'PermissionApproval', id: 'perm_2', done: false },
        ],
        streaming: true,
      },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages[0].blocks[0].done).toBe(true)  // Normal tool_use marked done
    expect(messages[0].blocks[1].done).toBe(false)  // PermissionApproval stays false
  })

  it('calls onRenderNeeded with forceFull=true', () => {
    const onRenderNeeded = vi.fn()
    forceCleanupStreamingState([], { onRenderNeeded })
    expect(onRenderNeeded).toHaveBeenCalledWith(true)
  })

  it('does not modify non-streaming messages', () => {
    const messages = [
      { role: 'user', content: 'hello' },
      { role: 'assistant', content: 'response', blocks: [{ type: 'text', text: 'response' }] },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages[0].content).toBe('hello')
    expect(messages[1].content).toBe('response')
  })

  it('calls onExtractScheduledTasks when streaming message found', () => {
    const messages = [
      { role: 'assistant', content: 'has content', blocks: [], streaming: true },
    ]
    const onExtractScheduledTasks = vi.fn()
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn(), onExtractScheduledTasks })
    expect(onExtractScheduledTasks).toHaveBeenCalledWith(messages)
  })

  it('does not call onExtractScheduledTasks when no streaming message', () => {
    const messages = [
      { role: 'user', content: 'hello' },
    ]
    const onExtractScheduledTasks = vi.fn()
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn(), onExtractScheduledTasks })
    expect(onExtractScheduledTasks).not.toHaveBeenCalled()
  })

  it('returns the streaming message when found', () => {
    const streamingMsg = { role: 'assistant', content: 'test', blocks: [], streaming: true }
    const messages = [streamingMsg]
    const result = forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(result).toBe(streamingMsg)
  })

  it('returns undefined when no streaming message', () => {
    const messages = [{ role: 'user', content: 'hello' }]
    const result = forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(result).toBeUndefined()
  })

  it('handles multiple messages with one streaming', () => {
    const messages: any[] = [
      { role: 'user', content: 'question' },
      { role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Read', id: '1', done: false }], streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages[0].content).toBe('question')  // Unchanged
    expect(messages[1]!.streaming).toBeUndefined()
    expect(messages[1]!.blocks[0]!.done).toBe(true)
  })

  it('removes empty streaming message (no content, empty blocks)', () => {
    const messages = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages).toHaveLength(0)
  })

  it('keeps streaming message with no blocks property but has content', () => {
    const messages = [
      { role: 'assistant', content: 'text only', streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages).toHaveLength(1)
    expect(messages[0].streaming).toBeUndefined()
  })

  it('removes streaming message with no blocks property and no content', () => {
    const messages = [
      { role: 'assistant', content: '', streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages).toHaveLength(0)
  })
})

describe('findStreamingMsg', () => {
  it('finds streaming assistant message', () => {
    const messages = [
      { role: 'user', content: 'hi' },
      { role: 'assistant', content: '', streaming: true },
    ]
    expect(findStreamingMsg(messages)).toBe(messages[1])
  })

  it('returns undefined when no streaming message', () => {
    const messages = [
      { role: 'user', content: 'hi' },
      { role: 'assistant', content: 'done' },
    ]
    expect(findStreamingMsg(messages)).toBeUndefined()
  })

  it('returns undefined for empty array', () => {
    expect(findStreamingMsg([])).toBeUndefined()
  })

  it('returns first streaming message when multiple exist', () => {
    const messages = [
      { role: 'assistant', content: 'a', streaming: true },
      { role: 'assistant', content: 'b', streaming: true },
    ]
    expect(findStreamingMsg(messages)).toBe(messages[0])
  })
})

describe('createPendingUserMessage', () => {
  it('creates message with text and files', () => {
    const msg = createPendingUserMessage('hello', ['/a.txt', '/b.txt'])
    expect(msg.role).toBe('user')
    expect(msg.content).toBe('hello')
    expect(msg.blocks).toEqual([{ type: 'text', text: 'hello' }])
    expect(msg.files).toEqual([{ path: '/a.txt' }, { path: '/b.txt' }])
    expect(msg.pending).toBe(true)
    expect(msg.createdAt).toBeDefined()
  })

  it('handles empty text', () => {
    const msg = createPendingUserMessage('')
    expect(msg.content).toBe('')
    expect(msg.blocks).toEqual([])
    expect(msg.files).toEqual([])
  })

  it('handles no files (default)', () => {
    const msg = createPendingUserMessage('hello')
    expect(msg.files).toEqual([])
  })

  it('handles undefined text', () => {
    const msg = createPendingUserMessage(undefined as any)
    expect(msg.content).toBe('')
    expect(msg.blocks).toEqual([])
  })
})

describe('drainQueueMessage', () => {
  const callbacks = {
    onRenderNeeded: vi.fn(),
    onExtractScheduledTasks: vi.fn(),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('finds and un-marks pending message then pushes streaming assistant', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    const result = drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].pending).toBeUndefined()
    expect(result!.streaming).toBe(true)
    expect(result!.backend).toBe('codebuddy')
    expect(messages).toHaveLength(2)
  })

  it('updates files on un-marked pending message', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello', pending: true, files: [], blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', ['/a.txt'], 'codebuddy', callbacks)
    expect(messages[0].files).toEqual([{ path: '/a.txt' }])
  })

  it('creates user message when pending not found and no existing user msg', () => {
    const messages: any[] = []
    const result = drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages).toHaveLength(2)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('hello')
    expect(result!.role).toBe('assistant')
  })

  it('skips creating user message when existing non-id user msg matches', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello', id: undefined, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    // Only the new streaming assistant is pushed, no duplicate user msg
    expect(messages).toHaveLength(2)
    expect(messages[0].content).toBe('hello')
    expect(messages[1].role).toBe('assistant')
  })

  it('finalizes streaming message before adding new ones (preserves empty msg)', () => {
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    // Old empty streaming is kept (not deleted) to avoid key shifts in v-for.
    // Its streaming flag is removed. New streaming was added.
    const streamingMsgs = messages.filter(m => m.streaming)
    expect(streamingMsgs).toHaveLength(1)
    expect(streamingMsgs[0].backend).toBe('codebuddy')
    // Total messages: old assistant (finalized) + user (un-marked) + new streaming
    expect(messages).toHaveLength(3)
    expect(messages[0].streaming).toBeUndefined()
  })

  it('calls onExtractScheduledTasks during cleanup', () => {
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [
      { role: 'assistant', content: 'has content', blocks: [], streaming: true },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks })
    expect(onExtractScheduledTasks).toHaveBeenCalled()
  })

  it('full queue drain scenario: atomically finalizes A and starts B', () => {
    // Simulate the full flow in a single atomic operation:
    // A is streaming, B is queued (pending) → queue_drain arrives
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    const callbacks = { onRenderNeeded, onExtractScheduledTasks }

    // Initial state — A streaming, B pending
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A msg', blocks: [{ type: 'text', text: 'A msg' }] },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
      { role: 'user', content: 'B msg', blocks: [{ type: 'text', text: 'B msg' }], pending: true },
    ]

    // Single queue_drain event replaces old queue_done + queue_consume + queue_update
    const result = drainQueueMessage(messages, 'B msg', [], 'codebuddy', callbacks)

    // A's assistant message is finalized but still present
    expect(messages).toHaveLength(4)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('A msg')
    expect(messages[1].role).toBe('assistant')
    expect(messages[1].blocks).toEqual([{ type: 'text', text: 'A reply' }])
    expect(messages[1].streaming).toBeUndefined()
    // B's pending flag removed
    expect(messages[2].role).toBe('user')
    expect(messages[2].content).toBe('B msg')
    expect(messages[2].pending).toBeUndefined()
    // New streaming assistant for B
    expect(messages[3].role).toBe('assistant')
    expect(messages[3].streaming).toBe(true)
    expect(result).toBe(messages[3])
  })

  it('preserves empty streaming assistant (no key shift)', () => {
    // drainQueueMessage never deletes messages, even empty ones,
    // to avoid index-based v-for key shifts.
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    // The old empty assistant is kept (streaming removed), not deleted
    expect(messages).toHaveLength(3)
    expect(messages[0].role).toBe('assistant')
    expect(messages[0].streaming).toBeUndefined()
    expect(messages[0].content).toBe('')
    expect(messages[0].blocks).toEqual([])
  })

  it('finalizes unfinished tool_use blocks in streaming message', () => {
    const messages: any[] = [
      {
        role: 'assistant',
        content: '',
        blocks: [
          { type: 'tool_use', name: 'Read', id: '1', done: false, output: '' },
          { type: 'tool_use', name: 'Write', id: '2', done: true, output: 'ok' },
        ],
        streaming: true,
      },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].blocks[0].done).toBe(true)
    expect(messages[0].blocks[1].done).toBe(true) // already was done
    expect(messages[0].streaming).toBeUndefined()
  })

  it('does NOT mark PermissionApproval blocks as done in streaming cleanup', () => {
    const messages: any[] = [
      {
        role: 'assistant',
        content: '',
        blocks: [
          { type: 'tool_use', name: 'Read', id: '1', done: false },
          { type: 'tool_use', name: 'PermissionApproval', id: 'perm_2', done: false },
        ],
        streaming: true,
      },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].blocks[0].done).toBe(true) // Normal tool finalized
    expect(messages[0].blocks[1].done).toBe(false) // PermissionApproval left alone
  })

  it('clears garbage output from finalized tool_use blocks', () => {
    const messages: any[] = [
      {
        role: 'assistant',
        content: '',
        blocks: [
          { type: 'tool_use', name: 'Read', id: '1', done: false, output: '}' },
          { type: 'tool_use', name: 'Write', id: '2', done: false, output: 'real output' },
        ],
        streaming: true,
      },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].blocks[0].output).toBe('') // garbage cleared
    expect(messages[0].blocks[1].output).toBe('real output') // meaningful output kept
  })

  it('preserves A reply with tool_use blocks during drain', () => {
    // A has tool call results, not just text blocks
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()

    const messages: any[] = [
      { role: 'user', id: 1, content: 'A msg', blocks: [{ type: 'text', text: 'A msg' }] },
      {
        role: 'assistant',
        id: 2,
        content: '',
        blocks: [
          { type: 'tool_use', name: 'Read', id: '1', done: true, output: 'file content' },
          { type: 'text', text: 'A summary' },
        ],
        streaming: true,
      },
      { role: 'user', content: 'B msg', blocks: [{ type: 'text', text: 'B msg' }], pending: true },
    ]

    drainQueueMessage(messages, 'B msg', [], 'codebuddy', { onRenderNeeded, onExtractScheduledTasks })

    expect(messages).toHaveLength(4)
    // A's reply preserved with tool_use + text blocks
    expect(messages[1].role).toBe('assistant')
    expect(messages[1].blocks).toHaveLength(2)
    expect(messages[1].blocks[0].name).toBe('Read')
    expect(messages[1].blocks[1].text).toBe('A summary')
    expect(messages[1].streaming).toBeUndefined()
    // B's pending removed
    expect(messages[2].pending).toBeUndefined()
    // New streaming for B
    expect(messages[3].streaming).toBe(true)
  })

  it('handles multiple messages in array during queue drain', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'round 1', blocks: [{ type: 'text', text: 'round 1' }] },
      { role: 'assistant', id: 2, content: 'r1 reply', blocks: [{ type: 'text', text: 'r1 reply' }] },
      { role: 'user', id: 3, content: 'A msg', blocks: [{ type: 'text', text: 'A msg' }] },
      { role: 'assistant', id: 4, content: '', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
      { role: 'user', content: 'B msg', blocks: [{ type: 'text', text: 'B msg' }], pending: true },
    ]

    drainQueueMessage(messages, 'B msg', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks: vi.fn() })

    expect(messages).toHaveLength(6)
    // All earlier messages intact
    expect(messages[0].content).toBe('round 1')
    expect(messages[1].content).toBe('r1 reply')
    expect(messages[2].content).toBe('A msg')
    // A's reply still there
    expect(messages[3].blocks).toEqual([{ type: 'text', text: 'A reply' }])
    expect(messages[3].streaming).toBeUndefined()
    // B un-marked, new streaming
    expect(messages[4].pending).toBeUndefined()
    expect(messages[5].streaming).toBe(true)
  })

  it('drainQueueMessage + syncPendingFromBackend adds new pending messages from backend queue', () => {
    // Simulate the real useChatStream queue_drain handler flow:
    // 1. drainQueueMessage (finalize streaming, un-mark pending, push new streaming)
    // 2. syncPendingFromBackend (sync with backend queue — may add new pending messages)
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'stale' }], streaming: true },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    // Backend queue has a different pending message (e.g. C was enqueued after B)
    const backendQueue = [{ text: 'C msg', files: [], filePaths: [] }]

    // Step 1: drainQueueMessage — finalizes streaming, un-marks B's pending
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    // Step 2: syncPendingFromBackend — adds C as pending
    syncPendingFromBackend(messages, backendQueue)

    // B's pending flag removed
    const bMsg = messages.find((m: any) => m.content === 'hello' && m.role === 'user')
    expect(bMsg.pending).toBeUndefined()
    // C added as pending from backend queue
    const cMsg = messages.find((m: any) => m.content === 'C msg')
    expect(cMsg).toBeDefined()
    expect(cMsg.pending).toBe(true)
  })

  it('drainQueueMessage + syncPendingFromBackend removes stale pending messages', () => {
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'stale' }], streaming: true },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
      { role: 'user', content: 'stale pending', pending: true, blocks: [{ type: 'text', text: 'stale pending' }] },
    ]

    // Step 1: drainQueueMessage
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    // Step 2: syncPendingFromBackend with empty queue — stale pending should be removed
    syncPendingFromBackend(messages, [])

    const staleMsg = messages.find((m: any) => m.content === 'stale pending')
    expect(staleMsg).toBeUndefined()
  })

  it('drainQueueMessage does not lose pending message when syncPendingFromBackend runs AFTER', () => {
    // Critical test: the backendQueue no longer contains the drained message B.
    // If syncPendingFromBackend ran BEFORE drainQueueMessage, it would delete B's
    // pending message. But since drainQueueMessage runs first and un-marks B's
    // pending flag, the subsequent syncPendingFromBackend correctly leaves B alone
    // (it only touches messages that still have the pending flag).
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
      { role: 'user', content: 'B msg', pending: true, blocks: [{ type: 'text', text: 'B msg' }] },
    ]
    // Backend queue is empty — B has been dequeued, no remaining items
    const backendQueue: any[] = []

    // Step 1: drainQueueMessage first — un-marks B's pending
    drainQueueMessage(messages, 'B msg', [], 'codebuddy', callbacks)
    // Step 2: syncPendingFromBackend with empty queue — B's message is NOT pending anymore, so it's preserved
    syncPendingFromBackend(messages, backendQueue)

    // B's user message must still exist and not be pending
    const bMsg = messages.find((m: any) => m.content === 'B msg')
    expect(bMsg).toBeDefined()
    expect(bMsg.pending).toBeUndefined()
  })

  it('does not call onRenderNeeded from drainQueueMessage', () => {
    // drainQueueMessage does not call onRenderNeeded itself —
    // the caller (useChatStream queue_drain handler) triggers renders.
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'stale' }], streaming: true },
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', { onRenderNeeded, onExtractScheduledTasks })
    expect(onRenderNeeded).not.toHaveBeenCalled()
    // But onExtractScheduledTasks should be called when a stale streaming msg was found
    expect(onExtractScheduledTasks).toHaveBeenCalled()
  })

  it('does not call onExtractScheduledTasks when no stale streaming msg exists', () => {
    const onExtractScheduledTasks = vi.fn()
    const onRenderNeeded = vi.fn()
    const messages: any[] = [
      { role: 'user', content: 'hello', pending: true, blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', { onRenderNeeded, onExtractScheduledTasks })
    expect(onExtractScheduledTasks).not.toHaveBeenCalled()
  })
})

describe('syncPendingFromBackend', () => {
  it('adds pending messages from backend that are not locally present', () => {
    const messages: any[] = []
    syncPendingFromBackend(messages, [{ text: 'hello' }])
    expect(messages).toHaveLength(1)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('hello')
    expect(messages[0].pending).toBe(true)
  })

  it('does not duplicate existing pending messages', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello', pending: true },
    ]
    syncPendingFromBackend(messages, [{ text: 'hello' }])
    expect(messages).toHaveLength(1)
  })

  it('removes pending messages not in backend queue', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello', pending: true },
    ]
    syncPendingFromBackend(messages, [])
    expect(messages).toHaveLength(0)
  })

  it('keeps non-pending user messages even if not in backend queue', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello' },
    ]
    syncPendingFromBackend(messages, [])
    expect(messages).toHaveLength(1)
  })

  it('merges files and filePaths from backend item', () => {
    const messages: any[] = []
    syncPendingFromBackend(messages, [{ text: 'hi', files: ['/a.txt'], filePaths: ['/b.txt'] }])
    expect(messages[0].files).toEqual([{ path: '/a.txt' }, { path: '/b.txt' }])
  })

  it('handles backend items with missing fields', () => {
    const messages: any[] = []
    syncPendingFromBackend(messages, [{ text: 'hi' }])
    expect(messages[0].files).toEqual([])
  })

  it('handles backend item with empty text', () => {
    const messages: any[] = []
    syncPendingFromBackend(messages, [{ text: '' }])
    expect(messages).toHaveLength(1)
    expect(messages[0].content).toBe('')
  })

  it('removes only stale pending messages while adding new ones', () => {
    const messages: any[] = [
      { role: 'user', content: 'old', pending: true },
    ]
    syncPendingFromBackend(messages, [{ text: 'new' }])
    expect(messages).toHaveLength(1)
    expect(messages[0].content).toBe('new')
    expect(messages[0].pending).toBe(true)
  })
})

describe('shouldRetryToolFetch', () => {
  it('returns true for 404 with retries remaining and overlay open', () => {
    expect(shouldRetryToolFetch(404, 0, true)).toBe(true)
    expect(shouldRetryToolFetch(404, 1, true)).toBe(true)
    expect(shouldRetryToolFetch(404, 2, true)).toBe(true)
  })

  it('returns false when retry count exhausted (3 retries)', () => {
    expect(shouldRetryToolFetch(404, 3, true)).toBe(false)
    expect(shouldRetryToolFetch(404, 4, true)).toBe(false)
  })

  it('returns false when overlay is closed', () => {
    expect(shouldRetryToolFetch(404, 0, false)).toBe(false)
    expect(shouldRetryToolFetch(404, 2, false)).toBe(false)
  })

  it('returns false for non-404 errors', () => {
    expect(shouldRetryToolFetch(500, 0, true)).toBe(false)
    expect(shouldRetryToolFetch(403, 0, true)).toBe(false)
    expect(shouldRetryToolFetch(200, 0, true)).toBe(false)
  })

  it('returns false for 404 with retries exhausted AND overlay closed', () => {
    expect(shouldRetryToolFetch(404, 3, false)).toBe(false)
  })

  it('respects custom maxRetries', () => {
    expect(shouldRetryToolFetch(404, 3, true, 5)).toBe(true)
    expect(shouldRetryToolFetch(404, 5, true, 5)).toBe(false)
  })

  it('boundary: retryCount equals maxRetries should not retry', () => {
    expect(shouldRetryToolFetch(404, 3, true, 3)).toBe(false)
  })

  it('boundary: retryCount one less than maxRetries should retry', () => {
    expect(shouldRetryToolFetch(404, 2, true, 3)).toBe(true)
  })
})

describe('resolveEffectiveMsgId', () => {
  it('uses overlay msgId when live block exists', () => {
    const liveBlock = { type: 'tool_use', name: 'Read', tool_id: 'call_123' }
    expect(resolveEffectiveMsgId(liveBlock, 999, 100)).toBe(999)
  })

  it('uses overlay msgId (string) when live block exists', () => {
    const liveBlock = { type: 'tool_use', name: 'Read', tool_id: 'call_123' }
    expect(resolveEffectiveMsgId(liveBlock, 'abc', 'original')).toBe('abc')
  })

  it('falls back to original msgId when live block is undefined', () => {
    expect(resolveEffectiveMsgId(undefined, 999, 100)).toBe(100)
  })

  it('falls back to original msgId when live block is null', () => {
    expect(resolveEffectiveMsgId(null, 999, 100)).toBe(100)
  })

  it('uses overlay msgId even when it differs from original', () => {
    // Scenario: loadHistory replaced messages array, msgId changed from 100 → 200
    const liveBlock = { type: 'tool_use', name: 'Read' }
    expect(resolveEffectiveMsgId(liveBlock, 200, 100)).toBe(200)
  })

  it('uses original msgId when overlay msgId is undefined and live block exists', () => {
    const liveBlock = { type: 'tool_use', name: 'Read' }
    expect(resolveEffectiveMsgId(liveBlock, undefined, 100)).toBe(100)
  })

  it('uses overlay msgId=0 when live block exists (0 is a valid value)', () => {
    // In the original code: liveBlock ? overlayMsgId : originalMsgId
    // If overlayMsgId is 0, it's used as-is (not falsy fallback)
    const liveBlock = { type: 'tool_use', name: 'Read' }
    expect(resolveEffectiveMsgId(liveBlock, 0, 100)).toBe(0)
  })
})



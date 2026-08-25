import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  FILE_MODIFYING_TOOLS,
  findLastBlockOfType,
  forceCleanupStreamingState,
  cancelPendingMessages,
  findStreamingMsg,
  drainQueueMessage,
  generateDrainId,
  shouldRetryToolFetch,
  resolveEffectiveMsgId,
  extractFileChanges,
  sortMessages,
  messageSortValue,
  isTransientMessage,
  nextClientSeq,
  anchorRepliesToQuestions,
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

describe('drainQueueMessage', () => {
  const callbacks = {
    onRenderNeeded: vi.fn(),
    onExtractScheduledTasks: vi.fn(),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('finalizes streaming assistant and pushes new streaming placeholder', () => {
    const messages: any[] = [
      { role: 'assistant', content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
    ]
    const result = drainQueueMessage(messages, '', 'B msg', [], 'codebuddy', callbacks)
    // Old streaming is finalized (flag removed)
    expect(messages[0].streaming).toBeUndefined()
    // Drained user message pushed
    expect(messages[1].role).toBe('user')
    expect(messages[1].content).toBe('B msg')
    // New streaming placeholder pushed
    expect(result!.streaming).toBe(true)
    expect(result!.backend).toBe('codebuddy')
    expect(result!.role).toBe('assistant')
    expect(messages).toHaveLength(3)
  })

  it('pushes new streaming placeholder even when no existing streaming message', () => {
    const messages: any[] = []
    const result = drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks)
    // User msg + streaming placeholder
    expect(messages).toHaveLength(2)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('hello')
    expect(messages[1].role).toBe('assistant')
    expect(messages[1].streaming).toBe(true)
    expect(messages[1].backend).toBe('codebuddy')
    expect(result).toBe(messages[1])
  })

  it('deduplicates user message by drain ID (not content text)', () => {
    const drainId = 'drain-1234567890-abc123'
    const messages: any[] = [
      { role: 'user', id: drainId, _drain: true, content: 'existing user msg', blocks: [{ type: 'text', text: 'existing user msg' }] },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    const result = drainQueueMessage(messages, '', 'existing user msg', [], 'codebuddy', callbacks, drainId)
    // No duplicate user message — dedup by drain ID
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(1)
    // Old assistant (finalized) + new streaming
    expect(messages).toHaveLength(3)
    expect(result!.streaming).toBe(true)
  })

  it('does NOT deduplicate by content text — same content with different drain IDs is allowed', () => {
    const messages: any[] = [
      { role: 'user', id: 'drain-111-first', _drain: true, content: 'same text', blocks: [{ type: 'text', text: 'same text' }] },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    // drainQueueMessage generates a NEW drain ID, different from 'drain-111-first'
    drainQueueMessage(messages, '', 'same text', [], 'codebuddy', callbacks)
    const userMsgs = messages.filter(m => m.role === 'user')
    // Both user messages kept — they have different drain IDs
    expect(userMsgs).toHaveLength(2)
  })

  it('finalizes streaming message and preserves it (never deletes, avoids key shifts)', () => {
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks)
    // Old empty streaming is kept (not deleted) to avoid v-for key shifts
    // Messages: old assistant(finalized) + user msg + new streaming
    expect(messages).toHaveLength(3)
    expect(messages[0].streaming).toBeUndefined()
    expect(messages[0].content).toBe('')
    expect(messages[0].blocks).toEqual([])
    // User message
    expect(messages[1].role).toBe('user')
    expect(messages[1].content).toBe('hello')
    // New streaming placeholder
    expect(messages[2].streaming).toBe(true)
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
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks)
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
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks)
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
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].blocks[0].output).toBe('') // garbage cleared
    expect(messages[0].blocks[1].output).toBe('real output') // meaningful output kept
  })

  it('calls onExtractScheduledTasks when streaming message is found', () => {
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [
      { role: 'assistant', content: 'has content', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks })
    expect(onExtractScheduledTasks).toHaveBeenCalledWith(messages)
  })

  it('does not call onExtractScheduledTasks when no stale streaming message exists', () => {
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = []
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks })
    expect(onExtractScheduledTasks).not.toHaveBeenCalled()
  })

  it('does not call onRenderNeeded from drainQueueMessage', () => {
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'stale' }], streaming: true },
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', { onRenderNeeded, onExtractScheduledTasks })
    expect(onRenderNeeded).not.toHaveBeenCalled()
    // But onExtractScheduledTasks should be called when a stale streaming msg was found
    expect(onExtractScheduledTasks).toHaveBeenCalled()
  })

  it('full queue drain scenario: atomically finalizes A and starts B', () => {
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    const callbacks = { onRenderNeeded, onExtractScheduledTasks }

    // Initial state — A streaming
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A msg', blocks: [{ type: 'text', text: 'A msg' }] },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
    ]

    // queue_drain event with B's user content
    const result = drainQueueMessage(messages, '', 'B msg', [], 'codebuddy', callbacks)

    // A's assistant message is finalized but still present
    // Messages: A user, A assistant(finalized), B user, B streaming
    expect(messages).toHaveLength(4)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('A msg')
    expect(messages[1].role).toBe('assistant')
    expect(messages[1].blocks).toEqual([{ type: 'text', text: 'A reply' }])
    expect(messages[1].streaming).toBeUndefined()
    // B's user message pushed
    expect(messages[2].role).toBe('user')
    expect(messages[2].content).toBe('B msg')
    // New streaming assistant for B
    expect(messages[3].role).toBe('assistant')
    expect(messages[3].streaming).toBe(true)
    expect(result).toBe(messages[3])
  })

  it('preserves A reply with tool_use blocks during drain', () => {
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
    ]

    drainQueueMessage(messages, '', 'B msg', [], 'codebuddy', { onRenderNeeded, onExtractScheduledTasks })

    expect(messages).toHaveLength(4)
    // A's reply preserved with tool_use + text blocks
    expect(messages[1].role).toBe('assistant')
    expect(messages[1].blocks).toHaveLength(2)
    expect(messages[1].blocks[0].name).toBe('Read')
    expect(messages[1].blocks[1].text).toBe('A summary')
    expect(messages[1].streaming).toBeUndefined()
    // B's user message
    expect(messages[2].role).toBe('user')
    expect(messages[2].content).toBe('B msg')
    // New streaming for B
    expect(messages[3].streaming).toBe(true)
  })

  it('handles multiple messages in array during queue drain', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'round 1', blocks: [{ type: 'text', text: 'round 1' }] },
      { role: 'assistant', id: 2, content: 'r1 reply', blocks: [{ type: 'text', text: 'r1 reply' }] },
      { role: 'user', id: 3, content: 'A msg', blocks: [{ type: 'text', text: 'A msg' }] },
      { role: 'assistant', id: 4, content: '', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
    ]

    drainQueueMessage(messages, '', 'B msg', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks: vi.fn() })

    expect(messages).toHaveLength(6)
    // All earlier messages intact
    expect(messages[0].content).toBe('round 1')
    expect(messages[1].content).toBe('r1 reply')
    expect(messages[2].content).toBe('A msg')
    // A's reply still there
    expect(messages[3].blocks).toEqual([{ type: 'text', text: 'A reply' }])
    expect(messages[3].streaming).toBeUndefined()
    // B's user message
    expect(messages[4].role).toBe('user')
    expect(messages[4].content).toBe('B msg')
    // New streaming
    expect(messages[5].streaming).toBe(true)
  })

  it('new streaming placeholder has correct createdAt and backend', () => {
    const before = new Date().toISOString()
    const messages: any[] = []
    const result = drainQueueMessage(messages, '', 'hello', [], 'claude', callbacks)
    const after = new Date().toISOString()
    expect(result!.backend).toBe('claude')
    expect(result!.createdAt >= before).toBe(true)
    expect(result!.createdAt <= after).toBe(true)
  })

  it('assigns drain ID to the pushed user message', () => {
    const messages: any[] = []
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks, 'drain-test-123')
    expect(messages[0].id).toBe('drain-test-123')
  })

  it('auto-generates drain ID when not provided', () => {
    const messages: any[] = []
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].id).toMatch(/^drain-\d+-[a-z0-9]+$/)
  })

  it('drain ID does not collide with DB numeric IDs', () => {
    const messages: any[] = [
      { role: 'user', id: 42, content: 'DB user msg', blocks: [{ type: 'text', text: 'DB user msg' }] },
    ]
    drainQueueMessage(messages, '', 'new msg', [], 'codebuddy', callbacks)
    const drainMsg = messages.find((m: any) => m.role === 'user' && m.content === 'new msg')
    expect(drainMsg).toBeDefined()
    expect(typeof drainMsg.id).toBe('string')
    expect(drainMsg.id.startsWith('drain-')).toBe(true)
    // Numeric DB IDs (42) and string drain IDs can never collide
    expect(drainMsg.id).not.toBe(42)
  })

  it('drain ID does not collide with optimistic push local- IDs', () => {
    const messages: any[] = [
      { role: 'user', id: 'local-1700000000000', content: 'optimistic msg', blocks: [{ type: 'text', text: 'optimistic msg' }] },
    ]
    drainQueueMessage(messages, '', 'drained msg', [], 'codebuddy', callbacks)
    const drainMsg = messages.find((m: any) => m.role === 'user' && m.content === 'drained msg')
    expect(drainMsg.id.startsWith('drain-')).toBe(true)
    expect(drainMsg.id.startsWith('local-')).toBe(false)
  })

  it('drain pushes a user message that loadHistory later replaces with DB rows', () => {
    const messages: any[] = []
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('hello')

    // Simulate loadHistory: replace with DB messages (numeric IDs)
    const dbMessages = [
      { role: 'user', id: 1, content: 'hello', blocks: [{ type: 'text', text: 'hello' }] },
      { role: 'assistant', id: 2, content: 'response', blocks: [{ type: 'text', text: 'response' }] },
    ]
    messages.length = 0
    messages.push(...dbMessages)

    // DB rows carry numeric ids — authoritative order
    expect(messages.every(m => typeof m.id === 'number')).toBe(true)
  })

  it('loadHistory race: DB message with different ID coexists with drained message', () => {
    const drainId = 'drain-1700000000000-abc123'
    const messages: any[] = [
      { role: 'user', id: 42, content: 'hello', blocks: [{ type: 'text', text: 'hello' }] },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks, drainId)
    // Both messages exist — the DB one and the drain one
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(2)
    expect(userMsgs[0].id).toBe(42)           // DB
    expect(userMsgs[1].id).toBe(drainId)      // drain
  })

  it('skips push when same drainId already exists (idempotent)', () => {
    const drainId = 'drain-1700000000000-xyz789'
    const messages: any[] = [
      { role: 'user', id: drainId, _drain: true, content: 'hello', blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks, drainId)
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(1)
  })

  // ── dbMessageId parameter (queue_drain carries DB message ID) ──

  it('adopts numeric DB id for a drained message when dbMessageId is provided', () => {
    const messages: any[] = [
      { role: 'assistant', content: 'reply', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'B', [], 'claude', callbacks, undefined, 42)
    const userMsg = messages.find(m => m.role === 'user' && m.content === 'B')
    expect(userMsg).toBeDefined()
    // The numeric dbMessageId IS adopted — the message is now a normal
    // chat_history row ordered by its DB id.
    expect(userMsg.id).toBe(42)
  })

  it('uses drain id (string) only when dbMessageId is not provided', () => {
    const messages: any[] = [
      { role: 'assistant', content: 'reply', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'B', [], 'claude', callbacks, 'drain-custom', undefined)
    const userMsg = messages.find(m => m.role === 'user' && m.content === 'B')
    expect(userMsg.id).toBe('drain-custom')
  })

  it('assigns stable drain ID to streaming assistant placeholder (never undefined)', () => {
    const messages: any[] = [
      { role: 'assistant', content: 'reply', blocks: [], streaming: true },
    ]
    const result = drainQueueMessage(messages, '', 'B', [], 'claude', callbacks, undefined, 42)
    // Streaming assistant must have a non-undefined id — prevents 'local-{index}' v-for key
    expect(result.id).toBeDefined()
    expect(typeof result.id).toBe('string')
    expect(result.id).toMatch(/^drain-/)
  })

  it('no message has undefined id after drain with dbMessageId', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [] },
      { role: 'assistant', id: 2, content: 'reply', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'B', [], 'claude', callbacks, undefined, 50)
    for (const msg of messages) {
      expect(msg.id).toBeDefined()
    }
  })

  // ── pending message flag clearing (new architecture) ──

  it('finds pending message and clears its flag instead of pushing duplicate', () => {
    const messages: any[] = [
      { role: 'user', id: 'queue-1', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], pending: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'queue-1', 'hello', [], 'claude', callbacks)
    // No duplicate user message — the existing pending one had its flag cleared
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(1)
    expect(userMsgs[0].pending).toBeUndefined()
    expect(userMsgs[0].content).toBe('hello')
  })

  it('keeps the found pending message transient (string id) when parent is still transient', () => {
    const messages: any[] = [
      { role: 'user', id: 'queue-1', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], pending: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'queue-1', 'hello', [], 'claude', callbacks, undefined, 42)
    const userMsg = messages.find(m => m.role === 'user')
    // Parent (none before it) is not DB-backed, so the drained message keeps
    // its transient string id (queueId) to avoid sorting above earlier
    // still-transient messages (M1).
    expect(userMsg.id).toBe('queue-1')
    expect(userMsg.pending).toBeUndefined()
  })

  it('falls back to push when no matching pending message found', () => {
    // Pending message content doesn't match drain content — push as fallback
    const messages: any[] = [
      { role: 'user', id: 'queue-1', content: 'other msg', blocks: [{ type: 'text', text: 'other msg' }], pending: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'hello', [], 'claude', callbacks)
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(2)
    // First pending message still has its flag
    expect(userMsgs[0].pending).toBe(true)
    // New message was pushed as fallback
    expect(userMsgs[1].content).toBe('hello')
  })

  it('FIFO: with two identical pending messages, first drain clears first pending', () => {
    const messages: any[] = [
      { role: 'user', id: 'queue-1', content: 'yes', blocks: [{ type: 'text', text: 'yes' }], pending: true },
      { role: 'user', id: 'queue-2', content: 'yes', blocks: [{ type: 'text', text: 'yes' }], pending: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'queue-1', 'yes', [], 'claude', callbacks)
    // First pending message (queue-1) has flag cleared, second (queue-2) still pending
    expect(messages.find((m: any) => m.id === 'queue-1').pending).toBeUndefined()
    expect(messages.find((m: any) => m.id === 'queue-2').pending).toBe(true)
  })

  // ── queueId matching ──

  it('matches pending message by queueId when provided (keeps transient string id)', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], pending: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'pending-1', 'hello', [], 'claude', callbacks, undefined, 42)
    const userMsg = messages.find(m => m.role === 'user')
    expect(userMsg.id).toBe('pending-1')
    expect(userMsg.pending).toBeUndefined()
  })

  it('prefers queueId match over content match', () => {
    // Two pending messages with same content but different IDs
    const messages: any[] = [
      { role: 'user', id: 'pending-A', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], pending: true },
      { role: 'user', id: 'pending-B', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], pending: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'pending-B', 'hello', [], 'claude', callbacks)
    // pending-B should have its flag cleared (queueId match), not pending-A
    expect(messages.find((m: any) => m.id === 'pending-A').pending).toBe(true)
    expect(messages.find((m: any) => m.id === 'pending-B').pending).toBeUndefined()
  })

  it('matches pending message by queueId (content match not needed)', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], pending: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'pending-1', 'hello', [], 'claude', callbacks)
    const userMsg = messages.find(m => m.role === 'user')
    expect(userMsg.pending).toBeUndefined()
  })

  // ── _remote message matching (cross-device sync) ──

  it('finds _remote message by _remoteQueueId and clears flag instead of pushing duplicate', () => {
    const messages: any[] = [
      { role: 'user', id: 'remote-1700000000000-abc', content: 'from phone', blocks: [{ type: 'text', text: 'from phone' }], _remote: true, _remoteQueueId: 'remote-q-1' },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'remote-q-1', 'from phone', [], 'codebuddy', callbacks, undefined, 42)
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(1)
    expect(userMsgs[0]._remote).toBeUndefined()
  })

  it('preserves a numeric id on a drained cross-device _remote message (no key churn)', () => {
    // A _remote message that arrived already persisted in the DB carries a real
    // numeric id. On drain it must be KEPT — replacing it would churn the v-for
    // key and drop per-bubble render state.
    const messages: any[] = [
      { role: 'user', id: 42, content: 'from phone', blocks: [{ type: 'text', text: 'from phone' }], _remote: true, _remoteQueueId: 'remote-q-1' },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'remote-q-1', 'from phone', [], 'codebuddy', callbacks, undefined, 99)
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(1)
    expect(userMsgs[0]._remote).toBeUndefined()
    // Numeric DB id is authoritative — not replaced by a drain id.
    expect(userMsgs[0].id).toBe(42)
  })

  it('prefers pending match over _remote match', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], pending: true },
      { role: 'user', id: 'remote-1', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], _remote: true },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, '', 'hello', [], 'codebuddy', callbacks, undefined, 99)
    const userMsgs = messages.filter(m => m.role === 'user')
    // Both get matched — pending flag cleared first (findIndex), _remote also matched on same content
    expect(userMsgs[0].pending).toBeUndefined()
  })

  // ── streaming placeholder insertion position ──

  it('inserts streaming assistant AFTER the drained user message, before pending messages', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 'queue-B', content: 'B', blocks: [{ type: 'text', text: 'B' }], pending: true, seq: 1 },
      { role: 'user', id: 'queue-C', content: 'C', blocks: [{ type: 'text', text: 'C' }], pending: true, seq: 2 },
    ]
    drainQueueMessage(messages, 'queue-B', 'B', [], 'claude', callbacks)
    expect(messages[2].role).toBe('user')
    expect(messages[2].content).toBe('B')
    expect(messages[2].pending).toBeUndefined()
    expect(messages[3].role).toBe('assistant')
    expect(messages[3].streaming).toBe(true)
    expect(messages[4].role).toBe('user')
    expect(messages[4].content).toBe('C')
    expect(messages[4].pending).toBe(true)
  })

  it('inserts streaming assistant after fallback push when pending not found', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 'queue-C', content: 'C', blocks: [{ type: 'text', text: 'C' }], pending: true },
    ]
    drainQueueMessage(messages, '', 'B', [], 'claude', callbacks)
    // B user message was pushed as fallback, streaming goes right after it
    const bIdx = messages.findIndex((m: any) => m.role === 'user' && m.content === 'B')
    expect(bIdx).not.toBe(-1)
    expect(messages[bIdx + 1].role).toBe('assistant')
    expect(messages[bIdx + 1].streaming).toBe(true)
  })

  it('single queued message: updates the queued bubble in place and places the reply directly below it', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
      { role: 'user', id: 'queue-B', content: 'B', blocks: [{ type: 'text', text: 'B' }], pending: true, seq: nextClientSeq() },
    ]
    sortMessages(messages)
    const result = drainQueueMessage(messages, 'queue-B', 'B', [], 'claude', callbacks, undefined, 3)

    // The queued bubble was updated in place (no duplicate) and adopts the
    // numeric DB id (parent A is DB-backed, so the DB-id domain is safe).
    const bUsers = messages.filter(m => m.role === 'user' && m.content === 'B')
    expect(bUsers).toHaveLength(1)
    expect(bUsers[0].id).toBe(3)
    expect(bUsers[0].pending).toBeUndefined()

    // The previous assistant (A reply) was finalized.
    const aReply = messages.find(m => m.content === 'A reply')
    expect(aReply.streaming).toBeUndefined()

    // The reply sorts immediately after its question.
    sortMessages(messages)
    const idxB = messages.findIndex(m => m.content === 'B')
    const idxOut = messages.findIndex(m => m === result)
    expect(idxOut).toBe(idxB + 1)
  })

  it('multiple queued messages: each reply stays between its own question and the next queued message', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A' },
      { role: 'assistant', id: 2, content: 'A reply' },
      { role: 'user', id: 'queue-B', content: 'B', pending: true, seq: nextClientSeq() },
      { role: 'user', id: 'queue-C', content: 'C', pending: true, seq: nextClientSeq() },
    ]
    sortMessages(messages)

    const rB = drainQueueMessage(messages, 'queue-B', 'B', [], 'claude', callbacks, undefined, 3)
    rB!.blocks!.push({ type: 'text', text: 'B reply' })
    const rC = drainQueueMessage(messages, 'queue-C', 'C', [], 'claude', callbacks, undefined, 5)
    rC!.blocks!.push({ type: 'text', text: 'C reply' })

    sortMessages(messages)
    const contents = messages.map(m => m.content || (m.blocks || []).map((b: any) => b.text || '').join(''))
    // Final order: A, A reply, B, B reply, C, C reply
    expect(contents.indexOf('A')).toBeLessThan(contents.indexOf('B'))
    expect(contents.indexOf('B')).toBeLessThan(contents.indexOf('B reply'))
    expect(contents.indexOf('B reply')).toBeLessThan(contents.indexOf('C'))
    expect(contents.indexOf('C')).toBeLessThan(contents.indexOf('C reply'))
    // No duplicate user messages.
    expect(messages.filter(m => m.role === 'user')).toHaveLength(3)
  })

  it('keeps an earlier still-transient question above a later drained queued message (regression)', () => {
    // Real-flow scenario: Q1 was sent while idle, so it is a plain optimistic
    // push with a string id (NOT pending) and its reply S1 is streaming. Q2 was
    // enqueued while S1 was generating, so it is pending. When Q2 is drained,
    // the drained message must NOT be moved to the DB-id domain — otherwise it
    // (and its reply) would sort above Q1/S1, whose DB ids the frontend doesn't
    // know yet, producing the queued-message display misalignment.
    const q1 = { role: 'user', id: 'pending-1', content: 'Q1', blocks: [{ type: 'text', text: 'Q1' }], seq: nextClientSeq() }
    const s1 = { role: 'assistant', id: 'drain-x', content: '', blocks: [{ type: 'text', text: 'S1 reply' }], streaming: true, seq: nextClientSeq(), parentQueueId: String(q1.id) }
    const q2 = { role: 'user', id: 'queue-B', content: 'Q2', blocks: [{ type: 'text', text: 'Q2' }], pending: true, seq: nextClientSeq() }
    const messages: any[] = [q1, s1, q2]
    sortMessages(messages)

    const s2 = drainQueueMessage(messages, 'queue-B', 'Q2', [], 'claude', callbacks, undefined, 3)
    s2!.blocks!.push({ type: 'text', text: 'S2 reply' })
    sortMessages(messages)

    const contents = messages.map(m => m.content || (m.blocks || []).map((b: any) => b.text || '').join(''))
    expect(contents).toEqual(['Q1', 'S1 reply', 'Q2', 'S2 reply'])
    // The drained Q2 keeps its transient string id (not the numeric 3).
    const q2After = messages.find((m: any) => m.content === 'Q2')
    expect(q2After.id).toBe('queue-B')
    expect(q2After.pending).toBeUndefined()
  })

  it('keeps the earlier question above an enqueued message that lacks a seq (regression)', () => {
    // Real enqueue path (enqueueAndMaybeStart) pushes the pending message WITHOUT
    // a `seq`. messageSortValue treats a missing seq as 0 → TRANSIENT_BASE + 0,
    // which sorts ABOVE every other transient message (seq >= 1). This sent the
    // queued message to the very top and pushed the earlier question+reply to the
    // bottom. The drained message must inherit a proper position.
    const q1 = { role: 'user', id: 'pending-1', content: 'Q1', blocks: [{ type: 'text', text: 'Q1' }], seq: nextClientSeq() }
    const s1 = { role: 'assistant', id: 'drain-x', content: '', blocks: [{ type: 'text', text: 'S1 reply' }], streaming: true, seq: nextClientSeq(), parentQueueId: String(q1.id) }
    // q2 has pending=true but NO seq — exactly what enqueueAndMaybeStart pushes.
    const q2 = { role: 'user', id: 'queue-B', content: 'Q2', blocks: [{ type: 'text', text: 'Q2' }], pending: true }
    const messages: any[] = [q1, s1, q2]
    sortMessages(messages)

    const s2 = drainQueueMessage(messages, 'queue-B', 'Q2', [], 'claude', callbacks, undefined, 3)
    s2!.blocks!.push({ type: 'text', text: 'S2 reply' })
    sortMessages(messages)

    const contents = messages.map(m => m.content || (m.blocks || []).map((b: any) => b.text || '').join(''))
    expect(contents).toEqual(['Q1', 'S1 reply', 'Q2', 'S2 reply'])
  })

  it('cancel while queued: removes the queued messages from the array', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A' },
      { role: 'user', id: 'queue-B', content: 'B', pending: true, seq: nextClientSeq() },
      { role: 'user', id: 'queue-C', content: 'C', pending: true, seq: nextClientSeq() },
    ]
    const removed = cancelPendingMessages(messages, ['queue-B'])
    expect(removed).toBe(1)
    expect(messages.some(m => m.content === 'B')).toBe(false)
    // The other still-queued message is untouched.
    expect(messages.some(m => m.content === 'C')).toBe(true)
  })
})

describe('sortMessages', () => {
  it('sorts DB-backed messages by numeric id ascending', () => {
    const messages = [
      { role: 'assistant', id: 3, content: 'r3' },
      { role: 'user', id: 1, content: 'u1' },
      { role: 'assistant', id: 2, content: 'r2' },
    ] as any[]
    sortMessages(messages)
    expect(messages.map(m => m.id)).toEqual([1, 2, 3])
  })

  it('places all transient messages after every DB-backed message', () => {
    const messages = [
      { role: 'user', id: 'pending-x', content: 'u2', pending: true, seq: 1 },
      { role: 'assistant', id: 2, content: 'r1' },
      { role: 'user', id: 1, content: 'u1' },
    ] as any[]
    sortMessages(messages)
    expect(messages.map(m => m.id)).toEqual([1, 2, 'pending-x'])
  })

  it('keeps a streaming reply anchored below its own question, above later pending messages', () => {
    const parent = { role: 'user', id: 'queue-B', content: 'B', pending: true, seq: 1 }
    const messages = [
      { role: 'user', id: 1, content: 'A' },
      { role: 'assistant', id: 2, content: 'A reply' },
      parent,
      { role: 'user', id: 'queue-C', content: 'C', pending: true, seq: 2 },
      { role: 'assistant', id: 'drain-1', content: '', streaming: true, seq: 3, parentQueueId: String(parent.id) },
    ] as any[]
    sortMessages(messages)
    const roles = messages.map(m => `${m.role}:${m.content}`)
    expect(roles).toEqual(['user:A', 'assistant:A reply', 'user:B', 'assistant:', 'user:C'])
  })

  it('never shows a new reply above an older reply even when physical order is scrambled', () => {
    // Simulate the reported bug interleaving: array physically scrambled, both
    // replies present. Sorting must restore DB order (older reply below older
    // user, newer reply below newer user).
    const parentB = { role: 'user', id: 4, content: 'B' }
    const messages = [
      { role: 'assistant', id: 5, content: 'B reply', streaming: true, seq: 4, parentQueueId: String(parentB.id) },
      { role: 'assistant', id: 2, content: 'A reply' },
      { role: 'user', id: 1, content: 'A' },
      { role: 'user', id: 4, content: 'B' },
    ] as any[]
    sortMessages(messages)
    const contents = messages.map(m => m.content)
    expect(contents).toEqual(['A', 'A reply', 'B', 'B reply'])
    // The new reply (B reply) must be BELOW the older reply (A reply).
    const idxA = contents.indexOf('A reply')
    const idxB = contents.indexOf('B reply')
    expect(idxB).toBeGreaterThan(idxA)
  })

  it('anchors a streaming reply to a DB-backed parent via parentQueueId (id + 0.5)', () => {
    const parent = { role: 'user', id: 3, content: 'B' }
    const messages = [
      { role: 'assistant', id: 4, content: 'B reply', streaming: true, seq: 9, parentQueueId: String(parent.id) },
      { role: 'assistant', id: 2, content: 'A reply' },
      { role: 'user', id: 1, content: 'A' },
      parent,
    ] as any[]
    sortMessages(messages)
    expect(messages.map(m => m.content)).toEqual(['A', 'A reply', 'B', 'B reply'])
  })

  it('treats a streaming placeholder with a numeric id as transient (stays anchored) until finalized', () => {
    const parent = { role: 'user', id: 3, content: 'B' }
    const streaming = { role: 'assistant', id: 7, content: '', streaming: true, seq: 1, parentQueueId: String(parent.id) }
    expect(isTransientMessage(streaming)).toBe(true)
    // While streaming, sortMessages anchors it after its parent via
    // parentQueueId — never by its numeric id.
    const msgs1: any[] = [streaming, parent]
    sortMessages(msgs1)
    expect(msgs1[0]).toBe(parent)
    expect(msgs1[1]).toBe(streaming)
    // Once finalized, it must STILL stay anchored: its parent may still be
    // transient (string id), in which case falling back to the numeric id
    // would sort the reply above its own question. Only loadHistory (which
    // rebuilds authoritative DB order) drops the anchor.
    delete streaming.streaming
    const msgs2: any[] = [streaming, parent]
    sortMessages(msgs2)
    expect(msgs2[0]).toBe(parent)
    expect(msgs2[1]).toBe(streaming)
  })

  it('keeps a finalized reply with a numeric id anchored after its still-transient question (regression)', () => {
    // stream_start set the reply's id to a numeric DB id, then the reply was
    // finalized (streaming removed). Because its question Q1 is still transient
    // (string id → TRANSIENT_BASE+seq, huge), the reply must NOT fall back to its
    // small numeric id — that would sort it ABOVE Q1 (the observed swap). It must
    // stay anchored via parentQueueId until loadHistory rebuilds everything.
    const q1 = { role: 'user', id: 'pending-1', content: 'Q1', blocks: [{ type: 'text', text: 'Q1' }], seq: 1 }
    const a1 = { role: 'assistant', id: 5, content: 'A1 reply', blocks: [{ type: 'text', text: 'A1 reply' }], parentQueueId: String(q1.id) }
    const q2 = { role: 'user', id: 'pending-2', content: 'Q2', blocks: [{ type: 'text', text: 'Q2' }], pending: true, seq: 2 }
    const a2 = { role: 'assistant', id: 6, content: 'A2 reply', blocks: [{ type: 'text', text: 'A2 reply' }], streaming: true, parentQueueId: String(q2.id) }
    const messages: any[] = [a1, q1, q2, a2]
    sortMessages(messages)
    const contents = messages.map(m => m.content)
    expect(contents).toEqual(['Q1', 'A1 reply', 'Q2', 'A2 reply'])
  })

  it('is idempotent — sorting an already-ordered array does not flip-flop', () => {
    const parent = { role: 'user', id: 'q1', content: 'Q', pending: true, seq: 1 }
    const messages = [
      { role: 'user', id: 1, content: 'u1' },
      { role: 'assistant', id: 2, content: 'r1' },
      parent,
      { role: 'assistant', id: 'drain', content: '', streaming: true, seq: 2, parentQueueId: String(parent.id) },
    ] as any[]
    const first = messages.map(m => `${m.role}:${m.content}`)
    sortMessages(messages)
    const second = messages.map(m => `${m.role}:${m.content}`)
    sortMessages(messages)
    const third = messages.map(m => `${m.role}:${m.content}`)
    expect(second).toEqual(first)
    expect(third).toEqual(second)
  })
})

describe('generateDrainId', () => {
  it('returns a string matching drain-* format', () => {
    const id = generateDrainId()
    expect(id).toMatch(/^drain-\d+-[a-z0-9]+$/)
  })

  it('starts with drain- prefix', () => {
    const id = generateDrainId()
    expect(id.startsWith('drain-')).toBe(true)
  })

  it('generates unique IDs on successive calls', () => {
    const ids = new Set<string>()
    for (let i = 0; i < 100; i++) {
      ids.add(generateDrainId())
    }
    expect(ids.size).toBe(100)
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

const fc = (path: string, toolIds: string[] = []) => ({ path, toolIds })

describe('extractFileChanges', () => {
  it('classifies Write as created and Edit as modified', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, file_path: 'web/src/foo.ts' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/bar.ts' },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: [fc('web/src/foo.ts')],
      modified: [fc('web/src/bar.ts')],
    })
  })

  it('deduplicates by file path but collects tool IDs', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, file_path: 'web/src/foo.ts', id: 'w1' },
      { type: 'tool_use', name: 'Write', done: true, file_path: 'web/src/foo.ts', id: 'w2' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/bar.ts', id: 'e1' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/bar.ts', id: 'e1' },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: [fc('web/src/foo.ts', ['w1', 'w2'])],
      modified: [fc('web/src/bar.ts', ['e1'])],
    })
  })

  it('only considers done blocks', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: false, file_path: 'web/src/pending.ts' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/done.ts' },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: [],
      modified: [fc('web/src/done.ts')],
    })
  })

  it('falls back to input.file_path when file_path is absent', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, input: { file_path: 'web/src/via-input.ts' } },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: [fc('web/src/via-input.ts')],
      modified: [],
    })
  })

  it('prefers top-level file_path over input.file_path', () => {
    const blocks = [
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/top.ts', input: { file_path: 'web/src/input.ts' } },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: [],
      modified: [fc('web/src/top.ts')],
    })
  })

  it('ignores non-Write/Edit tool_use blocks', () => {
    const blocks = [
      { type: 'tool_use', name: 'Read', done: true, file_path: 'web/src/read.ts' },
      { type: 'tool_use', name: 'Bash', done: true, input: { command: 'rm foo' } },
    ]
    expect(extractFileChanges(blocks)).toEqual({ created: [], modified: [] })
  })

  it('ignores non-tool_use blocks', () => {
    const blocks = [
      { type: 'text', text: 'some text' },
      { type: 'thinking', text: 'thinking...' },
    ]
    expect(extractFileChanges(blocks)).toEqual({ created: [], modified: [] })
  })

  it('returns empty arrays for empty blocks', () => {
    expect(extractFileChanges([])).toEqual({ created: [], modified: [] })
  })

  it('skips blocks without file_path', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, input: {} },
      { type: 'tool_use', name: 'Edit', done: true },
    ]
    expect(extractFileChanges(blocks)).toEqual({ created: [], modified: [] })
  })

  it('falls back to summaryCards plain-path arrays when blocks are empty', () => {
    const summaryCards = {
      createdFiles: ['web/src/new.ts'],
      modifiedFiles: ['web/src/a.ts', 'web/src/b.ts'],
    }
    expect(extractFileChanges([], summaryCards)).toEqual({
      created: [fc('web/src/new.ts')],
      modified: [fc('web/src/a.ts'), fc('web/src/b.ts')],
    })
  })

  it('captures tool IDs from summaryCards object form (summary-only view)', () => {
    const summaryCards = {
      createdFiles: [{ path: 'web/src/new.ts', toolIDs: ['w1', 'w2'] }],
      modifiedFiles: [{ path: 'web/src/a.ts', toolIDs: ['e1'] }],
    }
    expect(extractFileChanges([], summaryCards)).toEqual({
      created: [fc('web/src/new.ts', ['w1', 'w2'])],
      modified: [fc('web/src/a.ts', ['e1'])],
    })
  })

  it('merges blocks and summaryCards with dedup', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, file_path: 'web/src/new.ts', id: 'w1' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/a.ts', id: 'e1' },
    ]
    const summaryCards = {
      createdFiles: [{ path: 'web/src/new.ts', toolIDs: ['w1'] }, 'web/src/other.ts'],
      modifiedFiles: [{ path: 'web/src/a.ts', toolIDs: ['e1'] }],
    }
    expect(extractFileChanges(blocks, summaryCards)).toEqual({
      created: [fc('web/src/new.ts', ['w1']), fc('web/src/other.ts')],
      modified: [fc('web/src/a.ts', ['e1'])],
    })
  })

  it('returns empty when blocks and summaryCards are both empty', () => {
    expect(extractFileChanges([], {})).toEqual({ created: [], modified: [] })
  })
})

describe('cancelPendingMessages', () => {
  it('removes pending messages matching queueIds', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'A', pending: true },
      { role: 'user', id: 'pending-2', content: 'B', pending: true },
      { role: 'user', id: 'pending-3', content: 'C', pending: true },
    ]
    const removed = cancelPendingMessages(messages, ['pending-1', 'pending-3'])
    expect(removed).toBe(2)
    expect(messages).toHaveLength(1)
    expect(messages[0].id).toBe('pending-2')
  })

  it('does not remove non-pending messages even if IDs match', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'A', pending: true },
      { role: 'user', id: 'pending-2', content: 'B' }, // not pending
    ]
    const removed = cancelPendingMessages(messages, ['pending-1', 'pending-2'])
    expect(removed).toBe(1)
    expect(messages).toHaveLength(1)
    expect(messages[0].id).toBe('pending-2')
    expect(messages[0].pending).toBeUndefined()
  })

  it('returns 0 when no queueIds match', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'A', pending: true },
    ]
    const removed = cancelPendingMessages(messages, ['pending-999'])
    expect(removed).toBe(0)
    expect(messages).toHaveLength(1)
  })

  it('returns 0 for empty queueIds', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'A', pending: true },
    ]
    const removed = cancelPendingMessages(messages, [])
    expect(removed).toBe(0)
    expect(messages).toHaveLength(1)
  })

  it('handles numeric IDs by converting to string for matching', () => {
    const messages: any[] = [
      { role: 'user', id: 42, content: 'A', pending: true },
    ]
    const removed = cancelPendingMessages(messages, ['42'])
    expect(removed).toBe(1)
    expect(messages).toHaveLength(0)
  })

  it('removes any pending message regardless of role', () => {
    const messages: any[] = [
      { role: 'user', id: 'pending-1', content: 'A', pending: true },
      { role: 'assistant', id: 'pending-2', content: 'reply', pending: true },
    ]
    const removed = cancelPendingMessages(messages, ['pending-1', 'pending-2'])
    expect(removed).toBe(2)
    expect(messages).toHaveLength(0)
  })
})

describe('anchorRepliesToQuestions', () => {
  it('anchors replies to their own queued question after loadHistory', () => {
    const msgs = [
      { role: 'user', id: 1, content: 'msg1' },
      { role: 'assistant', id: 2, content: 'reply1' },
      { role: 'user', id: 3, content: 'msg2', queueId: 'q2' },
      { role: 'user', id: 4, content: 'msg3', queueId: 'q3' },
      { role: 'assistant', id: 5, content: 'reply2', queueId: 'q2' },
      { role: 'assistant', id: 6, content: 'reply3', queueId: 'q3' },
    ]
    const result = anchorRepliesToQuestions(msgs as any)
    // reply2 anchored to msg2 (queueId q2), reply3 to msg3 (queueId q3)
    const reply2 = result.find((m: any) => m.id === 5)
    const reply3 = result.find((m: any) => m.id === 6)
    expect(reply2.parentQueueId).toBe('q2')
    expect(reply3.parentQueueId).toBe('q3')
  })

  it('restores conversational order after anchor + sort', () => {
    const msgs = [
      { role: 'user', id: 1, content: 'msg1' },
      { role: 'assistant', id: 2, content: 'reply1' },
      { role: 'user', id: 3, content: 'msg2', queueId: 'q2' },
      { role: 'user', id: 4, content: 'msg3', queueId: 'q3' },
      { role: 'assistant', id: 5, content: 'reply2', queueId: 'q2' },
      { role: 'assistant', id: 6, content: 'reply3', queueId: 'q3' },
    ]
    anchorRepliesToQuestions(msgs as any)
    sortMessages(msgs as any)
    expect((msgs as any).map((m: any) => m.id)).toEqual([1, 2, 3, 5, 4, 6])
  })
})

describe('queued streaming order (integration)', () => {
  const callbacks = { onRenderNeeded: vi.fn(), onExtractScheduledTasks: vi.fn() }
  beforeEach(() => { vi.clearAllMocks() })

  const ids = (msgs: any[]) => msgs.map(m => String(m.id).slice(0, 12) + ':' + m.role + ':' + (m.pending ? 'P' : '') + (m.streaming ? 'S' : ''))

  it('keeps 1, reply1, 2, reply2, 3, reply3 order while draining (all transient)', () => {
    const messages: any[] = []
    // 发 1: 乐观气泡 (非 pending, string id)
    messages.push({ role: 'user', id: 'pending-1', content: '1', blocks: [], seq: nextClientSeq(), createdAt: '' })
    // connectStream 创建回复1, 锚定到消息1
    const parentIdx1 = messages.findLastIndex((m: any) => m.role === 'user')
    messages.push({ role: 'assistant', id: 'stream-1', content: '', blocks: [], streaming: true, seq: nextClientSeq(), parentQueueId: String(messages[parentIdx1].id), createdAt: '' })
    // 发 2、3 排队
    messages.push({ role: 'user', id: 'pending-2', content: '2', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' })
    messages.push({ role: 'user', id: 'pending-3', content: '3', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' })
    sortMessages(messages)
    // 初始顺序: 1, 回复1, 2, 3
    expect(messages.map(m => m.role + ':' + m.id).slice(0, 4)).toEqual(['user:pending-1', 'assistant:stream-1', 'user:pending-2', 'user:pending-3'])

    // drain 2 → 回复2
    drainQueueMessage(messages, 'pending-2', '2', [], 'codebuddy', callbacks, 'drain-2', 4)
    // drain 3 → 回复3
    drainQueueMessage(messages, 'pending-3', '3', [], 'codebuddy', callbacks, 'drain-3', 5)

    // 最终: 1, 回复1, 2, 回复2, 3, 回复3 (按 role + 内容)
    const order = messages.map(m => m.role + ':' + String(m.content || '').slice(0, 8))
    expect(order[0]).toBe('user:1')
    expect(order[1]).toContain('assistant:')
    expect(order[2]).toBe('user:2')
    expect(order[3]).toContain('assistant:')
    expect(order[4]).toBe('user:3')
    expect(order[5]).toContain('assistant:')
  })

  it('keeps replies anchored when drained messages adopt DB ids (parentIsDB)', () => {
    const messages: any[] = []
    // 消息1 已是 DB id
    messages.push({ role: 'user', id: 1, content: '1', blocks: [], createdAt: '' })
    messages.push({ role: 'assistant', id: 2, content: 'reply1', blocks: [], createdAt: '' })
    messages.push({ role: 'user', id: 'pending-2', content: '2', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' })
    messages.push({ role: 'user', id: 'pending-3', content: '3', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' })
    sortMessages(messages)

    drainQueueMessage(messages, 'pending-2', '2', [], 'codebuddy', callbacks, 'drain-2', 4)
    drainQueueMessage(messages, 'pending-3', '3', [], 'codebuddy', callbacks, 'drain-3', 5)

    const order = messages.map(m => m.role + ':' + String(m.content || '').slice(0, 8))
    expect(order[0]).toBe('user:1')
    expect(order[1]).toBe('assistant:reply1')
    expect(order[2]).toBe('user:2')
    expect(order[3]).toContain('assistant:')
    expect(order[4]).toBe('user:3')
    expect(order[5]).toContain('assistant:')
  })
})

describe('connectStream parent anchoring', () => {
  it('anchors the streaming reply to the last NON-pending user message, not a queued one', () => {
    // 用户快速连发 1、2、3。消息1 已发送（非 pending），2/3 排队（pending）。
    // connectStream 创建回复1 时应锚定到消息1，而不是最后一个 user（消息3）。
    const messages: any[] = [
      { role: 'user', id: 'local-1', content: '1', blocks: [], seq: nextClientSeq(), createdAt: '' },
      { role: 'user', id: 'pending-2', content: '2', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' },
      { role: 'user', id: 'pending-3', content: '3', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' },
    ]
    // 模拟 connectStream 的 parent 选择：应锚到最后一个非 pending user（消息1）
    const parentUserIdx = messages.findLastIndex((m: any) => m.role === 'user' && !m.pending)
    const reply1 = { role: 'assistant', id: 'stream-1', content: '', blocks: [], streaming: true, seq: nextClientSeq(), parentQueueId: parentUserIdx !== -1 ? String(messages[parentUserIdx].id) : undefined, createdAt: '' }
    messages.push(reply1)
    sortMessages(messages)

    // 回复1 锚定到消息1（TB+1.5），在消息2/3 之前
    expect(messages[0].id).toBe('local-1')
    expect(messages[1].id).toBe('stream-1')
    expect(messages[2].id).toBe('pending-2')
    expect(messages[3].id).toBe('pending-3')
  })
})

describe('parentQueueId dynamic anchoring', () => {
  const callbacks = { onRenderNeeded: vi.fn(), onExtractScheduledTasks: vi.fn() }
  beforeEach(() => { vi.clearAllMocks() })

  it('queued replies follow their parent when it adopts a DB id (no loadHistory)', () => {
    // 消息2、3 排队（父消息1 已是 DB id）。drain 时 parentIsDB=true → 消息2/3
    // 立即采纳 DB id 4/5，回复锚定 parentQueueId。随后排序无需 loadHistory。
    const messages: any[] = [
      { role: 'user', id: 1, content: '1', blocks: [], createdAt: '' },
      { role: 'assistant', id: 2, content: 'reply1', blocks: [], createdAt: '' },
      { role: 'user', id: 'pending-2', content: '2', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' },
      { role: 'user', id: 'pending-3', content: '3', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' },
    ]
    sortMessages(messages)

    drainQueueMessage(messages, 'pending-2', '2', [], 'codebuddy', callbacks, 'drain-reply2', 4)
    drainQueueMessage(messages, 'pending-3', '3', [], 'codebuddy', callbacks, 'drain-reply3', 5)

    // 消息2/3 已采纳为 4/5（queueId 保留为 pending-2/pending-3），回复跟随。
    const reply2 = messages.find((m: any) => m.role === 'assistant' && m.parentQueueId === 'pending-2')!
    const reply3 = messages.find((m: any) => m.role === 'assistant' && m.parentQueueId === 'pending-3')!
    expect(messages.map((m: any) => m.id)).toEqual([1, 2, 4, reply2.id, 5, reply3.id])
  })

  it('keeps conversational order for two queued messages end to end (no loadHistory)', () => {
    const messages: any[] = []
    // 消息1 已落库 (id=1) + 回复1 (id=2)
    messages.push({ role: 'user', id: 1, content: '1', blocks: [], createdAt: '' })
    messages.push({ role: 'assistant', id: 2, content: 'reply1', blocks: [], createdAt: '' })
    // 消息2、3 排队
    messages.push({ role: 'user', id: 'pending-2', content: '2', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' })
    messages.push({ role: 'user', id: 'pending-3', content: '3', blocks: [], pending: true, seq: nextClientSeq(), createdAt: '' })
    sortMessages(messages)

    // drain 2 → 回复2（锚到 pending-2）
    drainQueueMessage(messages, 'pending-2', '2', [], 'codebuddy', callbacks, 'drain-reply2', 4)
    // drain 3 → 回复3（锚到 pending-3）
    drainQueueMessage(messages, 'pending-3', '3', [], 'codebuddy', callbacks, 'drain-reply3', 5)

    // parentIsDB=true → 消息2/3 在 drain 时已采纳 DB id 4/5。
    // 回复必须锚定到各自问题之后，无需 loadHistory。
    const reply2 = messages.find((m: any) => m.role === 'assistant' && m.parentQueueId === 'pending-2')!
    const reply3 = messages.find((m: any) => m.role === 'assistant' && m.parentQueueId === 'pending-3')!
    expect(messages.map((m: any) => m.id)).toEqual([1, 2, 4, reply2.id, 5, reply3.id])
  })
})

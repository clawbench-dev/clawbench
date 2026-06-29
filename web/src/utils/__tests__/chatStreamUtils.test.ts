import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  FILE_MODIFYING_TOOLS,
  findLastBlockOfType,
  forceCleanupStreamingState,
  findStreamingMsg,
  drainQueueMessage,
  generateDrainId,
  shouldRetryToolFetch,
  resolveEffectiveMsgId,
  extractFileChanges,
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
    const result = drainQueueMessage(messages, 'B msg', [], 'codebuddy', callbacks)
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
    const result = drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
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
    const result = drainQueueMessage(messages, 'existing user msg', [], 'codebuddy', callbacks, drainId)
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
    drainQueueMessage(messages, 'same text', [], 'codebuddy', callbacks)
    const userMsgs = messages.filter(m => m.role === 'user')
    // Both user messages kept — they have different drain IDs
    expect(userMsgs).toHaveLength(2)
  })

  it('finalizes streaming message and preserves it (never deletes, avoids key shifts)', () => {
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
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
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].blocks[0].output).toBe('') // garbage cleared
    expect(messages[0].blocks[1].output).toBe('real output') // meaningful output kept
  })

  it('calls onExtractScheduledTasks when streaming message is found', () => {
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [
      { role: 'assistant', content: 'has content', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks })
    expect(onExtractScheduledTasks).toHaveBeenCalledWith(messages)
  })

  it('does not call onExtractScheduledTasks when no stale streaming message exists', () => {
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = []
    drainQueueMessage(messages, 'hello', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks })
    expect(onExtractScheduledTasks).not.toHaveBeenCalled()
  })

  it('does not call onRenderNeeded from drainQueueMessage', () => {
    // drainQueueMessage does not call onRenderNeeded itself —
    // the caller (useChatStream queue_drain handler) triggers renders.
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [
      { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'stale' }], streaming: true },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', { onRenderNeeded, onExtractScheduledTasks })
    expect(onRenderNeeded).not.toHaveBeenCalled()
    // But onExtractScheduledTasks should be called when a stale streaming msg was found
    expect(onExtractScheduledTasks).toHaveBeenCalled()
  })

  it('full queue drain scenario: atomically finalizes A and starts B', () => {
    // Simulate the full flow in a single atomic operation:
    // A is streaming → queue_drain arrives → finalize A, push new streaming for B
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    const callbacks = { onRenderNeeded, onExtractScheduledTasks }

    // Initial state — A streaming
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A msg', blocks: [{ type: 'text', text: 'A msg' }] },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
    ]

    // queue_drain event with B's user content
    const result = drainQueueMessage(messages, 'B msg', [], 'codebuddy', callbacks)

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
    ]

    drainQueueMessage(messages, 'B msg', [], 'codebuddy', { onRenderNeeded, onExtractScheduledTasks })

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

    drainQueueMessage(messages, 'B msg', [], 'codebuddy', { onRenderNeeded: vi.fn(), onExtractScheduledTasks: vi.fn() })

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
    const result = drainQueueMessage(messages, 'hello', [], 'claude', callbacks)
    const after = new Date().toISOString()
    expect(result!.backend).toBe('claude')
    expect(result!.createdAt >= before).toBe(true)
    expect(result!.createdAt <= after).toBe(true)
  })

  it('assigns drain ID to the pushed user message', () => {
    const messages: any[] = []
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks, 'drain-test-123')
    expect(messages[0].id).toBe('drain-test-123')
    expect(messages[0]._drain).toBe(true)
  })

  it('auto-generates drain ID when not provided', () => {
    const messages: any[] = []
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages[0].id).toMatch(/^drain-\d+-[a-z0-9]+$/)
    expect(messages[0]._drain).toBe(true)
  })

  it('drain ID does not collide with DB numeric IDs', () => {
    const messages: any[] = [
      { role: 'user', id: 42, content: 'DB user msg', blocks: [{ type: 'text', text: 'DB user msg' }] },
    ]
    drainQueueMessage(messages, 'new msg', [], 'codebuddy', callbacks)
    const drainMsg = messages.find((m: any) => m._drain === true)
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
    drainQueueMessage(messages, 'drained msg', [], 'codebuddy', callbacks)
    const drainMsg = messages.find((m: any) => m._drain === true)
    expect(drainMsg.id.startsWith('drain-')).toBe(true)
    expect(drainMsg.id.startsWith('local-')).toBe(false)
  })

  it('_drain marker enables loadHistory self-cleaning', () => {
    // Simulate: drain pushes message with _drain=true and drain- ID.
    // Then loadHistory replaces messages with DB data (numeric IDs).
    const messages: any[] = []
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks)
    expect(messages[0]._drain).toBe(true)
    expect(messages[0].id.startsWith('drain-')).toBe(true)

    // Simulate loadHistory: replace with DB messages (numeric IDs, no _drain)
    const dbMessages = [
      { role: 'user', id: 1, content: 'hello', blocks: [{ type: 'text', text: 'hello' }] },
      { role: 'assistant', id: 2, content: 'response', blocks: [{ type: 'text', text: 'response' }] },
    ]
    messages.length = 0
    messages.push(...dbMessages)

    // _drain marker and drain- ID are gone — self-cleaning
    expect(messages.every(m => !m._drain)).toBe(true)
    expect(messages.every(m => typeof m.id === 'number')).toBe(true)
  })

  it('loadHistory race: alreadyExists returns false for DB message with different ID', () => {
    // Scenario: loadHistory fetched the user message from DB before queue_drain.
    // The DB message has numeric id=42. The drain message gets drain- ID.
    // They are DIFFERENT messages (different IDs), so alreadyExists=false.
    const drainId = 'drain-1700000000000-abc123'
    const messages: any[] = [
      { role: 'user', id: 42, content: 'hello', blocks: [{ type: 'text', text: 'hello' }] },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks, drainId)
    // Both messages exist — the DB one and the drain one
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(2)
    expect(userMsgs[0].id).toBe(42)           // DB
    expect(userMsgs[1].id).toBe(drainId)      // drain
    expect(userMsgs[1]._drain).toBe(true)
  })

  it('skips push when same drainId already exists (idempotent)', () => {
    const drainId = 'drain-1700000000000-xyz789'
    const messages: any[] = [
      { role: 'user', id: drainId, _drain: true, content: 'hello', blocks: [{ type: 'text', text: 'hello' }] },
    ]
    drainQueueMessage(messages, 'hello', [], 'codebuddy', callbacks, drainId)
    const userMsgs = messages.filter(m => m.role === 'user')
    expect(userMsgs).toHaveLength(1)
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

describe('extractFileChanges', () => {
  it('classifies Write as created and Edit as modified', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, file_path: 'web/src/foo.ts' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/bar.ts' },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: ['web/src/foo.ts'],
      modified: ['web/src/bar.ts'],
    })
  })

  it('deduplicates by file path', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, file_path: 'web/src/foo.ts' },
      { type: 'tool_use', name: 'Write', done: true, file_path: 'web/src/foo.ts' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/bar.ts' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/bar.ts' },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: ['web/src/foo.ts'],
      modified: ['web/src/bar.ts'],
    })
  })

  it('only considers done blocks', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: false, file_path: 'web/src/pending.ts' },
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/done.ts' },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: [],
      modified: ['web/src/done.ts'],
    })
  })

  it('falls back to input.file_path when file_path is absent', () => {
    const blocks = [
      { type: 'tool_use', name: 'Write', done: true, input: { file_path: 'web/src/via-input.ts' } },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: ['web/src/via-input.ts'],
      modified: [],
    })
  })

  it('prefers top-level file_path over input.file_path', () => {
    const blocks = [
      { type: 'tool_use', name: 'Edit', done: true, file_path: 'web/src/top.ts', input: { file_path: 'web/src/input.ts' } },
    ]
    expect(extractFileChanges(blocks)).toEqual({
      created: [],
      modified: ['web/src/top.ts'],
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
})

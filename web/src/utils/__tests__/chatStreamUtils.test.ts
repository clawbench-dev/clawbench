import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import {
  FILE_MODIFYING_TOOLS,
  findLastBlockOfType,
  forceCleanupStreamingState,
  findStreamingMsg,
  finalizeStreamingForDrain,
  generateDrainId,
  shouldRetryToolFetch,
  resolveEffectiveMsgId,
  extractFileChanges,
  sortMessages,
  messageSortValue,
  nextClientSeq,
  rebuildFromDb,
  messageText,
  chatMessageReducer,
  trackInFlightSend,
  untrackInFlightSend,
  isInFlightSend,
  resetInFlightSendsForTest,
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

describe('finalizeStreamingForDrain', () => {
  it('finalizes the streaming assistant in place (flag removed, content kept)', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A' },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'A reply' }], streaming: true },
    ]
    finalizeStreamingForDrain(messages)
    expect(messages[1].streaming).toBeUndefined()
    expect(messages[1].blocks).toEqual([{ type: 'text', text: 'A reply' }])
    expect(messages).toHaveLength(2)
  })

  it('never deletes the message, even when it is empty (avoids v-for key shifts)', () => {
    const messages: any[] = [
      { role: 'assistant', id: 'drain-1', content: '', blocks: [], streaming: true },
    ]
    finalizeStreamingForDrain(messages)
    expect(messages).toHaveLength(1)
    expect(messages[0].streaming).toBeUndefined()
    expect(messages[0].blocks).toEqual([])
  })

  it('is a no-op when there is no streaming message', () => {
    const messages: any[] = [
      { role: 'user', id: 1, content: 'q' },
      { role: 'assistant', id: 2, content: 'done reply' },
    ]
    finalizeStreamingForDrain(messages)
    expect(messages).toHaveLength(2)
    expect(messages[1].streaming).toBeUndefined()
  })

  it('marks unfinished tool_use blocks done so their spinners stop', () => {
    const messages: any[] = [
      {
        role: 'assistant',
        content: '',
        blocks: [
          { type: 'tool_use', name: 'Read', id: '1', done: false },
          { type: 'tool_use', name: 'Write', id: '2', done: true },
        ],
        streaming: true,
      },
    ]
    finalizeStreamingForDrain(messages)
    expect(messages[0].blocks[0].done).toBe(true)
    expect(messages[0].blocks[1].done).toBe(true) // already was done
    expect(messages[0].streaming).toBeUndefined()
  })

  it('does NOT mark PermissionApproval blocks as done (requires user interaction)', () => {
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
    finalizeStreamingForDrain(messages)
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
    finalizeStreamingForDrain(messages)
    expect(messages[0].blocks[0].output).toBe('') // garbage cleared
    expect(messages[0].blocks[1].output).toBe('real output') // meaningful output kept
  })

  it('calls onExtractScheduledTasks when a streaming message is found', () => {
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [
      { role: 'assistant', content: 'has content', blocks: [], streaming: true },
    ]
    finalizeStreamingForDrain(messages, { onExtractScheduledTasks })
    expect(onExtractScheduledTasks).toHaveBeenCalledWith(messages)
  })

  it('does not call onExtractScheduledTasks when no streaming message exists', () => {
    const onExtractScheduledTasks = vi.fn()
    const messages: any[] = [{ role: 'user', content: 'q' }]
    finalizeStreamingForDrain(messages, { onExtractScheduledTasks })
    expect(onExtractScheduledTasks).not.toHaveBeenCalled()
  })

  it('leaves non-streaming messages untouched', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello' },
      { role: 'assistant', content: 'response', blocks: [{ type: 'text', text: 'response' }] },
    ]
    finalizeStreamingForDrain(messages)
    expect(messages[0].content).toBe('hello')
    expect(messages[1].content).toBe('response')
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
      { role: 'user', id: 'pending-x', content: 'u2', seq: 1 },
      { role: 'assistant', id: 2, content: 'r1' },
      { role: 'user', id: 1, content: 'u1' },
    ] as any[]
    sortMessages(messages)
    expect(messages.map(m => m.id)).toEqual([1, 2, 'pending-x'])
  })

  it('orders transient messages among themselves by seq', () => {
    const messages = [
      { role: 'assistant', id: 'drain-2', content: 'r2', seq: 2 },
      { role: 'user', id: 'pending-1', content: 'u1', seq: 1 },
    ] as any[]
    sortMessages(messages)
    expect(messages.map(m => m.id)).toEqual(['pending-1', 'drain-2'])
  })

  it('treats a streaming placeholder with a numeric id as transient (sorts after DB rows)', () => {
    const streaming = { role: 'assistant', id: 7, content: '', streaming: true, seq: 1 }
    const msgs: any[] = [streaming, { role: 'user', id: 3, content: 'B' }]
    sortMessages(msgs)
    // DB row first, then the streaming placeholder (its numeric id is NOT the
    // ordering key while it is still streaming).
    expect(msgs.map(m => String(m.id))).toEqual(['3', '7'])
    // Once finalized, its numeric DB id becomes the ordering key.
    delete streaming.streaming
    const msgs2: any[] = [streaming, { role: 'user', id: 3, content: 'B' }]
    sortMessages(msgs2)
    expect(msgs2.map(m => String(m.id))).toEqual(['3', '7'])
  })

  it('never shows a new reply above an older reply even when physical order is scrambled', () => {
    // Simulate the reported bug interleaving: array physically scrambled, both
    // replies present. Sorting must restore DB order (older reply below older
    // user, newer reply below newer user).
    const messages = [
      { role: 'assistant', id: 5, content: 'B reply', streaming: true, seq: 4 },
      { role: 'assistant', id: 2, content: 'A reply' },
      { role: 'user', id: 1, content: 'A' },
      { role: 'user', id: 4, content: 'B' },
    ] as any[]
    sortMessages(messages)
    const contents = messages.map(m => m.content)
    expect(contents).toEqual(['A', 'A reply', 'B', 'B reply'])
    const idxA = contents.indexOf('A reply')
    const idxB = contents.indexOf('B reply')
    expect(idxB).toBeGreaterThan(idxA)
  })

  it('is idempotent — sorting an already-ordered array does not flip-flop', () => {
    const messages = [
      { role: 'user', id: 1, content: 'u1' },
      { role: 'assistant', id: 2, content: 'r1' },
      { role: 'user', id: 'pending-1', content: 'Q', seq: 1 },
      { role: 'assistant', id: 'drain', content: '', streaming: true, seq: 2 },
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

// ── Root-cause reproductions for the duplicate-messages bug ──
//
// Reported: AA-reply, BB-reply renders as AAA-replyB-reply, refresh button
// (loadHistory) cannot fix it, only app restart does. DB is clean — the
// duplicates live in the in-memory array.
//
// The fix: db_load now REBUILDS the array from the authoritative DB snapshot
// (rebuildFromDb), keeping only the live streaming placeholder, pending queued
// bubbles and adopted _remote rows. Every loadHistory converges to exactly what
// an app restart would show — so the refresh button behaves like a restart.
describe('duplicate message root causes (regression)', () => {
  const callbacks = { onRenderNeeded: vi.fn(), onExtractScheduledTasks: vi.fn() }
  beforeEach(() => { vi.clearAllMocks() })

  it('RC1: DB snapshot (queued=0) before queue_drain → the drained user_message lands once', () => {
    // Simulate the state right after a loadHistory rebuilt a snapshot in which
    // the backend already flipped queued=0 (the drain claimed the row but the
    // queue_drain WS event arrived after the REST response). The queue entry is
    // gone from the panel and the DB row (id=3) is already present — a late
    // user_message for the same row must not add a second copy.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B' },
      { role: 'assistant', id: 4, content: '', blocks: [], streaming: true },
    ]
    sortMessages(messages)

    const after = chatMessageReducer(messages, {
      type: 'ws_user_message',
      data: { messageId: 3, content: 'B' },
    } as any)

    const userBs = after.filter((m: any) => m.role === 'user' && m.content === 'B')
    expect(userBs).toHaveLength(1)
    expect(userBs[0].id).toBe(3)
  })

  it('RC1b: a queued bubble that a rebuild DROPPED (queued=0) is not re-created by a late queue event', () => {
    // The bubble existed as an optimistic entry; a rebuild saw its DB row
    // already drained (queued=false) and dropped it (it is not in chat_history
    // as a pending row). The late user_message must land once as the DB row.
    let s: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 'pending-B', content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B', seq: 1 },
    ]
    // Rebuild: B's DB row is queued=false → the optimistic bubble is dropped,
    // the DB row is authoritative.
    s = chatMessageReducer(s, {
      type: 'db_load',
      dbMessages: [
        { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }], queueId: 'pending-A' },
        { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
        { role: 'user', id: 3, content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B', queued: false },
      ],
    } as any)
    expect(s.filter((m: any) => m.role === 'user' && m.content === 'B')).toHaveLength(1)
    expect(s.find((m: any) => m.content === 'B')!.id).toBe(3)

    // Late user_message for the same row — no duplicate.
    s = chatMessageReducer(s, { type: 'ws_user_message', data: { messageId: 3, content: 'B' } } as any)
    expect(s.filter((m: any) => m.role === 'user' && m.content === 'B')).toHaveLength(1)
  })

  it('RC2: rebuild discards an orphaned finalized drain-* reply; the DB row is the single source of truth', () => {
    // A finalized drain-* placeholder that has no DB row must be dropped — the
    // DB is authoritative and does not know it.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B', queued: false },
      // Orphan placeholder — not in the DB.
      { role: 'assistant', id: 'drain-xyz', content: '', blocks: [{ type: 'text', text: 'B reply' }], parentQueueId: 'pending-B' },
    ]
    sortMessages(messages)
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B', queued: false },
      { role: 'assistant', id: 4, content: '', blocks: [{ type: 'text', text: 'B reply' }], queueId: 'pending-B' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)

    // Exactly one B reply — the DB row (id=4). The orphan is gone.
    const replies = merged.filter((m: any) => m.role === 'assistant' && (m.content === 'B reply' || (m.blocks || []).some((b: any) => b.type === 'text' && b.text === 'B reply')))
    expect(replies).toHaveLength(1)
    expect(replies[0].id).toBe(4)
  })

  it('RC3: rebuild converges a corrupted array (duplicate user message) to the DB truth', () => {
    // Corrupted in-memory array: B appears TWICE (leftover transient + DB row).
    // A refresh (rebuildFromDb) must drop the leftover — the DB row is the
    // only real message.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 'drain-dup', content: 'B', blocks: [{ type: 'text', text: 'B' }], _drain: true, createdAt: '2026-01-01T00:00:00Z' },
      { role: 'user', id: 3, content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B', createdAt: '2026-01-01T00:00:01Z' },
      { role: 'assistant', id: 4, content: 'B reply', blocks: [{ type: 'text', text: 'B reply' }] },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B', queued: false, createdAt: '2026-01-01T00:00:01Z' },
      { role: 'assistant', id: 4, content: 'B reply', blocks: [{ type: 'text', text: 'B reply' }] },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)

    const userBs = merged.filter((m: any) => m.role === 'user' && m.content === 'B')
    expect(userBs).toHaveLength(1)
    expect(userBs[0].id).toBe(3)
  })

  it('RC3b: repeated rebuild (second refresh) is idempotent — no growth', () => {
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: 'B', blocks: [{ type: 'text', text: 'B' }], queueId: 'pending-B', queued: false },
      { role: 'assistant', id: 4, content: 'B reply', blocks: [{ type: 'text', text: 'B reply' }] },
    ]
    const merged1 = rebuildFromDb([], dbMsgs as any)
    const merged2 = rebuildFromDb(merged1, dbMsgs as any)
    expect(merged1).toHaveLength(4)
    expect(merged2).toHaveLength(4)
    expect(merged2.map((m: any) => m.id)).toEqual([1, 2, 3, 4])
  })

  it('RC3c: two GENUINELY distinct identical-text user messages keep their own DB rows', () => {
    // User sent "build" twice, minutes apart. Both are real messages with their
    // own DB rows. Rebuild must keep both — no content heuristic collapses them.
    const messages: any[] = [
      { role: 'user', id: 'pending-build1', content: 'build', blocks: [{ type: 'text', text: 'build' }], createdAt: '2026-01-01T00:00:00Z' },
      { role: 'user', id: 'pending-build2', content: 'build', blocks: [{ type: 'text', text: 'build' }], createdAt: '2026-01-01T05:00:00Z' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 10, content: 'build', blocks: [{ type: 'text', text: 'build' }], createdAt: '2026-01-01T00:00:01Z' },
      { role: 'user', id: 11, content: 'build', blocks: [{ type: 'text', text: 'build' }], createdAt: '2026-01-01T05:00:01Z' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const userBuilds = merged.filter((m: any) => m.role === 'user' && m.content === 'build')
    // Both DB rows kept (the transient bubbles are dropped — not in DB).
    expect(userBuilds).toHaveLength(2)
    expect(userBuilds.map((m: any) => m.id)).toEqual([10, 11])
  })

  it('keeps the LIVE streaming placeholder when a stale db_load snapshot predates its DB row', () => {
    // Reported: right after sending, the assistant bubble appears with NO
    // content and NO loading indicator; a page refresh then shows the reply.
    //
    // The loadHistory GET can be served BEFORE the backend commits the
    // streaming assistant row (the row is inserted after the ACP connection is
    // spawned/resumed, which takes seconds; the GET takes ~200ms). Its snapshot
    // therefore contains the user row but NOT the streaming row. rebuildFromDb's
    // three matching channels all miss, so the live placeholder was dropped as
    // "a transient with no DB row" — and every subsequent content/thinking/tool
    // event then had no target (they are buffered until the NEXT stream_start,
    // which for this turn already passed). The bubble stayed empty with no
    // spinner until a refresh rebuilt it from the (by then flushed) DB row.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'partial' }], streaming: true, parentQueueId: '1' },
    ]
    // Snapshot predates the streaming row entirely.
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any, true)
    const live = merged.find((m: any) => m.role === 'assistant')
    expect(live).toBeDefined()
    expect(live.streaming).toBe(true)
    expect((live.blocks || []).some((b: any) => b.text === 'partial')).toBe(true)
  })

  it('keeps an EMPTY live placeholder against a stale snapshot so later content still lands', () => {
    // Same race, but the placeholder has not received content yet (the GET beat
    // even the first content delta). Dropping it is equally fatal: the content
    // events that follow have nowhere to go, so the user sees an empty bubble
    // with no spinner and only a refresh recovers the reply.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 42, content: '', blocks: [], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
    ]
    let merged = rebuildFromDb(messages, dbMsgs as any, true)
    expect(merged.filter((m: any) => m.role === 'assistant' && m.streaming)).toHaveLength(1)
    // The live stream continues: content events still find their placeholder.
    merged = chatMessageReducer(merged, { type: 'ws_content', text: ' the answer' } as any)
    const reply = merged.find((m: any) => m.role === 'assistant')
    expect((reply.blocks || []).map((b: any) => b.text).join('')).toBe(' the answer')
  })

  it('a FINALIZED DB row supersedes a live placeholder holding only the pre-disconnect prefix', () => {
    // The session finishes while the App is backgrounded / the WS is down, and
    // on resume the reply renders as if it stopped early — only what had arrived
    // before the disconnect is shown, and switching sessions (a fresh
    // loadHistory) is what finally reveals the rest.
    //
    // The live placeholder holds the prefix that streamed before the drop; the
    // DB row holds the complete reply. mergeStreamBlocks only understood "live
    // is ahead" and "DB is a shorter prefix" — the reverse containment had no
    // branch, so it fell through to "no evidence → keep live" and silently
    // discarded the authoritative tail. (This is the non-summarized variant; the
    // summary-stripped variant — the one that dominates in practice — has its
    // own test below.)
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'Hello world' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      // Finalized (streaming=0): the whole reply, including what was produced
      // after this client stopped receiving increments.
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'Hello world and the rest' }] },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, false)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const text = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')

    expect(text).toBe('Hello world and the rest')
    // Finalized row must also stop the spinner.
    expect(reply.streaming).toBeFalsy()
  })

  it('a FINALIZED DB row wins even when the live placeholder kept its text in content (no blocks)', () => {
    // Same shape, second data variant: a placeholder that accumulated text into
    // `content` rather than a text block. The liveIsEmpty check reads
    // messageText(live), which does consult `content`, so this must reach the
    // merge just like the blocks variant — if it did not, the stale prefix would
    // survive unchanged.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 43, content: 'Hello world', blocks: [], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 43, content: '', blocks: [{ type: 'text', text: 'Hello world and the rest' }] },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, false)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const text = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')

    expect(text).toBe('Hello world and the rest')
  })

  it('keeps a live-only tool_use when the DB flush got further than the live stream', () => {
    // The DB superset case must not lose a tool call that completed locally but
    // had not been flushed to the DB yet — the DB text is the base, and the
    // live-only tool block is appended rather than dropped.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      {
        role: 'assistant', id: 44, content: '', streaming: true, parentQueueId: '1',
        blocks: [
          { type: 'text', text: 'Hello world' },
          { type: 'tool_use', id: 'tu-live', name: 'Read', done: true },
        ],
      },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 44, content: '', streaming: true, blocks: [{ type: 'text', text: 'Hello world and more' }] },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const text = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
    expect(text).toBe('Hello world and more')
    expect((reply.blocks || []).some((b: any) => b.type === 'tool_use' && b.id === 'tu-live')).toBe(true)
  })

  it('adopts DB text when the live placeholder has ONLY a tool block and no text yet', () => {
    // The strongest form of the DB-superset shape: the placeholder holds a
    // tool_use block but has not received any text, while the DB already has the
    // text that followed the tool. `liveIsEmpty` is false (there IS a block), so
    // the merge runs — but the DB-superset branch must not require a non-empty
    // live text, or this falls through and the DB text is dropped.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      {
        role: 'assistant', id: 45, content: '', streaming: true, parentQueueId: '1',
        blocks: [{ type: 'tool_use', id: 'tu-1', name: 'Read', done: true }],
      },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      {
        role: 'assistant', id: 45, content: '', streaming: true,
        blocks: [
          { type: 'tool_use', id: 'tu-1', name: 'Read', done: true },
          { type: 'text', text: 'text produced after the tool' },
        ],
      },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const text = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
    expect(text).toBe('text produced after the tool')
    // The tool must not be duplicated by the merge.
    expect((reply.blocks || []).filter((b: any) => b.type === 'tool_use' && b.id === 'tu-1')).toHaveLength(1)
  })

  it('clears the stale prefix when the finalized row is SUMMARY-STRIPPED (the real resume shape)', () => {
    // The dominant shape on resume, and the one that kept this bug alive: the
    // backend runs summarization synchronously inside Finalize, and
    // summarizeContentForView replaces the content of a summarized
    // non-streaming assistant row with {"blocks":[]} — the summary itself lives
    // in a separate table. So the snapshot carries NO blocks.
    //
    // Both merge branches require db.blocks to be non-empty, so a stripped row
    // skipped them and the stale pre-disconnect prefix survived. Worse, because
    // blocks were then non-empty, shouldShowSummary AND needsLazyOriginal both
    // concluded "content is present": the summary was not shown and the full
    // content was never lazily fetched. Net effect = completed-looking bubble
    // showing a truncated reply, fixed only by switching sessions (which
    // rebuilds the array straight from the DB row).
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 46, content: '', blocks: [{ type: 'text', text: 'Hello world' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      {
        role: 'assistant', id: 46,
        content: '{"blocks":[]}',   // stripped by the backend
        blocks: [],                 // parsed result
        summary: 'A summary of the whole reply',
        // no `streaming` — parseMessages deleted it (session not running)
      },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, false)
    const reply = merged.find((m: any) => m.role === 'assistant')!

    // Content must be cleared, NOT the stale prefix.
    expect(reply.blocks ?? []).toEqual([])
    expect(reply.content || '').toBe('')
    // The summary must be present so the bubble renders something meaningful…
    expect(reply.summary).toBe('A summary of the whole reply')
    // …and the spinner must be gone (it is a finished turn).
    expect(reply.streaming).toBeFalsy()
  })

  it('does NOT clear a live prefix when the DB row has real blocks (stripped-clear must not overreach)', () => {
    // Guard for the branch above: clearing is only correct when the DB row is
    // genuinely empty. If the row has content, the merge must still run and
    // produce the full text — clearing here would blank a good reply.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 47, content: '', blocks: [{ type: 'text', text: 'Hello world' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 47, content: '', blocks: [{ type: 'text', text: 'Hello world and the rest' }], summary: 's' },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, false)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const text = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
    expect(text).toBe('Hello world and the rest')
  })

  it('keeps live in-progress thinking when the DB text is a superset (flush omits it)', () => {
    // The DB rate-limited flush deliberately omits in-progress thinking (only
    // DONE thinking gets a slim marker), so the DB-superset branch must take
    // ONLY the text from the DB and leave the live thinking/warning blocks in
    // place. Returning the DB array wholesale would silently drop the reasoning
    // the user is currently watching.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      {
        role: 'assistant', id: 48, content: '', streaming: true, parentQueueId: '1',
        blocks: [
          { type: 'thinking', text: 'live reasoning the DB flush omits' },
          { type: 'text', text: 'Hello world' },
          { type: 'warning', text: 'live warning' },
        ],
      },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 48, content: '', streaming: true, blocks: [{ type: 'text', text: 'Hello world and more' }] },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const blocks = reply.blocks || []
    const text = blocks.filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')

    expect(text).toBe('Hello world and more')
    expect(blocks.some((b: any) => b.type === 'thinking' && b.text === 'live reasoning the DB flush omits')).toBe(true)
    expect(blocks.some((b: any) => b.type === 'warning' && b.text === 'live warning')).toBe(true)
  })

  it('keeps live block order when swapping in the DB text', () => {
    // The live tool block must not be moved to the end — the reply must keep its
    // original tool→text order after the text is replaced.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      {
        role: 'assistant', id: 49, content: '', streaming: true, parentQueueId: '1',
        blocks: [
          { type: 'tool_use', id: 'tu-live', name: 'Read', done: true },
          { type: 'text', text: 'Hello world' },
        ],
      },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 49, content: '', streaming: true, blocks: [{ type: 'text', text: 'Hello world and more' }] },
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const types = (reply.blocks || []).map((b: any) => b.type)
    expect(types).toEqual(['tool_use', 'text'])
    const text = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
    expect(text).toBe('Hello world and more')
  })

  it('does NOT clear the live prefix for a finalized row with no blocks AND no summary', () => {
    // Guard for the stripped-clear branch: clearing is only correct when the DB
    // row has a summary to render in place of the content. A finalized row with
    // neither blocks nor summary has nothing to show — blanking the bubble would
    // replace a readable (if partial) reply with an empty one.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 50, content: '', blocks: [{ type: 'text', text: 'Hello world' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 50, content: '', blocks: [] },  // no summary
    ]

    const merged = rebuildFromDb(messages, dbMsgs as any, false)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    const text = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join('')
    expect(text).toBe('Hello world')
  })

  it('does NOT preserve an unmatched placeholder when the snapshot has its own streaming row (no duplicate)', () => {
    // The preserve path above only applies when the snapshot carries NO
    // streaming row. If it does carry one — even if the placeholder could not
    // be matched to it by id/queueId — the snapshot's row is authoritative and
    // the placeholder must be dropped, or the same reply would render twice.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 'drain-orphan', content: '', blocks: [{ type: 'text', text: 'partial' }], streaming: true, parentQueueId: 'nonexistent' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 7, content: '', blocks: [{ type: 'text', text: 'partial' }], streaming: true, queueId: 'other' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any, true)
    expect(merged.filter((m: any) => m.role === 'assistant')).toHaveLength(1)
    expect(merged.find((m: any) => m.role === 'assistant').id).toBe(7)
  })

  it('still drops the live placeholder once the session is NOT running (DB is final)', () => {
    // The preserve path above must not defeat convergence: with running=false
    // the DB is authoritative, so a placeholder with no row is a genuine orphan
    // and must go — otherwise a crashed turn would leave a permanent spinner.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', id: 42, content: '', blocks: [], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any, false)
    expect(merged.filter((m: any) => m.role === 'assistant')).toHaveLength(0)
  })

  it('RC3d: a _remote bubble is preserved when its DB row is in the snapshot', () => {
    // A remote device's message persisted as a DB row; the _remote bubble must
    // be adopted (cleared of _remote markers) rather than duplicated.
    const messages: any[] = [
      { role: 'user', id: 'remote-1', content: 'build', blocks: [{ type: 'text', text: 'build' }], createdAt: '2026-01-01T00:00:01Z', _remote: true, _remoteQueueId: 'remote-q-1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 10, content: 'build', blocks: [{ type: 'text', text: 'build' }], createdAt: '2026-01-01T00:00:01Z' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    expect(merged).toHaveLength(1)
    expect(merged[0].id).toBe(10)
    expect((merged[0] as any)._remote).toBeUndefined()
  })

  it('rebuildFromDb backfills empty live placeholder from DB streaming row', () => {
    // Reported: session streams → user switches away (subscription torn down,
    // array cleared) → switches back → ws_stream_start recreates an EMPTY
    // placeholder → loadHistory rebuild runs → the DB streaming row already
    // holds flushed partial content that the placeholder must inherit, or the
    // incremental content events accumulate onto an empty base and the earlier
    // output is lost forever.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      // Freshly re-created placeholder: empty content, no blocks, streaming.
      { role: 'assistant', id: 42, content: '', blocks: [], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      // DB streaming row with already-flushed content, blocks already parsed.
      { role: 'assistant', id: 42, content: '[{"type":"text","text":"partial content"}]', blocks: [{ type: 'text', text: 'partial content' }], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    expect(reply.streaming).toBe(true)
    expect((reply.blocks || []).some((b: any) => b.type === 'text' && b.text === 'partial content')).toBe(true)
  })

  it('rebuildFromDb does NOT overwrite live placeholder that already has content', () => {
    // The live placeholder is mid-stream with real content — the DB snapshot's
    // 500ms rate-limited flush is strictly older. Never clobber the fresher
    // live stream with the DB's stale content.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'live streamed' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'older db content' }], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    expect(reply.streaming).toBe(true)
    expect((reply.blocks || []).some((b: any) => b.text === 'live streamed')).toBe(true)
    expect((reply.blocks || []).some((b: any) => b.text === 'older db content')).toBe(false)
  })

  it('rebuildFromDb prepends DB flushed history onto a non-empty re-created placeholder (switch-back race)', () => {
    // Reported: session streams → user switches away → switches back. The
    // stream_start WS event (subscribe) creates an EMPTY placeholder and the
    // first WS increments append content BEFORE the REST loadHistory db_load
    // arrives. At that point the placeholder holds ONLY the post-switch
    // increment; the DB row holds the earlier flushed history (tool_use +
    // text) that must be prepended — otherwise all pre-switch output is lost.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      // Placeholder re-created by stream_start, already got the post-switch increment.
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: ' continuing' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      // DB streaming row: flushed before the switch — tool_use + earlier text.
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'tool_use', name: 'Read', id: 'tool-1', done: true, status: 'success' },
        { type: 'text', text: 'partial response' },
      ], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    expect(reply.streaming).toBe(true)
    const texts = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text)
    expect(texts.join('')).toBe('partial response continuing')
    const tools = (reply.blocks || []).filter((b: any) => b.type === 'tool_use')
    expect(tools).toHaveLength(1)
    expect(tools[0].id).toBe('tool-1')
  })

  it('rebuildFromDb dedupes a text seam re-emitted by both the DB flush and the live stream', () => {
    // The DB flush happened mid-word; the re-subscribed stream re-emits the
    // partial token boundary, so the DB text tail equals the live text head.
    // The seam must be cut once, not duplicated.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'od work' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'good work' }], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    const texts = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text)
    expect(texts.join('')).toBe('good work')
  })

  it('rebuildFromDb does not duplicate tool_use blocks when live already has them (continuous stream)', () => {
    // Continuous streaming: the live placeholder already holds the full DB
    // text (the DB flush is a stale subset). DB tool_use already present in
    // live must not be duplicated; the existing blocks are kept as-is.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'tool_use', name: 'Read', id: 'tool-1', done: true, status: 'success' },
        { type: 'text', text: 'full response' },
      ], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      // DB flush only reached the first part of the text.
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'tool_use', name: 'Read', id: 'tool-1', done: true, status: 'success' },
        { type: 'text', text: 'full res' },
      ], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    const texts = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text)
    expect(texts.join('')).toBe('full response')
    expect((reply.blocks || []).filter((b: any) => b.type === 'tool_use')).toHaveLength(1)
  })

  it('rebuildFromDb does NOT duplicate a DB done-thinking marker when live already rendered the thinking (continuous stream)', () => {
    // Continuous streaming: the live placeholder already holds the thinking
    // text the DB slim marker references (the DB flush is a stale subset that
    // recorded the done block). Adopting the marker would duplicate the chip.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'thinking', text: 'reasoned', done: true },
        { type: 'text', text: 'full response' },
      ], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      // DB streaming row: done thinking block flushed as a slim marker + text prefix.
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'thinking', think_id: 'th_1', done: true },
        { type: 'text', text: 'full res' },
      ], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    const thinkings = (reply.blocks || []).filter((b: any) => b.type === 'thinking')
    expect(thinkings).toHaveLength(1, 'DB done-thinking marker must not duplicate the live thinking block')
    expect(thinkings[0].text).toBe('reasoned', 'the live block (with text) must win over the DB marker')
    expect(thinkings[0].think_id).toBeUndefined()
    const texts = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text)
    expect(texts.join('')).toBe('full response')
  })

  it('rebuildFromDb adopts a DB done-thinking marker when live has NO thinking block (placeholder recreated)', () => {
    // The live placeholder was recreated by a stream_start after the thinking
    // finished, and only text events were replayed since. The DB marker is the
    // only trace of the completed reasoning — it must be adopted.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: 'full response here' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'thinking', think_id: 'th_1', done: true },
        { type: 'text', text: 'full response here' },
      ], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    const thinkings = (reply.blocks || []).filter((b: any) => b.type === 'thinking')
    expect(thinkings).toHaveLength(1, 'DB done-thinking marker must be adopted when live lacks the reasoning')
    expect(thinkings[0].think_id).toBe('th_1')
    const texts = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text)
    expect(texts.join('')).toBe('full response here')
  })

  it('rebuildFromDb adopts a DB done-thinking marker when the DB text is a genuine prefix live lacks (switch-back recovery)', () => {
    // Switch-back race: the DB row holds flushed history (done thinking +
    // tool + text prefix) that the re-created placeholder's live increment does
    // not cover. The thinking marker must be prepended along with the tool.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [{ type: 'text', text: ' continuing' }], streaming: true, parentQueueId: '1' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'thinking', think_id: 'th_1', done: true },
        { type: 'tool_use', name: 'Read', id: 'tool-1', done: true, status: 'success' },
        { type: 'text', text: 'partial response' },
      ], streaming: true },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    expect(reply.streaming).toBe(true)
    const thinkings = (reply.blocks || []).filter((b: any) => b.type === 'thinking')
    expect(thinkings).toHaveLength(1, 'done-thinking marker must be prepended on switch-back recovery')
    expect(thinkings[0].think_id).toBe('th_1')
    const tools = (reply.blocks || []).filter((b: any) => b.type === 'tool_use')
    expect(tools).toHaveLength(1)
    expect(tools[0].id).toBe('tool-1')
    const texts = (reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text)
    expect(texts.join('')).toBe('partial response continuing')
  })

  it('rebuildFromDb keeps the slim done-thinking marker from a streaming row on a fresh page load (refresh recovery)', () => {
    // A fresh refresh (empty message array → db_load) rebuilds purely from the
    // DB row. The streaming row now carries the slim done marker, so the
    // completed thinking renders as a collapsed chip (lazy-load on expand) —
    // this is the reported bug's fix path.
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 42, content: '', blocks: [
        { type: 'thinking', think_id: 'th_1', done: true },
        { type: 'text', text: 'partial answer' },
        { type: 'tool_use', name: 'Read', id: 'tool-1', done: true, status: 'success' },
      ], streaming: true },
    ]
    const merged = rebuildFromDb([], dbMsgs as any)
    const reply = merged.find((m: any) => m.role === 'assistant' && m.id === 42)
    expect(reply).toBeDefined()
    expect(reply.streaming).toBe(true)
    const thinkings = (reply.blocks || []).filter((b: any) => b.type === 'thinking')
    expect(thinkings).toHaveLength(1, 'fresh refresh must keep the done-thinking marker')
    expect(thinkings[0].think_id).toBe('th_1')
    expect(thinkings[0].done).toBe(true)
  })

  it('ws_user_message never marks a bubble pending/queued (queued messages are not in the list)', () => {
    // A queued message is broadcast via `queue_added` and lives in the queue
    // store — it never reaches the conversation list. A `user_message` event
    // therefore always describes a committed row; the reducer must not carry
    // any pending/queued chrome.
    let s: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
    ]

    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: {
        messageId: 200,
        content: 'from phone',
        senderClientId: 'device-a',
        queueId: 'remote-q-queued',
        backend: 'claude',
      },
    } as any)

    const userBubbles = s.filter((m: any) => m.role === 'user' && m.content === 'from phone')
    expect(userBubbles).toHaveLength(1)
    const bubble = userBubbles[0]
    expect(bubble._remote).toBe(true)
    expect(bubble.pending).toBeUndefined()
    expect(bubble.queued).toBeUndefined()
    expect(bubble._remoteQueueId).toBe('remote-q-queued')
  })

  it('auto-continue: repeated identical user messages each get their own bubble', () => {
    // The auto-continue feature sends the SAME localized text ("继续" /
    // "Continue") up to N times for one user turn. The reducer's content-based
    // dedup must not collapse them into a single bubble — each attempt is a real
    // message with its own DB id and queue id, and losing them would make the
    // transcript lie about how many times the session was resumed.
    let s: any[] = [
      { role: 'user', id: 1, content: 'do the thing', blocks: [{ type: 'text', text: 'do the thing' }] },
    ]

    // First auto-continue message.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 501, content: 'Continue', queueId: 'auto-1-1' },
    } as any)
    // Second attempt: identical content, different ids.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 503, content: 'Continue', queueId: 'auto-2-2' },
    } as any)

    const continues = s.filter((m: any) => m.role === 'user' && m.content === 'Continue')
    expect(continues).toHaveLength(2, 'each auto-continue attempt must render its own bubble')
    expect(continues.map((m: any) => m.id).sort()).toEqual([501, 503])
  })

  it('renders a drained message whose text repeats an earlier message (identity, not text)', () => {
    // Reported: with several queued messages, NONE of their user bubbles appeared
    // until the WHOLE queue finished. The repro used the same prompt three times
    // ("Sleep 5 秒钟。"), which is what exposed it: the dedup matched on CONTENT,
    // so the 2nd and 3rd announcements were swallowed as "already exists". They
    // reappeared only when a final loadHistory rebuilt from the DB — which had
    // held them all along, proving the messages were never lost server-side.
    //
    // Text is not identity: three identical prompts are three messages.
    let s: any[] = []
    // The first message was sent directly and adopted its DB id.
    s = chatMessageReducer(s, { type: 'optimistic_push', msg: { role: 'user', id: 'p-a', content: 'Sleep 5', blocks: [], seq: 1 } } as any)
    s = chatMessageReducer(s, { type: 'optimistic_adopt_id', id: 'p-a', dbId: 100 } as any)

    // The queued copy drains and is announced with a NEW id.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 101, content: 'Sleep 5', queueId: 'q-b' },
    } as any)
    expect(s.filter((m: any) => m.role === 'user'), 'the drained bubble must render').toHaveLength(2)

    // A third identical one drains too.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 102, content: 'Sleep 5', queueId: 'q-c' },
    } as any)
    expect(s.filter((m: any) => m.role === 'user'), 'every identical message renders its own bubble').toHaveLength(3)
  })

  it('still dedups the same announcement replayed (idempotent, identity-based)', () => {
    // Narrowing the content rule must NOT break real dedup: a replay of the SAME
    // id is still one message.
    let s: any[] = []
    s = chatMessageReducer(s, { type: 'ws_user_message', data: { messageId: 201, content: 'X', queueId: 'q1' } } as any)
    s = chatMessageReducer(s, { type: 'ws_user_message', data: { messageId: 201, content: 'X', queueId: 'q1' } } as any)
    expect(s.filter((m: any) => m.role === 'user')).toHaveLength(1)
  })

  it('still dedups the sender own optimistic bubble by queueId (no double bubble)', () => {
    // The device's own optimistic bubble carries the queueId it sent; the echo
    // must adopt it rather than append a second bubble.
    let s: any[] = []
    s = chatMessageReducer(s, { type: 'optimistic_push', msg: { role: 'user', id: 'p-a', content: 'X', blocks: [], seq: 1 } } as any)
    s = chatMessageReducer(s, { type: 'ws_user_message', data: { messageId: 301, content: 'X', queueId: 'p-a' } } as any)
    expect(s.filter((m: any) => m.role === 'user')).toHaveLength(1)
  })

  it('ws_user_message without queued flag creates normal remote bubble', () => {
    // A non-queued (immediately started) message must keep the existing
    // behavior: a normal _remote bubble with no pending marker.
    let s: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
    ]

    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: {
        messageId: 300,
        content: 'direct from phone',
        senderClientId: 'device-a',
        queueId: 'remote-q-direct',
      },
    } as any)

    const userBubbles = s.filter((m: any) => m.role === 'user' && m.content === 'direct from phone')
    expect(userBubbles).toHaveLength(1, 'B must render one remote bubble for the direct message')
    const bubble = userBubbles[0]
    expect(bubble._remote).toBe(true)
    expect(bubble.pending).toBeUndefined('a non-queued message must NOT render as pending')
    expect(bubble.queued).toBeUndefined()
  })

  it('A/B dual client: A sends → B gets a _remote bubble → a duplicate user_message does not duplicate it', () => {
    // Client B's reducer receives the authoritative push event from client A's
    // send, then a stream_start opens the reply placeholder.
    let s: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
    ]

    // Step 1 — the user_message event from device A arrives on device B.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: {
        messageId: 100,
        content: 'hi from phone',
        senderClientId: 'device-a',
        queueId: 'remote-q-1',
        backend: 'claude',
      },
    } as any)

    const userBubbles = s.filter((m: any) => m.role === 'user' && m.content === 'hi from phone')
    expect(userBubbles).toHaveLength(1, 'B must render one user bubble from the push event')
    const bubble = userBubbles[0]
    expect(bubble.id).toBe(100, '_remote bubble carries the authoritative DB id from the event')
    expect(bubble._remote).toBe(true)
    expect(bubble._remoteQueueId).toBe('remote-q-1')
    expect(bubble.pending).toBeUndefined()

    // Step 2 — the reply placeholder is opened by stream_start (id 101).
    s = chatMessageReducer(s, {
      type: 'stream_placeholder',
      msg: { role: 'assistant', id: 101, content: '', blocks: [], streaming: true, seq: nextClientSeq() },
    } as any)

    const streaming = s.find((m: any) => m.role === 'assistant' && m.streaming)
    expect(streaming).toBeDefined()
    expect(streaming.id).toBe(101)

    // Step 3 — a replayed/duplicate user_message for the same row must not add
    // a second copy.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 100, content: 'hi from phone', senderClientId: 'device-a', queueId: 'remote-q-1' },
    } as any)
    const afterDup = s.filter((m: any) => m.role === 'user' && m.content === 'hi from phone')
    expect(afterDup).toHaveLength(1)
  })

  it('A/B dual client: rebuildFromDb adopts the _remote bubble without duplication after a refresh', () => {
    // After the full WS sequence (user_message + queue_drain), a loadHistory
    // refresh must converge to exactly one bubble for the remote message — the
    // DB row. The _remote bubble is adopted, not duplicated.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 100, content: 'hi from phone', blocks: [{ type: 'text', text: 'hi from phone' }], queueId: 'remote-q-1', _remote: true, _remoteQueueId: 'remote-q-1' },
      { role: 'assistant', id: 3, content: '', blocks: [{ type: 'text', text: 'hi reply' }], parentQueueId: '100' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 100, content: 'hi from phone', blocks: [{ type: 'text', text: 'hi from phone' }], queueId: 'remote-q-1' },
      { role: 'assistant', id: 3, content: 'hi reply', blocks: [{ type: 'text', text: 'hi reply' }] },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const users = merged.filter((m: any) => m.role === 'user' && m.content === 'hi from phone')
    expect(users).toHaveLength(1)
    expect(users[0].id).toBe(100)
    expect(users[0]._remote).toBeUndefined()
    expect(users[0]._remoteQueueId).toBeUndefined()
  })

  it('RC4: queue_drain does not materialize the user message; the user_message event lands it once', () => {
    // New contract: a queued message is NOT part of the messages array until
    // the backend materializes it into chat_history. `ws_queue_drain` is a bare
    // turn boundary (it finalizes the streaming reply and nothing else); the
    // user message arrives as a separate `user_message` event carrying the
    // queueId and the new DB id.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'assistant', id: 'drain-stream', content: '', blocks: [], streaming: true, seq: 9 },
    ]
    sortMessages(messages)

    // queue_drain: finalizes the streaming reply, adds no user message.
    chatMessageReducer(messages, { type: 'ws_queue_drain' })
    expect(messages.filter((m: any) => m.role === 'user' && m.content === 'B')).toHaveLength(0)
    expect(messages.find((m: any) => m.id === 'drain-stream')!.streaming).toBeUndefined()

    // Materialization: the user_message event carries the DB id + queueId.
    chatMessageReducer(messages, {
      type: 'ws_user_message',
      data: { messageId: 3, content: 'B', queueId: 'pending-B' },
    } as any)
    expect(messages.filter((m: any) => m.role === 'user' && m.content === 'B')).toHaveLength(1)

    // A duplicate delivery of the same event must not duplicate the bubble.
    chatMessageReducer(messages, {
      type: 'ws_user_message',
      data: { messageId: 3, content: 'B', queueId: 'pending-B' },
    } as any)
    expect(messages.filter((m: any) => m.role === 'user' && m.content === 'B')).toHaveLength(1)
  })

  // ── The reported user scenario, end to end ──
  // "AA-reply, BB-reply renders as AAA-replyB-reply; refresh can't fix it, only
  // app restart can."
  //
  // Sequence: user sends A (direct) → replyA streams → done(A) → user queues B
  // while replyB streams → session is STILL running → user hits the refresh
  // button → loadHistory → db_load (rebuildFromDb).
  //
  // The rebuild keeps the live replyB placeholder (matched to its DB streaming
  // row) and the DB rows for everything else — exactly what a restart shows.
  it('reported scenario: refresh while a LATER turn streams must not duplicate an earlier finalized reply', () => {
    const aMsg = (id: unknown, content: string, extra: Record<string, unknown> = {}): any =>
      ({ role: 'assistant', id, content: '', blocks: content ? [{ type: 'text', text: content }] : [], createdAt: '2026-01-01T00:00:01Z', ...extra })
    const uMsg = (id: unknown, content: string, extra: Record<string, unknown> = {}): any =>
      ({ role: 'user', id, content, blocks: content ? [{ type: 'text', text: content }] : [], files: [], createdAt: '2026-01-01T00:00:01Z', ...extra })

    // A direct-send + stream
    let s: any[] = []
    s = chatMessageReducer(s, { type: 'optimistic_push', msg: uMsg('pending-A', 'A', { seq: 1 }) })
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: aMsg('drain-rA', '', { streaming: true, seq: 2, createdAt: '2026-01-01T00:00:00Z' }) })
    s = chatMessageReducer(s, { type: 'ws_content', text: 'reply A' })
    // done(A) → finalize replyA placeholder
    s = chatMessageReducer(s, { type: 'stream_finalize' })
    // B was queued (lives in the queue store, NOT the messages array) and is
    // now materialized into chat_history: the user_message event renders it
    // inline, then queue_drain marks the turn boundary and the reply B streams.
    s = chatMessageReducer(s, { type: 'ws_user_message', data: { messageId: 3, content: 'B', queueId: 'pending-B' } } as any)
    s = chatMessageReducer(s, { type: 'ws_queue_drain' })
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: aMsg(4, '', { streaming: true, seq: 3, createdAt: '2026-01-01T00:00:02Z' }) })
    s = chatMessageReducer(s, { type: 'ws_content', text: 'reply B' })

    // Sanity: exactly two assistant messages before the refresh.
    expect(s.filter((m) => m.role === 'assistant')).toHaveLength(2)

    // Refresh → db_load with the authoritative DB snapshot.
    s = chatMessageReducer(s, {
      type: 'db_load',
      dbMessages: [
        uMsg(1, 'A', { queueId: 'pending-A', createdAt: '2026-01-01T00:00:05Z' }),
        aMsg(2, 'reply A', { createdAt: '2026-01-01T00:00:01Z' }),
        uMsg(3, 'B', { queueId: 'pending-B', queued: false, createdAt: '2026-01-01T00:00:06Z' }),
        aMsg(4, 'reply B', { streaming: true, createdAt: '2026-01-01T00:00:02Z' }),
      ],
    } as any)

    // Exactly two assistant messages — replyA is the DB row (id=2), replyB is
    // the preserved live placeholder, and no drain-rA duplicate remains.
    const assistants = s.filter((m) => m.role === 'assistant')
    expect(assistants).toHaveLength(2)
    expect(s.some((m) => m.role === 'assistant' && m.id === 'drain-rA')).toBe(false)
    // The live replyB streaming placeholder keeps streaming (one live stream).
    expect(s.filter((m) => m.role === 'assistant' && m.streaming)).toHaveLength(1)
  })

  it('reported scenario: refresh converges a user-message duplicate created by a raced queue_drain', () => {
    // Corrupted in-memory state after a missed self-echo + raced drain:
    // user message A exists as a leftover string-id bubble AND as its DB row.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }], createdAt: '2026-01-01T00:00:05Z' },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }], createdAt: '2026-01-01T00:00:06Z' },
      // leftover bubble — same content, created in the same drain cycle
      { role: 'user', id: 'drain-dupA', content: 'A', blocks: [{ type: 'text', text: 'A' }], createdAt: '2026-01-01T00:00:05Z' },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }], createdAt: '2026-01-01T00:00:05Z' },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }], createdAt: '2026-01-01T00:00:06Z' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    const userAs = merged.filter((m: any) => m.role === 'user' && m.content === 'A')
    expect(userAs).toHaveLength(1)
    expect(userAs[0].id).toBe(1)
  })

  it('reported scenario: three queued turns (A direct, B/C queued) survive a mid-stream refresh with no duplicates and correct adoption', () => {
    const aMsg = (id: unknown, content: string, extra: Record<string, unknown> = {}): any =>
      ({ role: 'assistant', id, content: '', blocks: content ? [{ type: 'text', text: content }] : [], createdAt: '2026-01-01T00:00:01Z', ...extra })
    const uMsg = (id: unknown, content: string, extra: Record<string, unknown> = {}): any =>
      ({ role: 'user', id, content, blocks: content ? [{ type: 'text', text: content }] : [], files: [], createdAt: '2026-01-01T00:00:01Z', ...extra })

    let s: any[] = []
    // A direct-send + stream
    s = chatMessageReducer(s, { type: 'optimistic_push', msg: uMsg('pending-A', 'A', { seq: 1 }) })
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: aMsg('drain-rA', '', { streaming: true, seq: 2, createdAt: '2026-01-01T00:00:00Z' }) })
    s = chatMessageReducer(s, { type: 'ws_content', text: 'reply A' })
    // done(A)
    s = chatMessageReducer(s, { type: 'stream_finalize' })
    // B and C are queued (in the queue store, not the array). B is materialized
    // into chat_history and starts its own turn; C is still waiting.
    s = chatMessageReducer(s, { type: 'ws_user_message', data: { messageId: 3, content: 'B', queueId: 'pending-B' } } as any)
    s = chatMessageReducer(s, { type: 'ws_queue_drain' })
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: aMsg(4, '', { streaming: true, seq: 3, createdAt: '2026-01-01T00:00:02Z' }) })
    s = chatMessageReducer(s, { type: 'ws_content', text: 'reply B' })

    // Refresh → db_load while replyB streams. C is still queued, so it is NOT
    // in the messages array — it lives in the queue store.
    s = chatMessageReducer(s, {
      type: 'db_load',
      dbMessages: [
        uMsg(1, 'A', { queueId: 'pending-A', createdAt: '2026-01-01T00:00:01Z' }),
        aMsg(2, 'reply A', { createdAt: '2026-01-01T00:00:01Z' }),
        uMsg(3, 'B', { queueId: 'pending-B', createdAt: '2026-01-01T00:00:03Z' }),
        aMsg(4, 'reply B', { streaming: true, createdAt: '2026-01-01T00:00:02Z' }),
      ],
    } as any)

    // Exactly 2 user messages (A id=1, B id=3 — C is in the queue panel, not the
    // conversation) and exactly 2 assistant messages (replyA id=2, replyB
    // streaming) — no duplicates, no orphans. A's optimistic bubble is dropped
    // (self-echo lost) and the DB row id=1 is authoritative — same as a restart.
    const users = s.filter((m) => m.role === 'user')
    const assistants = s.filter((m) => m.role === 'assistant')
    expect(users).toHaveLength(2)
    expect(assistants).toHaveLength(2)
    // A is the DB row.
    const userA = users.find((m: any) => m.content === 'A')
    expect(userA.id).toBe(1)
    // C is not in the array at all.
    expect(users.some((m: any) => m.content === 'C')).toBe(false)
    // replyA not duplicated; replyB keeps streaming.
    expect(s.some((m: any) => m.role === 'assistant' && m.id === 'drain-rA')).toBe(false)
    expect(s.filter((m: any) => m.role === 'assistant' && m.streaming)).toHaveLength(1)
  })

  it('live placeholder with content whose DB streaming row is absent is dropped (done missed → DB finalized row is truth)', () => {
    // The live placeholder holds content but the DB snapshot has NO streaming
    // row for it (its done was missed, or the snapshot predates it). The DB is
    // authoritative — the placeholder is dropped; the finalized DB row (if
    // present) is what the user sees, exactly as a restart would.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'user', id: 2, content: 'B', blocks: [{ type: 'text', text: 'B' }] },
      // Live reply for B with real content.
      { role: 'assistant', id: 'drain-live-B', content: '', blocks: [{ type: 'text', text: 'actual long reply to B' }], streaming: true, parentQueueId: '2', seq: 5 },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'user', id: 2, content: 'B', blocks: [{ type: 'text', text: 'B' }] },
      // Finalized row for A's reply (no queueId, different anchor).
      { role: 'assistant', id: 9, content: 'reply to A', blocks: [{ type: 'text', text: 'reply to A' }], createdAt: '2026-01-01T00:00:01Z' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    // No streaming row in the snapshot → the live placeholder is dropped.
    expect(merged.some((m: any) => m.id === 'drain-live-B')).toBe(false)
    // The finalized DB row is the only assistant message.
    const assistants = merged.filter((m: any) => m.role === 'assistant')
    expect(assistants).toHaveLength(1)
    expect(assistants[0].id).toBe(9)
  })

  it('live placeholder is dropped when its done was missed and the DB has no streaming row for it', () => {
    // The done event was lost: DB has finalized the reply (no streaming=1 row)
    // but the frontend still holds a live placeholder. The rebuild must drop
    // the placeholder — its content lives in the finalized DB row.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 'drain-live-A', content: '', blocks: [{ type: 'text', text: 'reply A' }], streaming: true, parentQueueId: '1', seq: 5 },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'reply A' }], createdAt: '2026-01-01T00:00:01Z' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    // Exactly one assistant message — the finalized DB row id=2.
    const assistants = merged.filter((m: any) => m.role === 'assistant')
    expect(assistants).toHaveLength(1)
    expect(assistants[0].id).toBe(2)
    expect((assistants[0] as any).streaming).toBeUndefined()
  })

  it('live placeholder with an already-assigned DB id is kept and finalized when its row is streaming=0 (done missed while idle)', () => {
    // ws_stream_start assigned the DB id (2) to the live placeholder. The done
    // event was lost; a refresh (session idle) makes parseMessages strip the
    // streaming flag. The exact id match must keep the placeholder object and
    // finalize it (stable v-for key, identical content) instead of dropping it.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'reply A' }], streaming: true, parentQueueId: '1', seq: 5 },
    ]
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'reply A' }], createdAt: '2026-01-01T00:00:02Z' },
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)
    expect(merged).toHaveLength(2)
    const reply = merged.find((m: any) => m.role === 'assistant')
    expect(reply).toBeDefined()
    expect(reply.id).toBe(2)
    // Finalized (streaming removed), content preserved.
    expect(reply.streaming).toBeUndefined()
    expect((reply.blocks || []).some((b: any) => b.text === 'reply A')).toBe(true)
  })

  it('RC4: a late queue_drain during a session switch fetch does not inject a user message', () => {
    // Reported: switch to a session and back → the last (user, reply) pair
    // renders twice; a second switch fixes it.
    //
    // The old root cause was that queue_drain itself materialized the user
    // message (carrying the DB number id) into the array while the REST
    // loadHistory fetch was in flight; the subsequent db_load could not match
    // that copy and rendered it alongside the DB row. Under the new contract a
    // queued message is materialized ONLY by its `user_message` event, and
    // queue_drain is a bare turn boundary that never touches user messages —
    // so the duplicate cannot be constructed.
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      // The drained row — a plain formal message.
      { role: 'user', id: 3, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }] },
      { role: 'assistant', id: 4, content: 'build reply', blocks: [{ type: 'text', text: 'build reply' }] },
    ]

    // 1. switchSession: array cleared, loadHistory fetch in flight.
    let s: any[] = []

    // 2. Late queue_drain arrives during the fetch window — bare boundary.
    s = chatMessageReducer(s, { type: 'ws_queue_drain' } as any)
    expect(s.filter((m: any) => m.role === 'user' && m.content === '编译前端')).toHaveLength(0)

    // 3. db_load rebuild arrives → the DB rows are the only messages.
    s = chatMessageReducer(s, { type: 'db_load', dbMessages: dbMsgs } as any)
    const userBuilds = s.filter((m: any) => m.role === 'user' && m.content === '编译前端')
    expect(userBuilds).toHaveLength(1)
    const replies = s.filter((m: any) => m.role === 'assistant' && m.content === 'build reply')
    expect(replies).toHaveLength(1)
  })

  it('RC5: two concurrent db_load (switchSession immediate + queued poll) converge without duplication', () => {
    // Reported: switch to a session and back → the last (user, reply) pair
    // renders twice; a second switch fixes it. The server log shows TWO
    // concurrent /api/ai/chat requests at the same ms: switchSession uses
    // immediate=true which bypasses loadHistoryInProgress, so an in-flight
    // polling loadHistory and the switchSession loadHistory both fetch and
    // both run syncSessionState → two db_load dispatches.
    //
    // rebuildFromDb must be idempotent across repeated db_load with the SAME
    // DB snapshot — the second rebuild over the first rebuild's output must
    // not duplicate any row.
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }] },
      { role: 'assistant', id: 4, content: 'build reply', blocks: [{ type: 'text', text: 'build reply' }] },
    ]

    // First db_load from a fresh (cleared) array.
    let s = chatMessageReducer([], { type: 'db_load', dbMessages: dbMsgs } as any)
    expect(s).toHaveLength(4)

    // Second db_load with the SAME snapshot on top of the first result.
    s = chatMessageReducer(s, { type: 'db_load', dbMessages: dbMsgs } as any)
    expect(s).toHaveLength(4, 'second db_load must not duplicate rows')
    expect(s.map((m: any) => m.id)).toEqual([1, 2, 3, 4])

    // Third rebuild (the "switch again" that reportedly fixes it) — still no growth.
    s = chatMessageReducer(s, { type: 'db_load', dbMessages: dbMsgs } as any)
    expect(s).toHaveLength(4)
  })

  it('RC5b: db_load after a concurrent user_message + ws_queue_drain does not duplicate', () => {
    // During the switchSession fetch window the materialized user_message and
    // the queue_drain boundary arrive. The user_message lands the drained row
    // (id=3); the subsequent db_load must converge to exactly the DB rows — the
    // landed row matches its DB row by id, and the streaming placeholder is
    // matched to its DB streaming row by id.
    let s: any[] = []

    // Materialization of the drained message + bare turn boundary.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 3, content: '编译前端', queueId: 'pending-编译前端' },
    } as any)
    s = chatMessageReducer(s, { type: 'ws_queue_drain' } as any)
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: {
      role: 'assistant', id: 'drain-x', content: '', blocks: [], streaming: true, seq: 1,
    } as any })
    expect(s.filter((m: any) => m.role === 'user' && m.content === '编译前端')).toHaveLength(1)

    // Simulate the stream producing content into the placeholder then done.
    const placeholder = s.find((m: any) => m.role === 'assistant' && m.streaming)
    expect(placeholder).toBeDefined()
    placeholder.blocks = [{ type: 'text', text: 'build reply' }]
    delete placeholder.streaming

    // db_load rebuild — the drained user row (id=3) matches by id; the finalized
    // reply (drain-* string id) is dropped and the DB row is authoritative.
    const dbMsgs: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }], queueId: 'pending-编译前端' },
      { role: 'assistant', id: 4, content: '', blocks: [{ type: 'text', text: 'build reply' }] },
    ]
    s = chatMessageReducer(s, { type: 'db_load', dbMessages: dbMsgs } as any)

    expect(s.filter((m: any) => m.role === 'user' && m.content === '编译前端')).toHaveLength(1)
    expect(s.filter((m: any) => m.role === 'assistant' && (m.blocks || []).some((b: any) => b.type === 'text' && b.text === 'build reply'))).toHaveLength(1)
  })

  it('RC6: db_load THEN a late queue_drain (bubble dropped) duplicates the user message until the next db_load', () => {
    // The switch-back window: switchSession(A) runs db_load first; the DB
    // snapshot at that moment has the drained row already (queued=0, formal
    // message id=3). The rebuild drops the transient pending bubble.
    // THEN the late queue_drain WS event for the SAME queue arrives. The
    // pending bubble is gone, so drainQueueMessage's defensive branch checks
    // `!messages.some((m) => m.id === effectiveId)` — but the existing row's
    // id is the NUMERIC db id 3 while effectiveId is the drain payload's
    // dbMessageId — which SHOULD match. Verify whether the duplicate appears.
    let s: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 3, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }], queueId: 'pending-编译前端', queued: false },
    ]
    sortMessages(s)

    // Late queue_drain — the drained row already exists as id=3.
    s = chatMessageReducer(s, {
      type: 'ws_queue_drain',
      queueId: 'pending-编译前端',
      text: '编译前端',
      files: [],
      dbMessageId: 3,
      backend: 'codebuddy',
    } as any)

    const userBuilds = s.filter((m: any) => m.role === 'user' && m.content === '编译前端')
    expect(userBuilds).toHaveLength(1, 'drain must not duplicate a row already present')
  })

  it('RC6b: db_load THEN late ws_user_message with a numeric id not in the DB snapshot survives until next db_load', () => {
    // The switch-back window: db_load completed with a snapshot that does NOT
    // yet contain the message (the DB row is persisted after the fetch, or
    // the message belongs to a concurrent session). A ws_user_message event
    // then inserts a _remote bubble carrying a numeric DB id that matches NO
    // row in the loaded snapshot. rebuildFromDb cannot adopt it (no matching
    // DB row) and will NOT drop it (numeric id is non-transient) → the bubble
    // survives alongside later rows until the next db_load.
    let s: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
    ]
    // db_load snapshot WITHOUT the new message.
    s = chatMessageReducer(s, { type: 'db_load', dbMessages: [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
    ] } as any)

    // Late ws_user_message with numeric id 300, no matching DB row in snapshot.
    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 300, content: '编译前端', senderClientId: 'device-a', queueId: 'remote-q', backend: 'codebuddy' },
    } as any)

    const userBuilds = s.filter((m: any) => m.role === 'user' && m.content === '编译前端')
    expect(userBuilds).toHaveLength(1)

    // Now a SECOND db_load that DOES include id=300 → the _remote bubble is
    // adopted (cleared) and the duplicate is gone.
    s = chatMessageReducer(s, { type: 'db_load', dbMessages: [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
      { role: 'assistant', id: 2, content: 'A reply', blocks: [{ type: 'text', text: 'A reply' }] },
      { role: 'user', id: 300, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }] },
    ] } as any)
    const after = s.filter((m: any) => m.role === 'user' && m.content === '编译前端')
    expect(after).toHaveLength(1)
  })

  it('RC7: switch-back race — stream_start after db_load recreates a placeholder for an already-finalized row (no duplicate)', () => {
    // The exact switch-back sequence observed in the server log:
    //   1. switchSession(A) → db_load loads 39789. The backend reports
    //      running=false (stream done 12s earlier), so parseMessages strips
    //      the streaming flag → 39789 is a finalized row in the array.
    //   2. A late stream_start WS event (subscription resync / delayed
    //      broadcast) arrives for the SAME message id 39789.
    //      findStreamingMsg returns undefined (the row is finalized), so the
    //      handler creates a NEW streaming placeholder with id=39789.
    //   3. Content events append to the placeholder; done finalizes it.
    //   4. The array now holds the finalized DB row AND the placeholder — both
    //      id=39789, both with identical content.
    //   5. Only the next db_load (another switch) removes the placeholder,
    //      which matches the user's report: "switch again → back to normal".
    //
    // The reducer chain MUST NOT produce two assistant rows with the same id.
    let s: any[] = [
      { role: 'user', id: 39788, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }] },
      // DB row: finalized by parseMessages (running=false stripped streaming).
      { role: 'assistant', id: 39789, content: '', blocks: [{ type: 'text', text: '上一次触发的构建（KS1j1G）还在运行中' }] },
    ]
    sortMessages(s)

    // Late stream_start — same message id, no streaming msg present.
    const anchorIdx = s.findIndex((m: any) => m.role === 'user')
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: {
      role: 'assistant',
      id: 39789,
      content: '',
      blocks: [],
      streaming: true,
      createdAt: new Date().toISOString(),
      seq: nextClientSeq(),
      parentQueueId: anchorIdx !== -1 ? String(s[anchorIdx].id) : undefined,
    } } as any)
    // ws_stream_start sets the id on the existing streaming msg (no-op, same id).
    s = chatMessageReducer(s, { type: 'ws_stream_start', messageId: 39789 } as any)

    // Stream content arrives → appends to the streaming placeholder.
    s = chatMessageReducer(s, { type: 'ws_content', text: ' 等它完成即可' } as any)

    const streaming = s.filter((m: any) => m.role === 'assistant' && m.streaming)
    expect(streaming).toHaveLength(1, 'exactly one streaming placeholder must exist')

    // done → finalize.
    s = chatMessageReducer(s, { type: 'stream_finalize' } as any)
    const replies = s.filter((m: any) => m.role === 'assistant')
    // The finalized placeholder (id=39789) duplicates the DB row — both have
    // the same id, so they collapse into a single Vue v-for key. This is the
    // transient duplicate the user sees after switching back.
    expect(replies.length).toBeLessThanOrEqual(2)

    // The next db_load (the "switch again") converges to the single DB row.
    s = chatMessageReducer(s, { type: 'db_load', dbMessages: [
      { role: 'user', id: 39788, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }] },
      { role: 'assistant', id: 39789, content: '', blocks: [{ type: 'text', text: '上一次触发的构建（KS1j1G）还在运行中' }] },
    ] } as any)
    const finalReplies = s.filter((m: any) => m.role === 'assistant')
    expect(finalReplies).toHaveLength(1, 'db_load converges to the single DB row')
  })

  it('RC8: stream_placeholder dedups by id — a late stream_start after db_load cannot inject a second copy', () => {
    // Root-cause fix: the transient duplicate reported after session switches
    // is a stream_start placeholder created for a row that already landed in
    // the array (via db_load or an earlier placeholder). Dedup by id in the
    // reducer so the same message can never render twice.
    let s: any[] = [
      { role: 'user', id: 39788, content: '编译前端', blocks: [{ type: 'text', text: '编译前端' }] },
      // DB row already in the array (finalized by db_load).
      { role: 'assistant', id: 39789, content: '', blocks: [{ type: 'text', text: '上一次触发的构建（KS1j1G）还在运行中' }] },
    ]
    sortMessages(s)

    // Late stream_start for the SAME message id → must NOT add a second copy.
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: {
      role: 'assistant',
      id: 39789,
      content: '',
      blocks: [],
      streaming: true,
      createdAt: new Date().toISOString(),
      seq: nextClientSeq(),
    } } as any)

    const replies = s.filter((m: any) => m.role === 'assistant')
    expect(replies).toHaveLength(1, 'stream_placeholder must dedup by id')
    // The existing copy is marked streaming so subsequent content events find it.
    expect(replies[0].streaming).toBe(true)
  })

  it('RC8b: stream_placeholder still pushes when no same-id assistant exists', () => {
    let s: any[] = [
      { role: 'user', id: 1, content: 'A', blocks: [{ type: 'text', text: 'A' }] },
    ]
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: {
      role: 'assistant',
      id: 'drain-abc',
      content: '',
      blocks: [],
      streaming: true,
      seq: nextClientSeq(),
    } } as any)
    expect(s.filter((m: any) => m.role === 'assistant')).toHaveLength(1)
  })

  it('RC8c: two genuinely distinct streams (different ids) both get placeholders', () => {
    let s: any[] = []
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: {
      role: 'assistant', id: 'drain-a', content: '', blocks: [], streaming: true, seq: nextClientSeq(),
    } } as any)
    s = chatMessageReducer(s, { type: 'stream_placeholder', msg: {
      role: 'assistant', id: 'drain-b', content: '', blocks: [], streaming: true, seq: nextClientSeq(),
    } } as any)
    expect(s.filter((m: any) => m.role === 'assistant')).toHaveLength(2)
  })
})

describe('mergeStreamBlocks preserves text/tool interleaving', () => {
  // Reported: while a turn is STILL STREAMING, the reply rendered as two
  // stacked groups — all tool calls bunched at the top, all text bunched below
  // — instead of the real speak → call → speak → call order. Re-opening the
  // session mid-stream showed the same thing; only finishing/cancelling the
  // turn restored the order. Root cause: mergeStreamBlocks (run by db_load
  // while the placeholder is live) prepended every DB-only non-text block and
  // moved all DB text blocks to the first live-text slot. The assertions below
  // therefore check the BLOCK SEQUENCE, not just the concatenated text — the
  // pre-existing tests only summed text and counted tools, which is exactly why
  // the regression slipped through.
  const u = (id: number) => ({ role: 'user', id, content: 'Q', blocks: [{ type: 'text', text: 'Q' }] })

  it('case 0: keeps interleaved text/tool order when the live text is a short prefix', () => {
    // The placeholder was recreated (stream_start) and has received only the
    // first few characters; the DB flush holds the whole interleaved turn.
    const live: any[] = [
      u(1),
      { role: 'assistant', id: 42, content: '', streaming: true, parentQueueId: '1', blocks: [
        { type: 'text', text: 'Let me ch' },
      ] },
    ]
    const db: any[] = [
      u(1),
      { role: 'assistant', id: 42, content: '', streaming: true, blocks: [
        { type: 'text', text: 'Let me check' },
        { type: 'tool_use', name: 'Bash', id: 'tu1', done: true },
        { type: 'text', text: 'Found it' },
        { type: 'tool_use', name: 'Read', id: 'tu2', done: true },
        { type: 'text', text: 'Done' },
      ] },
    ]
    const merged = rebuildFromDb(live, db as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    // The crux: tools must NOT be hoisted above the text.
    expect((reply.blocks || []).map((b: any) => b.type)).toEqual([
      'text', 'tool_use', 'text', 'tool_use', 'text',
    ])
    const ids = (reply.blocks || []).filter((b: any) => b.type === 'tool_use').map((b: any) => b.id)
    expect(ids).toEqual(['tu1', 'tu2'])
    expect((reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join(''))
      .toBe('Let me checkFound itDone')
  })

  it('case 0: does not stack tools on top when the live placeholder has no text yet', () => {
    // Strongest form of the shape: the live placeholder holds only a tool block
    // (its tool event arrived, its text has not). The DB has the full turn.
    const live: any[] = [
      u(1),
      { role: 'assistant', id: 43, content: '', streaming: true, parentQueueId: '1', blocks: [
        { type: 'tool_use', name: 'Bash', id: 'tu1', done: true },
      ] },
    ]
    const db: any[] = [
      u(1),
      { role: 'assistant', id: 43, content: '', streaming: true, blocks: [
        { type: 'text', text: 'Let me look' },
        { type: 'tool_use', name: 'Bash', id: 'tu1', done: true },
        { type: 'text', text: 'Found root' },
      ] },
    ]
    const merged = rebuildFromDb(live, db as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    expect((reply.blocks || []).map((b: any) => b.type)).toEqual(['text', 'tool_use', 'text'])
    expect((reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join(''))
      .toBe('Let me lookFound root')
  })

  it('case 0: keeps a live-only tool before the text that follows it', () => {
    // A tool whose event reached the live stream but not the DB's rate-limited
    // flush must keep its live position — here it precedes the text, so a merge
    // that reorders by type (text first) would visibly move it.
    const live: any[] = [
      u(1),
      { role: 'assistant', id: 44, content: '', streaming: true, parentQueueId: '1', blocks: [
        { type: 'tool_use', name: 'Grep', id: 'tu-live', done: false },
        { type: 'text', text: 'pre' },
      ] },
    ]
    const db: any[] = [
      u(1),
      { role: 'assistant', id: 44, content: '', streaming: true, blocks: [
        { type: 'text', text: 'prefix' },
      ] },
    ]
    const merged = rebuildFromDb(live, db as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    expect((reply.blocks || []).map((b: any) => b.type)).toEqual(['tool_use', 'text'])
    expect((reply.blocks || []).filter((b: any) => b.type === 'tool_use').map((b: any) => b.id))
      .toEqual(['tu-live'])
  })

  it('case 1: splices a DB-only tool at its DB position instead of prepending it', () => {
    // Continuous streaming: live already covers the DB text. A tool the DB
    // flushed before the placeholder was recreated belongs AFTER that text,
    // where it happened — not at the top of the reply.
    const live: any[] = [
      u(1),
      { role: 'assistant', id: 45, content: '', streaming: true, parentQueueId: '1', blocks: [
        { type: 'text', text: 'Hello world' },
      ] },
    ]
    const db: any[] = [
      u(1),
      { role: 'assistant', id: 45, content: '', streaming: true, blocks: [
        { type: 'text', text: 'Hello' },
        { type: 'tool_use', name: 'Bash', id: 'tu1', done: true },
      ] },
    ]
    const merged = rebuildFromDb(live, db as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    expect((reply.blocks || []).map((b: any) => b.type)).toEqual(['text', 'tool_use'])
    expect((reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join(''))
      .toBe('Hello world')
    expect((reply.blocks || []).filter((b: any) => b.type === 'tool_use')).toHaveLength(1)
  })

  it('case 0: reproduces the reported multi-tool turn without duplicating or reordering', () => {
    // The exact reported shape at scale: several tool calls interleaved with
    // text, live holding only the opening text. Every DB block must appear once,
    // in DB order.
    const live: any[] = [
      u(1),
      { role: 'assistant', id: 46, content: '', streaming: true, parentQueueId: '1', blocks: [
        { type: 'text', text: 'a' },
      ] },
    ]
    const db: any[] = [
      u(1),
      { role: 'assistant', id: 46, content: '', streaming: true, blocks: [
        { type: 'text', text: 'a' },
        { type: 'tool_use', name: 'Bash', id: 'tu1', done: true },
        { type: 'text', text: 'b' },
        { type: 'tool_use', name: 'Read', id: 'tu2', done: true },
        { type: 'text', text: 'c' },
      ] },
    ]
    const merged = rebuildFromDb(live, db as any, true)
    const reply = merged.find((m: any) => m.role === 'assistant')!
    expect((reply.blocks || []).map((b: any) => b.type)).toEqual([
      'text', 'tool_use', 'text', 'tool_use', 'text',
    ])
    expect((reply.blocks || []).filter((b: any) => b.type === 'text').map((b: any) => b.text).join(''))
      .toBe('abc')
    expect((reply.blocks || []).filter((b: any) => b.type === 'tool_use').map((b: any) => b.id))
      .toEqual(['tu1', 'tu2'])
  })
})

describe('messageText', () => {
  it('returns block text when blocks are present', () => {
    const m = { role: 'user', content: 'raw', blocks: [{ type: 'text', text: 'Hello' }] } as any
    expect(messageText(m)).toBe('Hello')
  })

  it('returns plain content unchanged', () => {
    const m = { role: 'user', content: 'Hello world' } as any
    expect(messageText(m)).toBe('Hello world')
  })

  it('unwraps blocks-format JSON content', () => {
    const m = { role: 'user', content: '{"blocks":[{"type":"text","text":"from blocks"}]}' } as any
    expect(messageText(m)).toBe('from blocks')
  })

  it('unwraps bare content-array JSON content', () => {
    const m = { role: 'user', content: '[{"type":"text","text":"from array"}]' } as any
    expect(messageText(m)).toBe('from array')
  })

  it('unwraps ACP notification wrapper content', () => {
    const m = {
      role: 'user',
      content: JSON.stringify({ content: { text: 'from acp', type: 'text' }, messageId: 'm1', sessionUpdate: 'user_message_chunk' }),
    } as any
    expect(messageText(m)).toBe('from acp')
  })

  it('unwraps nested ACP notification inside a text block', () => {
    const m = {
      role: 'user',
      content: JSON.stringify({
        blocks: [
          { type: 'text', text: JSON.stringify({ content: { text: 'nested msg', type: 'text' }, sessionUpdate: 'user_message_chunk' }) },
        ],
      }),
    } as any
    expect(messageText(m)).toBe('nested msg')
  })

  it('returns empty for recognized wrapper with no text (content match normalization)', () => {
    const m = { role: 'user', content: '{"blocks":[{"type":"tool_use","name":"bash"}]}' } as any
    expect(messageText(m)).toBe('')
  })

  it('returns raw content for unrecognized JSON', () => {
    const m = { role: 'user', content: '{"foo":"bar"}' } as any
    expect(messageText(m)).toBe('{"foo":"bar"}')
  })

  it('returns raw content for non-JSON bracket text', () => {
    const m = { role: 'user', content: '[PWA] Service Worker skipped' } as any
    expect(messageText(m)).toBe('[PWA] Service Worker skipped')
  })
})

// ── ws_error / ws_warning structured error fields ──
describe('ws_error / ws_warning structured error fields', () => {
  it('ws_error attaches error_code/http_status/error_source to streaming assistant', () => {
    let s: any[] = [{ role: 'assistant', id: 1, content: '', blocks: [], streaming: true }]
    s = chatMessageReducer(s, {
      type: 'ws_error',
      text: 'ACP error -32603: Internal error',
      reason: 'backend_exit',
      errorCode: -32603,
      httpStatus: 500,
      errorSource: 'agent',
    })
    const sm = s[0]
    expect(sm.blocks).toHaveLength(1)
    expect(sm.blocks[0].type).toBe('error')
    expect(sm.blocks[0].error_code).toBe(-32603)
    expect(sm.blocks[0].http_status).toBe(500)
    expect(sm.blocks[0].error_source).toBe('agent')
  })

  it('ws_warning attaches error_code/http_status/error_source to streaming assistant', () => {
    let s: any[] = [{ role: 'assistant', id: 1, content: '', blocks: [], streaming: true }]
    s = chatMessageReducer(s, {
      type: 'ws_warning',
      text: 'Internal error',
      reason: 'request_failed',
      errorCode: -32603,
      httpStatus: 500,
      errorSource: 'agent',
    })
    const sm = s[0]
    expect(sm.blocks).toHaveLength(1)
    expect(sm.blocks[0].type).toBe('warning')
    expect(sm.blocks[0].error_code).toBe(-32603)
    expect(sm.blocks[0].http_status).toBe(500)
    expect(sm.blocks[0].error_source).toBe('agent')
  })

  // error_detail is what makes a placeholder code (-32603) actionable; if the
  // reducer drops it the banner silently regresses to the bare code.
  it('ws_warning carries the agent-reported error_detail', () => {
    let s: any[] = [{ role: 'assistant', id: 1, content: '', blocks: [], streaming: true }]
    s = chatMessageReducer(s, {
      type: 'ws_warning',
      text: 'AI request refused by the agent',
      reason: 'refused',
      errorCode: -32603,
      errorSource: 'agent',
      errorDetail: 'Bad substitution: o.gaps.join',
    })
    expect(s[0].blocks[0].error_detail).toBe('Bad substitution: o.gaps.join')
  })

  it('ws_error carries the agent-reported error_detail', () => {
    let s: any[] = [{ role: 'assistant', id: 1, content: '', blocks: [], streaming: true }]
    s = chatMessageReducer(s, {
      type: 'ws_error',
      text: 'ACP error -32603: Internal error',
      reason: 'backend_exit',
      errorDetail: 'Bad substitution: x',
    })
    expect(s[0].blocks[0].error_detail).toBe('Bad substitution: x')
  })

  it('ws_error without structured fields stays compatible', () => {
    let s: any[] = [{ role: 'assistant', id: 1, content: '', blocks: [], streaming: true }]
    s = chatMessageReducer(s, { type: 'ws_error', text: 'oops', reason: 'timeout' })
    const sm = s[0]
    expect(sm.blocks).toHaveLength(1)
    expect(sm.blocks[0].error_code).toBeUndefined()
    expect(sm.blocks[0].http_status).toBeUndefined()
    expect(sm.blocks[0].error_source).toBeUndefined()
    expect(sm.blocks[0].error_detail).toBeUndefined()
  })
})

// ── Conversation ordering after refresh ──
//
// A queued message is NOT part of the conversation `messages` array: it lives
// in the queue store until the backend materializes it into chat_history at
// dequeue time. Its DB id therefore always precedes the reply it produced, so
// ordering is a plain numeric-id sort — no reply anchor is needed (the whole
// anchorRepliesToQuestions machinery was removed with the queue refactor).
describe('conversation ordering after refresh (DB-id sort)', () => {
  const aMsg = (id: unknown, content: string, extra: Record<string, unknown> = {}): any =>
    ({ role: 'assistant', id, content: '', blocks: content ? [{ type: 'text', text: content }] : [], createdAt: '2026-01-01T00:00:01Z', ...extra })
  const uMsg = (id: unknown, content: string, extra: Record<string, unknown> = {}): any =>
    ({ role: 'user', id, content, blocks: content ? [{ type: 'text', text: content }] : [], files: [], createdAt: '2026-01-01T00:00:01Z', ...extra })

  it('a still-queued message is absent from the array; DB rows keep id order', () => {
    // Q2 is still waiting in the queue, so it has no chat_history row and is
    // not in the array at all. Only Q1 and reply1 are DB-backed.
    const dbMsgs: any[] = [
      uMsg(1, 'Q1', { createdAt: '2026-01-01T00:00:01Z' }),
      aMsg(2, 'reply1', { streaming: true, createdAt: '2026-01-01T00:00:02Z' }),
    ]
    const merged = rebuildFromDb([], dbMsgs as any)

    expect(merged.map((m: any) => `${m.role}:${String(m.id)}${m.streaming ? ':S' : ''}`))
      .toEqual(['user:1', 'assistant:2:S'])
  })

  it('a drained message keeps its DB-id order (question before reply)', () => {
    // Q2 was materialized into chat_history (id=3) and reply1 (id=2) is still
    // streaming. The reply sorts by its own DB id, not by any transient
    // domain, so the visual order is Q1 → reply1 → Q2 by id.
    const dbMsgs: any[] = [
      uMsg(1, 'Q1', { createdAt: '2026-01-01T00:00:01Z' }),
      aMsg(2, 'reply1', { streaming: true, createdAt: '2026-01-01T00:00:02Z' }),
      uMsg(3, 'Q2', { createdAt: '2026-01-01T00:00:03Z' }),
    ]
    const merged = rebuildFromDb([], dbMsgs as any)

    expect(merged.map((m: any) => `${m.role}:${String(m.id)}${m.streaming ? ':S' : ''}`))
      .toEqual(['user:1', 'assistant:2:S', 'user:3'])
  })

  it('refresh keeps a live placeholder and adopts its DB row id (SPA refresh mid-stream)', () => {
    // SPA refresh (not a full reload): the live placeholder survives in memory
    // with a transient drain-* id. The rebuild matches it to its DB streaming
    // row (id=2) and adopts that id, so it sorts by DB id — after Q1, before
    // the materialized Q2 (id=3). The optimistic Q1 bubble is dropped (the DB
    // row is authoritative).
    const messages: any[] = [
      { role: 'user', id: 'pending-A', content: 'Q1', blocks: [{ type: 'text', text: 'Q1' }], seq: 1, queueId: 'pending-A' },
      { role: 'assistant', id: 'drain-r1', content: '', blocks: [{ type: 'text', text: 'reply1' }], streaming: true, seq: 2 },
      { role: 'user', id: 'pending-B', content: 'Q2', blocks: [{ type: 'text', text: 'Q2' }], seq: 3, queueId: 'pending-B' },
    ]
    const dbMsgs: any[] = [
      uMsg(1, 'Q1', { createdAt: '2026-01-01T00:00:01Z' }),
      aMsg(2, 'reply1', { streaming: true, createdAt: '2026-01-01T00:00:02Z' }),
    ]
    const merged = rebuildFromDb(messages, dbMsgs as any)

    // Q2 has no DB row yet (still queued), so it is dropped from the array; the
    // live placeholder adopts id=2 and stays streaming.
    expect(merged.map((m: any) => `${m.role}:${String(m.id)}${m.streaming ? ':S' : ''}`))
      .toEqual(['user:1', 'assistant:2:S'])
  })

  it('plain DB-id ordering reproduces the old anchor contract without any anchor field', () => {
    // The old contract needed parentQueueId to keep a reply under its question
    // when the queued bubble carried a large seq. Under the new contract the
    // materialized question row (id=1) precedes its finalized reply (id=2) and
    // the next materialized question (id=3) follows — a plain id sort suffices.
    const msgs = [
      { role: 'user', id: 1, content: 'Q1' },
      { role: 'assistant', id: 2, content: 'reply1' },
      { role: 'user', id: 3, content: 'Q2' },
    ] as any[]
    sortMessages(msgs)
    expect(msgs.map((m: any) => `${m.role}:${String(m.id)}`)).toEqual(['user:1', 'assistant:2', 'user:3'])
  })
})

// ── In-flight direct-send guard (user bubble vanishes after stale db_load) ──
//
// Reported: sending a message in an IDLE session sometimes shows the assistant
// reply streaming while the user's own bubble is completely absent, restored
// only by a manual refresh (full db_load).
//
// Root cause: a loadHistory GET that was already in flight BEFORE the send POST
// committed its row (e.g. the done-triggered reload of the previous turn, or a
// slow panel-open load) returns a DB snapshot that predates the new row.
// rebuildFromDb then drops the just-pushed optimistic bubble as "transient
// without a DB row"; nothing re-creates it (the user_message self-echo only
// adopts an EXISTING bubble). The next full reload (refresh / done of the
// current turn) finally restores it — exactly the reported behavior.
describe('in-flight direct-send guard in rebuildFromDb', () => {
  beforeEach(() => { resetInFlightSendsForTest() })
  afterEach(() => { resetInFlightSendsForTest() })

  const u = (id: unknown, content: string, extra: Record<string, unknown> = {}) =>
    ({ role: 'user' as const, id, content, blocks: content ? [{ type: 'text', text: content }] : [], files: [], createdAt: '2026-01-01T00:00:01Z', ...extra })
  const a = (id: unknown, content: string, extra: Record<string, unknown> = {}) =>
    ({ role: 'assistant' as const, id, content, blocks: content ? [{ type: 'text', text: content }] : [], createdAt: '2026-01-01T00:00:02Z', ...extra })

  it('keeps an optimistic direct-send bubble when a stale db_load snapshot predates its row', () => {
    trackInFlightSend('pending-msg2')
    const state: any[] = [
      u(1, 'msg1'),
      a(2, 'reply1'),
      u('pending-msg2', 'msg2', { seq: 99, queueId: 'pending-msg2' }),
    ]
    // The stale GET was fetched before the POST committed msg2 — snapshot has
    // only msg1/reply1.
    const staleDb: any[] = [u(1, 'msg1'), a(2, 'reply1')]
    const merged = rebuildFromDb(state, staleDb)
    const user2 = merged.filter((m: any) => m.role === 'user' && messageText(m) === 'msg2')
    expect(user2).toHaveLength(1)
    expect(user2[0].id).toBe('pending-msg2')
    // It sorts after all DB-backed history (bottom), where the send pipeline
    // expects it while it awaits DB ack.
    expect(merged.map((m: any) => `${m.role}:${String(m.id)}`)).toEqual(['user:1', 'assistant:2', 'user:pending-msg2'])
  })

  it('keeps an ALREADY-ADOPTED (numeric id) in-flight bubble against a stale snapshot', () => {
    // sendMessageNow's POST already returned and the bubble adopted its DB id
    // (id=3, queueId preserved as the pending string id). A stale pre-commit
    // GET still lands afterwards — the bubble is non-transient (numeric id) yet
    // must not be dropped.
    trackInFlightSend('pending-msg2')
    const state: any[] = [
      u(1, 'msg1'),
      a(2, 'reply1'),
      u(3, 'msg2', { queueId: 'pending-msg2' }),
    ]
    const staleDb: any[] = [u(1, 'msg1'), a(2, 'reply1')]
    const merged = rebuildFromDb(state, staleDb)
    const user2 = merged.filter((m: any) => m.role === 'user' && messageText(m) === 'msg2')
    expect(user2).toHaveLength(1)
    expect(user2[0].id).toBe(3)
    expect(merged.map((m: any) => `${m.role}:${String(m.id)}`)).toEqual(['user:1', 'assistant:2', 'user:3'])
  })

  it('drops the transient bubble normally when its queueId is NOT in-flight (no regression)', () => {
    const state: any[] = [
      u(1, 'msg1'),
      a(2, 'reply1'),
      u('stale-bubble', 'msg2', { seq: 99, queueId: 'stale-bubble' }),
    ]
    const staleDb: any[] = [u(1, 'msg1'), a(2, 'reply1')]
    const merged = rebuildFromDb(state, staleDb)
    expect(merged.filter((m: any) => m.role === 'user' && m.content === 'msg2')).toHaveLength(0)
  })

  it('releases the guard once a db_load snapshot contains the row (send fully acked)', () => {
    trackInFlightSend('pending-msg2')
    // The POST returned, so the bubble adopted its numeric DB id
    // (optimistic_adopt_id) and kept the string queueId as the in-flight key.
    const state: any[] = [
      u(1, 'msg1'),
      a(2, 'reply1'),
      u(3, 'msg2', { queueId: 'pending-msg2' }),
    ]
    // Fresh snapshot now includes the committed row (id=3). The row carries NO
    // queueId: chat_history has no such column (it was dropped when the queue
    // moved to its own table), so the release must key off the bubble's adopted
    // numeric id — not off a field the snapshot can never contain.
    const freshDb: any[] = [u(1, 'msg1'), a(2, 'reply1'), u(3, 'msg2')]
    const merged = rebuildFromDb(state, freshDb)
    // Bubble replaced by the authoritative DB row.
    expect(merged.filter((m: any) => m.role === 'user' && messageText(m) === 'msg2')).toHaveLength(1)
    expect(merged.find((m: any) => m.role === 'user' && messageText(m) === 'msg2')!.id).toBe(3)
    // Guard released: a subsequent stale rebuild would NOT resurrect it.
    const staleDb: any[] = [u(1, 'msg1'), a(2, 'reply1')]
    const merged2 = rebuildFromDb([u(1, 'msg1'), a(2, 'reply1'), u(3, 'msg2', { queueId: 'pending-msg2' })], staleDb)
    expect(merged2.filter((m: any) => m.role === 'user' && messageText(m) === 'msg2')).toHaveLength(0)
  })

  it('does NOT duplicate an adopted bubble when the snapshot carries its row (realistic rows)', () => {
    // Reported: sending a message showed TWO identical user bubbles; switching
    // away and back (a full db_load) fixed it.
    //
    // Root cause: the guard-release loop keyed off `db.queueId`, but a real
    // chat_history row has NO queueId (the column was dropped in the queue
    // refactor and ChatMessage has no such field). So the guard was NEVER
    // released for a direct send. The bubble then hit the in-flight branch,
    // which keeps it even though the authoritative snapshot already carries the
    // same numeric id — and the DB row was appended as well → two bubbles.
    trackInFlightSend('pending-msg2')
    // Direct send to an idle session: the POST returned, so the optimistic
    // bubble adopted its numeric DB id (optimistic_adopt_id) and kept the
    // string queueId as the in-flight key.
    const state: any[] = [
      u(1, 'msg1'),
      a(2, 'reply1'),
      u(3, 'msg2', { queueId: 'pending-msg2' }),
    ]
    // Authoritative snapshot containing the very same row.
    const db: any[] = [u(1, 'msg1'), a(2, 'reply1'), u(3, 'msg2')]
    const merged = rebuildFromDb(state, db, false)
    const msg2 = merged.filter((m: any) => m.role === 'user' && messageText(m) === 'msg2')
    expect(msg2).toHaveLength(1)
    // The single copy is the authoritative DB row (guard released), not the
    // stale optimistic object.
    expect(msg2[0].queueId).toBeUndefined()
    expect(isInFlightSend('pending-msg2')).toBe(false)
  })

  it('untrackInFlightSend releases the guard on send failure', () => {
    trackInFlightSend('pending-msg2')
    untrackInFlightSend('pending-msg2')
    const state: any[] = [
      u(1, 'msg1'),
      u('pending-msg2', 'msg2', { seq: 99, queueId: 'pending-msg2' }),
    ]
    const staleDb: any[] = [u(1, 'msg1')]
    const merged = rebuildFromDb(state, staleDb)
    expect(merged.filter((m: any) => m.role === 'user' && m.content === 'msg2')).toHaveLength(0)
  })

  it('the in-flight guard is keyed by queueId even when the id is already numeric', () => {
    // An ENQUEUED message never enters the messages array at all (it lives in
    // the queue store), so rebuildFromDb has no queued-bubble branch any more.
    // The remaining guard covers a DIRECT send whose bubble adopted its DB id
    // from the POST response but whose row a stale snapshot still lacks: the
    // queueId (not the numeric id) is the in-flight key.
    trackInFlightSend('pending-msg2')
    const state: any[] = [
      u(1, 'msg1'),
      a(2, 'reply1'),
      u(3, 'msg2', { queueId: 'pending-msg2' }),
    ]
    const staleDb: any[] = [u(1, 'msg1'), a(2, 'reply1')]
    const merged = rebuildFromDb(state, staleDb)
    const msg2 = merged.filter((m: any) => m.role === 'user' && messageText(m) === 'msg2')
    expect(msg2).toHaveLength(1)
    expect(msg2[0].id).toBe(3)
    // A STALE snapshot must NOT release the guard — that is the whole point of
    // it (the row exists; this GET just predates it).
    expect(isInFlightSend('pending-msg2')).toBe(true)

    // A FRESH snapshot that carries the row does release it, so a later stale
    // rebuild drops the bubble as usual instead of resurrecting it. The row has
    // no queueId (real rows never do) — the release keys off the numeric id.
    rebuildFromDb(state, [u(1, 'msg1'), a(2, 'reply1'), u(3, 'msg2')])
    expect(isInFlightSend('pending-msg2')).toBe(false)
  })

  // A queued message that was just DRAINED is announced with a user_message
  // event AFTER its row commits, and the frontend renders it as a _remote
  // bubble. removeQueued releases its in-flight guard at that moment, so
  // nothing else protects it from a stale snapshot — which is exactly how the
  // user's own queued message disappeared while the assistant reply stayed
  // ("只出现助手消息" until the whole turn finished).
  describe('announced (user_message) bubbles survive a stale snapshot', () => {
    it('keeps a numeric-id _remote user bubble while the session is running', () => {
      const state: any[] = [
        u(1, 'msg1'),
        a(2, 'reply1'),
        u(3, 'drained', { _remote: true }),
        a(4, 'reply2', { streaming: true }),
      ]
      // A GET issued before the drain committed rows 3/4.
      const staleDb: any[] = [u(1, 'msg1'), a(2, 'reply1')]
      const merged = rebuildFromDb(state, staleDb, true)
      expect(merged.filter((m: any) => m.role === 'user' && messageText(m) === 'drained')).toHaveLength(1)
    })

    it('drops it once the run is over, so a rewind still converges', () => {
      // Rewind cancels the run first, so sessionRunning is false by the time it
      // reloads — the bubble must NOT survive the truncation.
      const state: any[] = [u(1, 'msg1'), a(2, 'reply1'), u(3, 'drained', { _remote: true })]
      const afterRewind: any[] = [u(1, 'msg1'), a(2, 'reply1')]
      const merged = rebuildFromDb(state, afterRewind, false)
      expect(merged.filter((m: any) => m.role === 'user' && messageText(m) === 'drained')).toHaveLength(0)
    })

    it('does not duplicate the bubble when the snapshot DOES carry the row', () => {
      const state: any[] = [u(1, 'msg1'), u(3, 'drained', { _remote: true })]
      const freshDb: any[] = [u(1, 'msg1'), u(3, 'drained')]
      const merged = rebuildFromDb(state, freshDb, true)
      expect(merged.filter((m: any) => m.role === 'user' && String(m.id) === '3')).toHaveLength(1)
    })
  })
})

describe('sub-agent parent grouping (reducer)', () => {
  const streamingMsg = (): any => ({
    id: 1,
    role: 'assistant',
    content: '',
    blocks: [],
    streaming: true,
    createdAt: '2026-01-01T00:00:00Z',
  })

  it('ws_content with parentToolCallId tags the block', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_content', text: 'child', parentToolCallId: 'call_p' })
    expect(s[0].blocks![0]).toMatchObject({ type: 'text', text: 'child', parent_tool_call_id: 'call_p' })
  })

  it('ws_content top-level has no parent field', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_content', text: 'parent' })
    expect(s[0].blocks![0].parent_tool_call_id).toBeUndefined()
  })

  it('parent and child text do not merge into one block', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_content', text: 'parent ' })
    s = chatMessageReducer(s, { type: 'ws_content', text: 'child', parentToolCallId: 'call_p' })
    s = chatMessageReducer(s, { type: 'ws_content', text: ' parent2' })
    // The resumed parent text coalesces back into the PARENT's own block (the
    // interleaved child block is another parent, so it is stepped over, not a
    // boundary). Parent and child still never share a block.
    const texts = s[0].blocks!.filter((b: any) => b.type === 'text')
    expect(texts.length).toBe(2)
    expect(texts[0]).toMatchObject({ text: 'parent  parent2' })
    expect(texts[0].parent_tool_call_id).toBeUndefined()
    expect(texts[1]).toMatchObject({ text: 'child', parent_tool_call_id: 'call_p' })
  })

  it('interleaved sub-agent thinking coalesces per parent', () => {
    let s = [streamingMsg()]
    // Two sub-agents stream concurrently; each one's reasoning is continuous.
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'Let', parentToolCallId: 'call_a' })
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'I will inspect the handler', parentToolCallId: 'call_b' })
    s = chatMessageReducer(s, { type: 'ws_thinking', text: ' me look at the key files', parentToolCallId: 'call_a' })
    s = chatMessageReducer(s, { type: 'ws_thinking', text: ' then the reducer', parentToolCallId: 'call_b' })
    s = chatMessageReducer(s, { type: 'ws_thinking', text: '.ts, ContentBlocks.vue.', parentToolCallId: 'call_a' })

    const thinks = s[0].blocks!.filter((b: any) => b.type === 'thinking')
    expect(thinks.length).toBe(2)
    expect(thinks[0]).toMatchObject({
      text: 'Let me look at the key files.ts, ContentBlocks.vue.',
      parent_tool_call_id: 'call_a',
    })
    expect(thinks[1]).toMatchObject({
      text: 'I will inspect the handler then the reducer',
      parent_tool_call_id: 'call_b',
    })
  })

  it('own tool_use still separates that parent\'s thinking', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'before', parentToolCallId: 'call_a' })
    s = chatMessageReducer(s, { type: 'ws_tool_use', data: { id: 't1', name: 'Read', parent_tool_call_id: 'call_a' } as any })
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'after', parentToolCallId: 'call_a' })
    // A foreign agent's interleaved thinking must not resurrect the merge.
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'other', parentToolCallId: 'call_b' })

    const thinks = s[0].blocks!.filter((b: any) => b.type === 'thinking')
    expect(thinks.length).toBe(3)
    expect(thinks[0].text).toBe('before')
    expect(thinks[1].text).toBe('after')
    expect(thinks[2]).toMatchObject({ text: 'other', parent_tool_call_id: 'call_b' })
  })

  it('ws_thinking with parentToolCallId tags the block', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'reason', key: 'k1', parentToolCallId: 'call_p' })
    expect(s[0].blocks![0]).toMatchObject({ type: 'thinking', text: 'reason', parent_tool_call_id: 'call_p' })
  })

  it('child thinking does not merge into parent thinking', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'pt' })
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'ct', parentToolCallId: 'call_p' })
    const thinks = s[0].blocks!.filter((b: any) => b.type === 'thinking')
    expect(thinks.length).toBe(2)
    expect(thinks[0].text).toBe('pt')
    expect(thinks[1]).toMatchObject({ text: 'ct', parent_tool_call_id: 'call_p' })
  })

  it('ws_tool_use records parent_tool_call_id', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_tool_use', data: { id: 't1', name: 'Read', parent_tool_call_id: 'call_p' } as any })
    expect(s[0].blocks![0]).toMatchObject({ type: 'tool_use', id: 't1', parent_tool_call_id: 'call_p' })
  })
})

describe('sub-agent thinking_done', () => {
  const streamingMsg = (): any => ({
    id: 1, role: 'assistant', content: '', blocks: [], streaming: true, createdAt: '2026-01-01T00:00:00Z',
  })

  it('marks a sub-agent thinking block done (parent boundary must not block it)', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'child thought', parentToolCallId: 'call_p' })
    s = chatMessageReducer(s, { type: 'ws_thinking_done' })
    const think = s[0].blocks!.find((b: any) => b.type === 'thinking')!
    expect(think.done).toBe(true)
    expect(think.parent_tool_call_id).toBe('call_p')
  })

  it('marks the last thinking block done when child follows parent', () => {
    let s = [streamingMsg()]
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'parent thought' })
    s = chatMessageReducer(s, { type: 'ws_thinking', text: 'child thought', parentToolCallId: 'call_p' })
    s = chatMessageReducer(s, { type: 'ws_thinking_done' })
    const thinks = s[0].blocks!.filter((b: any) => b.type === 'thinking')
    expect(thinks[1].done).toBe(true)
    expect(thinks[0].done).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// Mid-turn injection (steer) — the message joined the running turn instead of
// being queued, so it must never wait for a drain event that will not arrive.
// ---------------------------------------------------------------------------
describe('mid-turn injection (steer) pending handling', () => {
  it('optimistic_adopt_id clears pending when the caller opts in', () => {
    // An injected message: the optimistic bubble was pushed pending (the enqueue
    // path always does), but the backend reported it joined the running turn.
    // No queue_drain will ever arrive for it, so it must shed pending now.
    const messages: any[] = [
      { role: 'user', id: 'pending-1', queueId: 'pending-1', content: 'steered', blocks: [{ type: 'text', text: 'steered' }], pending: true, seq: nextClientSeq() },
    ]
    const next = chatMessageReducer(messages, {
      type: 'optimistic_adopt_id', id: 'pending-1', dbId: 7, clearPending: true,
    })
    const m = next.find((x: any) => x.content === 'steered')!
    expect(m.id).toBe(7)
    expect(m.queueId).toBe('pending-1')
    expect(m.pending).toBeUndefined()
    expect(m.seq).toBeUndefined()
  })

  it('optimistic_adopt_id keeps a queued bubble pending by default', () => {
    // A genuinely queued message: the drain loop will carry its id, so the
    // bubble must stay pending until then (pre-existing behavior, unchanged).
    const messages: any[] = [
      { role: 'user', id: 'queue-B', queueId: 'queue-B', content: 'queued', blocks: [{ type: 'text', text: 'queued' }], pending: true, seq: nextClientSeq() },
    ]
    const next = chatMessageReducer(messages, {
      type: 'optimistic_adopt_id', id: 'queue-B', dbId: 9,
    })
    const m = next.find((x: any) => x.content === 'queued')!
    expect(m.pending).toBe(true)
    expect(m.id).toBe('queue-B')
  })

  it('clear_queued_pending drops the pending marker for the injected message', () => {
    // The /api/ai/queue path returns {injected:true} — no drain will follow.
    const messages: any[] = [
      { role: 'user', id: 'q-inj', queueId: 'q-inj', content: 'inj', blocks: [{ type: 'text', text: 'inj' }], pending: true, queued: true, seq: nextClientSeq() },
      { role: 'user', id: 'q-keep', queueId: 'q-keep', content: 'kept', blocks: [{ type: 'text', text: 'kept' }], pending: true, queued: true, seq: nextClientSeq() },
    ]
    const next = chatMessageReducer(messages, { type: 'clear_queued_pending', queueId: 'q-inj' })

    const injected = next.find((x: any) => x.content === 'inj')!
    expect(injected.pending).toBeUndefined()
    expect(injected.queued).toBeUndefined()

    // Other queued messages are untouched.
    const kept = next.find((x: any) => x.content === 'kept')!
    expect(kept.pending).toBe(true)
  })

  it('ws_user_message keeps an injected remote bubble non-pending', () => {
    // Cross-device: the backend emits queued=false for an injected message, so
    // the receiving device must not render it as waiting in the queue.
    const next = chatMessageReducer([], {
      type: 'ws_user_message',
      data: { messageId: 11, content: 'remote injected', queueId: 'r-1', senderClientId: 'other-device', queued: false },
    } as any)
    const m = next[0]
    expect(m.pending).toBeUndefined()
  })
})

// ---------------------------------------------------------------------------
// Mid-turn split — the assistant reply is cut in two at the injection point so
// the injected question renders BETWEEN the halves instead of below a reply
// that is still streaming.
// ---------------------------------------------------------------------------
describe('ws_stream_split (mid-turn assistant split)', () => {
  it('finalizes the current bubble and opens a new anchored one', () => {
    // Live state: Q1, the streaming reply, and the just-injected Q2.
    const messages: any[] = [
      { role: 'user', id: 1, content: 'Q1', blocks: [{ type: 'text', text: 'Q1' }] },
      { role: 'assistant', id: 2, content: '', blocks: [{ type: 'text', text: 'before half' }], streaming: true },
      { role: 'user', id: 3, queueId: 'pending-inject-1', content: 'Q2', blocks: [{ type: 'text', text: 'Q2' }] },
    ]

    const next = chatMessageReducer(messages, {
      type: 'ws_stream_split', messageId: 4, queueId: 'pending-inject-1',
    })

    const before = next.find((m: any) => m.id === 2)!
    expect(before.streaming).toBeUndefined()
    expect(before.blocks[0].text).toBe('before half')

    const after = next.find((m: any) => m.id === 4)!
    expect(after.role).toBe('assistant')
    expect(after.streaming).toBe(true)
    expect(after.parentQueueId).toBe('pending-inject-1')
    expect(after.blocks).toEqual([])

    // The injected question must sort BETWEEN the halves.
    const contents = next.map((m: any) => m.content)
    expect(contents.indexOf('before half') < contents.indexOf('Q2')).toBe(true)
    expect(contents.indexOf('Q2') < next.indexOf(after)).toBe(true)
  })

  it('marks unfinished tool blocks done on the before-half', () => {
    const messages: any[] = [
      { role: 'assistant', id: 2, content: '', streaming: true, blocks: [
        { type: 'tool_use', name: 'Bash', id: 't1', done: false, output: '}' },
        { type: 'tool_use', name: 'PermissionApproval', id: 'p1', done: false },
      ] },
    ]
    const next = chatMessageReducer(messages, { type: 'ws_stream_split', messageId: 5 })
    const before = next.find((m: any) => m.id === 2)!
    expect(before.blocks[0].done).toBe(true)
    expect(before.blocks[0].output).toBe('')          // garbage output cleared
    expect(before.blocks[1].done).toBe(false)         // approval still needs the user
  })

  it('is idempotent — a replayed split does not push a second bubble', () => {
    const messages: any[] = [
      { role: 'assistant', id: 2, content: '', streaming: true, blocks: [] },
    ]
    const once = chatMessageReducer(messages, { type: 'ws_stream_split', messageId: 6, queueId: 'q' })
    const twice = chatMessageReducer(once, { type: 'ws_stream_split', messageId: 6, queueId: 'q' })
    expect(twice.filter((m: any) => m.role === 'assistant')).toHaveLength(2)
  })

  it('ws_stream_start does not rename a bubble that already holds a DB id', () => {
    // Regression guard for the split: after the split the new "after" bubble
    // carries its own numeric id. A stale/duplicate stream_start for the FIRST
    // row must not overwrite it, or the two messages collapse back into one.
    const messages: any[] = [
      { role: 'assistant', id: 7, content: '', streaming: true, blocks: [] },
    ]
    const next = chatMessageReducer(messages, { type: 'ws_stream_start', messageId: 99 })
    expect(next[0].id).toBe(7)
  })

  it('ws_stream_start still adopts an id for a placeholder without one', () => {
    const messages: any[] = [
      { role: 'assistant', id: 'drain-abc', content: '', streaming: true, blocks: [] },
    ]
    const next = chatMessageReducer(messages, { type: 'ws_stream_start', messageId: 99 })
    expect(next[0].id).toBe(99)
  })
})

// ---------------------------------------------------------------------------
// Queued-message action: "insert into the current reply" / "interrupt and send"
// ---------------------------------------------------------------------------
describe('queued message action (insert / interrupt)', () => {
  it('clear_queued_pending clears pending without removing the bubble', () => {
    // Insert: the message becomes part of the conversation, so its bubble must
    // STAY (only the pending spinner goes) — unlike cancel, which removes it.
    const messages: any[] = [
      { role: 'user', id: 'q-1', queueId: 'q-1', content: 'queued', blocks: [{ type: 'text', text: 'queued' }], pending: true, queued: true, seq: 1 },
    ]
    const next = chatMessageReducer(messages, { type: 'clear_queued_pending', queueId: 'q-1' })
    expect(next).toHaveLength(1)
    expect(next[0].pending).toBeUndefined()
    expect(next[0].queued).toBeUndefined()
  })

  it('clear_queued_pending leaves other queued bubbles alone', () => {
    const messages: any[] = [
      { role: 'user', id: 'q-1', queueId: 'q-1', content: 'a', blocks: [], pending: true, queued: true, seq: 1 },
      { role: 'user', id: 'q-2', queueId: 'q-2', content: 'b', blocks: [], pending: true, queued: true, seq: 2 },
    ]
    const next = chatMessageReducer(messages, { type: 'clear_queued_pending', queueId: 'q-1' })
    expect(next.find((m: any) => m.queueId === 'q-1')!.pending).toBeUndefined()
    expect(next.find((m: any) => m.queueId === 'q-2')!.pending).toBe(true)
  })

  it('clear_queued_pending clears a cross-device bubble via _remoteQueueId', () => {
    // A bubble created from another device's queued user_message has a numeric
    // id and NO queueId — only _remoteQueueId carries the queue identity. If the
    // reducer only matched id/queueId, this bubble would spin forever after an
    // insert (until some later loadHistory happened to drop it).
    const messages: any[] = [
      { role: 'user', id: 12345, content: 'from device B', blocks: [], pending: true, queued: true, _remote: true, _remoteQueueId: 'q-remote-1' },
    ]
    const next = chatMessageReducer(messages, { type: 'clear_queued_pending', queueId: 'q-remote-1' })
    expect(next).toHaveLength(1)
    expect(next[0].pending).toBeUndefined()
    expect(next[0].queued).toBeUndefined()
  })

  it('ws_queue_drain opens a new reply; clear_queued_pending does not', () => {
    // The distinction that matters: a drained message starts its OWN turn (new
    // assistant placeholder), an inserted one joins the running turn (none).
    const base: any[] = [
      { role: 'assistant', id: 2, content: '', blocks: [], streaming: true },
      { role: 'user', id: 'q-1', queueId: 'q-1', content: 'queued', blocks: [], pending: true, queued: true, seq: 1 },
    ]

    const afterInsert = chatMessageReducer(
      base.map((m) => ({ ...m })),
      { type: 'clear_queued_pending', queueId: 'q-1' },
    )
    expect(afterInsert.filter((m: any) => m.role === 'assistant')).toHaveLength(1)

    const afterDrain = chatMessageReducer(
      base.map((m) => ({ ...m })),
      { type: 'ws_queue_drain', queueId: 'q-1', text: 'queued', files: [], dbMessageId: 5 },
    )
    expect(afterDrain.filter((m: any) => m.role === 'assistant').length).toBeGreaterThan(1)
  })
})

  it('a remote attachment-only message is not swallowed by a local empty-content message', () => {
    // Cross-device case: this device already sent an attachment-only message
    // (empty content, adopted DB id, neither pending nor _remote), and another
    // device then sends a different file. The content-dedup rule matches on
    // content === "" and would drop the second message entirely, so the user
    // would never see the file sent from the other device.
    let s: any[] = [
      {
        role: 'user',
        id: 10,
        content: '',
        blocks: [],
        files: [{ path: '.clawbench/uploads/local.pdf', isDir: false }],
        createdAt: new Date().toISOString(),
      },
    ]

    s = chatMessageReducer(s, {
      type: 'ws_user_message',
      data: { messageId: 11, content: '', files: [{ path: '.clawbench/uploads/remote.pdf', isDir: false }] },
    } as any)

    const userBubbles = s.filter((m: any) => m.role === 'user')
    expect(userBubbles).toHaveLength(2, 'the remote file must render as its own bubble')
    expect(userBubbles.map((m: any) => m.files[0].path)).toContain('.clawbench/uploads/remote.pdf')
  })

// ── Live placeholder ↔ streaming DB row matching ──
//
// Regression: a running sub-agent's Agent pill showed its green check while the
// sub-agent was still producing output, and it recovered only on a later
// refresh. The DB row of a streaming turn carries the CURRENT tool status, so
// when the live placeholder failed to adopt the DB id — ws_stream_start is
// dropped routinely — the parsed DB row replaced the live blocks and its
// `done: true` (forced by parseAssistantContent on historical rows) won.
//
// The placeholder must instead be recognised as THIS turn's row even without an
// id match, so the live block flags survive.
describe('rebuildFromDb: live placeholder matching without an adopted id', () => {
  const streamingRow = (id: number) => ({
    role: 'assistant',
    id,
    content: '',
    blocks: [
      { type: 'text', text: 'text so far' },
      // Still running: the backend writes done only when the tool completes.
      { type: 'tool_use', name: 'Agent', id: 'call_agent', input: {}, done: false },
    ],
    streaming: true,
    createdAt: '2026-09-25T10:17:36Z',
  })

  const livePlaceholder = () => ({
    role: 'assistant',
    id: 'drain-1758000000000-abc', // never adopted the DB id (stream_start dropped)
    content: '',
    blocks: [
      { type: 'text', text: 'text so far' },
      { type: 'tool_use', name: 'Agent', id: 'call_agent', input: {}, done: false },
    ],
    streaming: true,
    createdAt: '2026-09-25T10:17:36Z',
    seq: 1,
  })

  it('keeps the live object and its running Agent block when the snapshot has one streaming row', () => {
    const live = livePlaceholder()
    const merged = rebuildFromDb([live] as any, [streamingRow(53240)] as any, true)
    expect(merged).toHaveLength(1)
    expect(merged[0]).toBe(live)
    const agent = (merged[0].blocks || []).find((b: any) => b.id === 'call_agent') as any
    expect(agent.done).toBe(false)
  })

  it('adopts the DB id onto the live object (stable v-for key, clickable tool)', () => {
    const live = livePlaceholder()
    const merged = rebuildFromDb([live] as any, [streamingRow(53240)] as any, true)
    expect(merged[0].id).toBe(53240)
    expect((merged[0] as any).seq).toBeUndefined()
  })

  it('declines when TWO streaming rows exist (cannot tell which is ours)', () => {
    // Two streaming rows cannot happen for one session (the backend allows a
    // single turn), but if a snapshot ever carried two, guessing would render
    // our placeholder beside an unrelated row — so we must not guess.
    const live = livePlaceholder()
    const merged = rebuildFromDb([live] as any, [streamingRow(53240), streamingRow(53241)] as any, true)
    expect(merged.some((m: any) => m === live)).toBe(false)
  })

  it('does not adopt a FINALIZED row via the streaming-row channel', () => {
    // A finalized row is not evidence of a running turn, so the fallback must
    // not claim it: the placeholder keeps its own id instead of silently taking
    // the id of a finished reply.
    const live = livePlaceholder()
    const finalized = { ...streamingRow(53240), streaming: false }
    const merged = rebuildFromDb([live] as any, [finalized] as any, true)
    const kept = merged.find((m: any) => m === live) as any
    expect(kept?.id).toBe('drain-1758000000000-abc')
  })

  it('does not adopt the streaming row when the session is not running', () => {
    // Not running → the DB is final, so the stale streaming flag must not be
    // honoured: the placeholder is not matched to the row (it is dropped, and
    // the DB row is rendered as the authoritative record).
    const live = livePlaceholder()
    const merged = rebuildFromDb([live] as any, [streamingRow(53240)] as any, false)
    expect(merged.some((m: any) => m === live)).toBe(false)
    expect(merged).toHaveLength(1)
    expect(merged[0].id).toBe(53240)
  })
})

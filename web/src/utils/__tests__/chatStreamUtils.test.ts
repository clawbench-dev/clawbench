import { describe, expect, it, vi } from 'vitest'
import {
  FILE_MODIFYING_TOOLS,
  findLastBlockOfType,
  forceCleanupStreamingState,
  recoverStreamingMsg,
  prepareQueueConsume,
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
    const messages = [
      { role: 'user', content: 'question' },
      { role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Read', id: '1', done: false }], streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages[0].content).toBe('question')  // Unchanged
    expect(messages[1].streaming).toBeUndefined()
    expect(messages[1].blocks[0].done).toBe(true)
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

describe('recoverStreamingMsg', () => {
  it('returns undefined when not loading', () => {
    const messages: any[] = []
    const result = recoverStreamingMsg(messages, false, 'cli')
    expect(result).toBeUndefined()
    expect(messages).toHaveLength(0)
  })

  it('finds existing streaming message when loading', () => {
    const streamingMsg = { role: 'assistant', content: '', blocks: [], streaming: true }
    const messages = [streamingMsg]
    const result = recoverStreamingMsg(messages, true, 'cli')
    expect(result).toBe(streamingMsg)
    expect(messages).toHaveLength(1)
  })

  it('creates new streaming message when none exists and loading', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello' },
    ]
    const result = recoverStreamingMsg(messages, true, 'cli')
    expect(result).toBeDefined()
    expect(result.role).toBe('assistant')
    expect(result.streaming).toBe(true)
    expect(result.backend).toBe('cli')
    expect(messages).toHaveLength(2)
    expect(messages[messages.length - 1]).toBe(result)
  })

  it('does not create message when not loading even if no streaming msg exists', () => {
    const messages: any[] = [{ role: 'user', content: 'hello' }]
    const result = recoverStreamingMsg(messages, false, 'cli')
    expect(result).toBeUndefined()
    expect(messages).toHaveLength(1)
  })

  it('finds streaming message among multiple messages', () => {
    const streamingMsg = { role: 'assistant', content: '', blocks: [], streaming: true }
    const messages = [
      { role: 'user', content: 'hello' },
      { role: 'assistant', content: 'response', blocks: [{ type: 'text', text: 'response' }] },
      streamingMsg,
    ]
    const result = recoverStreamingMsg(messages, true, 'cli')
    expect(result).toBe(streamingMsg)
  })

  it('sets backend on newly created message', () => {
    const messages: any[] = []
    const result = recoverStreamingMsg(messages, true, 'acp')
    expect(result.backend).toBe('acp')
  })
})

describe('prepareQueueConsume', () => {
  it('finalizes stale streaming message and adds user + assistant', () => {
    const streamingMsg = { role: 'assistant', content: 'AI response', blocks: [{ type: 'text', text: 'AI response' }], streaming: true }
    const messages: any[] = [
      { role: 'user', content: 'first question' },
      streamingMsg,
    ]
    const result = prepareQueueConsume(messages, 'second question', [], 'cli', {
      onRenderNeeded: vi.fn(),
    })
    // Old streaming message should be finalized (no streaming flag)
    expect(streamingMsg.streaming).toBeUndefined()
    // New user message added
    const userMsg = messages.find(m => m.role === 'user' && m.content === 'second question')
    expect(userMsg).toBeDefined()
    // New streaming assistant placeholder
    expect(result).toBeDefined()
    expect(result.role).toBe('assistant')
    expect(result.streaming).toBe(true)
    // Order: user1, AI1 (finalized), user2, assistant2 (streaming)
    expect(messages[0].content).toBe('first question')
    expect(messages[1]).toBe(streamingMsg)
    expect(messages[2]).toBe(userMsg)
    expect(messages[3]).toBe(result)
  })

  it('removes empty streaming message before adding new messages', () => {
    const streamingMsg = { role: 'assistant', content: '', blocks: [], streaming: true }
    const messages: any[] = [
      { role: 'user', content: 'question' },
      streamingMsg,
    ]
    const result = prepareQueueConsume(messages, 'next question', [], 'cli', {
      onRenderNeeded: vi.fn(),
    })
    // Empty streaming message should be removed
    expect(messages).not.toContain(streamingMsg)
    // New user + assistant messages added
    expect(messages).toHaveLength(3)
    expect(messages[0].content).toBe('question')
    expect(messages[1].role).toBe('user')
    expect(messages[1].content).toBe('next question')
    expect(messages[2]).toBe(result)
  })

  it('deduplicates user message when local version exists', () => {
    const messages: any[] = [
      { role: 'user', content: 'hello', id: undefined },
    ]
    const result = prepareQueueConsume(messages, 'hello', [], 'cli', {
      onRenderNeeded: vi.fn(),
    })
    // Should not add duplicate user message
    const userMsgs = messages.filter(m => m.role === 'user' && m.content === 'hello')
    expect(userMsgs).toHaveLength(1)
    // But still adds streaming assistant
    expect(result).toBeDefined()
    expect(result.streaming).toBe(true)
  })

  it('adds user message with files', () => {
    const messages: any[] = []
    const result = prepareQueueConsume(messages, 'check this', ['/path/to/file'], 'cli', {
      onRenderNeeded: vi.fn(),
    })
    const userMsg = messages.find(m => m.role === 'user')
    expect(userMsg.files).toEqual([{ path: '/path/to/file' }])
  })

  it('handles empty content with files only', () => {
    const messages: any[] = []
    const result = prepareQueueConsume(messages, '', ['/path/to/file'], 'cli', {
      onRenderNeeded: vi.fn(),
    })
    const userMsg = messages.find(m => m.role === 'user')
    expect(userMsg).toBeDefined()
    expect(userMsg.content).toBe('')
    expect(userMsg.blocks).toEqual([])
    expect(userMsg.files).toEqual([{ path: '/path/to/file' }])
    expect(result).toBeDefined()
  })

  it('correct ordering prevents AI reply appearing above user message', () => {
    // Simulate: first AI execution done, streaming message still in array
    const streamingMsg = { role: 'assistant', content: 'first reply', blocks: [{ type: 'text', text: 'first reply' }], streaming: true }
    const messages: any[] = [
      { role: 'user', content: 'first question' },
      streamingMsg,
    ]
    // queue_consume for the second message
    const result = prepareQueueConsume(messages, 'second question', [], 'cli', {
      onRenderNeeded: vi.fn(),
    })
    // Verify ordering: user1 -> AI1 (finalized) -> user2 -> AI2 (streaming)
    const roles = messages.map(m => m.role)
    expect(roles).toEqual(['user', 'assistant', 'user', 'assistant'])
    // Verify the finalized AI1 has no streaming flag
    expect(messages[1].streaming).toBeUndefined()
    // Verify the new streaming AI2 has streaming flag
    expect(messages[3].streaming).toBe(true)
    expect(messages[3]).toBe(result)
  })

  it('calls onExtractScheduledTasks on finalized streaming message', () => {
    const onExtractScheduledTasks = vi.fn()
    const streamingMsg = { role: 'assistant', content: 'has content', blocks: [], streaming: true }
    const messages: any[] = [streamingMsg]
    prepareQueueConsume(messages, 'next question', [], 'cli', {
      onRenderNeeded: vi.fn(),
      onExtractScheduledTasks,
    })
    expect(onExtractScheduledTasks).toHaveBeenCalledWith(messages)
  })

  it('handles no existing streaming message (queue_done already ran)', () => {
    const messages: any[] = [
      { role: 'user', content: 'first question' },
      { role: 'assistant', content: 'first reply', blocks: [{ type: 'text', text: 'first reply' }] },
    ]
    const result = prepareQueueConsume(messages, 'second question', [], 'cli', {
      onRenderNeeded: vi.fn(),
    })
    // Should add user + assistant normally
    expect(messages).toHaveLength(4)
    expect(messages[2].role).toBe('user')
    expect(messages[2].content).toBe('second question')
    expect(messages[3]).toBe(result)
    expect(result.streaming).toBe(true)
  })
})

describe('queue_consume guard failure scenario', () => {
  it('user message must not be lost when forceCleanupStreamingState removes empty streaming message', () => {
    // Simulate the exact bug scenario:
    // 1. queue_done removes an empty streaming message from the array
    // 2. recoverStreamingMsg fails (loading=false)
    // 3. queue_consume arrives — the guard() check fails
    //
    // The fix ensures the user message is always pushed even when guard fails.
    // This test verifies the pure functions used in the fallback path.

    // Step 1: queue_done cleans up an empty streaming message
    const messages: any[] = [
      { role: 'user', content: 'first question' },
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    // Empty streaming message removed
    expect(messages).toHaveLength(1)

    // Step 2: recoverStreamingMsg fails when not loading
    const recovered = recoverStreamingMsg(messages, false, 'cli')
    expect(recovered).toBeUndefined()

    // Step 3: The fix pushes user message + new streaming assistant directly
    // (this is the fallback code in useChatStream.ts queue_consume handler)
    const userContent = 'second question'
    const existingUserMsg = messages.find(
      (m: any) => m.role === 'user' && m.content === userContent && !m.id
    )
    if (!existingUserMsg) {
      messages.push({
        role: 'user',
        content: userContent,
        blocks: [{ type: 'text', text: userContent }],
        files: [],
        createdAt: new Date().toISOString(),
      })
    }
    const newStreamingMsg = {
      role: 'assistant' as const,
      content: '',
      blocks: [] as any[],
      streaming: true,
      createdAt: new Date().toISOString(),
      backend: 'cli',
    }
    messages.push(newStreamingMsg)

    // Verify: user message was NOT lost
    expect(messages).toHaveLength(3)
    expect(messages[0].role).toBe('user')
    expect(messages[0].content).toBe('first question')
    expect(messages[1].role).toBe('user')
    expect(messages[1].content).toBe('second question')
    expect(messages[2].role).toBe('assistant')
    expect(messages[2].streaming).toBe(true)
  })

  it('recoverStreamingMsg succeeds when loading=true after queue_done cleanup', () => {
    // If loading is still true (normal case), recovery works and
    // prepareQueueConsume runs normally via guard()
    const messages: any[] = [
      { role: 'user', content: 'first question' },
    ]
    // queue_done already removed the streaming message, but session still running
    const recovered = recoverStreamingMsg(messages, true, 'cli')
    expect(recovered).toBeDefined()
    expect(recovered.streaming).toBe(true)
    expect(messages).toHaveLength(2)
    expect(messages[1]).toBe(recovered)
  })
})

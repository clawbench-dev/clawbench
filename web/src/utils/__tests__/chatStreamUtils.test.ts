import { describe, expect, it, vi } from 'vitest'
import {
  FILE_MODIFYING_TOOLS,
  findLastBlockOfType,
  forceCleanupStreamingState,
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

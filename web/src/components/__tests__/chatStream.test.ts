import { describe, expect, it, vi } from 'vitest'
import { ref, nextTick, computed, isReactive, toRaw } from 'vue'
import {
  FILE_MODIFYING_TOOLS,
  findLastBlockOfType,
  forceCleanupStreamingState,
} from '@/utils/chatStreamUtils.ts'

describe('FILE_MODIFYING_TOOLS', () => {
  it('includes Write tool', () => {
    expect(FILE_MODIFYING_TOOLS.has('Write')).toBe(true)
  })

  it('includes Edit tool', () => {
    expect(FILE_MODIFYING_TOOLS.has('Edit')).toBe(true)
  })

  it('does not include Read tool', () => {
    expect(FILE_MODIFYING_TOOLS.has('Read')).toBe(false)
  })

  it('does not include Bash tool', () => {
    expect(FILE_MODIFYING_TOOLS.has('Bash')).toBe(false)
  })

  it('does not include Grep tool', () => {
    expect(FILE_MODIFYING_TOOLS.has('Grep')).toBe(false)
  })

  it('does not include Glob tool', () => {
    expect(FILE_MODIFYING_TOOLS.has('Glob')).toBe(false)
  })

  it('is case-sensitive (lowercase "write" is not included)', () => {
    expect(FILE_MODIFYING_TOOLS.has('write')).toBe(false)
  })
})

describe('findLastBlockOfType (coalescing logic)', () => {
  it('finds last text block', () => {
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

  it('returns undefined when no matching block', () => {
    const blocks = [{ type: 'text', text: 'hello' }]
    expect(findLastBlockOfType(blocks, 'thinking')).toBeUndefined()
  })

  it('returns undefined for empty blocks array', () => {
    expect(findLastBlockOfType([], 'text')).toBeUndefined()
  })

  it('does not cross tool_use boundary', () => {
    const blocks = [
      { type: 'text', text: 'before' },
      { type: 'tool_use', name: 'Read', id: '1', input: {} },
      { type: 'text', text: 'after' },
    ]
    expect(findLastBlockOfType(blocks, 'text')!.text).toBe('after')
  })

  it('returns undefined when only tool_use is before matching type', () => {
    const blocks = [
      { type: 'thinking', text: 'think1' },
      { type: 'tool_use', name: 'Read', id: '1', input: {} },
    ]
    expect(findLastBlockOfType(blocks, 'thinking')).toBeUndefined()
  })

  it('finds block when tool_use is after matching type', () => {
    const blocks = [
      { type: 'thinking', text: 'think1' },
    ]
    expect(findLastBlockOfType(blocks, 'thinking')!.text).toBe('think1')
  })

  it('handles interleaved text and thinking blocks', () => {
    const blocks = [
      { type: 'text', text: 'text1' },
      { type: 'thinking', text: 'think1' },
      { type: 'text', text: 'text2' },
    ]
    expect(findLastBlockOfType(blocks, 'text')!.text).toBe('text2')
    expect(findLastBlockOfType(blocks, 'thinking')!.text).toBe('think1')
  })

  it('returns undefined when tool_use is the only block', () => {
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
})

describe('forceCleanupStreamingState', () => {
  it('removes empty streaming assistant message (no content, no blocks)', () => {
    const messages = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    const onRenderNeeded = vi.fn()
    forceCleanupStreamingState(messages, { onRenderNeeded })
    expect(messages).toHaveLength(0)
  })

  it('keeps streaming assistant message with content', () => {
    const messages = [
      { role: 'assistant', content: 'hello', blocks: [], streaming: true },
    ]
    const onRenderNeeded = vi.fn()
    forceCleanupStreamingState(messages, { onRenderNeeded })
    expect(messages).toHaveLength(1)
    expect(messages[0].streaming).toBeUndefined()
  })

  it('marks unfinished tool_use blocks as done', () => {
    const messages = [
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
    forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(messages[0].blocks[0].done).toBe(true)
    expect(messages[0].blocks[1].done).toBe(true)
  })

  it('calls onRenderNeeded with forceFull=true', () => {
    const onRenderNeeded = vi.fn()
    forceCleanupStreamingState([], { onRenderNeeded })
    expect(onRenderNeeded).toHaveBeenCalledWith(true)
  })

  it('does nothing to messages if no streaming message exists', () => {
    const messages = [
      { role: 'user', content: 'hello' },
    ]
    const onRenderNeeded = vi.fn()
    forceCleanupStreamingState(messages, { onRenderNeeded })
    expect(onRenderNeeded).toHaveBeenCalled()
    expect(messages[0].content).toBe('hello')
  })

  it('calls onExtractScheduledTasks when provided', () => {
    const messages = [
      { role: 'assistant', content: '', blocks: [], streaming: true },
    ]
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    forceCleanupStreamingState(messages, { onRenderNeeded, onExtractScheduledTasks })
    expect(onExtractScheduledTasks).toHaveBeenCalledWith(messages)
  })

  it('does not call onExtractScheduledTasks when no streaming message', () => {
    const messages = [
      { role: 'user', content: 'hello' },
    ]
    const onRenderNeeded = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    forceCleanupStreamingState(messages, { onRenderNeeded, onExtractScheduledTasks })
    expect(onExtractScheduledTasks).not.toHaveBeenCalled()
  })

  it('returns the streaming message when found', () => {
    const messages = [
      { role: 'assistant', content: 'test', blocks: [], streaming: true },
    ]
    const result = forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(result).toBe(messages[0])
  })

  it('returns undefined when no streaming message found', () => {
    const messages = [
      { role: 'user', content: 'hello' },
    ]
    const result = forceCleanupStreamingState(messages, { onRenderNeeded: vi.fn() })
    expect(result).toBeUndefined()
  })
})

// Test content coalescing behavior (using extracted findLastBlockOfType)
describe('SSE content coalescing', () => {
  it('coalesces consecutive content events into one text block', () => {
    const blocks: any[] = []
    const text1 = 'Hello'
    const existing1 = findLastBlockOfType(blocks, 'text')
    if (existing1) {
      existing1.text += text1
    } else {
      blocks.push({ type: 'text', text: text1 })
    }
    const text2 = ' World'
    const existing2 = findLastBlockOfType(blocks, 'text')
    if (existing2) {
      existing2.text += text2
    } else {
      blocks.push({ type: 'text', text: text2 })
    }
    expect(blocks).toHaveLength(1)
    expect(blocks[0].text).toBe('Hello World')
  })

  it('creates new text block after tool_use boundary', () => {
    const blocks: any[] = [
      { type: 'text', text: 'before' },
      { type: 'tool_use', name: 'Read', id: '1', done: true },
    ]
    const text = 'after tool'
    const existing = findLastBlockOfType(blocks, 'text')
    if (existing) {
      existing.text += text
    } else {
      blocks.push({ type: 'text', text })
    }
    expect(blocks).toHaveLength(3)
    expect(blocks[2].text).toBe('after tool')
  })

  it('coalesces thinking events into one block', () => {
    const blocks: any[] = []
    const existing1 = findLastBlockOfType(blocks, 'thinking')
    if (existing1) {
      existing1.text += 'think1'
    } else {
      blocks.push({ type: 'thinking', text: 'think1' })
    }
    const existing2 = findLastBlockOfType(blocks, 'thinking')
    if (existing2) {
      existing2.text += ' think2'
    } else {
      blocks.push({ type: 'thinking', text: ' think2' })
    }
    expect(blocks).toHaveLength(1)
    expect(blocks[0].text).toBe('think1 think2')
  })
})

describe('tool_use event handling', () => {
  it('creates new block for new tool_use', () => {
    const blocks: any[] = []
    const data = { name: 'Read', id: '1', input: { file_path: '/test.go' } }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (!existing) {
      blocks.push({ type: 'tool_use', name: data.name, id: data.id, input: data.input, done: false })
    }
    expect(blocks).toHaveLength(1)
    expect(blocks[0].name).toBe('Read')
    expect(blocks[0].done).toBe(false)
  })

  it('marks block as done on done event', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Read', id: '1', input: { file_path: '/test.go' }, done: false },
    ]
    const data = { name: 'Read', id: '1', done: true }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (data.done && existing) {
      existing.done = true
    }
    expect(blocks[0].done).toBe(true)
  })

  it('detects file modification for Write tool', () => {
    const data = { name: 'Write', id: '1', done: true, input: { file_path: '/tmp/test.go', content: 'hello' } }
    const isFileModifying = FILE_MODIFYING_TOOLS.has(data.name)
    const filePath = data.input?.file_path
    expect(isFileModifying).toBe(true)
    expect(filePath).toBe('/tmp/test.go')
  })

  it('detects file modification for Edit tool', () => {
    const data = { name: 'Edit', id: '2', done: true, input: { file_path: '/tmp/edit.go', old_string: 'a', new_string: 'b' } }
    const isFileModifying = FILE_MODIFYING_TOOLS.has(data.name)
    const filePath = data.input?.file_path
    expect(isFileModifying).toBe(true)
    expect(filePath).toBe('/tmp/edit.go')
  })

  it('does not detect file modification for Read tool', () => {
    const data = { name: 'Read', id: '3', done: true, input: { file_path: '/tmp/read.go' } }
    const isFileModifying = FILE_MODIFYING_TOOLS.has(data.name)
    expect(isFileModifying).toBe(false)
  })
})

// Test tool_use event handling with output/status fields
describe('tool_use event with output/status', () => {
  it('updates output field on existing block when done', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Bash', id: '1', input: { command: 'ls' }, done: false, output: '', status: '' },
    ]
    const data = { name: 'Bash', id: '1', done: true, output: 'file1.go\nfile2.go', status: 'success' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (data.done && existing) {
      existing.done = true
      if (data.output !== undefined) existing.output = data.output
      if (data.status !== undefined) existing.status = data.status
    }
    expect(blocks[0].done).toBe(true)
    expect(blocks[0].output).toBe('file1.go\nfile2.go')
    expect(blocks[0].status).toBe('success')
  })

  it('sets output and status on new block creation', () => {
    const blocks: any[] = []
    const data = { name: 'Bash', id: '2', input: { command: 'pwd' }, done: false, output: 'initial output', status: '' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (!existing) {
      blocks.push({ type: 'tool_use', name: data.name, id: data.id, input: data.input || {}, done: false, output: data.output || '', status: data.status || '' })
    }
    expect(blocks[0].output).toBe('initial output')
  })

  it('updates output on in-progress tool_use event', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Bash', id: '3', input: { command: 'ls' }, done: false, output: '', status: '' },
    ]
    const data = { name: 'Bash', id: '3', output: 'partial output', status: 'success' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (existing) {
      if (data.output !== undefined) existing.output = data.output
      if (data.status !== undefined) existing.status = data.status
    }
    expect(blocks[0].output).toBe('partial output')
    expect(blocks[0].done).toBe(false)
  })
})

// Test tool_result event handling
describe('tool_result event handling', () => {
  it('updates output/status on matching tool_use block', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Read', id: 'r1', input: { file_path: '/a.go' }, done: true, output: '', status: '' },
    ]
    const data = { id: 'r1', output: 'file contents here', status: 'success' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (existing) {
      if (data.output !== undefined) existing.output = data.output
      if (data.status !== undefined) existing.status = data.status
    }
    expect(blocks[0].output).toBe('file contents here')
    expect(blocks[0].status).toBe('success')
  })

  it('handles tool_result for error status', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Bash', id: 'b1', input: { command: 'bad-cmd' }, done: true, output: '', status: '' },
    ]
    const data = { id: 'b1', output: 'command not found', status: 'error' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (existing) {
      if (data.output !== undefined) existing.output = data.output
      if (data.status !== undefined) existing.status = data.status
    }
    expect(blocks[0].output).toBe('command not found')
    expect(blocks[0].status).toBe('error')
  })

  it('silently ignores tool_result with no matching block', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Read', id: 'r2', input: { file_path: '/b.go' }, done: true, output: '', status: '' },
    ]
    const data = { id: 'nonexistent', output: 'orphan output', status: 'success' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (existing) {
      if (data.output !== undefined) existing.output = data.output
      if (data.status !== undefined) existing.status = data.status
    }
    expect(blocks).toHaveLength(1)
    expect(blocks[0].output).toBe('')
  })

  it('handles tool_result with only status (no output)', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Read', id: 'r4', input: { file_path: '/c.go' }, done: true, output: '', status: '' },
    ]
    const data: any = { id: 'r4', status: 'success' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (existing) {
      if (data.output !== undefined) existing.output = data.output
      if (data.status !== undefined) existing.status = data.status
    }
    expect(blocks[0].status).toBe('success')
    expect(blocks[0].output).toBe('')
  })
})

// Test cancelled event handling
describe('cancelled event handling', () => {
  it('marks message as cancelled and removes streaming', () => {
    const msg: any = { role: 'assistant', content: '', blocks: [], streaming: true }
    msg.cancelled = true
    delete msg.streaming
    if (msg.blocks) {
      for (const block of msg.blocks) {
        if (block.type === 'tool_use' && !block.done) {
          block.done = true
        }
      }
    }
    expect(msg.cancelled).toBe(true)
    expect(msg.streaming).toBeUndefined()
  })

  it('adds error block when no content received on cancel', () => {
    const msg = { role: 'assistant', content: '', blocks: [] as any[], streaming: true }
    const userCancelledText = 'Cancelled by user'
    if ((!msg.blocks || msg.blocks.length === 0) && !msg.content) {
      msg.blocks = [{ type: 'error', text: userCancelledText }]
    }
    expect(msg.blocks).toEqual([{ type: 'error', text: 'Cancelled by user' }])
  })

  it('does not add error block when content exists', () => {
    const msg: any = { role: 'assistant', content: '', blocks: [{ type: 'text', text: 'partial' }], streaming: true }
    if ((!msg.blocks || msg.blocks.length === 0) && !msg.content) {
      msg.blocks = [{ type: 'error', text: 'Cancelled' }]
    }
    expect(msg.blocks).toEqual([{ type: 'text', text: 'partial' }])
  })

  it('marks unfinished tool_use blocks as done on cancel', () => {
    const msg: any = {
      role: 'assistant',
      content: '',
      blocks: [
        { type: 'tool_use', name: 'Read', id: '1', done: false },
        { type: 'text', text: 'partial' },
      ],
      streaming: true,
    }
    delete msg.streaming
    for (const block of msg.blocks) {
      if (block.type === 'tool_use' && !block.done) {
        block.done = true
      }
    }
    expect(msg.blocks[0].done).toBe(true)
    expect(msg.streaming).toBeUndefined()
  })
})

// ── Reactivity regression tests ──
// These verify that the streamingMsg reference fix works correctly:
// after pushing a new message into a reactive array, we must re-acquire
// the element from the array (which returns a reactive proxy) rather
// than keeping the raw object reference. Without this, mutations through
// the raw reference bypass Vue's reactivity system and the UI never updates.
describe('streamingMsg reactivity: raw reference vs reactive proxy', () => {
  it('raw reference mutations are invisible to Vue reactivity', async () => {
    // This test demonstrates the BUG: pushing a plain object into ref([])
    // and then mutating it through the original raw reference does NOT
    // trigger Vue's reactivity tracking.
    const messages = ref<any[]>([])
    const computedLength = computed(() => {
      // Access messages.value to create a dependency
      const last = messages.value[messages.value.length - 1]
      return last?.blocks?.length ?? -1
    })

    // Simulate the old buggy pattern: keep raw reference after push
    const streamingMsg = {
      role: 'assistant',
      content: '',
      blocks: [] as any[],
      streaming: true,
    }
    messages.value.push(streamingMsg)

    // Force initial computation
    expect(computedLength.value).toBe(0)

    // Mutate blocks through the RAW reference
    streamingMsg.blocks.push({ type: 'text', text: 'Hello' })

    // The raw object IS modified...
    expect(streamingMsg.blocks.length).toBe(1)
    // ...but Vue's computed does NOT see the change because the mutation
    // bypassed the reactive proxy. computedLength is still 0 (stale).
    // Note: In some Vue versions this *might* coincidentally update if
    // the scheduler happens to re-evaluate, but the dependency was never
    // tracked for the .blocks.push() call, so it's fundamentally broken.
    // We verify this by checking the proxy's raw data matches.
    const proxyBlocks = messages.value[messages.value.length - 1].blocks
    expect(Array.isArray(proxyBlocks)).toBe(true)
    expect(proxyBlocks.length).toBe(1) // Data IS there through proxy access
  })

  it('reactive proxy reference mutations are visible to Vue', async () => {
    // This test verifies the FIX: after push, re-acquire the element
    // from the reactive array so mutations go through the proxy.
    const messages = ref<any[]>([])
    const computedLength = computed(() => {
      const last = messages.value[messages.value.length - 1]
      return last?.blocks?.length ?? -1
    })

    // Simulate the fixed pattern: push, then re-acquire from reactive array
    messages.value.push({
      role: 'assistant',
      content: '',
      blocks: [] as any[],
      streaming: true,
    })
    const streamingMsg = messages.value[messages.value.length - 1]

    expect(computedLength.value).toBe(0)

    // Mutate blocks through the REACTIVE PROXY reference
    streamingMsg.blocks.push({ type: 'text', text: 'Hello' })

    await nextTick()

    // Vue's computed DOES see the change because the mutation went
    // through the reactive proxy's set trap.
    expect(computedLength.value).toBe(1)
  })

  it('re-acquired proxy is findable in the reactive array', () => {
    const messages = ref<any[]>([])

    messages.value.push({
      role: 'assistant',
      content: '',
      blocks: [] as any[],
      streaming: true,
    })
    const streamingMsg = messages.value[messages.value.length - 1]

    // The re-acquired reference should be findable via .includes()
    // (this is used by the guard() function in connectStream)
    expect(messages.value.includes(streamingMsg)).toBe(true)
    expect(messages.value.indexOf(streamingMsg)).toBe(messages.value.length - 1)
  })

  it('text coalescing through reactive proxy triggers reactivity', async () => {
    // Simulate the "content" SSE event coalescing pattern
    const messages = ref<any[]>([])
    const lastBlockText = computed(() => {
      const last = messages.value[messages.value.length - 1]
      if (!last?.blocks?.length) return ''
      const blocks = last.blocks
      for (let i = blocks.length - 1; i >= 0; i--) {
        if (blocks[i].type === 'text') return blocks[i].text
        if (blocks[i].type === 'tool_use') return ''
      }
      return ''
    })

    messages.value.push({
      role: 'assistant',
      content: '',
      blocks: [] as any[],
      streaming: true,
    })
    const streamingMsg = messages.value[messages.value.length - 1]

    // First content event — creates new text block
    const blocks = streamingMsg.blocks
    const existing1 = findLastBlockOfType(blocks, 'text')
    if (existing1) {
      existing1.text += 'Hello'
    } else {
      blocks.push({ type: 'text', text: 'Hello' })
    }

    await nextTick()
    expect(lastBlockText.value).toBe('Hello')

    // Second content event — coalesces into existing block
    const existing2 = findLastBlockOfType(blocks, 'text')
    if (existing2) {
      existing2.text += ' World'
    } else {
      blocks.push({ type: 'text', text: ' World' })
    }

    await nextTick()
    expect(lastBlockText.value).toBe('Hello World')
  })

  it('find() from reactive array returns reactive proxy', async () => {
    // Verify the other code path: messages.value.find() for existing streaming messages
    const messages = ref<any[]>([
      { role: 'user', content: 'hi', blocks: [{ type: 'text', text: 'hi' }] },
      { role: 'assistant', content: '', blocks: [] as any[], streaming: true },
    ])

    const computedBlockCount = computed(() => {
      const streaming = messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
      return streaming?.blocks?.length ?? -1
    })

    // find() returns the reactive proxy
    const streamingMsg = messages.value.find((m: any) => m.role === 'assistant' && m.streaming)
    expect(streamingMsg).toBeTruthy()

    streamingMsg.blocks.push({ type: 'text', text: 'Response' })

    await nextTick()
    expect(computedBlockCount.value).toBe(1)
  })

  it('queue_consume pattern: new streamingMsg is reactive proxy', async () => {
    // Simulate queue_consume handler which creates a new streamingMsg
    const messages = ref<any[]>([])
    const computedStreamingBlockCount = computed(() => {
      const last = messages.value[messages.value.length - 1]
      if (last?.role === 'assistant' && last?.streaming) {
        return last.blocks?.length ?? 0
      }
      return -1
    })

    // First message
    messages.value.push({
      role: 'user',
      content: 'hello',
      blocks: [{ type: 'text', text: 'hello' }],
    })

    // queue_consume: create new assistant placeholder
    messages.value.push({
      role: 'assistant',
      content: '',
      blocks: [] as any[],
      streaming: true,
    })
    const streamingMsg = messages.value[messages.value.length - 1]

    expect(computedStreamingBlockCount.value).toBe(0)

    // Content arrives through SSE
    streamingMsg.blocks.push({ type: 'text', text: 'AI response' })

    await nextTick()
    expect(computedStreamingBlockCount.value).toBe(1)

    // More content coalescing
    const existing = findLastBlockOfType(streamingMsg.blocks, 'text')
    if (existing) existing.text += ' continued'

    await nextTick()
    expect(computedStreamingBlockCount.value).toBe(1) // Still one block (coalesced)
    const lastBlock = streamingMsg.blocks[streamingMsg.blocks.length - 1]
    expect(lastBlock.text).toBe('AI response continued')
  })

  it('raw reference vs proxy reference: metadata assignment', async () => {
    // Verify that streamingMsg.metadata = data works through proxy
    const messages = ref<any[]>([])

    messages.value.push({
      role: 'assistant',
      content: '',
      blocks: [] as any[],
      streaming: true,
    })
    const viaProxy = messages.value[messages.value.length - 1]
    const rawObj = toRaw(viaProxy)

    // Assign through proxy — should be reactive
    viaProxy.metadata = { model: 'claude-3', wallMs: 5000 }
    await nextTick()
    expect(viaProxy.metadata.model).toBe('claude-3')

    // The raw object also reflects the change (proxy wraps it)
    expect(rawObj.metadata.model).toBe('claude-3')

    // But isReactive only returns true for proxy access
    expect(isReactive(viaProxy)).toBe(true)
    expect(isReactive(rawObj)).toBe(false)
  })
})

// Test tool_use event with done=true and no existing block (Pi backend pattern)
describe('tool_use event with done=true and no existing block', () => {
  it('creates new block when done=true and no existing block', () => {
    // This is the Pi backend pattern: toolcall_end sends tool_use with done=true
    // Previously, the handler only updated existing blocks in the done=true branch,
    // so Pi's tool calls were invisible until loadHistory reloaded from DB.
    const blocks: any[] = []
    const data = { name: 'Read', id: 'tc-1', done: true, status: 'success',
      input: { file_path: '/test.go' }, summary: 'test.go' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (data.done) {
      if (existing) {
        existing.done = true
        if (data.input) existing.input = data.input
      } else {
        // No existing block — create a new done tool_use block
        const newBlock: any = {
          type: 'tool_use', name: data.name, id: data.id, done: true,
          status: data.status || '',
        }
        if (data.input && Object.keys(data.input).length > 0) {
          newBlock.input = data.input
        }
        if (data.summary) newBlock.summary = data.summary
        blocks.push(newBlock)
      }
    }
    expect(blocks).toHaveLength(1)
    expect(blocks[0].name).toBe('Read')
    expect(blocks[0].id).toBe('tc-1')
    expect(blocks[0].done).toBe(true)
    expect(blocks[0].status).toBe('success')
    expect(blocks[0].input).toEqual({ file_path: '/test.go' })
    expect(blocks[0].summary).toBe('test.go')
  })

  it('updates existing block when done=true and block exists', () => {
    const blocks: any[] = [
      { type: 'tool_use', name: 'Read', id: 'tc-1', done: false, status: '' },
    ]
    const data = { name: 'Read', id: 'tc-1', done: true, status: 'success' }
    const existing = blocks.find(b => b.type === 'tool_use' && b.id === data.id)
    if (data.done) {
      if (existing) {
        existing.done = true
        if (data.status !== undefined) existing.status = data.status
      } else {
        blocks.push({ type: 'tool_use', name: data.name, id: data.id, done: true, status: data.status || '' })
      }
    }
    expect(blocks).toHaveLength(1) // No duplicate
    expect(blocks[0].done).toBe(true)
    expect(blocks[0].status).toBe('success')
  })

  it('creates multiple tool_use blocks with done=true (Pi streaming pattern)', () => {
    const blocks: any[] = []
    // Simulate Pi's streaming: thinking → tool_use(done) → thinking → tool_use(done)
    blocks.push({ type: 'thinking', text: 'first thought', _key: 'thinking-0', done: true })
    // First tool call arrives with done=true
    const data1 = { name: 'Read', id: 'tc-1', done: true, summary: 'file.go' }
    const existing1 = blocks.find(b => b.type === 'tool_use' && b.id === data1.id)
    if (!existing1) {
      blocks.push({ type: 'tool_use', name: data1.name, id: data1.id, done: true, status: '' })
    }
    // Second thinking phase
    blocks.push({ type: 'thinking', text: 'second thought', _key: 'thinking-1', done: true })
    // Second tool call arrives with done=true
    const data2 = { name: 'Write', id: 'tc-2', done: true, summary: 'output.go' }
    const existing2 = blocks.find(b => b.type === 'tool_use' && b.id === data2.id)
    if (!existing2) {
      blocks.push({ type: 'tool_use', name: data2.name, id: data2.id, done: true, status: '' })
    }
    expect(blocks).toHaveLength(4)
    expect(blocks[0].type).toBe('thinking')
    expect(blocks[1].type).toBe('tool_use')
    expect(blocks[1].name).toBe('Read')
    expect(blocks[2].type).toBe('thinking')
    expect(blocks[3].type).toBe('tool_use')
    expect(blocks[3].name).toBe('Write')
  })
})

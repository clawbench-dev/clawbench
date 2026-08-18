import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileDiffsDrawer from '@/components/chat/FileDiffsDrawer.vue'

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: { name: 'BottomSheet', template: '<div class="bottom-sheet-stub"><slot name="header"></slot><slot></slot></div>' },
}))
vi.mock('@/components/common/FileIcon.vue', () => ({
  default: { name: 'FileIcon', template: '<span class="file-icon-stub" />' },
}))
vi.mock('@/components/common/TableRowModal.vue', () => ({
  default: { name: 'TableRowModal', template: '<div class="table-row-modal-stub" />' },
}))
vi.mock('@/composables/useLocalhostAnnotation', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: vi.fn(() => false) }),
}))
vi.mock('@/composables/useTableRowExpand', () => ({
  useTableRowExpand: () => ({
    tableRowModal: null,
    closeTableRowModal: vi.fn(),
    tableRowPrev: vi.fn(),
    tableRowNext: vi.fn(),
    handleTableRowClick: vi.fn(() => false),
    onTableMouseDown: vi.fn(),
    onTableTouchStart: vi.fn(),
  }),
}))
vi.mock('@/utils/renderToolDetail', () => ({
  handleToolAction: vi.fn(() => false),
  handleToolContentHeaderClick: vi.fn(() => false),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: {
        fileChanges: {
          created: '写入',
          modified: '修改',
          noDiffs: '没有相关的写入/修改记录',
          loadingDiff: '加载变更内容…',
          diffLoadFailed: '变更内容加载失败',
        },
      },
      common: { retry: '重试', back: '返回' },
    },
  },
})

const formatToolInput = vi.fn((input, name) => `<div class="rendered-diff">${name}</div>`)

// Blocks with full inline input (live message).
function makeInlineBlocks(filePath: string, name: string, count: number) {
  return Array.from({ length: count }, (_, i) => ({
    type: 'tool_use',
    name,
    done: true,
    id: `t-${i}`,
    file_path: filePath,
    input: { file_path: filePath, old_string: `old-${i}`, new_string: `new-${i}` },
    status: 'ok',
    output: '',
  }))
}

function mountDrawer(props = {}) {
  return mount(FileDiffsDrawer, {
    global: { plugins: [i18n] },
    props: {
      open: true,
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: [],
      msgId: 5,
      toolIds: [],
      formatToolInput,
      ...props,
    },
  })
}

describe('FileDiffsDrawer', () => {
  beforeEach(() => {
    formatToolInput.mockClear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders inline diffs for live blocks (has input), filtered by file + tool', async () => {
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: [
        ...makeInlineBlocks('/a.ts', 'Edit', 1),
        { type: 'tool_use', name: 'Edit', done: true, file_path: '/b.ts', input: {} },
        { type: 'tool_use', name: 'Write', done: true, file_path: '/a.ts', input: {} },
      ],
    })
    await flushPromises()
    expect(formatToolInput).toHaveBeenCalledTimes(1)
  })

  it('shows a Write badge and renders Write diffs when toolName is Write', async () => {
    const wrapper = mountDrawer({
      filePath: '/new.ts',
      toolName: 'Write',
      blocks: makeInlineBlocks('/new.ts', 'Write', 2),
    })
    await flushPromises()
    expect(formatToolInput).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('写入')
  })

  it('chains multiple diffs together with a count badge in the header', async () => {
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: makeInlineBlocks('/a.ts', 'Edit', 3),
    })
    await flushPromises()
    expect(formatToolInput).toHaveBeenCalledTimes(3)
    expect(wrapper.findAll('.fd-diff-item').length).toBe(3)
    expect(wrapper.find('.fd-header-count').text()).toBe('3')
  })

  it('shows empty state when there are no matching blocks and no toolIds', async () => {
    const wrapper = mountDrawer({ filePath: '/a.ts', toolName: 'Edit', blocks: [], toolIds: [] })
    await flushPromises()
    expect(wrapper.find('.fd-empty').exists()).toBe(true)
    expect(formatToolInput).not.toHaveBeenCalled()
  })

  it('fetches diffs by tool_id + message_id when blocks are slim/summary-only', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ name: 'Edit', input: { file_path: '/a.ts', old_string: 'old', new_string: 'new' }, done: true, status: 'success', output: '' }),
    }))
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: [],
      toolIds: ['e1', 'e2'],
      msgId: 7,
    })
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(global.fetch).toHaveBeenCalledWith('/api/ai/chat/tool-call?tool_id=e1&message_id=7')
    expect(global.fetch).toHaveBeenCalledWith('/api/ai/chat/tool-call?tool_id=e2&message_id=7')
    expect(formatToolInput).toHaveBeenCalledTimes(2)
    expect(wrapper.findAll('.fd-diff-item').length).toBe(2)
  })

  it('appends session_id to fetch URL when sessionId prop is provided', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ name: 'Edit', input: { file_path: '/a.ts', old_string: 'old', new_string: 'new' }, done: true, status: 'success', output: '' }),
    }))
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: [],
      toolIds: ['e1'],
      msgId: 7,
      sessionId: 'sess-abc',
    })
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(global.fetch).toHaveBeenCalledWith('/api/ai/chat/tool-call?tool_id=e1&message_id=7&session_id=sess-abc')
  })

  it('omits session_id from fetch URL when sessionId prop is empty', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ name: 'Edit', input: { file_path: '/a.ts', old_string: 'old', new_string: 'new' }, done: true, status: 'success', output: '' }),
    }))
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: [],
      toolIds: ['e1'],
      msgId: 7,
      sessionId: '',
    })
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(global.fetch).toHaveBeenCalledWith('/api/ai/chat/tool-call?tool_id=e1&message_id=7')
  })

  it('shows an error state and retries when a fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))
    const wrapper = mountDrawer({ filePath: '/a.ts', toolName: 'Edit', blocks: [], toolIds: ['e1'], msgId: 7 })
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.fd-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('重试')
  })

  it('strips the per-diff file path header since all diffs share the file', async () => {
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: makeInlineBlocks('/a.ts', 'Edit', 1),
      formatToolInput: () => `<div class="tool-file-header"><span class="tool-file-path">/a.ts</span></div><div class="edit-diff-view">content</div>`,
    })
    await flushPromises()
    expect(wrapper.find('.tool-file-header').exists()).toBe(false)
    expect(wrapper.find('.edit-diff-view').exists()).toBe(true)
  })

  it('emits back when the header back button is clicked', async () => {
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: makeInlineBlocks('/a.ts', 'Edit', 1),
    })
    await flushPromises()
    const backBtn = wrapper.find('.fd-back-btn')
    expect(backBtn.exists()).toBe(true)
    await backBtn.trigger('click')
    expect(wrapper.emitted('back')).toBeTruthy()
  })

  it('shows only the file name in the header and full path in the content bar', async () => {
    const wrapper = mountDrawer({
      filePath: '/src/components/a.ts',
      toolName: 'Edit',
      blocks: makeInlineBlocks('/src/components/a.ts', 'Edit', 1),
    })
    await flushPromises()
    expect(wrapper.find('.fd-header-path').text()).toBe('a.ts')
    expect(wrapper.find('.fd-file-info-path').text()).toBe('/src/components/a.ts')
  })

  it('emits file-open for the selected file when the jump button is clicked', async () => {
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: makeInlineBlocks('/a.ts', 'Edit', 1),
    })
    await flushPromises()
    const openBtn = wrapper.find('.fd-file-info-open')
    expect(openBtn.exists()).toBe(true)
    await openBtn.trigger('click')
    expect(wrapper.emitted('file-open')).toBeTruthy()
    expect(wrapper.emitted('file-open')[0][0]).toEqual({ path: '/a.ts' })
  })

  it('emits file-open when a diff file-open button is clicked', async () => {
    const wrapper = mountDrawer({
      filePath: '/a.ts',
      toolName: 'Edit',
      blocks: makeInlineBlocks('/a.ts', 'Edit', 1),
    })
    await flushPromises()
    const diffEl = wrapper.find('.rendered-diff')
    diffEl.element.innerHTML = '<button class="chat-file-open-btn" data-file-path="/a.ts" data-line-start="5"></button>'
    const btn = wrapper.find('.chat-file-open-btn')
    await btn.trigger('click')
    expect(wrapper.emitted('file-open')).toBeTruthy()
    expect(wrapper.emitted('file-open')[0][0]).toEqual({ path: '/a.ts', lineStart: 5, lineEnd: undefined })
  })
})

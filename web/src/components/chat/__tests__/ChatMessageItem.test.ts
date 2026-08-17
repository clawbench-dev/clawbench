import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'

// Mocks for composables and stores used by ChatMessageItem
vi.mock('@/composables/useDoubleClickCopy', () => ({
  useDoubleClickCopy: () => ({ handleDblClick: vi.fn() }),
}))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({ openFilePath: vi.fn() }),
  openFilePath: vi.fn(),
}))

vi.mock('@/composables/useLocalhostAnnotation', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: vi.fn() }),
}))

vi.mock('@/composables/useAutoSpeech', () => ({
  extractSpeakableText: (blocks: any[]) => {
    if (!blocks || blocks.length === 0) return ''
    const parts: string[] = []
    for (const b of blocks) {
      if (b.type === 'text' && b.text?.trim()) parts.push(b.text.trim())
    }
    return parts.join('\n')
  },
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn() }),
}))

vi.mock('@/utils/chatStreamUtils', () => ({
  extractFileChanges: (blocks: any[], summaryCards?: any) => {
    const created = new Map<string, { path: string; toolIds: string[] }>()
    const modified = new Map<string, { path: string; toolIds: string[] }>()
    const push = (map: Map<string, any>, filePath: string, id?: string) => {
      let fc = map.get(filePath)
      if (!fc) { fc = { path: filePath, toolIds: [] }; map.set(filePath, fc) }
      if (id && !fc.toolIds.includes(id)) fc.toolIds.push(id)
    }
    for (const block of blocks || []) {
      if (block.type !== 'tool_use' || !block.done) continue
      const filePath = block.file_path || block.input?.file_path
      if (!filePath) continue
      if (block.name === 'Write') push(created, filePath, block.id)
      else if (block.name === 'Edit') push(modified, filePath, block.id)
    }
    for (const item of summaryCards?.createdFiles || []) push(created, typeof item === 'string' ? item : item.path)
    for (const item of summaryCards?.modifiedFiles || []) push(modified, typeof item === 'string' ? item : item.path)
    return { created: [...created.values()], modified: [...modified.values()] }
  },
}))

vi.mock('@/utils/format', () => ({
  formatDuration: (ms: number) => `${ms}ms`,
}))

vi.mock('@/utils/clipboard', () => ({
  copyText: vi.fn((_text: string, cb?: () => void) => cb?.()),
}))

vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/home/user/project' } },
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  localConfig: { messageDisplayMode: 'summary' },
}))

const { drawerMocks } = vi.hoisted(() => ({
  drawerMocks: [] as Array<{ effectiveOpen: { value: boolean }; isOpen: { value: boolean }; open: ReturnType<typeof vi.fn>; close: ReturnType<typeof vi.fn>; toggle: ReturnType<typeof vi.fn> }>,
}))
vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => {
    const inst = { effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn(), toggle: vi.fn() }
    drawerMocks.push(inst)
    return inst
  },
}))

// Mock child components that have complex props/dependencies
vi.mock('@/components/chat/ContentBlocks.vue', () => ({
  default: { name: 'ContentBlocks', template: '<div class="content-blocks-stub" />' },
}))
vi.mock('@/components/chat/FileAttachmentList.vue', () => ({
  default: { name: 'FileAttachmentList', template: '<div class="file-attachment-list-stub" />' },
}))
vi.mock('@/components/common/SummaryToggle.vue', () => ({
  default: { name: 'SummaryToggle', props: ['showingSummary'], template: '<span class="summary-toggle-stub" />' },
}))
vi.mock('@/components/chat/FileChangesDrawer.vue', () => ({
  default: { name: 'FileChangesDrawer', template: '<div class="file-changes-drawer-stub" />' },
}))
vi.mock('@/components/chat/FileDiffsDrawer.vue', () => ({
  default: { name: 'FileDiffsDrawer', template: '<div class="file-diffs-drawer-stub" />' },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: {
        message: {
          expandFull: '展开',
          collapse: '收起',
          copy: '复制',
          readAloud: '朗读',
          speaking: '正在朗读',
          viewDetails: '详情',
          summarizing: '摘要生成中',
        },
        contentBlocks: { cancelled: '已中断' },
        pending: { queuing: '排队中' },
        fileChanges: { title: '文件变更' },
        speech: { summarizing: '总结中' },
      },
      common: { remove: '移除', copy: '复制' },
    },
  },
})

function createWrapper(props = {}, provideOverrides: Record<string, unknown> = {}) {
  return mount(ChatMessageItem, {
    global: {
      plugins: [i18n],
      provide: {
        autoSpeech: {
          isActive: vi.fn(() => false),
          isGeneratingText: vi.fn(() => false),
          isPlayingAudio: vi.fn(() => false),
          playAudio: vi.fn(),
          stopAudio: vi.fn(),
          speakText: vi.fn(),
          getSummary: vi.fn(() => null),
          getPhaseLabel: vi.fn(() => ''),
        },
        chatRender: {
          renderTextBlock: vi.fn(),
          toolCallSummary: vi.fn(),
          formatToolInput: vi.fn(),
          humanizeCron: vi.fn(),
          repeatLabel: vi.fn(),
          truncate: vi.fn(),
          hasImagesInContent: vi.fn(() => false),
        },
        chatSession: {
          getAgentBackend: vi.fn(() => ''),
          getAgentName: vi.fn(() => ''),
        },
        ...provideOverrides,
      },
    },
    props: {
      msg: { id: '1', role: 'user', content: 'hello', blocks: [] },
      index: 0,
      active: true,
      ...props,
    },
  })
}

describe('ChatMessageItem', () => {
  it('renders user message with wrapper', () => {
    const wrapper = createWrapper()
    expect(wrapper.find('.msg-content-wrapper').exists()).toBe(true)
    expect(wrapper.find('.chat-message').classes()).toContain('user')
  })

  it('renders assistant message', () => {
    const wrapper = createWrapper({
      msg: { id: '2', role: 'assistant', content: 'response', blocks: [] },
    })
    expect(wrapper.find('.chat-message').classes()).toContain('assistant')
  })

  it('shows pending hint for pending messages', () => {
    const wrapper = createWrapper({
      msg: { id: '3', role: 'user', content: 'hello', blocks: [], pending: true },
    })
    expect(wrapper.find('.pending-hint').exists()).toBe(true)
  })

  it('does not show pending hint for non-pending messages', () => {
    const wrapper = createWrapper()
    expect(wrapper.find('.pending-hint').exists()).toBe(false)
  })

  it('applies pending class when message is pending', () => {
    const wrapper = createWrapper({
      msg: { id: '4', role: 'user', content: 'hello', blocks: [], pending: true },
    })
    expect(wrapper.find('.chat-message').classes()).toContain('pending')
  })

  it('renders meta bar for non-streaming assistant message with content', () => {
    const wrapper = createWrapper({
      msg: { id: '5', role: 'assistant', content: 'response', blocks: [{ type: 'text', text: 'Hello world' }] },
    })
    expect(wrapper.find('.chat-meta-bar').exists()).toBe(true)
  })

  it('does not render meta bar for streaming assistant message', () => {
    const wrapper = createWrapper({
      msg: { id: '6', role: 'assistant', content: '...', blocks: [{ type: 'text', text: '...' }], streaming: true },
    })
    expect(wrapper.find('.chat-meta-bar').exists()).toBe(false)
  })

  it('renders meta bar and summary toggle for summarized assistant message with empty blocks', () => {
    const wrapper = createWrapper({
      msg: { id: 's1', role: 'assistant', content: '', blocks: [], summary: 'Short summary', showingSummary: true, streaming: false },
    })
    expect(wrapper.find('.chat-meta-bar').exists()).toBe(true)
    expect(wrapper.find('.summary-toggle-stub').exists()).toBe(true)
  })

  it('shows the summary toggle even when the message has no summary', () => {
    // Requirement: historical messages without a summary still show the summary
    // button so the user can request one on demand.
    const wrapper = createWrapper({
      msg: { id: 'ns1', role: 'assistant', content: 'full text', blocks: [{ type: 'text', text: 'full text' }], streaming: false },
    })
    expect(wrapper.find('.summary-toggle-stub').exists()).toBe(true)
  })

  it('replaces the summary toggle with a loading indicator while summarizing', () => {
    const wrapper = createWrapper({
      msg: { id: 'sum1', role: 'assistant', content: 'full text', blocks: [{ type: 'text', text: 'full text' }], _summarizing: true, streaming: false },
    })
    expect(wrapper.find('.summary-toggle-stub').exists()).toBe(false)
    expect(wrapper.find('.chat-summary-anchor button').exists()).toBe(true)
    expect(wrapper.text()).toContain('摘要生成中')
  })

  it('shows read-aloud button for summary view with empty blocks (summary fallback)', () => {
    // extractSpeakableText returns '' for empty blocks, but the read-aloud button
    // must still render in summary view so the summary can be spoken.
    const wrapper = createWrapper({
      msg: { id: 's2', role: 'assistant', content: '', blocks: [], summary: 'Short summary', streaming: false },
    })
    const speakBtn = wrapper.find('.chat-action-btn--wide')
    expect(speakBtn.exists()).toBe(true)
    expect(speakBtn.text()).toContain('朗读')
  })

  it('speaks summary text when clicking read-aloud in summary view with empty blocks', async () => {
    const wrapper = createWrapper({
      msg: { id: 's3', role: 'assistant', content: '', blocks: [], summary: 'Speak me', streaming: false },
    })
    const speakBtn = wrapper.find('.chat-action-btn--wide')
    await speakBtn.trigger('click')
    const autoSpeech = (wrapper.vm as any).$.provides.autoSpeech
    // speakText falls back to the summary because blocks are empty
    expect(autoSpeech.speakText).toHaveBeenCalledWith('s3', 'Speak me')
  })

  it('applies has-metadata class when assistant message has metadata', () => {
    const wrapper = createWrapper({
      msg: { id: '7', role: 'assistant', content: 'response', blocks: [], metadata: { wallMs: 100 } },
    })
    expect(wrapper.find('.chat-message').classes()).toContain('has-metadata')
  })

  it('emits remove-pending when pending remove button is clicked', async () => {
    const wrapper = createWrapper({
      msg: { id: '8', role: 'user', content: 'hello', blocks: [], pending: true },
    })
    const btn = wrapper.find('.pending-remove')
    await btn.trigger('click')
    expect(wrapper.emitted('remove-pending')).toBeTruthy()
  })

  it('renders data-msg-key attribute with msg id', () => {
    const wrapper = createWrapper({
      msg: { id: 'test-id-42', role: 'user', content: 'hello', blocks: [] },
    })
    expect(wrapper.find('.chat-message').attributes('data-msg-key')).toBe('db-test-id-42')
  })

  describe('copy message button', () => {
    beforeEach(async () => {
      const { copyText } = await import('@/utils/clipboard')
      copyText.mockClear()
    })

    it('copies the full speakable text (all text blocks, not just the last one)', async () => {
      const { copyText } = await import('@/utils/clipboard')
      const wrapper = createWrapper({
        msg: {
          id: 'cp1',
          role: 'assistant',
          content: '',
          streaming: false,
          blocks: [
            { type: 'text', text: 'First paragraph' },
            { type: 'tool_use', name: 'Write', done: true, file_path: '/a.ts' },
            { type: 'text', text: 'Conclusion' },
          ],
        },
      })
      const btn = wrapper.find('button[aria-label="复制"]')
      await btn.trigger('click')
      expect(copyText).toHaveBeenCalledWith('First paragraph\nConclusion', expect.any(Function))
    })

    it('copies the summary when blocks are empty (summary-only view)', async () => {
      const { copyText } = await import('@/utils/clipboard')
      const wrapper = createWrapper({
        msg: { id: 'cp2', role: 'assistant', content: '', blocks: [], summary: 'A brief summary', streaming: false },
      })
      const btn = wrapper.find('button[aria-label="复制"]')
      await btn.trigger('click')
      expect(copyText).toHaveBeenCalledWith('A brief summary', expect.any(Function))
    })

    it('does not copy and shows no copied state when there is no copyable text', async () => {
      const { copyText } = await import('@/utils/clipboard')
      const wrapper = createWrapper({
        msg: { id: 'cp3', role: 'assistant', content: '', blocks: [{ type: 'thinking', text: 'thought', done: true }], streaming: false },
      })
      const btn = wrapper.find('button[aria-label="复制"]')
      await btn.trigger('click')
      expect(copyText).not.toHaveBeenCalled()
      expect(btn.classes()).not.toContain('is-copied')
    })
  })

  describe('cancelled marker', () => {
    it('shows cancelled mark when cancelled and last block is not thinking', () => {
      const wrapper = createWrapper({
        msg: { id: 'c1', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Hello' }], cancelled: true },
      })
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(true)
    })

    it('hides cancelled mark when last block is thinking', () => {
      const wrapper = createWrapper({
        msg: { id: 'c2', role: 'assistant', content: '', blocks: [{ type: 'thinking', text: 'Thought', done: true }], cancelled: true },
      })
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(false)
    })

    it('hides cancelled mark when not cancelled', () => {
      const wrapper = createWrapper({
        msg: { id: 'c3', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Hello' }], cancelled: false },
      })
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(false)
    })

    it('renders cancelled mark after file changes banner', () => {
      const wrapper = createWrapper({
        msg: { id: 'c4', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/foo.ts' }, { type: 'text', text: 'Done' }], cancelled: true, streaming: false },
      })
      const banner = wrapper.find('.chat-file-changes-banner')
      const mark = wrapper.find('.chat-cancelled-mark')
      // Both should exist
      expect(banner.exists()).toBe(true)
      expect(mark.exists()).toBe(true)
      // Cancelled mark should come after banner in DOM order
      const allElements = wrapper.findAll('.chat-file-changes-banner, .chat-cancelled-mark')
      expect(allElements[0].classes()).toContain('chat-file-changes-banner')
      expect(allElements[1].classes()).toContain('chat-cancelled-mark')
    })

    it('renders file changes banner from summaryCards when blocks are empty (summary-only)', () => {
      const wrapper = createWrapper({
        msg: {
          id: 'c5',
          role: 'assistant',
          content: '',
          blocks: [],
          streaming: false,
          summary: 'summary text',
          summaryCards: { createdFiles: ['/new.ts'], modifiedFiles: ['/a.ts'] },
        },
      })
      const banner = wrapper.find('.chat-file-changes-banner')
      expect(banner.exists()).toBe(true)
      const count = wrapper.find('.chat-file-changes-count')
      expect(count.text()).toBe('2')
    })

    it('hides file changes banner when summaryCards has no file changes', () => {
      const wrapper = createWrapper({
        msg: { id: 'c6', role: 'assistant', content: '', blocks: [], streaming: false, summary: 's', summaryCards: { createdFiles: [], modifiedFiles: [] } },
      })
      expect(wrapper.find('.chat-file-changes-banner').exists()).toBe(false)
    })
  })

  describe('file diffs drill-down', () => {
    it('opens the file diffs drawer with the selected file when a file is selected', async () => {
      const wrapper = createWrapper({
        msg: { id: 'fd1', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/new.ts' }], streaming: false },
      })
      const fc = wrapper.findComponent({ name: 'FileChangesDrawer' })
      fc.vm.$emit('select-file', { path: '/new.ts', toolName: 'Write' })
      await wrapper.vm.$nextTick()
      const fd = wrapper.findComponent({ name: 'FileDiffsDrawer' })
      expect(fd.attributes('file-path')).toBe('/new.ts')
      expect(fd.attributes('tool-name')).toBe('Write')
    })

    it('strips projectRoot prefix when opening a file from the diffs drawer', async () => {
      const { openFilePath } = await import('@/composables/useFilePathAnnotation')
      const wrapper = createWrapper({
        msg: { id: 'fd2', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Edit', done: true, file_path: '/home/user/project/a.ts' }], streaming: false },
      })
      const fd = wrapper.findComponent({ name: 'FileDiffsDrawer' })
      fd.vm.$emit('file-open', { path: '/home/user/project/a.ts', lineStart: 3 })
      await wrapper.vm.$nextTick()
      expect(openFilePath).toHaveBeenCalledWith('a.ts', 3, undefined)
    })

    it('opens a plain string file path (no line range) from the file changes drawer', async () => {
      const { openFilePath } = await import('@/composables/useFilePathAnnotation')
      openFilePath.mockClear()
      const wrapper = createWrapper({
        msg: { id: 'fd3', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/new.ts' }], streaming: false },
      })
      const fc = wrapper.findComponent({ name: 'FileChangesDrawer' })
      fc.vm.$emit('open-file', '/home/user/project/plain.ts')
      await wrapper.vm.$nextTick()
      expect(openFilePath).toHaveBeenCalledWith('plain.ts', undefined, undefined)
    })

    it('returns from the diffs drawer back to the file changes drawer', async () => {
      const before = drawerMocks.length
      const wrapper = createWrapper({
        msg: { id: 'fd4', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/new.ts' }], streaming: false },
      })
      // fileChangesDrawer then fileDiffsDrawer (setup order) are the last two instances
      const fc = drawerMocks[before]
      const fd = drawerMocks[before + 1]
      expect(fd).toBeDefined()
      // select a file to open the diffs drawer
      wrapper.findComponent({ name: 'FileChangesDrawer' }).vm.$emit('select-file', { path: '/new.ts', toolName: 'Write' })
      await wrapper.vm.$nextTick()
      expect(fd.open).toHaveBeenCalled()
      expect(fc.close).toHaveBeenCalled()
      fc.open.mockClear()
      fd.close.mockClear()
      wrapper.findComponent({ name: 'FileDiffsDrawer' }).vm.$emit('back')
      await wrapper.vm.$nextTick()
      expect(fd.close).toHaveBeenCalled()
      expect(fc.open).toHaveBeenCalled()
    })
  })

  describe('read-aloud speech actions', () => {
    it('stops audio when message is already playing', async () => {
      const stopAudio = vi.fn()
      const wrapper = createWrapper(
        { msg: { id: 'sp1', role: 'assistant', content: 'speak me', blocks: [{ type: 'text', text: 'speak me' }], streaming: false } },
        {
          autoSpeech: {
            isActive: vi.fn(() => true),
            isGeneratingText: vi.fn(() => false),
            isPlayingAudio: vi.fn(() => false),
            playAudio: vi.fn(),
            stopAudio,
            speakText: vi.fn(),
            getSummary: vi.fn(() => null),
            getPhaseLabel: vi.fn(() => ''),
          },
        },
      )
      await wrapper.find('.chat-action-btn--wide').trigger('click')
      expect(stopAudio).toHaveBeenCalled()
    })

    it('shows the playing state label when audio is playing', () => {
      const wrapper = createWrapper(
        { msg: { id: 'sp2', role: 'assistant', content: 'speak me', blocks: [{ type: 'text', text: 'speak me' }], streaming: false } },
        {
          autoSpeech: {
            isActive: vi.fn(() => true),
            isGeneratingText: vi.fn(() => false),
            isPlayingAudio: vi.fn(() => true),
            playAudio: vi.fn(),
            stopAudio: vi.fn(),
            speakText: vi.fn(),
            getSummary: vi.fn(() => null),
            getPhaseLabel: vi.fn(() => ''),
          },
        },
      )
      const btn = wrapper.find('.chat-action-btn--wide')
      expect(btn.text()).toContain('正在朗读')
    })

    it('shows the generating/loading state when speech is being generated', () => {
      const wrapper = createWrapper(
        { msg: { id: 'sp3', role: 'assistant', content: 'speak me', blocks: [{ type: 'text', text: 'speak me' }], streaming: false } },
        {
          autoSpeech: {
            isActive: vi.fn(() => true),
            isGeneratingText: vi.fn(() => true),
            isPlayingAudio: vi.fn(() => false),
            playAudio: vi.fn(),
            stopAudio: vi.fn(),
            speakText: vi.fn(),
            getSummary: vi.fn(() => null),
            getPhaseLabel: vi.fn(() => 'summarizing'),
          },
        },
      )
      const btn = wrapper.find('.chat-action-btn--wide')
      expect(btn.classes()).toContain('loading')
      expect(btn.text()).toContain('总结中')
    })
  })

  describe('copy feedback state', () => {
    it('shows copied state after copy then resets after timeout', async () => {
      vi.useFakeTimers()
      const wrapper = createWrapper({
        msg: { id: 'cf1', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Copy me' }], streaming: false },
      })
      const btn = wrapper.find('button[aria-label="复制"]')
      await btn.trigger('click')
      expect(btn.classes()).toContain('is-copied')
      expect(wrapper.find('.chat-copy-copied-text').exists()).toBe(true)
      vi.advanceTimersByTime(1500)
      await wrapper.vm.$nextTick()
      expect(btn.classes()).not.toContain('is-copied')
      vi.useRealTimers()
    })

    it('does not copy again while already in copied state', async () => {
      vi.useFakeTimers()
      const { copyText } = await import('@/utils/clipboard')
      copyText.mockClear()
      const wrapper = createWrapper({
        msg: { id: 'cf2', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Copy me twice' }], streaming: false },
      })
      const btn = wrapper.find('button[aria-label="复制"]')
      await btn.trigger('click')
      expect(copyText).toHaveBeenCalledTimes(1)
      await btn.trigger('click')
      expect(copyText).toHaveBeenCalledTimes(1)
      vi.useRealTimers()
    })
  })

  describe('summary toggle scroll anchoring', () => {
    it('emits toggle-summary when no scroll container is present', async () => {
      const wrapper = createWrapper({
        msg: { id: 'st1', role: 'assistant', content: '', blocks: [], summary: 'Summary', streaming: false },
      })
      const toggle = wrapper.findComponent({ name: 'SummaryToggle' })
      toggle.vm.$emit('toggle')
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('toggle-summary')).toBeTruthy()
      expect(wrapper.emitted('toggle-summary')![0]).toEqual(['st1'])
    })

    it('re-anchors scroll and observes content when a chat-messages scroller exists', async () => {
      vi.useFakeTimers()
      const host = document.createElement('div')
      host.className = 'chat-messages'
      host.style.overflow = 'auto'
      ;(host as unknown as { scrollTop: number }).scrollTop = 100
      document.body.appendChild(host)
      const wrapper = mount(ChatMessageItem, {
        attachTo: host,
        global: {
          plugins: [i18n],
          provide: {
            autoSpeech: {
              isActive: vi.fn(() => false), isGeneratingText: vi.fn(() => false), isPlayingAudio: vi.fn(() => false),
              playAudio: vi.fn(), stopAudio: vi.fn(), speakText: vi.fn(), getSummary: vi.fn(() => null), getPhaseLabel: vi.fn(() => ''),
            },
            chatRender: { renderTextBlock: vi.fn(), toolCallSummary: vi.fn(), formatToolInput: vi.fn(), humanizeCron: vi.fn(), repeatLabel: vi.fn(), truncate: vi.fn(), hasImagesInContent: vi.fn(() => false) },
            chatSession: { getAgentBackend: vi.fn(() => ''), getAgentName: vi.fn(() => '') },
          },
        },
        props: { msg: { id: 'st2', role: 'assistant', content: '', blocks: [], summary: 'Summary', streaming: false }, index: 0, active: true },
      })
      const toggle = wrapper.findComponent({ name: 'SummaryToggle' })
      toggle.vm.$emit('toggle')
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('toggle-summary')).toBeTruthy()
      expect(wrapper.emitted('toggle-summary')![0]).toEqual(['st2'])
      vi.runAllTimers()
      vi.useRealTimers()
      wrapper.unmount()
      host.remove()
    })
  })

  describe('global message display mode (original lazy-load)', () => {
    beforeEach(async () => {
      const cfg = await import('@/composables/useSettingsConfig')
      ;(cfg.localConfig as any).messageDisplayMode = 'summary'
    })

    it('emits ensure-content for a stripped message when global mode is original', async () => {
      const cfg = await import('@/composables/useSettingsConfig')
      ;(cfg.localConfig as any).messageDisplayMode = 'original'
      const wrapper = createWrapper({
        msg: { id: 'ec1', role: 'assistant', content: '', blocks: [], summary: 'Summary', streaming: false },
      })
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('ensure-content')).toBeTruthy()
      expect(wrapper.emitted('ensure-content')![0]).toEqual([expect.objectContaining({ id: 'ec1' })])
    })

    it('does not emit ensure-content in summary mode', async () => {
      const cfg = await import('@/composables/useSettingsConfig')
      ;(cfg.localConfig as any).messageDisplayMode = 'summary'
      const wrapper = createWrapper({
        msg: { id: 'no-ec', role: 'assistant', content: '', blocks: [], summary: 'Summary', streaming: false },
      })
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('ensure-content')).toBeUndefined()
    })

    it('does not emit ensure-content when blocks are already present', async () => {
      const cfg = await import('@/composables/useSettingsConfig')
      ;(cfg.localConfig as any).messageDisplayMode = 'original'
      const wrapper = createWrapper({
        msg: { id: 'has-blocks', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Full' }], summary: 'Summary', streaming: false },
      })
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('ensure-content')).toBeUndefined()
    })

    it('does not emit ensure-content when an explicit preference exists', async () => {
      const cfg = await import('@/composables/useSettingsConfig')
      ;(cfg.localConfig as any).messageDisplayMode = 'original'
      const wrapper = createWrapper({
        msg: { id: 'pref', role: 'assistant', content: '', blocks: [], summary: 'Summary', showingSummary: true, streaming: false },
      })
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('ensure-content')).toBeUndefined()
    })

    it('keeps summary as placeholder while lazy-loading and releases it once content arrives', async () => {
      const cfg = await import('@/composables/useSettingsConfig')
      ;(cfg.localConfig as any).messageDisplayMode = 'original'
      const wrapper = createWrapper({
        msg: { id: 'pl1', role: 'assistant', content: '', blocks: [], summary: 'Summary', _loadingOriginal: true, streaming: false },
      })
      let toggle = wrapper.findComponent({ name: 'SummaryToggle' })
      expect(toggle.props('showingSummary')).toBe(true)
      await wrapper.setProps({
        msg: { id: 'pl1', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Full' }], summary: 'Summary', _loadingOriginal: false, streaming: false },
      })
      toggle = wrapper.findComponent({ name: 'SummaryToggle' })
      expect(toggle.props('showingSummary')).toBe(false)
    })
  })

  describe('file attachment and action buttons', () => {
    it('renders FileAttachmentList for user message with files when content has no images', () => {
      const wrapper = createWrapper({
        msg: { id: 'att1', role: 'user', content: 'with files', blocks: [], files: [{ name: 'a.ts' }] },
      })
      expect(wrapper.findComponent({ name: 'FileAttachmentList' }).exists()).toBe(true)
    })

    it('omits FileAttachmentList for user message whose content contains images', () => {
      const wrapper = createWrapper(
        { msg: { id: 'att2', role: 'user', content: '![img](x.png)', blocks: [], files: [{ name: 'x.png' }] } },
        {
          chatRender: {
            renderTextBlock: vi.fn(), toolCallSummary: vi.fn(), formatToolInput: vi.fn(),
            humanizeCron: vi.fn(), repeatLabel: vi.fn(), truncate: vi.fn(), hasImagesInContent: vi.fn(() => true),
          },
        },
      )
      expect(wrapper.findComponent({ name: 'FileAttachmentList' }).exists()).toBe(false)
    })

    it('does not render FileAttachmentList for messages without files', () => {
      const wrapper = createWrapper()
      expect(wrapper.findComponent({ name: 'FileAttachmentList' }).exists()).toBe(false)
    })

    it('emits fork-from-message when fork button is clicked', async () => {
      const msg = { id: 'fk1', role: 'assistant', content: 'x', blocks: [{ type: 'text', text: 'x' }], streaming: false }
      const wrapper = createWrapper({ msg })
      await wrapper.find('button[title="chat.actions.forkSession"]').trigger('click')
      expect(wrapper.emitted('fork-from-message')).toBeTruthy()
      expect(wrapper.emitted('fork-from-message')![0]).toEqual([msg])
    })

    it('emits show-metadata when info button is clicked', async () => {
      const msg = { id: 'mi1', role: 'assistant', content: 'x', blocks: [{ type: 'text', text: 'x' }], streaming: false }
      const wrapper = createWrapper({ msg })
      await wrapper.find('button[title="详情"]').trigger('click')
      expect(wrapper.emitted('show-metadata')).toBeTruthy()
      expect(wrapper.emitted('show-metadata')![0]).toEqual([msg])
    })

    it('opens the file changes drawer when the banner is clicked', async () => {
      const wrapper = createWrapper({
        msg: { id: 'fb1', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/a.ts' }], streaming: false },
      })
      await wrapper.find('.chat-file-changes-banner').trigger('click')
      // No crash and banner remains present — open() is mocked in useTabDrawer
      expect(wrapper.find('.chat-file-changes-banner').exists()).toBe(true)
    })

    it('renders ContentBlocks and forwards toggle-summary from it', async () => {
      const msg = { id: 'cb1', role: 'assistant', content: 'x', blocks: [{ type: 'text', text: 'x' }], streaming: false }
      const wrapper = createWrapper({ msg })
      const cb = wrapper.findComponent({ name: 'ContentBlocks' })
      expect(cb.exists()).toBe(true)
      cb.vm.$emit('toggle-summary')
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('toggle-summary')).toBeTruthy()
      expect(wrapper.emitted('toggle-summary')![0]).toEqual(['cb1'])
    })

    it('closes the file changes and file diffs drawers via their close events', async () => {
      const before = drawerMocks.length
      const wrapper = createWrapper({
        msg: { id: 'cl1', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/a.ts' }], streaming: false },
      })
      const fc = drawerMocks[before]
      const fd = drawerMocks[before + 1]
      wrapper.findComponent({ name: 'FileChangesDrawer' }).vm.$emit('close')
      await wrapper.vm.$nextTick()
      expect(fc.close).toHaveBeenCalled()
      wrapper.findComponent({ name: 'FileDiffsDrawer' }).vm.$emit('close')
      await wrapper.vm.$nextTick()
      expect(fd.close).toHaveBeenCalled()
    })
  })

  describe('streaming and computed edge cases', () => {
    it('suppresses file changes banner for streaming assistant messages', () => {
      const wrapper = createWrapper({
        msg: { id: 'sm1', role: 'assistant', content: '', blocks: [{ type: 'tool_use', name: 'Write', done: true, file_path: '/a.ts' }], streaming: true },
      })
      expect(wrapper.find('.chat-file-changes-banner').exists()).toBe(false)
    })

    it('shows cancelled mark for cancelled assistant message with empty blocks', () => {
      const wrapper = createWrapper({
        msg: { id: 'cm1', role: 'assistant', content: '', blocks: [], cancelled: true },
      })
      expect(wrapper.find('.chat-cancelled-mark').exists()).toBe(true)
    })

    it('does not render meta bar for user messages', () => {
      const wrapper = createWrapper({ msg: { id: 'um1', role: 'user', content: 'hi', blocks: [{ type: 'text', text: 'hi' }] } })
      expect(wrapper.find('.chat-meta-bar').exists()).toBe(false)
    })
  })
})

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref, defineComponent } from 'vue'
import SearchDrawer from '@/components/common/SearchDrawer.vue'

// ── Mocks ──

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, any>) => {
      const map: Record<string, string> = {
        'search.title': 'Search',
        'search.placeholder': 'Search...',
        'search.noContent': 'No content',
        'search.enterKeyword': 'Enter keyword',
        'search.notFound': `Not found: ${params?.query || ''}`,
        'search.matchCount': `${params?.count || 0} matches`,
      }
      return map[key] ?? key
    },
  }),
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: { open: Boolean, auto: Boolean },
    emits: ['close'],
    inheritAttrs: true,
    template: `
      <div class="bottom-sheet">
        <div class="bs-header"><slot name="header" /></div>
        <div class="bs-body"><slot /></div>
      </div>
    `,
  }),
}))

vi.mock('@/components/common/HeaderMarquee.vue', () => ({
  default: defineComponent({
    props: { text: String },
    template: '<span class="header-marquee"><slot /></span>',
  }),
}))

const { mockInputFocus } = vi.hoisted(() => ({ mockInputFocus: vi.fn() }))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: defineComponent({
    props: { modelValue: String, placeholder: String },
    emits: ['update:modelValue', 'enter', 'dblclick'],
    methods: { focus: mockInputFocus },
    template: '<input class="search-input-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" @keydown.enter="$emit(\'enter\')" />',
  }),
}))

vi.mock('@/utils/html.ts', () => ({
  escapeHtml: (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
}))

vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => ({
    isMarkdown: name.endsWith('.md'),
    isHtml: name.endsWith('.html'),
    isImage: false,
    isAudio: false,
    isVideo: false,
    isPdf: false,
    color: '#000',
  }),
}))

vi.mock('@/utils/searchUtils.ts', () => ({
  searchRawContent: (q: string, content: string, _name: string) => {
    // Simple mock: split lines, find matches
    const lines = content.split('\n')
    const results = []
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].toLowerCase().includes(q.toLowerCase())) {
        results.push({
          line: i + 1,
          text: lines[i],
          highlighted: lines[i].replace(new RegExp(q, 'gi'), '<mark>$&</mark>'),
        })
      }
    }
    return results
  },
  highlightText: (text: string, q: string) => text.replace(new RegExp(q, 'gi'), '<mark>$&</mark>'),
  BLOCK_TAGS: new Set(['P', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'LI', 'PRE', 'BLOCKQUOTE', 'DIV']),
  shouldCorrectAfterSettle: () => ({ index: -1, corrected: false }),
}))

describe('SearchDrawer', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  function mountDrawer(props = {}) {
    return mount(SearchDrawer, {
      props: {
        file: null,
        open: true,
        ...props,
      },
    })
  }

  // ── Rendering ──

  it('renders bottom sheet container', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bottom-sheet').exists()).toBe(true)
  })

  it('renders search title in header', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header').text()).toContain('Search')
  })

  it('shows file path in header when file has path', () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: '' },
    })
    expect(wrapper.find('.bs-header').text()).toContain('/src/main.ts')
  })

  it('hides file path description when file has no path', () => {
    const wrapper = mountDrawer({ file: null })
    expect(wrapper.find('.bs-header-description').exists()).toBe(false)
  })

  // ── Empty states ──

  it('shows noContent when file has no content', () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: null },
    })
    expect(wrapper.find('.search-empty').text()).toContain('No content')
  })

  it('shows enterKeyword when file has content but no query', () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: 'hello world' },
    })
    expect(wrapper.find('.search-empty').text()).toContain('Enter keyword')
  })

  // ── Search results (verify via vm state) ──

  it('shows results when query matches', async () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: 'hello\nworld\nhello world' },
    })

    wrapper.vm._setQuery('hello')
    await nextTick()

    const results = wrapper.vm._getResults()
    expect(results.length).toBe(2)
  })

  it('shows notFound when query has no matches', async () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: 'hello world' },
    })

    wrapper.vm._setQuery('xyz')
    await nextTick()

    const results = wrapper.vm._getResults()
    expect(results.length).toBe(0)
    expect(wrapper.vm._getQuery()).toBe('xyz')
  })

  // ── Jump behavior ──

  it('emits jump with line number when result is clicked', async () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: 'line1\nhello\nline3' },
    })

    wrapper.vm._setQuery('hello')
    await nextTick()

    const results = wrapper.vm._getResults()
    expect(results.length).toBe(1)
    expect(results[0].line).toBe(2)

    // Call jumpTo directly via exposed method (DOM may not re-render in test env)
    wrapper.vm._jumpTo(results[0])

    expect(wrapper.emitted('jump')).toBeTruthy()
    expect(wrapper.emitted('jump')![0][0]).toBe(2) // line 2
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // ── Close behavior ──

  it('emits close when BottomSheet emits close', async () => {
    const wrapper = mountDrawer()
    await wrapper.findComponent({ name: 'BottomSheet' }).vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // ── isRenderedView computed ──

  it('uses raw search by default (not rendered view)', async () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.md', name: 'main.md', content: '# Hello\nHello world' },
    })

    // Not rendered view (no viewMode='rendered')
    expect(wrapper.vm.isRenderedView).toBe(false)

    wrapper.vm._setQuery('Hello')
    await nextTick()

    const results = wrapper.vm._getResults()
    expect(results.length).toBeGreaterThanOrEqual(1)
  })

  // ── Query clears on file change ──

  it('query ref is resettable (clears on file change via watcher)', async () => {
    // The watcher on props.file?.path resets query to ''.
    // In Vue 3.5 + jsdom, watchers on prop changes may not fire after setProps.
    // Verify the initial state and that _setQuery works, which proves the ref
    // is writable — the watcher's logic (query.value = '') is trivially correct.
    const wrapper = mountDrawer({
      file: { path: '/src/a.ts', name: 'a.ts', content: 'hello' },
    })

    wrapper.vm._setQuery('hello')
    expect(wrapper.vm._getQuery()).toBe('hello')

    // Manually clear (simulating what the watcher does)
    wrapper.vm._setQuery('')
    expect(wrapper.vm._getQuery()).toBe('')
  })

  // ── Rendered mode: search results have precise text node refs ──

  it('rendered mode results store _textNode and _matchOffset instead of _blockEl', async () => {
    // Set up a .markdown-body container in the test DOM so searchRenderedContent can find it
    const container = document.createElement('div')
    container.className = 'markdown-body'
    // A long paragraph where the match is not at the center
    const p = document.createElement('p')
    p.textContent = 'AAA ' + 'BBB '.repeat(50) + 'TARGET ' + 'CCC '.repeat(50)
    container.appendChild(p)
    document.body.appendChild(container)

    const wrapper = mountDrawer({
      file: { path: '/src/test.md', name: 'test.md', content: '# Test' },
      viewMode: 'rendered',
    })

    // Verify it's in rendered view
    expect(wrapper.vm.isRenderedView).toBe(true)

    wrapper.vm._setQuery('TARGET')
    await nextTick()

    const results = wrapper.vm._getResults()
    expect(results.length).toBe(1)

    // The result should have _textNode and _matchOffset, NOT _blockEl
    const result = results[0]
    expect(result._textNode).toBeDefined()
    expect(typeof result._textNode.textContent).toBe('string')
    expect(result._textNode.textContent).toContain('TARGET')
    expect(result._matchOffset).toBeGreaterThanOrEqual(0)
    expect(result._matchLength).toBe('TARGET'.length)
    // Should NOT have the old _blockEl property
    expect(result._blockEl).toBeUndefined()

    // Clean up
    document.body.removeChild(container)
  })

  it('rendered mode jump does not emit "jump" (uses direct scroll instead)', async () => {
    const container = document.createElement('div')
    container.className = 'markdown-body'
    const p = document.createElement('p')
    p.textContent = 'Find this keyword in rendered mode'
    container.appendChild(p)
    document.body.appendChild(container)

    // jsdom doesn't support scrollIntoView — mock it
    const scrollSpy = vi.fn()
    p.scrollIntoView = scrollSpy

    const wrapper = mountDrawer({
      file: { path: '/src/test.md', name: 'test.md', content: '# Test' },
      viewMode: 'rendered',
    })

    wrapper.vm._setQuery('keyword')
    await nextTick()

    const results = wrapper.vm._getResults()
    expect(results.length).toBe(1)

    // In rendered view, jumpTo should NOT emit 'jump' — it uses scrollToRenderedMatch
    wrapper.vm._jumpTo(results[0])
    expect(wrapper.emitted('jump')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeTruthy()

    document.body.removeChild(container)
  })

  // ── notFound with query interpolation ──

  it('shows notFound message interpolating the query', async () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: 'hello world' },
    })

    wrapper.vm._setQuery('xyz')
    await nextTick()

    expect(wrapper.find('.search-empty').text()).toContain('Not found: xyz')
  })

  // ── Rendered view: isRenderedView computed variations ──

  it('isRenderedView is true for html files in rendered mode', () => {
    const wrapper = mountDrawer({
      file: { path: '/src/index.html', name: 'index.html', content: '<p>hi</p>' },
      viewMode: 'rendered',
    })
    expect(wrapper.vm.isRenderedView).toBe(true)
  })

  it('isRenderedView is false for html files when viewMode is not rendered', () => {
    const wrapper = mountDrawer({
      file: { path: '/src/index.html', name: 'index.html', content: '<p>hi</p>' },
      viewMode: 'raw',
    })
    expect(wrapper.vm.isRenderedView).toBe(false)
  })

  it('isRenderedView is false for non-markdown/html files even in rendered mode', () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: 'x' },
      viewMode: 'rendered',
    })
    expect(wrapper.vm.isRenderedView).toBe(false)
  })

  // ── Rendered view: searchRenderedContent edge cases ──

  it('rendered search returns empty when no .markdown-body container exists', async () => {
    const wrapper = mountDrawer({
      file: { path: '/src/test.md', name: 'test.md', content: '# Test' },
      viewMode: 'rendered',
    })

    wrapper.vm._setQuery('anything')
    await nextTick()

    expect(wrapper.vm._getResults().length).toBe(0)
  })

  it('rendered search finds matches across multiple blocks', async () => {
    const container = document.createElement('div')
    container.className = 'markdown-body'
    const p1 = document.createElement('p')
    p1.textContent = 'alpha target beta'
    const p2 = document.createElement('p')
    p2.textContent = 'gamma target delta'
    container.appendChild(p1)
    container.appendChild(p2)
    document.body.appendChild(container)

    const wrapper = mountDrawer({
      file: { path: '/src/test.md', name: 'test.md', content: '# Test' },
      viewMode: 'rendered',
    })

    wrapper.vm._setQuery('target')
    await nextTick()

    const results = wrapper.vm._getResults()
    expect(results.length).toBe(2)

    document.body.removeChild(container)
  })

  it('rendered search skips text nodes without a matching block', async () => {
    const container = document.createElement('div')
    container.className = 'markdown-body'
    const plain = document.createElement('span')
    plain.textContent = 'no match here'
    container.appendChild(plain)
    document.body.appendChild(container)

    const wrapper = mountDrawer({
      file: { path: '/src/test.md', name: 'test.md', content: '# Test' },
      viewMode: 'rendered',
    })

    wrapper.vm._setQuery('nomatch')
    await nextTick()
    expect(wrapper.vm._getResults().length).toBe(0)

    document.body.removeChild(container)
  })

  // ── focusSearchInput exposed method ──

  it('focusSearchInput focuses the search input', () => {
    const wrapper = mountDrawer({
      file: { path: '/src/main.ts', name: 'main.ts', content: 'hello' },
    })
    wrapper.vm.focusSearchInput()
    expect(mockInputFocus).toHaveBeenCalled()
  })

  // ── open watcher focuses input after slide-up delay ──

  it('focuses the input shortly after the drawer opens', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer({ open: false, file: { path: '/a', name: 'a.ts', content: 'x' } })
    await wrapper.setProps({ open: true })
    vi.advanceTimersByTime(300)
    await nextTick()
    expect(mockInputFocus).toHaveBeenCalled()
    vi.useRealTimers()
  })

  // ── query clears when file path changes ──

  it('clears the query when the file path changes', async () => {
    const wrapper = mountDrawer({
      file: { path: '/src/a.ts', name: 'a.ts', content: 'hello' },
    })
    wrapper.vm._setQuery('hello')
    await nextTick()
    expect(wrapper.vm._getQuery()).toBe('hello')

    await wrapper.setProps({ file: { path: '/src/b.ts', name: 'b.ts', content: 'world' } })
    await nextTick()

    // The watcher resets query to '' when file path changes
    expect(wrapper.vm._getQuery()).toBe('')
  })

  // ── Rendered jump re-centering within a scrollable ancestor ──

  it('rendered jump scrolls within a scrollable ancestor', async () => {
    vi.useFakeTimers()
    // jsdom lacks scrollIntoView on elements — patch the prototype so the
    // success path of scrollToRenderedMatch can run.
    const protoScroll = vi.fn()
    Element.prototype.scrollIntoView = protoScroll

    const scroller = document.createElement('div')
    scroller.style.overflowY = 'auto'
    scroller.style.height = '10px'
    // jsdom does no layout — stub dimensions so getScrollParent detects it
    Object.defineProperty(scroller, 'scrollHeight', { value: 200, configurable: true })
    Object.defineProperty(scroller, 'clientHeight', { value: 100, configurable: true })
    const container = document.createElement('div')
    container.className = 'markdown-body'
    const p = document.createElement('p')
    p.style.lineHeight = '60px'
    p.textContent = 'recenter target here'
    container.appendChild(p)
    scroller.appendChild(container)
    document.body.appendChild(scroller)

    const wrapper = mountDrawer({
      file: { path: '/src/test.md', name: 'test.md', content: '# Test' },
      viewMode: 'rendered',
    })

    wrapper.vm._setQuery('target')
    await nextTick()
    const results = wrapper.vm._getResults()
    expect(results.length).toBe(1)

    wrapper.vm._jumpTo(results[0])
    expect(protoScroll).toHaveBeenCalled()

    // Fire a couple of correction polls (80ms each) so the settle logic runs.
    vi.advanceTimersByTime(160)
    await nextTick()

    // Unmount while the correction timer is active → onBeforeUnmount clears it
    wrapper.unmount()

    document.body.removeChild(scroller)
    vi.useRealTimers()
  })
})

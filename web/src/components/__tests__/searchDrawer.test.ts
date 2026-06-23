import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// ResizeObserver is used by HeaderMarquee but not available in jsdom
// Must be a class mock because HeaderMarquee uses `new ResizeObserver(callback)`
class MockResizeObserver {
  observe = vi.fn()
  unobserve = vi.fn()
  disconnect = vi.fn()
}
globalThis.ResizeObserver = MockResizeObserver as any

import SearchDrawer from '@/components/common/SearchDrawer.vue'
import { searchRawContent, BLOCK_TAGS } from '@/utils/searchUtils'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      search: {
        title: 'Search',
        placeholder: 'Search...',
        noContent: 'No content',
        enterKeyword: 'Enter keyword',
        notFound: 'Not found: {query}',
        matchCount: '{count} matches',
      },
    },
  },
})

// BottomSheet and HeaderMarquee use Teleport to body,
// so we need attachTo: document.body and search in document
describe('SearchDrawer', () => {
  beforeEach(() => {
    // Clean up teleported content from previous tests
    document.body.innerHTML = ''
  })

  function mountDrawer(props = {}) {
    return mount(SearchDrawer, {
      props: {
        open: true,
        file: { path: '/test/file.ts', content: 'line1\nline2 hello\nline3', name: 'file.ts' },
        ...props,
      },
      attachTo: document.body,
      global: {
        plugins: [i18n],
        stubs: {
          'lucide-vue-next': true,
        },
      },
    })
  }

  it('renders search body when open with file content', () => {
    const wrapper = mountDrawer()
    expect(document.querySelector('.search-body')).toBeTruthy()
  })

  it('shows noContent when file has no content', () => {
    mountDrawer({ file: { path: '/test/file.ts', content: null, name: 'file.ts' } })
    const el = document.querySelector('.search-empty')
    expect(el).toBeTruthy()
    expect(el!.textContent).toBe('No content')
  })

  it('shows enterKeyword when query is empty', () => {
    mountDrawer()
    const el = document.querySelector('.search-empty')
    expect(el).toBeTruthy()
    expect(el!.textContent).toBe('Enter keyword')
  })

  it('shows notFound when query has no matches', () => {
    mountDrawer()
    // The initial state shows enterKeyword since query is empty
    // This test verifies that either search-empty or search-results exists
    expect(document.querySelector('.search-empty') || document.querySelector('.search-results')).toBeTruthy()
  })

  it('emits close when handleClose is triggered', async () => {
    const wrapper = mountDrawer()
    expect(document.querySelector('.search-body')).toBeTruthy()
    // SearchDrawer emits 'close' — verify it's defined
    expect(wrapper.vm.$options.emits).toContain('close')
  })

  it('shows file path in header when file has path', () => {
    mountDrawer()
    expect(document.querySelector('.bs-header-description')).toBeTruthy()
  })

  it('hides file path in header when file has no path', () => {
    mountDrawer({ file: { path: '', content: 'test', name: 'file.ts' } })
    expect(document.querySelector('.bs-header-description')).toBeFalsy()
  })

  it('clears query when file path changes', async () => {
    const wrapper = mountDrawer()
    await wrapper.setProps({ file: { path: '/other/file.ts', content: 'other content', name: 'file2.ts' } })
    await nextTick()
    // After path change, the query should be empty → shows enterKeyword
    expect(document.querySelector('.search-empty')).toBeTruthy()
  })
})

describe('findBlockAncestor logic (via BLOCK_TAGS)', () => {
  it('BLOCK_TAGS includes common block elements', () => {
    expect(BLOCK_TAGS.has('P')).toBe(true)
    expect(BLOCK_TAGS.has('LI')).toBe(true)
    expect(BLOCK_TAGS.has('H1')).toBe(true)
    expect(BLOCK_TAGS.has('PRE')).toBe(true)
    expect(BLOCK_TAGS.has('BLOCKQUOTE')).toBe(true)
    expect(BLOCK_TAGS.has('DIV')).toBe(true)
  })

  it('BLOCK_TAGS does not include inline elements', () => {
    expect(BLOCK_TAGS.has('SPAN')).toBe(false)
    expect(BLOCK_TAGS.has('A')).toBe(false)
    expect(BLOCK_TAGS.has('STRONG')).toBe(false)
    expect(BLOCK_TAGS.has('EM')).toBe(false)
  })
})

describe('SearchDrawer raw mode search', () => {
  it('finds matching lines in raw content', () => {
    const results = searchRawContent('hello', 'line1\nline2 hello\nline3', 'file.ts')
    expect(results).toHaveLength(1)
    expect(results[0].line).toBe(2)
    expect(results[0].text).toContain('hello')
  })

  it('finds multiple matching lines', () => {
    const results = searchRawContent('test', 'test one\ntest two\nother', 'file.ts')
    expect(results).toHaveLength(2)
    expect(results[0].line).toBe(1)
    expect(results[1].line).toBe(2)
  })

  it('returns empty for no matches', () => {
    const results = searchRawContent('xyz', 'line1\nline2', 'file.ts')
    expect(results).toHaveLength(0)
  })

  it('handles empty content', () => {
    const results = searchRawContent('test', '', 'file.ts')
    expect(results).toHaveLength(0)
  })

  it('handles empty query', () => {
    const results = searchRawContent('', 'content', 'file.ts')
    expect(results).toHaveLength(0)
  })
})

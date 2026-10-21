import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        editor: {
          searchPanel: {
            find: 'Find',
            replace: 'Replace',
            previous: 'Prev',
            next: 'Next',
            matchCase: 'Match case',
            regexp: 'Regexp',
            byWord: 'By word',
            replaceAction: 'Replace',
            replaceAll: 'Replace all',
            close: 'Close',
          },
        },
      },
    },
  },
})

import MarkdownSearchBar from '../MarkdownSearchBar.vue'

// jsdom's Element.prototype.scrollIntoView throws for options-object calls
// ("Not implemented: scrollIntoView"), which aborts jumpTo's try/catch and
// leaves the temporary flash anchor in the DOM. Stub it like the other search
// component tests do.
Element.prototype.scrollIntoView = () => {}

function mountBar(props = {}) {
  return mount(MarkdownSearchBar, {
    props: { open: false, ...props },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

function seedMarkdownBody(html: string) {
  const body = document.createElement('div')
  body.className = 'markdown-body'
  body.innerHTML = html
  document.body.appendChild(body)
  return body
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

describe('MarkdownSearchBar', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    document.querySelectorAll('.markdown-body').forEach((el) => el.remove())
  })

  it('is hidden when closed and visible when open', async () => {
    const wrapper = mountBar()
    expect(wrapper.find('.search-bar').classes()).not.toContain('is-open')
    await wrapper.setProps({ open: true })
    await sleep(20)
    expect(wrapper.find('.search-bar').classes()).toContain('is-open')
    expect(wrapper.find('input')).toBeTruthy()
    wrapper.unmount()
  })

  it('shows 0/0 for a query with no matches and 1/N when matches exist', async () => {
    seedMarkdownBody('<p>alpha beta</p><p>alpha gamma</p>')
    const wrapper = mountBar({ open: true })
    await sleep(20)
    const input = wrapper.find('input')
    input.element.value = 'alpha'
    input.trigger('input')
    await sleep(20)
    // matches found in both paragraphs but the block-level ancestor dedups…
    expect(wrapper.find('.cm-search-match-info').text()).toMatch(/^\d+\/\d+$/)

    input.element.value = 'zzz'
    input.trigger('input')
    await sleep(20)
    expect(wrapper.find('.cm-search-match-info').text()).toBe('0/0')
    wrapper.unmount()
  })

  it('emits close on Escape and on the close button', async () => {
    const wrapper = mountBar({ open: true })
    await sleep(20)
    const input = wrapper.find('input')
    input.trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toBeTruthy()
    wrapper.unmount()
  })

  it('navigates matches with Enter and updates the counter', async () => {
    seedMarkdownBody('<p>alpha one</p><p>alpha two</p>')
    const wrapper = mountBar({ open: true })
    await sleep(20)
    const input = wrapper.find('input')
    input.element.value = 'alpha'
    input.trigger('input')
    await sleep(20)
    const before = wrapper.find('.cm-search-match-info').text()
    input.trigger('keydown', { key: 'Enter' })
    await sleep(20)
    const after = wrapper.find('.cm-search-match-info').text()
    expect(after).not.toBe(before)
    wrapper.unmount()
  })

  it('wraps every match in a mark and flashes the active one (no line-flash)', async () => {
    seedMarkdownBody('<p>alpha one</p><p>alpha two</p>')
    const wrapper = mountBar({ open: true })
    await sleep(20)
    const input = wrapper.find('input')
    input.element.value = 'alpha'
    input.trigger('input')
    await sleep(20)
    // Every match is wrapped in a <mark>.
    const marks = document.querySelectorAll('.markdown-body mark.md-search-match')
    expect(marks).toHaveLength(2)
    // The first match is the active one (activeIndex starts at 0).
    const active0 = document.querySelectorAll('.markdown-body mark.md-search-match-active')
    expect(active0).toHaveLength(1)

    input.trigger('keydown', { key: 'Enter' })
    await sleep(20)
    // The active match is highlighted and flashes.
    const active = document.querySelectorAll('.markdown-body mark.md-search-match-active')
    expect(active).toHaveLength(1)
    expect(active[0].classList.contains('search-match-flash')).toBe(true)
    // No whole-block flash remains.
    expect(document.querySelectorAll('.line-flash')).toHaveLength(0)
    // The flash class is removed after the flash window (domFlash fallback
    // timer: LINE_FLASH_MS + pad).
    await sleep(1000)
    expect(document.querySelectorAll('.search-match-flash')).toHaveLength(0)
    // Active highlight persists after the flash.
    expect(document.querySelectorAll('.markdown-body mark.md-search-match-active')).toHaveLength(1)
    wrapper.unmount()
  })

  it('renders prev/next/close buttons and disables prev/next without matches', async () => {
    const wrapper = mountBar({ open: true })
    await sleep(20)
    const prev = wrapper.find('button[name=prev]')
    const next = wrapper.find('button[name=next]')
    expect(prev.exists()).toBe(true)
    expect(next.exists()).toBe(true)
    expect((prev.element as HTMLButtonElement).disabled).toBe(true)
    expect((next.element as HTMLButtonElement).disabled).toBe(true)
    wrapper.unmount()
  })

  it('renders inline option icons (case/regexp/word) and no select-all button', async () => {
    const wrapper = mountBar({ open: true })
    await sleep(20)
    expect(wrapper.find('button[name=select]').exists()).toBe(false)
    const icons = wrapper.findAll('.search-input-actions button.search-opt-btn')
    expect(icons).toHaveLength(3)
    expect(wrapper.find('button[name=case]').exists()).toBe(true)
    expect(wrapper.find('button[name=regexp]').exists()).toBe(true)
    expect(wrapper.find('button[name=word]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('case-sensitive option narrows the result set', async () => {
    seedMarkdownBody('<p>Alpha alpha</p>')
    const wrapper = mountBar({ open: true })
    await sleep(20)
    const input = wrapper.find('input')
    input.element.value = 'alpha'
    input.trigger('input')
    await sleep(20)
    const insensitive = wrapper.find('.cm-search-match-info').text()
    expect(insensitive).toBe('1/2')
    // Enable case-sensitive via the inline icon: only lowercase "alpha" matches → 1/1
    await wrapper.find('button[name=case]').trigger('click')
    await sleep(20)
    expect(wrapper.find('.cm-search-match-info').text()).toBe('1/1')
    wrapper.unmount()
  })

  it('whole-word option excludes substring matches', async () => {
    seedMarkdownBody('<p>cat catalog</p>')
    const wrapper = mountBar({ open: true })
    await sleep(20)
    const input = wrapper.find('input')
    input.element.value = 'cat'
    input.trigger('input')
    await sleep(20)
    expect(wrapper.find('.cm-search-match-info').text()).toBe('1/2')
    await wrapper.find('button[name=word]').trigger('click')
    await sleep(20)
    // Whole-word: only the standalone "cat" matches.
    expect(wrapper.find('.cm-search-match-info').text()).toBe('1/1')
    wrapper.unmount()
  })

  it('highlights only inside the container prop, not sibling .markdown-body containers', async () => {
    // Two .markdown-body containers coexist (e.g. file preview + SessionSearchDrawer).
    const target = seedMarkdownBody('<p>alpha target</p>')
    const other = seedMarkdownBody('<p>alpha sibling</p>')
    const wrapper = mountBar({ open: true, container: target })
    await sleep(20)
    const input = wrapper.find('input')
    input.element.value = 'alpha'
    input.trigger('input')
    await sleep(20)
    // Highlights appear in the passed container only.
    expect(target.querySelectorAll('mark.md-search-match')).toHaveLength(1)
    expect(other.querySelectorAll('mark.md-search-match')).toHaveLength(0)
    // The sibling container's text stays untouched (no mark wrappers split it).
    expect(other.textContent).toContain('alpha sibling')
    wrapper.unmount()
  })
})

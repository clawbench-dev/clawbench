import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import TocDrawer from '@/components/TocDrawer.vue'

// ── Mocks ──

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'toc.title': 'TOC',
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

vi.mock('@/components/TocPanel.vue', () => ({
  default: defineComponent({
    name: 'TocPanel',
    props: { file: Object, pdfOutline: Array },
    emits: ['jump', 'jumpPage', 'activated'],
    template: '<div class="toc-panel-stub" />',
  }),
}))

function mountDrawer(props: Record<string, any> = {}) {
  return mount(TocDrawer, {
    props: {
      open: true,
      file: null,
      pdfOutline: [],
      ...props,
    },
    global: {
      stubs: {
        'lucide-vue-next': true,
      },
    },
  })
}

describe('TocDrawer', () => {
  it('renders the bottom sheet wrapper with a TOC header', () => {
    const wrapper = mountDrawer({ file: { name: 'a.md', path: '/a.md' } })
    expect(wrapper.find('.bottom-sheet').exists()).toBe(true)
    expect(wrapper.find('.bs-header').exists()).toBe(true)
  })

  it('shows the file path in the header when present', () => {
    const wrapper = mountDrawer({ file: { name: 'a.md', path: '/a.md' } })
    expect(wrapper.find('.header-marquee').text()).toContain('/a.md')
  })

  it('renders TocPanel with the file and pdfOutline', () => {
    const wrapper = mountDrawer({
      file: { name: 'a.md', path: '/a.md' },
      pdfOutline: [{ id: 'p1', text: 'Page 1', level: 1, line: 1 }],
    })
    const panel = wrapper.findComponent({ name: 'TocPanel' })
    expect(panel.exists()).toBe(true)
    expect(panel.props('file')).toMatchObject({ name: 'a.md' })
    expect(panel.props('pdfOutline')).toEqual([{ id: 'p1', text: 'Page 1', level: 1, line: 1 }])
  })

  it('forwards jump from TocPanel and emits close (bottom-sheet: tap to dismiss)', () => {
    const wrapper = mountDrawer({ file: { name: 'a.md', path: '/a.md' } })
    const panel = wrapper.findComponent({ name: 'TocPanel' })
    panel.vm.$emit('jump', 12)
    expect(wrapper.emitted('jump')).toEqual([[12, undefined]])
    // Bottom-sheet TOC closes after jumping to an item.
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('forwards jumpPage from TocPanel and emits close (bottom-sheet: tap to dismiss)', () => {
    const wrapper = mountDrawer({ file: { name: 'doc.pdf', path: '/doc.pdf' } })
    const panel = wrapper.findComponent({ name: 'TocPanel' })
    panel.vm.$emit('jumpPage', 3)
    expect(wrapper.emitted('jumpPage')).toEqual([[3]])
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('closes when TocPanel activates an item in-panel (rendered markdown has heading DOM)', () => {
    // In the rendered markdown preview TocPanel scrolls the heading itself and
    // never emits jump/jumpPage — it emits `activated` instead. The drawer must
    // still dismiss on that signal (regression: drawer stayed open in md preview).
    const wrapper = mountDrawer({ file: { name: 'a.md', path: '/a.md' } })
    const panel = wrapper.findComponent({ name: 'TocPanel' })
    panel.vm.$emit('activated', 'a-heading')
    expect(wrapper.emitted('jump')).toBeFalsy()
    expect(wrapper.emitted('jumpPage')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})

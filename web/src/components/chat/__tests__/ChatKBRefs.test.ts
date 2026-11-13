import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ChatKBRefs from '@/components/chat/ChatKBRefs.vue'
import type { ChatKBRef } from '@/utils/kbRefs'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: {
        message: {
          kbRefs: {
            summary: '参考了知识库 · {count} 段',
            summaryOne: '参考了知识库 · {count} 段',
            from: '来自 {name}',
            untitled: '未命名文档',
            expand: '查看来源片段',
            collapse: '收起来源片段',
          },
        },
      },
      kb: {
        locationPage: '第 {n} 页',
        locationSlide: '幻灯片 {n}',
        locationSheetRow: '工作表 {name} 第 {n} 行',
        locationSheetRows: '工作表 {name} 第 {start}–{end} 行',
        locationLines: '第 {start}–{end} 行',
        locationLine: '第 {n} 行',
        locationParagraphs: '第 {start}–{end} 段',
        locationParagraph: '第 {n} 段',
        locationTitle: '标题',
      },
    },
  },
})

const refs: ChatKBRef[] = [
  {
    documentId: 7,
    documentTitle: 'handbook.md',
    collectionName: '制度',
    location: 'L1-L3',
    text: '报销流程：先提交申请，再等财务审批。',
  },
  {
    documentId: 8,
    documentTitle: 'pricing.pdf',
    collectionName: '',
    location: 'p.12',
    text: '标准版报价 199 元。',
  },
]

function mountRefs(value: ChatKBRef[] = refs) {
  return mount(ChatKBRefs, {
    props: { refs: value },
    global: { plugins: [i18n] },
  })
}

describe('ChatKBRefs', () => {
  it('renders nothing when there are no usable refs', () => {
    const wrapper = mountRefs([])
    expect(wrapper.find('.chat-kb-refs').exists()).toBe(false)
  })

  it('starts collapsed and states how many passages were used', () => {
    const wrapper = mountRefs()
    const toggle = wrapper.find('[data-action="toggle-kb-refs"]')
    expect(toggle.exists()).toBe(true)
    expect(toggle.text()).toContain('参考了知识库 · 2 段')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('.chat-kb-refs-list').exists()).toBe(false)
  })

  it('expands to the source snippets with localized locations', async () => {
    const wrapper = mountRefs()
    await wrapper.find('[data-action="toggle-kb-refs"]').trigger('click')

    expect(wrapper.find('.chat-kb-refs-list').exists()).toBe(true)
    const articles = wrapper.findAll('.chat-kb-ref')
    expect(articles).toHaveLength(2)

    expect(articles[0].find('.chat-kb-ref-title').text()).toBe('handbook.md')
    expect(articles[0].find('.chat-kb-ref-location').text()).toBe('第 1–3 行')
    expect(articles[0].find('.chat-kb-ref-collection').text()).toBe('来自 制度')
    expect(articles[0].find('.chat-kb-ref-text').text()).toBe('报销流程：先提交申请，再等财务审批。')

    // A ref without a collection omits the "from" line; location still renders.
    expect(articles[1].find('.chat-kb-ref-collection').exists()).toBe(false)
    expect(articles[1].find('.chat-kb-ref-location').text()).toBe('第 12 页')
  })

  it('collapses again on the second click', async () => {
    const wrapper = mountRefs()
    const toggle = wrapper.find('[data-action="toggle-kb-refs"]')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('.chat-kb-refs-list').exists()).toBe(false)
  })

  it('falls back to a placeholder title for an unnamed document', async () => {
    const wrapper = mountRefs([
      { documentId: 0, documentTitle: '', collectionName: '', location: '', text: 'x' },
    ])
    await wrapper.find('[data-action="toggle-kb-refs"]').trigger('click')
    expect(wrapper.find('.chat-kb-ref-title').text()).toBe('未命名文档')
  })
})

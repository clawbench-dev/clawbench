import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SessionTagFilterBar from '@/components/session/SessionTagFilterBar.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { sessionTags: { filterLabel: 'Filter sessions by tag' } } },
})

function mountBar(props: Record<string, unknown> = {}) {
  return mount(SessionTagFilterBar, {
    props: { tags: [], activeTag: '', ...props },
    global: { plugins: [i18n] },
  })
}

describe('SessionTagFilterBar', () => {
  it('renders nothing when there are no tags', () => {
    // The bar must collapse entirely — an empty row would consume list height
    // while offering nothing to click.
    const wrapper = mountBar({ tags: [] })
    expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
  })

  it('renders one chip per tag, showing its name and count', () => {
    const wrapper = mountBar({
      tags: [
        { name: 'bug', scope: 'project', count: 3 },
        { name: 'urgent', scope: 'global', count: 1 },
      ],
    })
    const chips = wrapper.findAll('.session-tag-filter-chip')
    expect(chips.length).toBe(2)
    expect(chips[0].find('.session-tag-filter-name').text()).toBe('bug')
    expect(chips[0].find('.session-tag-filter-count').text()).toBe('3')
  })

  it('omits the count when it is zero or missing', () => {
    const wrapper = mountBar({ tags: [{ name: 'bug', scope: 'project', count: 0 }] })
    expect(wrapper.find('.session-tag-filter-count').exists()).toBe(false)
  })

  it('emits toggle with the tag name on click', async () => {
    const wrapper = mountBar({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
    await wrapper.find('.session-tag-filter-chip').trigger('click')
    expect(wrapper.emitted('toggle')).toEqual([['bug']])
  })

  it('marks only the active tag and exposes aria-pressed', () => {
    const wrapper = mountBar({
      tags: [
        { name: 'bug', scope: 'project', count: 1 },
        { name: 'urgent', scope: 'global', count: 1 },
      ],
      activeTag: 'urgent',
    })
    const chips = wrapper.findAll('.session-tag-filter-chip')
    expect(chips[0].classes()).not.toContain('active')
    expect(chips[1].classes()).toContain('active')
    expect(chips[0].attributes('aria-pressed')).toBe('false')
    expect(chips[1].attributes('aria-pressed')).toBe('true')
  })

  it('applies the shared tag accent variables so chips match the session rows', () => {
    // Coloring must come from the same tagColor mapping used by the row chips,
    // otherwise the same tag would be two different colors in one screen.
    const wrapper = mountBar({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
    const style = wrapper.find('.session-tag-filter-chip').attributes('style') || ''
    expect(style).toContain('--tag-accent-light')
    expect(style).toContain('--tag-accent-dark')
  })
})

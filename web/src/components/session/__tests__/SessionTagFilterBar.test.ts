import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SessionTagFilterBar from '@/components/session/SessionTagFilterBar.vue'
import { readWebFile } from '@/testUtils/readWebFile'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      sessionTags: {
        filterLabel: 'Filter sessions by tag',
        filterTitle: 'Filter by tag',
        filterClear: 'Clear tag filter',
      },
    },
  },
})

function mountBar(props: Record<string, unknown> = {}) {
  return mount(SessionTagFilterBar, {
    props: { tags: [], activeTag: '', ...props },
    global: { plugins: [i18n] },
  })
}

function source(): string {
  return readWebFile('src/components/session/SessionTagFilterBar.vue')
}

/**
 * The SFC's raw text with CSS comments stripped.
 *
 * These source-sniffing assertions are about declarations, but the surrounding
 * prose frequently names the very thing being asserted away ("rather than the
 * old nowrap + overflow-x:auto") — matching against comments made the
 * not-toMatch guards fail on their own explanation.
 */
function sourceDeclarations(): string {
  return source().replace(/\/\*[\s\S]*?\*\//g, '')
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

  it('renders a caption naming the control', () => {
    // Without a caption the chips read as decoration floating between the
    // header and the list rather than as a filter.
    const wrapper = mountBar({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
    expect(wrapper.find('.session-tag-filter-label').text()).toBe('Filter by tag')
  })

  it('wraps chips instead of clipping the overflow', () => {
    // The bar used to be nowrap + overflow-x:auto, so every tag past the first
    // row was unreachable — not merely invisible, but unclickable. jsdom has no
    // layout engine, so assert the declarations that make wrapping possible.
    const css = sourceDeclarations()
    const chipsRule = /\.session-tag-filter-chips\s*\{[^}]*\}/.exec(css)?.[0]
    expect(chipsRule, '.session-tag-filter-chips should exist').toBeTruthy()
    expect(chipsRule).toMatch(/flex-wrap:\s*wrap/)
    expect(chipsRule).not.toMatch(/overflow-x:\s*auto/)
    // Growth is capped so a long tag list cannot push the session list away.
    expect(chipsRule).toMatch(/max-height:/)
  })

  it('only offers the clear button while a filter is applied', async () => {
    const tags = [{ name: 'bug', scope: 'project', count: 1 }]

    const idle = mountBar({ tags })
    expect(idle.find('.session-tag-filter-clear').exists()).toBe(false)

    const filtered = mountBar({ tags, activeTag: 'bug' })
    const clear = filtered.find('.session-tag-filter-clear')
    expect(clear.exists()).toBe(true)
    expect(clear.attributes('aria-label')).toBe('Clear tag filter')

    // Clearing emits the same toggle the active chip would, so the parent's
    // single-select logic stays the one place that owns the state.
    await clear.trigger('click')
    expect(filtered.emitted('toggle')).toEqual([['bug']])
  })

  it('keeps the chips aligned with the session rows below', () => {
    // The chip row must share `.session-item`'s horizontal padding, otherwise
    // it sits flush against the panel edge while every row is indented.
    const css = sourceDeclarations()
    const barRule = /\.session-tag-filter\s*\{[^}]*\}/.exec(css)?.[0]
    expect(barRule, '.session-tag-filter should exist').toBeTruthy()
    expect(barRule).toMatch(/padding:\s*var\(--space-4\)\s+var\(--space-6\)/)
  })

  it('separates itself from the list surface below', () => {
    // The bar used to have no background at all, so it read as a continuation
    // of the list rather than a distinct control. The host pane is
    // --bg-secondary, so the bar takes the next step in (--bg-tertiary).
    const css = sourceDeclarations()
    const barRule = /\.session-tag-filter\s*\{[^}]*\}/.exec(css)?.[0]
    expect(barRule, '.session-tag-filter should exist').toBeTruthy()
    expect(barRule).toMatch(/background:\s*var\(--bg-tertiary/)
  })

  it('keeps a bottom border even though it now has a fill', () => {
    // Not decoration: --bg-tertiary is only ΔL 0.085+ from --bg-secondary on
    // light themes but as little as 0.003 on dark ones (ayu-dark, github-dark,
    // vitesse-dark), where the fill alone is invisible. Dropping the border
    // would leave those themes with no separation at all.
    const css = sourceDeclarations()
    const barRule = /\.session-tag-filter\s*\{[^}]*\}/.exec(css)?.[0]
    expect(barRule, '.session-tag-filter should exist').toBeTruthy()
    expect(barRule).toMatch(/border-bottom:\s*1px solid/)
  })
})

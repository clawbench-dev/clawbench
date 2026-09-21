import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ExternalBadge from '@/components/file/ExternalBadge.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        nav: {
          external: 'External',
          externalTip: 'Outside the project directory',
        },
      },
    },
  },
})

function mountBadge(props: Record<string, any> = {}) {
  return mount(ExternalBadge, {
    props,
    global: { plugins: [i18n] },
  })
}

describe('ExternalBadge', () => {
  it('shows the short label for a directory', () => {
    expect(mountBadge({ kind: 'dir' }).text()).toContain('External')
  })

  it('shows the short label for a file', () => {
    expect(mountBadge({ kind: 'file' }).text()).toContain('External')
  })

  it('keeps the label to a single short word', () => {
    // Regression guard: the badge sits inline in dense rows, so a phrase like
    // "file outside the project" pushed the row's own content aside. The icon
    // carries the file-vs-dir distinction instead.
    for (const kind of ['dir', 'file']) {
      const text = mountBadge({ kind }).find('.external-badge').text().trim()
      expect(text).toBe('External')
      expect(text.split(/\s+/)).toHaveLength(1)
    }
  })

  it('spells the full meaning out in the tooltip', () => {
    // The short label alone is ambiguous, so the explanation has to live here.
    expect(mountBadge({ kind: 'dir' }).find('.external-badge').attributes('title'))
      .toBe('Outside the project directory')
  })

  it('renders an icon alongside the label', () => {
    // The icon carries the file-vs-directory distinction, which the single
    // shared label deliberately does not. It renders as an inline <svg> — a
    // module-level `lucide-vue-next` stub would NOT apply here, since the
    // component imports FolderOpen/FileText as named bindings.
    const wrapper = mountBadge({ kind: 'dir' })
    expect(wrapper.find('.external-badge svg').exists()).toBe(true)
    expect(wrapper.find('.external-badge').text().trim()).toBe('External')
  })
})

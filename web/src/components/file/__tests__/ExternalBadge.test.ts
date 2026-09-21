import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ExternalBadge from '@/components/file/ExternalBadge.vue'

const LucideStub = { template: '<span class="lucide-stub" />' }

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        nav: {
          externalDir: 'Directory outside project',
          externalFile: 'File outside project',
        },
      },
    },
  },
})

function mountBadge(props: Record<string, any> = {}) {
  return mount(ExternalBadge, {
    props,
    global: { stubs: { 'lucide-vue-next': LucideStub }, plugins: [i18n] },
  })
}

describe('ExternalBadge', () => {
  it('labels a directory as outside the project', () => {
    const wrapper = mountBadge({ kind: 'dir' })
    expect(wrapper.text()).toContain('Directory outside project')
  })

  it('labels a file as outside the project', () => {
    const wrapper = mountBadge({ kind: 'file' })
    expect(wrapper.text()).toContain('File outside project')
  })

  it('defaults to the file wording', () => {
    // The badge is dropped next to a filename in the common case, so a caller
    // that forgets `kind` should still read correctly rather than showing dir.
    expect(mountBadge().text()).toContain('File outside project')
  })

  it('exposes the label as a title for the icon-only reading', () => {
    const wrapper = mountBadge({ kind: 'dir' })
    expect(wrapper.find('.external-badge').attributes('title')).toBe('Directory outside project')
  })
})

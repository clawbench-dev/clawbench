import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileDetailsDrawer from '../FileDetailsDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { copy: 'Copy', copied: 'Copied' },
      file: {
        details: {
          title: 'File Details',
          fileName: 'File name',
          path: 'Path',
          type: 'Type',
          unknownType: 'Unknown',
          size: 'Size',
          modifiedTime: 'Modified',
          lineCount: 'Lines',
          charCount: 'Characters',
          encoding: 'Encoding',
          linkTarget: 'Actual path',
          brokenLink: '(Broken link)',
        },
      },
    },
  },
})

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: { projectRoot: '/project', currentDir: '', dirEntries: [] },
  },
}))

// BottomSheet is a wrapper; render its slot content directly for assertions.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    template: '<div><slot name="header" /><slot /></div>',
  },
}))

function mountDrawer(file) {
  return mount(FileDetailsDrawer, {
    props: { file, open: true },
    global: { plugins: [i18n], provide: { toast: null } },
  })
}

describe('FileDetailsDrawer', () => {
  it('renders linkTarget row for symlinked file', () => {
    const wrapper = mountDrawer({
      name: 'linked.txt',
      path: 'linked.txt',
      content: 'x',
      size: 1,
      isSymlink: true,
      linkTarget: 'real-files/target.txt',
    })
    const labels = wrapper.findAll('.details-label').map(n => n.text())
    expect(labels).toContain('Actual path')
    const values = wrapper.findAll('.details-value').map(n => n.text())
    expect(values).toContain('real-files/target.txt')
  })

  it('does not render linkTarget row for regular file', () => {
    const wrapper = mountDrawer({
      name: 'plain.txt',
      path: 'plain.txt',
      content: 'x',
      size: 1,
    })
    const labels = wrapper.findAll('.details-label').map(n => n.text())
    expect(labels).not.toContain('Actual path')
  })

  it('shows broken link placeholder when symlink target missing', () => {
    const wrapper = mountDrawer({
      name: 'dangling',
      path: 'dangling',
      isSymlink: true,
    })
    const values = wrapper.findAll('.details-value').map(n => n.text())
    expect(values).toContain('(Broken link)')
  })
})

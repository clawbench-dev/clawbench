import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileChangesDrawer from '@/components/chat/FileChangesDrawer.vue'

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: { name: 'BottomSheet', template: '<div class="bottom-sheet-stub"><slot name="header"></slot><slot></slot></div>' },
}))
vi.mock('@/components/common/FileIcon.vue', () => ({
  default: { name: 'FileIcon', template: '<span class="file-icon-stub" />' },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      chat: { fileChanges: { created: '新建', modified: '修改', openFile: '在文件管理器中打开' } },
      common: { close: '关闭' },
    },
  },
})

const CREATED = [{ path: '/proj/src/a.ts', toolIds: ['t1'] }]
const MODIFIED = [{ path: '/proj/src/b.ts', toolIds: ['t2'] }]

function mountDrawer(props = {}) {
  return mount(FileChangesDrawer, {
    global: { plugins: [i18n] },
    props: { open: true, created: CREATED, modified: MODIFIED, ...props },
  })
}

describe('FileChangesDrawer', () => {
  it('lists created and modified files', () => {
    const wrapper = mountDrawer()
    const names = wrapper.findAll('.fc-file-name').map((n) => n.text())
    expect(names).toEqual(['a.ts', 'b.ts'])
  })

  // Both sections carry the jump button, so a guard that only covered one would
  // leave the other dead in the share page.
  it('emits open-file for each section', async () => {
    const wrapper = mountDrawer()
    const jumps = wrapper.findAll('.fc-file-jump')
    expect(jumps).toHaveLength(2)
    await jumps[0].trigger('click')
    await jumps[1].trigger('click')
    expect(wrapper.emitted('open-file')).toEqual([['/proj/src/a.ts'], ['/proj/src/b.ts']])
  })
})

// The public share page renders this drawer from the inlined summary cards, but
// "open in file manager" routes through openFilePath, which the share page
// short-circuits. The button would look live and do nothing, so a read-only host
// hides it — while keeping the file rows themselves (they open the diff, which
// renders from the inlined tool input).
describe('FileChangesDrawer readOnly', () => {
  it('hides both jump buttons and keeps the file rows', () => {
    const wrapper = mountDrawer({ readOnly: true })
    expect(wrapper.findAll('.fc-file-main')).toHaveLength(2)
    expect(wrapper.findAll('.fc-file-jump')).toHaveLength(0)
  })

  it('keeps the jump buttons in a normal chat', () => {
    const wrapper = mountDrawer()
    expect(wrapper.findAll('.fc-file-jump')).toHaveLength(2)
  })
})

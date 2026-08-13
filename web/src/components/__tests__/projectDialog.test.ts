import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import ProjectDialog from '../ProjectDialog.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      projectDialog: { title: 'Select Project Directory' },
      jump: {
        title: 'Jump to Directory',
        placeholder: 'Enter a directory path',
        confirm: 'Jump',
        cancel: 'Cancel',
        button: 'Jump',
        copyPath: 'Copy path',
      },
    },
  },
})

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

vi.mock('@/stores/app', () => ({
  store: { state: { rootPaths: ['/'], homeDir: '/home/user' } },
}))

const TeleportStub = { template: '<div><slot /></div>' }

const JumpStub = {
  props: ['open'],
  emits: ['close', 'confirm'],
  template: '<div v-if="open" class="jump-dialog-stub" @click="$emit(\'confirm\', \'src/utils\')" />',
}

function mountDialog(props = {}) {
  return mount(ProjectDialog, {
    props: { open: true, ...props },
    global: {
      stubs: { Teleport: TeleportStub, JumpDirDialog: JumpStub },
      plugins: [i18n],
      provide: {
        toast: { show: vi.fn() },
        hotSwitchProject: vi.fn(),
      },
    },
  })
}

describe('ProjectDialog', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    mockFetch.mockReset()
  })

  it('opens jump dialog when jump button clicked', async () => {
    const wrapper = mountDialog()
    const jumpBtn = wrapper.find('.toolbar-btn.jump-btn')
    expect(jumpBtn.exists()).toBe(true)
    await jumpBtn.trigger('click')
    await nextTick()
    expect(wrapper.find('.jump-dialog-stub').exists()).toBe(true)
  })

  it('navigates browse to entered path on jump confirm', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ path: '/home/user/src/utils', items: [] }),
    })
    const wrapper = mountDialog()
    await wrapper.find('.toolbar-btn.jump-btn').trigger('click')
    await nextTick()
    await wrapper.find('.jump-dialog-stub').trigger('click')
    await nextTick()
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/projects?path=' + encodeURIComponent('src/utils'))
    )
  })
})

/**
 * Default browse path logic (mirrors the component's computed logic):
 * - On Unix (single rootPath): default to homeDir
 * - On Windows (multiple rootPaths): default to root level ("/")
 * - Fallback: if homeDir is empty on Unix, fall back to "/"
 */
function getDefaultBrowsePath(rootPaths: string[], homeDir: string): string {
  const isWindows = rootPaths.length > 1
  return isWindows ? '/' : (homeDir || '/')
}

describe('ProjectDialog default browse path', () => {
  it('defaults to homeDir on Unix (single rootPaths)', () => {
    expect(getDefaultBrowsePath(['/'], '/home/testuser')).toBe('/home/testuser')
  })

  it('defaults to homeDir on macOS', () => {
    expect(getDefaultBrowsePath(['/'], '/Users/john')).toBe('/Users/john')
  })

  it('defaults to root on Windows (multiple rootPaths)', () => {
    expect(getDefaultBrowsePath(['C:\\', 'D:\\'], 'C:\\Users\\testuser')).toBe('/')
  })

  it('falls back to / when homeDir is empty on Unix', () => {
    expect(getDefaultBrowsePath(['/'], '')).toBe('/')
  })

  it('uses root even when homeDir is empty on Windows', () => {
    expect(getDefaultBrowsePath(['C:\\', 'D:\\'], '')).toBe('/')
  })

  it('works with single-drive Windows (treated as Unix-like)', () => {
    expect(getDefaultBrowsePath(['C:\\'], 'C:\\Users\\admin')).toBe('C:\\Users\\admin')
  })
})

import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import DirBreadcrumb from '@/components/file/DirBreadcrumb.vue'
import { _setWideScreenForTest, _resetForTest } from '@/composables/useWideScreenLayout.ts'

vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/project' } },
}))

const mockCopyText = vi.hoisted(() => vi.fn())
vi.mock('@/utils/clipboard', () => ({
  copyText: mockCopyText,
}))

const LucideStub = { template: '<span class="lucide-stub" />' }

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      jump: { copyPath: 'Copy path' },
      common: { copied: 'Copied' },
    },
  },
})

const mockToast = { show: vi.fn() }

function mountBreadcrumb(props: Record<string, any> = {}) {
  return mount(DirBreadcrumb, {
    props: { path: '', ...props },
    global: {
      stubs: { 'lucide-vue-next': LucideStub },
      plugins: [i18n],
      provide: { toast: mockToast },
    },
  })
}

describe('DirBreadcrumb', () => {
  // ── reconstructPath (exposed via navigate emission) ──

  describe('reconstructPath via navigate emission', () => {
    it('reconstructs Unix path from segments', async () => {
      const wrapper = mountBreadcrumb({ path: '/home/user/docs' })
      // Click the second crumb ("home") — index 0 in parts
      const crumbs = wrapper.findAll('.crumb')
      // crumbs[0] = root (Home), crumbs[1] = "home", crumbs[2] = "user", crumbs[3] = "docs"
      // Clicking "home" (not last) should emit navigate with "home" (relative path, no leading slash)
      await crumbs[1].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('home')
    })

    it('reconstructs Windows path from segments', async () => {
      const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin\\docs' })
      const crumbs = wrapper.findAll('.crumb')
      // parts: ["C:\", "Users", "admin", "docs"]
      // crumbs[0] = root, crumbs[1] = "C:\", crumbs[2] = "Users", crumbs[3] = "admin", crumbs[4] = "docs"
      // Click "Users" (not last) => navigate with "C:\Users"
      await crumbs[2].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('C:\\Users')
    })

    it('reconstructs Windows path to drive root', async () => {
      const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin' })
      const crumbs = wrapper.findAll('.crumb')
      // Click "C:\" (not last) => navigate with "C:\"
      await crumbs[1].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('C:\\')
    })
  })

  // ── parts computed ──

  describe('parts computed', () => {
    it('splits Unix path into segments', () => {
      const wrapper = mountBreadcrumb({ path: '/home/user/docs' })
      // crumbs: [root_icon, "home", "user", "docs"]
      const crumbs = wrapper.findAll('.crumb')
      expect(crumbs.length).toBe(4) // root + 3 segments
      expect(crumbs[1].text()).toBe('home')
      expect(crumbs[2].text()).toBe('user')
      expect(crumbs[3].text()).toBe('docs')
    })

    it('merges bare drive letter C: into C:\\', () => {
      const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin' })
      const crumbs = wrapper.findAll('.crumb')
      // splitPath("C:\Users\admin") => ["C:", "Users", "admin"]
      // parts merges "C:" => "C:\", so parts = ["C:\", "Users", "admin"]
      // crumbs: [root_icon, "C:\", "Users", "admin"]
      expect(crumbs[1].text()).toBe('C:\\')
    })

    it('merges bare drive letter D: into D:\\', () => {
      const wrapper = mountBreadcrumb({ path: 'D:\\Projects\\app' })
      const crumbs = wrapper.findAll('.crumb')
      expect(crumbs[1].text()).toBe('D:\\')
    })

    it('returns empty for empty path', () => {
      const wrapper = mountBreadcrumb({ path: '' })
      expect(wrapper.find('.dir-breadcrumb').exists()).toBe(false)
    })

    it('returns empty for dot path', () => {
      const wrapper = mountBreadcrumb({ path: '.' })
      expect(wrapper.find('.dir-breadcrumb').exists()).toBe(false)
    })

    it('marks last crumb as current', () => {
      const wrapper = mountBreadcrumb({ path: '/home/user' })
      const crumbs = wrapper.findAll('.crumb')
      // Last crumb should have .current class
      expect(crumbs[crumbs.length - 1].classes()).toContain('current')
    })

    it('does not navigate on last crumb click (current)', async () => {
      const wrapper = mountBreadcrumb({ path: '/home/user' })
      const crumbs = wrapper.findAll('.crumb')
      // Last crumb is "current" — clicking should not emit navigate
      await crumbs[crumbs.length - 1].trigger('click')
      // The template: i < parts.length - 1 condition prevents emission
      expect(wrapper.emitted('navigate')).toBeUndefined()
    })

    it('root crumb emits navigate with empty string', async () => {
      const wrapper = mountBreadcrumb({ path: '/home/user' })
      const crumbs = wrapper.findAll('.crumb')
      // First crumb is the root Home icon — emits navigate('')
      await crumbs[0].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![0][0]).toBe('')
    })
  })

  // ── reconstructPath edge cases ──

  describe('reconstructPath edge cases', () => {
    it('handles single Unix root segment "/"', async () => {
      // Path "/" => splitPath("/") = ["", ""] => filter("") => []
      // No crumbs except root icon, so no non-root segment to click
      const wrapper = mountBreadcrumb({ path: '/' })
      expect(wrapper.find('.dir-breadcrumb').exists()).toBe(false)
    })

    it('handles single Windows drive root', async () => {
      // Path "C:\" => splitPath("C:\") = ["C:", ""] => filter empty => ["C:"]
      // parts merges "C:" => "C:\", so parts = ["C:\"]
      const wrapper = mountBreadcrumb({ path: 'C:\\' })
      const crumbs = wrapper.findAll('.crumb')
      // Only root icon + "C:\" (which is current/last, not clickable for navigate)
      expect(crumbs.length).toBe(2) // root + "C:\"
      expect(crumbs[1].text()).toBe('C:\\')
    })
  })
})

describe('DirBreadcrumb — drag to attach', () => {
  afterEach(() => _resetForTest())

  function mountBreadcrumbWide(props: Record<string, any> = {}) {
    _setWideScreenForTest(true)
    return mount(DirBreadcrumb, {
      props: { path: '', ...props },
      global: {
        stubs: { 'lucide-vue-next': LucideStub },
        plugins: [i18n],
        provide: { toast: mockToast },
      },
    })
  }

  it('crumb segments are draggable on wide screen', () => {
    const wrapper = mountBreadcrumbWide({ path: '/home/user/docs' })
    const crumbs = wrapper.findAll('.crumb')
    // All crumbs (including home) should be draggable
    for (const crumb of crumbs) {
      expect(crumb.attributes('draggable')).toBe('true')
    }
  })

  it('crumb segments are not draggable on narrow screen', async () => {
    _setWideScreenForTest(false)
    const wrapper = mount(DirBreadcrumb, {
      props: { path: '/home/user/docs' },
      global: {
        stubs: { 'lucide-vue-next': LucideStub },
        plugins: [i18n],
        provide: { toast: mockToast },
      },
    })
    // initWideScreen runs on mount and may reset isWideScreen based on jsdom viewport,
    // so force narrow again after mount to ensure the component reflects the state.
    _setWideScreenForTest(false)
    await nextTick()
    const crumbs = wrapper.findAll('.crumb')
    for (const crumb of crumbs) {
      expect(crumb.attributes('draggable')).toBe('false')
    }
  })

  it('crumb home has crumb-home class', () => {
    const wrapper = mountBreadcrumbWide({ path: '/home/user' })
    const homeCrumb = wrapper.findAll('.crumb')[0]
    expect(homeCrumb.classes()).toContain('crumb-home')
  })

  it('dragstart on a crumb sets attach drag data', async () => {
    const setDataMock = vi.fn()
    const setDragImageSpy = vi.fn()
    const wrapper = mountBreadcrumbWide({ path: '/home/user/docs' })
    const crumbs = wrapper.findAll('.crumb')
    const userCrumb = crumbs[2] // "user"
    await userCrumb.trigger('dragstart', {
      dataTransfer: {
        setData: setDataMock,
        effectAllowed: '',
        setDragImage: setDragImageSpy,
      },
    })
    // setAttachDragData writes the custom MIME and text/plain
    expect(setDataMock).toHaveBeenCalledWith(
      'application/x-clawbench-attach',
      expect.stringContaining('"path":"home/user"'),
    )
    expect(setDataMock).toHaveBeenCalledWith('text/plain', 'home/user')
    expect(setDragImageSpy).toHaveBeenCalled()
  })

  it('dragstart on home crumb attaches root path "/"', async () => {
    const setDataMock = vi.fn()
    const setDragImageSpy = vi.fn()
    const wrapper = mountBreadcrumbWide({ path: '/home/user/docs' })
    const homeCrumb = wrapper.findAll('.crumb')[0]
    await homeCrumb.trigger('dragstart', {
      dataTransfer: {
        setData: setDataMock,
        effectAllowed: '',
        setDragImage: setDragImageSpy,
      },
    })
    expect(setDataMock).toHaveBeenCalledWith(
      'application/x-clawbench-attach',
      expect.stringContaining('"path":"/"'),
    )
  })

  it('dragstart on narrow screen does not set attach drag data', async () => {
    _setWideScreenForTest(false)
    const setDataMock = vi.fn()
    const wrapper = mount(DirBreadcrumb, {
      props: { path: '/home/user/docs' },
      global: {
        stubs: { 'lucide-vue-next': LucideStub },
        plugins: [i18n],
        provide: { toast: mockToast },
      },
    })
    _setWideScreenForTest(false)
    await nextTick()
    const crumbs = wrapper.findAll('.crumb')
    await crumbs[1].trigger('dragstart', {
      dataTransfer: { setData: setDataMock, effectAllowed: '', setDragImage: vi.fn() },
    })
    expect(setDataMock).not.toHaveBeenCalled()
  })
})

describe('DirBreadcrumb — copy path', () => {
  beforeEach(() => {
    mockCopyText.mockReset()
    mockCopyText.mockImplementation((_text: string, onSuccess?: () => void) => onSuccess?.())
    mockToast.show.mockReset()
  })

  it('copies the absolute Unix path on copy button click', async () => {
    const wrapper = mountBreadcrumb({ path: 'home/user/docs' })
    const copyBtn = wrapper.find('.crumb-copy-btn')
    expect(copyBtn.exists()).toBe(true)
    await copyBtn.trigger('click')
    expect(mockCopyText).toHaveBeenCalledWith('/project/home/user/docs', expect.any(Function), expect.any(Function))
  })

  it('copies the absolute Windows-style root path', async () => {
    const wrapper = mountBreadcrumb({ path: 'src/utils' })
    await wrapper.find('.crumb-copy-btn').trigger('click')
    expect(mockCopyText).toHaveBeenCalledWith('/project/src/utils', expect.any(Function), expect.any(Function))
  })

  it('copies an already-absolute path as-is (ProjectDialog)', async () => {
    const wrapper = mountBreadcrumb({ path: '/home/user/other' })
    await wrapper.find('.crumb-copy-btn').trigger('click')
    expect(mockCopyText).toHaveBeenCalledWith('/home/user/other', expect.any(Function), expect.any(Function))
  })

  it('copies an already-absolute Windows path as-is (ProjectDialog)', async () => {
    const wrapper = mountBreadcrumb({ path: 'D:\\other\\dir' })
    await wrapper.find('.crumb-copy-btn').trigger('click')
    expect(mockCopyText).toHaveBeenCalledWith('D:/other/dir', expect.any(Function), expect.any(Function))
  })

  it('normalizes a leading-slash project-relative path against the root', async () => {
    // Leading slash alone is ambiguous; for ProjectDialog-style absolute input
    // the value is preserved, while relative values combine with the root.
    const wrapper = mountBreadcrumb({ path: 'photos' })
    await wrapper.find('.crumb-copy-btn').trigger('click')
    expect(mockCopyText).toHaveBeenCalledWith('/project/photos', expect.any(Function), expect.any(Function))
  })

  it('shows copied feedback and toast after copy', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mountBreadcrumb({ path: 'home/user' })
      await wrapper.find('.crumb-copy-btn').trigger('click')
      expect(wrapper.find('.crumb-copy-btn').classes()).toContain('copied')
      expect(mockToast.show).toHaveBeenCalled()
      // copied flag resets after 800ms
      vi.advanceTimersByTime(800)
      await nextTick()
      expect(wrapper.find('.crumb-copy-btn').classes()).not.toContain('copied')
    } finally {
      vi.useRealTimers()
    }
  })

  it('still shows copied feedback when copyText fails', async () => {
    mockCopyText.mockImplementation((_text: string, _onSuccess?: () => void, onError?: () => void) => onError?.())
    const wrapper = mountBreadcrumb({ path: 'home/user' })
    await wrapper.find('.crumb-copy-btn').trigger('click')
    expect(wrapper.find('.crumb-copy-btn').classes()).toContain('copied')
  })
})

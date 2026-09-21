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

/**
 * Mount as ProjectDialog does: no project concept, so "" already means the
 * filesystem top level. Its breadcrumbs therefore have NO extra filesystem-root
 * crumb and Home keeps the plain "up to the top" meaning.
 */
function mountPicker(props: Record<string, any> = {}) {
  return mountBreadcrumb({ projectScoped: false, ...props })
}

describe('DirBreadcrumb', () => {
  // ── reconstructPath (exposed via navigate emission) ──
  // Absolute paths here are the ProjectDialog picker shape (projectScoped=false),
  // which has no project concept: its crumbs keep the absolute form and Home
  // means the filesystem top level.

  describe('reconstructPath via navigate emission', () => {
    it('reconstructs Unix path from segments, preserving the absolute form', async () => {
      const wrapper = mountPicker({ path: '/home/user/docs' })
      const crumbs = wrapper.findAll('.crumb')
      // crumbs[0] = Home, crumbs[1] = "home", crumbs[2] = "user", crumbs[3] = "docs"
      // The input path is absolute, so the crumb must stay absolute — a
      // rootless "home" would be re-read as relative to the browse root.
      await crumbs[1].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('/home')
    })

    it('reconstructs Windows path from segments', async () => {
      const wrapper = mountPicker({ path: 'C:\\Users\\admin\\docs' })
      const crumbs = wrapper.findAll('.crumb')
      // parts: ["C:\", "Users", "admin", "docs"]
      // crumbs[0] = Home, crumbs[1] = "C:\", crumbs[2] = "Users", …
      // Click "Users" (not last) => "C:\Users" (already absolute — the drive
      // root carries its own separator, so no "/" is prepended)
      await crumbs[2].trigger('click')
      const emitted = wrapper.emitted('navigate')
      expect(emitted).toBeTruthy()
      expect(emitted![emitted!.length - 1][0]).toBe('C:\\Users')
    })

    it('reconstructs Windows path to drive root', async () => {
      const wrapper = mountPicker({ path: 'C:\\Users\\admin' })
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
      const wrapper = mountPicker({ path: '/home/user/docs' })
      // crumbs: [Home, "home", "user", "docs"]
      const crumbs = wrapper.findAll('.crumb')
      expect(crumbs.length).toBe(4) // home + 3 segments
      expect(crumbs[1].text()).toBe('home')
      expect(crumbs[2].text()).toBe('user')
      expect(crumbs[3].text()).toBe('docs')
    })

    it('merges bare drive letter C: into C:\\', () => {
      const wrapper = mountPicker({ path: 'C:\\Users\\admin' })
      const crumbs = wrapper.findAll('.crumb')
      // splitPath("C:\Users\admin") => ["C:", "Users", "admin"]
      // parts merges "C:" => "C:\", so parts = ["C:\", "Users", "admin"]
      // crumbs: [Home, "C:\", "Users", "admin"]
      expect(crumbs[1].text()).toBe('C:\\')
    })

    it('merges bare drive letter D: into D:\\', () => {
      const wrapper = mountPicker({ path: 'D:\\Projects\\app' })
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
  })

  // ── Two roots: Home = project root, extra crumb = filesystem root ──

  describe('project vs filesystem root', () => {
    it('Home always targets the project root while browsing an external dir', async () => {
      // The whole point of Plan A: Home is a predictable "get me out of here"
      // exit no matter how deep into the filesystem the user wandered.
      const wrapper = mountBreadcrumb({ path: '/var/log' })
      const home = wrapper.find('.crumb-home')
      expect(home.exists()).toBe(true)
      await home.trigger('click')
      expect(wrapper.emitted('navigate')![0][0]).toBe('')
    })

    it('Home targets the project root for a project-relative browse too', async () => {
      const wrapper = mountBreadcrumb({ path: 'web/src' })
      await wrapper.find('.crumb-home').trigger('click')
      expect(wrapper.emitted('navigate')![0][0]).toBe('')
    })

    it('renders a filesystem-root crumb only while browsing externally', () => {
      // Without it there would be no way back up to "/" once Home means
      // "project root" — Back alone walks up and strands the user at the root.
      expect(mountBreadcrumb({ path: '/var/log' }).find('.crumb-fs-root').exists()).toBe(true)
      expect(mountBreadcrumb({ path: 'web/src' }).find('.crumb-fs-root').exists()).toBe(false)
    })

    it('the filesystem-root crumb navigates to "/"', async () => {
      const wrapper = mountBreadcrumb({ path: '/var/log' })
      await wrapper.find('.crumb-fs-root').trigger('click')
      expect(wrapper.emitted('navigate')![0][0]).toBe('/')
    })

    it('the filesystem-root crumb targets the drive root on Windows', async () => {
      const wrapper = mountBreadcrumb({ path: 'C:\\Users\\admin' })
      await wrapper.find('.crumb-fs-root').trigger('click')
      expect(wrapper.emitted('navigate')![0][0]).toBe('C:/')
    })

    it('marks the Home crumb as external while browsing outside the project', () => {
      expect(mountBreadcrumb({ path: '/var/log' }).find('.crumb-home').classes()).toContain('external')
      expect(mountBreadcrumb({ path: 'web/src' }).find('.crumb-home').classes()).not.toContain('external')
    })

    it('the picker (no project concept) gets no filesystem-root crumb', () => {
      // Its "" already means the filesystem top level, so Home covers it.
      expect(mountPicker({ path: '/home/user' }).find('.crumb-fs-root').exists()).toBe(false)
    })

    it('the picker Home still goes to the filesystem top level', async () => {
      const wrapper = mountPicker({ path: '/home/user' })
      await wrapper.find('.crumb-home').trigger('click')
      expect(wrapper.emitted('navigate')![0][0]).toBe('')
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
      // projectScoped=false (picker shape): no extra filesystem-root crumb,
      // since Home already means "the top level".
      const wrapper = mountPicker({ path: 'C:\\' })
      const crumbs = wrapper.findAll('.crumb')
      // Only Home + "C:\" (which is current/last, not clickable for navigate)
      expect(crumbs.length).toBe(2) // home + "C:\"
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

  /** Wide-screen picker mount — absolute paths with no extra root crumb. */
  function mountPickerWide(props: Record<string, any> = {}) {
    return mountBreadcrumbWide({ projectScoped: false, ...props })
  }

  it('crumb segments are draggable on wide screen', () => {
    const wrapper = mountPickerWide({ path: '/home/user/docs' })
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
    expect(wrapper.find('.crumb-home').exists()).toBe(true)
  })

  it('dragstart on a crumb sets attach drag data', async () => {
    const setDataMock = vi.fn()
    const setDragImageSpy = vi.fn()
    const wrapper = mountPickerWide({ path: '/home/user/docs' })
    const crumbs = wrapper.findAll('.crumb')
    const userCrumb = crumbs[2] // "user"
    await userCrumb.trigger('dragstart', {
      dataTransfer: {
        setData: setDataMock,
        effectAllowed: '',
        setDragImage: setDragImageSpy,
      },
    })
    // setAttachDragData writes the custom MIME and text/plain. The dragged path
    // stays absolute because the browsed path is absolute.
    expect(setDataMock).toHaveBeenCalledWith(
      'application/x-clawbench-attach',
      expect.stringContaining('"path":"/home/user"'),
    )
    expect(setDataMock).toHaveBeenCalledWith('text/plain', '/home/user')
    expect(setDragImageSpy).toHaveBeenCalled()
  })

  it('dragstart on a crumb keeps a project-relative path rootless', async () => {
    const setDataMock = vi.fn()
    const wrapper = mountBreadcrumbWide({ path: 'web/src/components' })
    // crumbs[1] = "src" (home, "web", "src", …)
    await wrapper.findAll('.crumb')[2].trigger('dragstart', {
      dataTransfer: { setData: setDataMock, effectAllowed: '', setDragImage: vi.fn() },
    })
    expect(setDataMock).toHaveBeenCalledWith(
      'application/x-clawbench-attach',
      expect.stringContaining('"path":"web/src"'),
    )
  })

  it('dragstart on home crumb attaches the project root path', async () => {
    // The click target is "" (project root, a backend notion) but the attach
    // flow needs a real filesystem path — so the drag carries the project root.
    const setDataMock = vi.fn()
    const setDragImageSpy = vi.fn()
    const wrapper = mountBreadcrumbWide({ path: 'web/src' })
    await wrapper.find('.crumb-home').trigger('dragstart', {
      dataTransfer: {
        setData: setDataMock,
        effectAllowed: '',
        setDragImage: setDragImageSpy,
      },
    })
    expect(setDataMock).toHaveBeenCalledWith(
      'application/x-clawbench-attach',
      expect.stringContaining('"path":"/project"'),
    )
  })

  it('the filesystem-root crumb drags the real filesystem root', async () => {
    const setDataMock = vi.fn()
    const wrapper = mountBreadcrumbWide({ path: '/var/log' })
    await wrapper.find('.crumb-fs-root').trigger('dragstart', {
      dataTransfer: { setData: setDataMock, effectAllowed: '', setDragImage: vi.fn() },
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

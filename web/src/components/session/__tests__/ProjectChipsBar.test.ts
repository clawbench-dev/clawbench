import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ProjectChipsBar from '@/components/session/ProjectChipsBar.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

function okJson(body: unknown) {
  return Promise.resolve({ ok: true, json: () => Promise.resolve(body) })
}

describe('ProjectChipsBar', () => {
  let wrapper: ReturnType<typeof mount> | undefined

  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockImplementation(() => okJson(['/home/me/alpha', '/home/me/beta', '/home/me/gamma']))
  })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined })

  function mountBar(props = {}, options = {}) {
    wrapper = mount(ProjectChipsBar, {
      props: { projectRoot: '/home/me/alpha', homeDir: '/home/me', ...props },
      ...options,
    })
    return wrapper
  }

  it('fetches recent projects', async () => {
    const wrapper = mountBar()
    await flushPromises()
    expect(mockFetch).toHaveBeenCalledWith('/api/recent-projects')
  })

  it('excludes the current project from the chips', async () => {
    const wrapper = mountBar({ projectRoot: '/home/me/beta' })
    await flushPromises()
    const names = wrapper.findAll('.project-chips-scroll .project-chip').map((c) => c.text())
    expect(names).toContain('alpha')
    expect(names).toContain('gamma')
    expect(names).not.toContain('beta')
  })

  it('renders nothing when the current project is the only recent project', async () => {
    mockFetch.mockImplementation(() => okJson(['/home/me/alpha']))
    const wrapper = mountBar({ projectRoot: '/home/me/alpha' })
    await flushPromises()
    expect(wrapper.find('.project-chips-bar').exists()).toBe(false)
  })

  it('switches project via injected hotSwitchProject on click', async () => {
    const hotSwitchProject = vi.fn().mockResolvedValue(undefined)
    const wrapper = mountBar({}, { global: { provide: { hotSwitchProject } } })
    await flushPromises()
    const beta = wrapper.findAll('.project-chips-scroll .project-chip').find((c) => c.text() === 'beta')!
    await beta.trigger('click')
    expect(hotSwitchProject).toHaveBeenCalledWith('/home/me/beta')
  })

  it('refetches when the project root changes', async () => {
    const wrapper = mountBar()
    await flushPromises()
    expect(mockFetch).toHaveBeenCalledTimes(1)
    await wrapper.setProps({ projectRoot: '/home/me/beta' })
    await flushPromises()
    expect(mockFetch).toHaveBeenCalledTimes(2)
  })

  it('renders nothing when the fetch fails', async () => {
    mockFetch.mockImplementation(() => Promise.reject(new Error('network')))
    const wrapper = mountBar()
    await flushPromises()
    expect(wrapper.find('.project-chips-bar').exists()).toBe(false)
  })

  it('shows every chip when the bar width is not measurable (jsdom default)', async () => {
    const wrapper = mountBar()
    await flushPromises()
    // clientWidth is 0 in jsdom → degrade to showing all non-current chips.
    const names = wrapper.findAll('.project-chips-scroll .project-chip').map((c) => c.text())
    expect(names).toEqual(['beta', 'gamma'])
  })
})

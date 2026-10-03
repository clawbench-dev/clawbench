import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ProjectsSetting from '@/components/settings/ProjectsSetting.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// The current project comes from the app store; a plain reactive object keeps
// the comparison live without mounting the whole store.
vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/home/me/current' } },
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { loading: 'Loading…' },
      settings: {
        items: {
          projectListEmpty: 'No projects',
          projectListSearchPlaceholder: 'Search projects',
          projectListCurrent: 'Current',
          projectListMissing: 'Directory deleted',
          projectListLoadFailed: 'Failed to load projects',
          projectDetailDirExists: 'Exists',
          projectDetailDirMissing: 'Deleted',
        },
      },
    },
  },
})

function listResponse(projects: unknown[]) {
  return { ok: true, json: async () => ({ projects }) } as unknown as Response
}

function row(over: Record<string, unknown> = {}) {
  return {
    id: 1,
    path: '/home/me/alpha',
    session_count: 3,
    last_active_at: '2026-01-01 00:00:00',
    created_at: '2025-12-01 00:00:00',
    exists: true,
    ...over,
  }
}

function mountCard() {
  return mount(ProjectsSetting, { global: { plugins: [i18n] } })
}

describe('ProjectsSetting', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('fetch', vi.fn(async () => listResponse([])))
  })

  it('fetches and renders each project', async () => {
    const fetchMock = vi.fn(async () => listResponse([
      row({ id: 1, path: '/home/me/alpha' }),
      row({ id: 2, path: '/home/me/beta', exists: false }),
    ]))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mountCard()
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/projects/list')
    const rows = wrapper.findAll('.projects-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('alpha')
    expect(rows[1].text()).toContain('beta')
  })

  it('shows the empty state when the registry has no projects', async () => {
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.text()).toContain('No projects')
  })

  it('flags a deleted directory with the gone dot', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => listResponse([
      row({ id: 1, path: '/home/me/gone', exists: false }),
    ])))
    const wrapper = mountCard()
    await flushPromises()

    expect(wrapper.find('.projects-row__dot--gone').exists()).toBe(true)
    expect(wrapper.find('.projects-row__dot--ok').exists()).toBe(false)
  })

  it('marks the current project with a badge', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => listResponse([
      row({ id: 1, path: '/home/me/current' }),
      row({ id: 2, path: '/home/me/other' }),
    ])))
    const wrapper = mountCard()
    await flushPromises()

    const current = wrapper.find('.projects-row--current')
    expect(current.exists()).toBe(true)
    expect(current.text()).toContain('Current')
    expect(wrapper.findAll('.projects-row__badge')).toHaveLength(1)
  })

  it('filters by path or basename as you type', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => listResponse([
      row({ id: 1, path: '/home/me/alpha' }),
      row({ id: 2, path: '/home/me/beta' }),
    ])))
    const wrapper = mountCard()
    await flushPromises()
    expect(wrapper.findAll('.projects-row')).toHaveLength(2)

    await wrapper.find('.projects-search__input').setValue('alp')
    await flushPromises()
    const rows = wrapper.findAll('.projects-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('alpha')
  })

  it('emits navigate with project:<id> when a row is clicked', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => listResponse([
      row({ id: 42, path: '/home/me/alpha' }),
    ])))
    const wrapper = mountCard()
    await flushPromises()

    await wrapper.find('.projects-row').trigger('click')
    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['project:42'])
  })

  it('shows an error toast when the request fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500 }) as unknown as Response))
    const wrapper = mountCard()
    await flushPromises()

    expect(mockToastShow).toHaveBeenCalledWith('Failed to load projects', expect.anything())
    expect(wrapper.find('.projects-row').exists()).toBe(false)
  })
})

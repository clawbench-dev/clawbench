import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SessionMoveDialog from '@/components/session/SessionMoveDialog.vue'

const { mockGet, mockPatch } = vi.hoisted(() => ({
  mockGet: vi.fn(),
  mockPatch: vi.fn(),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params && 'members' in params ? `${key}[${params.members}]` : key,
    }),
  }
})
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))
vi.mock('@/utils/api', () => ({ apiGet: mockGet, apiPatch: mockPatch }))
// ModalDialog teleports to body; stub it so the body is inspectable in place.
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    props: ['open', 'title', 'zIndex'],
    template: '<div class="modal-stub" v-if="open"><slot /><slot name="footer" /></div>',
  },
}))

function projects() {
  return {
    projects: [
      { id: 1, path: '/w/src', exists: true },
      { id: 2, path: '/w/target', exists: true },
      { id: 3, path: '/w/gone', exists: false },
    ],
  }
}

describe('SessionMoveDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue(projects())
    mockPatch.mockResolvedValue({ ok: true })
  })

  async function mountDialog() {
    const w = mount(SessionMoveDialog, {
      props: { open: false, sessionId: 's1', currentPath: '/w/src' },
    })
    await w.setProps({ open: true })
    await flushPromises()
    return w
  }

  it('lists only other projects that still exist on disk', async () => {
    const w = await mountDialog()
    const items = w.findAll('.sm-item')
    expect(items).toHaveLength(1)
    expect(items[0].text()).toContain('/w/target')
    // The current project and a missing directory are never offered.
    expect(w.text()).not.toContain('/w/gone')
  })

  it('PATCHes projectPath and reports the move', async () => {
    const w = await mountDialog()
    await w.find('.sm-item').trigger('click')
    await flushPromises()

    expect(mockPatch).toHaveBeenCalledWith(
      '/api/ai/session/update?session_id=s1',
      { projectPath: '/w/target' },
    )
    expect(w.emitted('moved')?.[0]).toEqual(['/w/target'])
    expect(w.emitted('close')).toBeTruthy()
  })

  it('explains a running conversation instead of a generic failure', async () => {
    mockPatch.mockRejectedValueOnce(Object.assign(new Error('conflict'), { msgKey: 'SessionRunning' }))
    const w = await mountDialog()
    await w.find('.sm-item').trigger('click')
    await flushPromises()

    expect(w.find('.sm-error').text()).toBe('sessionMove.running')
    // Still open so the user can retry once the turn ends.
    expect(w.emitted('close')).toBeFalsy()
  })

  it('explains that only conversations and groups can move', async () => {
    mockPatch.mockRejectedValueOnce(Object.assign(new Error('nope'), { msgKey: 'MoveNotAllowed' }))
    const w = await mountDialog()
    await w.find('.sm-item').trigger('click')
    await flushPromises()

    expect(w.find('.sm-error').text()).toBe('sessionMove.notAllowed')
  })

  it('names the members that are unavailable in the target project', async () => {
    mockPatch.mockRejectedValueOnce(Object.assign(new Error('members'), {
      msgKey: 'MoveMembersUnavailable',
      detail: { members: ['Alice', 'Bob'] },
    }))
    const w = await mountDialog()
    await w.find('.sm-item').trigger('click')
    await flushPromises()

    expect(w.find('.sm-error').text()).toContain('sessionMove.membersUnavailable')
    expect(w.find('.sm-error').text()).toContain('Alice')
    expect(w.find('.sm-error').text()).toContain('Bob')
  })

  it('filters by name or path', async () => {
    const w = await mountDialog()
    await w.find('.search-pill input').setValue('targ')
    expect(w.findAll('.sm-item')).toHaveLength(1)
    await w.find('.search-pill input').setValue('zzz')
    expect(w.findAll('.sm-item')).toHaveLength(0)
  })

  it('sends exactly one move while the first one is in flight', async () => {
    // A second tap during the round trip would send a second PATCH, which can
    // only fail (the row is already gone) and would report "移动失败" for a move
    // that actually worked.
    let release: (value: unknown) => void = () => {}
    mockPatch.mockImplementationOnce(() => new Promise(resolve => { release = resolve }))

    const w = await mountDialog()
    const item = w.find('.sm-item')
    await item.trigger('click')
    await item.trigger('click')
    await item.trigger('click')

    expect(mockPatch).toHaveBeenCalledTimes(1)
    expect(item.attributes('disabled')).toBeDefined()

    release({ ok: true })
    await flushPromises()
    expect(w.emitted('moved')).toHaveLength(1)
  })
})

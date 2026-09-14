import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import SessionTagDialog from '@/components/session/SessionTagDialog.vue'

const { mockStore, mockDialogHolder, mockGet, mockDelete, mockPatch } = vi.hoisted(() => ({
  mockStore: { state: { sessionListVersion: 0 } },
  mockDialogHolder: { confirm: null as null | ((m: string, o?: any) => Promise<boolean>) },
  mockGet: vi.fn(),
  mockDelete: vi.fn(),
  mockPatch: vi.fn(),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    // Include params so assertions can verify interpolation reached the key.
    useI18n: () => ({ t: (key: string, params?: any) => (params ? `${key}:${JSON.stringify(params)}` : key) }),
  }
})
vi.mock('@/stores/app', () => ({ store: mockStore }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))
vi.mock('@/utils/api', () => ({
  apiGet: mockGet,
  apiDelete: mockDelete,
  apiPatch: mockPatch,
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: (m: string, o?: any) => mockDialogHolder.confirm!(m, o),
  }),
}))
// ModalDialog teleports to body; stub it to a plain wrapper that forwards the
// default + footer slots so the dialog body is inspectable in-place.
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    props: ['open', 'title', 'zIndex'],
    template: '<div class="modal-stub" v-if="open"><slot /><slot name="footer" /></div>',
  },
}))

function tagFixtures() {
  return {
    tags: [
      { name: 'bug', scope: 'project', count: 2 },
      { name: 'shared', scope: 'global', count: 1 },
    ],
  }
}

describe('SessionTagDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGet.mockResolvedValue(tagFixtures())
    mockDelete.mockResolvedValue({ ok: true })
    mockPatch.mockResolvedValue({ ok: true })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    mockStore.state.sessionListVersion = 0
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  async function mountDialog(props = {}) {
    return mount(SessionTagDialog, {
      props: { open: true, sessionId: 's1', initialTags: [], ...props },
    })
  }

  it('loads candidates on open', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    expect(mockGet).toHaveBeenCalledWith('/api/ai/session/tags')
    expect(wrapper.vm.candidates.length).toBe(2)
  })

  it('seeds selection from initialTags', async () => {
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()
    expect(wrapper.vm.isSelected('bug')).toBe(true)
    expect(wrapper.vm.isSelected('shared')).toBe(false)
  })

  it('toggles a candidate on and off', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    wrapper.vm.toggleTag('bug')
    expect(wrapper.vm.selected).toEqual(['bug'])
    wrapper.vm.toggleTag('bug')
    expect(wrapper.vm.selected).toEqual([])
  })

  it('saves the selection as name+scope pairs', async () => {
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    wrapper.vm.toggleTag('shared')
    await wrapper.vm.save()

    expect(mockPatch).toHaveBeenCalledWith(
      '/api/ai/session/update?session_id=s1',
      { tags: [{ name: 'bug', scope: 'project' }, { name: 'shared', scope: 'global' }] }
    )
    expect(wrapper.emitted('saved')).toBeTruthy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('bumps sessionListVersion so other rows refresh', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()
    expect(mockStore.state.sessionListVersion).toBe(1)
  })

  it('does not save when the session id is missing', async () => {
    const wrapper = await mountDialog({ sessionId: '' })
    await flushPromises()
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()
    expect(mockPatch).not.toHaveBeenCalled()
  })

  it('keeps the dialog open when saving fails', async () => {
    mockPatch.mockRejectedValue(new Error('boom'))
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()
    // A failed save must not look successful: no close, no refresh.
    expect(wrapper.emitted('close')).toBeFalsy()
    expect(mockStore.state.sessionListVersion).toBe(0)
  })

  it('addNewTag creates a candidate and selects it', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    wrapper.vm.newTagName = '  fresh  '
    wrapper.vm.newTagScope = 'global'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.newTagName).toBe('')
    expect(wrapper.vm.selected).toEqual(['fresh'])
    const created = wrapper.vm.candidates.find((c: any) => c.name === 'fresh')
    expect(created.scope).toBe('global')
  })

  it('addNewTag selects an existing candidate instead of duplicating it', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    const before = wrapper.vm.candidates.length

    wrapper.vm.newTagName = 'bug'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.candidates.length).toBe(before)
    expect(wrapper.vm.selected).toEqual(['bug'])
  })

  it('addNewTag collapses internal whitespace like the backend', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    // The backend normalizes with strings.Fields + join, so "needs   review"
    // and "needs review" are the SAME tag. If the dialog kept the raw spacing,
    // the chip would silently change text after saving.
    wrapper.vm.newTagName = '  needs   review  '
    wrapper.vm.addNewTag()

    expect(wrapper.vm.selected).toEqual(['needs review'])
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'needs review')).toBe(true)
  })

  it('addNewTag folds case like the backend', async () => {
    const wrapper = await mountDialog()
    await flushPromises()

    // The backend folds tag names to lowercase, so "Bug" IS "bug". Without the
    // same fold here the dialog would create a second, near-identical chip.
    wrapper.vm.newTagName = 'Bug'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.selected).toEqual(['bug'])
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'bug')).toBe(true)
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'Bug')).toBe(false)
  })

  it('addNewTag does not duplicate an existing tag via a case variant', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    const before = wrapper.vm.candidates.length

    // 'bug' already exists as a candidate (fixture).
    wrapper.vm.newTagName = 'BUG'
    wrapper.vm.addNewTag()

    expect(wrapper.vm.candidates.length).toBe(before)
    expect(wrapper.vm.selected).toEqual(['bug'])
  })

  it('addNewTag ignores a blank name', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.newTagName = '   '
    wrapper.vm.addNewTag()
    expect(wrapper.vm.selected).toEqual([])
    expect(wrapper.vm.canAdd).toBe(false)
  })

  it('an existing tag keeps its own scope on save', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    // Picker set to global, but 'bug' already exists as a project tag — the
    // backend would refuse the re-scope anyway, so the payload must not ask.
    wrapper.vm.newTagScope = 'global'
    wrapper.vm.toggleTag('bug')
    await wrapper.vm.save()

    expect(mockPatch).toHaveBeenCalledWith(
      '/api/ai/session/update?session_id=s1',
      { tags: [{ name: 'bug', scope: 'project' }] }
    )
  })

  it('requestDelete confirms, deletes, and unselects the tag', async () => {
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.vm.requestDelete({ name: 'bug', scope: 'project' })
    await flushPromises()

    expect(mockDialogHolder.confirm).toHaveBeenCalled()
    expect(mockDelete).toHaveBeenCalledWith('/api/ai/session/tags?name=bug&scope=project')
    expect(wrapper.vm.candidates.some((c: any) => c.name === 'bug')).toBe(false)
    expect(wrapper.vm.selected).toEqual([])
    // Deleting affects every session carrying the label, not just this one.
    expect(mockStore.state.sessionListVersion).toBe(1)
  })

  it('requestDelete does nothing when the user cancels', async () => {
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(false)
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.vm.requestDelete({ name: 'bug', scope: 'project' })

    expect(mockDelete).not.toHaveBeenCalled()
    expect(wrapper.vm.selected).toEqual(['bug'])
  })

  it('URL-encodes the tag name on delete', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    await wrapper.vm.requestDelete({ name: 'needs review', scope: 'project' })
    await flushPromises()
    expect(mockDelete).toHaveBeenCalledWith('/api/ai/session/tags?name=needs%20review&scope=project')
  })

  it('keeps the tag when deletion fails', async () => {
    mockDelete.mockRejectedValue(new Error('nope'))
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.vm.requestDelete({ name: 'bug', scope: 'project' })
    await flushPromises()

    expect(wrapper.vm.candidates.some((c: any) => c.name === 'bug')).toBe(true)
    expect(mockStore.state.sessionListVersion).toBe(0)
  })

  it('resets transient input each time it reopens', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    wrapper.vm.newTagName = 'leftover'
    wrapper.vm.newTagScope = 'global'

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(wrapper.vm.newTagName).toBe('')
    expect(wrapper.vm.newTagScope).toBe('project')
  })

  it('re-seeds the selection from initialTags on reopen', async () => {
    const wrapper = await mountDialog({ initialTags: [] })
    await flushPromises()
    wrapper.vm.toggleTag('bug')

    await wrapper.setProps({ open: false })
    await wrapper.setProps({ initialTags: ['shared'], open: true })
    await flushPromises()

    // Stale selection from the previous session must not leak into the new one.
    expect(wrapper.vm.selected).toEqual(['shared'])
  })

  it('renders a global scope badge only for global tags', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    await nextTick()

    const badges = wrapper.findAll('.st-scope-badge')
    expect(badges.length).toBe(1)
    expect(wrapper.findAll('.st-candidate').length).toBe(2)
  })

  it('shows the global hint only when the picker is global', async () => {
    const wrapper = await mountDialog()
    await flushPromises()
    expect(wrapper.find('.st-scope-hint').exists()).toBe(false)

    wrapper.vm.newTagScope = 'global'
    await nextTick()
    expect(wrapper.find('.st-scope-hint').exists()).toBe(true)
  })

  it('surfaces a load failure without crashing', async () => {
    mockGet.mockRejectedValue(new Error('offline'))
    const wrapper = await mountDialog()
    await flushPromises()
    expect(wrapper.vm.candidates).toEqual([])
    expect(wrapper.vm.loading).toBe(false)
  })
  // The global reset zeroes padding/margin on every element (web/css/base.css),
  // so a control that declares only layout (flex/min-width) renders as bare UA
  // chrome — no border, no background, no hit area. That is exactly how this
  // dialog shipped once: the input read as plain text and the create button as
  // a text link, while the chips above (which do declare a surface) looked
  // right. jsdom does not cascade CSS, so assert on the declared source rules.
  describe('form controls declare their own surface', () => {
    const src = readFileSync(resolve(process.cwd(), 'src/components/session/SessionTagDialog.vue'), 'utf8')
    const ruleFor = (selector: string) => {
      // Match ".selector { ... }" and take the declaration block.
      const m = new RegExp(`\\${selector}\\s*\\{([^}]*)\\}`).exec(src)
      return m ? m[1] : ''
    }

    it.each([
      ['.st-input', 'the text field'],
      ['.st-scope-select', 'the scope dropdown'],
    ])('%s (%s) declares border, background and padding', (selector) => {
      const rule = ruleFor(selector)
      expect(rule, `${selector} rule must exist`).not.toBe('')
      expect(rule).toMatch(/border:/)
      expect(rule).toMatch(/background:/)
      expect(rule).toMatch(/padding:/)
    })

    it('.st-add-btn declares a button surface, not just layout', () => {
      const rule = ruleFor('.st-add-btn')
      expect(rule).not.toBe('')
      // A <button> with no border declaration keeps the UA border, which reads
      // as an unstyled element next to the bordered input.
      expect(rule).toMatch(/border:/)
      expect(rule).toMatch(/background:/)
      expect(rule).toMatch(/padding:/)
    })

    it('.st-checkbox declares an explicit size', () => {
      const rule = ruleFor('.st-checkbox')
      expect(rule).toMatch(/width:/)
      expect(rule).toMatch(/height:/)
    })

    it('.session-tags-dialog insets its content from the card edge', () => {
      // The shared .modal-body is deliberately padding-free (modal-card.css) —
      // it only supplies the scroll container. A dialog that does not inset its
      // own root renders flush against the card border, which is how this one
      // shipped once.
      const rule = ruleFor('.session-tags-dialog')
      expect(rule).not.toBe('')
      expect(rule).toMatch(/padding:/)
    })
  })

  // Regression: the 创建 button was the only way to commit a typed name, so
  // typing a name and pressing 确定 saved nothing — the field was cleared and
  // the tag silently vanished, with no error shown.
  it('saves a name typed in the input even when 创建 was never pressed', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('  Needs   Review  ')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(mockPatch).toHaveBeenCalledTimes(1)
    // Normalized the same way the backend does (collapse + lowercase).
    expect(mockPatch.mock.calls[0][1]).toEqual({
      tags: [{ name: 'needs review', scope: 'project' }],
    })
  })

  it('keeps checked tags AND the pending input name when saving', async () => {
    mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog({ initialTags: ['bug'] })
    await flushPromises()

    await wrapper.find('.st-input').setValue('urgent')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    // The pending name must be additive, not a replacement for the selection.
    expect(mockPatch.mock.calls[0][1]).toEqual({
      tags: [
        { name: 'bug', scope: 'project' },
        { name: 'urgent', scope: 'project' },
      ],
    })
  })

  it('does not duplicate the pending name when it is already selected', async () => {
    mockGet.mockResolvedValue({ tags: [] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog()
    await flushPromises()

    await wrapper.find('.st-input').setValue('bug')
    await wrapper.find('.st-add-btn').trigger('click')  // commit → now selected
    await wrapper.find('.st-input').setValue('bug')     // retype the same name
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    expect(mockPatch.mock.calls[0][1]).toEqual({
      tags: [{ name: 'bug', scope: 'project' }],
    })
  })

  it('sends an empty tag list when nothing is selected and the input is blank', async () => {
    mockGet.mockResolvedValue({ tags: [{ name: 'bug', scope: 'project', count: 1 }] })
    mockPatch.mockResolvedValue({})
    const wrapper = await mountDialog({ initialTags: [] })
    await flushPromises()

    await wrapper.find('.st-input').setValue('   ')
    await wrapper.find('.fbtn-primary').trigger('click')
    await flushPromises()

    // Whitespace-only input is not a tag; clearing the selection still works.
    expect(mockPatch.mock.calls[0][1]).toEqual({ tags: [] })
  })
})
